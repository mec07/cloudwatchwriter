package cloudwatchwriter_test

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/mec07/cloudwatchwriter"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

const sequenceToken = "next-sequence-token"

type mockClient struct {
	sync.RWMutex
	putLogEventsShouldError bool
	logEvents               []*cloudwatchlogs.InputLogEvent
	logGroupName            *string
	logStreamName           *string
	expectedSequenceToken   *string
}

func (c *mockClient) DescribeLogStreams(*cloudwatchlogs.DescribeLogStreamsInput) (*cloudwatchlogs.DescribeLogStreamsOutput, error) {
	c.RLock()
	defer c.RUnlock()

	if c.logGroupName == nil {
		return nil, awserr.New(cloudwatchlogs.ErrCodeResourceNotFoundException, "blah", nil)
	}

	var streams []*cloudwatchlogs.LogStream
	if c.logStreamName != nil {
		streams = append(streams, &cloudwatchlogs.LogStream{
			LogStreamName:       c.logStreamName,
			UploadSequenceToken: c.expectedSequenceToken,
		})
	}

	return &cloudwatchlogs.DescribeLogStreamsOutput{
		LogStreams: streams,
	}, nil
}

func (c *mockClient) CreateLogGroup(input *cloudwatchlogs.CreateLogGroupInput) (*cloudwatchlogs.CreateLogGroupOutput, error) {
	c.Lock()
	defer c.Unlock()

	c.logGroupName = input.LogGroupName
	return nil, nil
}

func (c *mockClient) CreateLogStream(input *cloudwatchlogs.CreateLogStreamInput) (*cloudwatchlogs.CreateLogStreamOutput, error) {
	c.Lock()
	defer c.Unlock()

	c.logStreamName = input.LogStreamName
	return nil, nil
}

func (c *mockClient) PutLogEvents(putLogEvents *cloudwatchlogs.PutLogEventsInput) (*cloudwatchlogs.PutLogEventsOutput, error) {
	c.Lock()
	defer c.Unlock()
	if c.putLogEventsShouldError {
		return nil, errors.New("should error")
	}

	if putLogEvents == nil {
		return nil, errors.New("received nil *cloudwatchlogs.PutLogEventsInput")
	}

	// At the first PutLogEvents c.expectedSequenceToken should be nil, as we
	// set it in this call. If it is not nil then we can compare the received
	// sequence token and the expected one.
	if c.expectedSequenceToken != nil {
		if putLogEvents.SequenceToken == nil || *putLogEvents.SequenceToken != *c.expectedSequenceToken {
			return nil, &cloudwatchlogs.InvalidSequenceTokenException{
				ExpectedSequenceToken: c.expectedSequenceToken,
			}
		}
	} else {
		c.expectedSequenceToken = aws.String(sequenceToken)
	}

	c.logEvents = append(c.logEvents, putLogEvents.LogEvents...)
	output := &cloudwatchlogs.PutLogEventsOutput{
		NextSequenceToken: c.expectedSequenceToken,
	}
	return output, nil
}

func (c *mockClient) getLogEvents() []*cloudwatchlogs.InputLogEvent {
	c.RLock()
	defer c.RUnlock()

	logEvents := make([]*cloudwatchlogs.InputLogEvent, len(c.logEvents))
	copy(logEvents, c.logEvents)

	return logEvents
}

func (c *mockClient) setExpectedSequenceToken(token *string) {
	c.Lock()
	defer c.Unlock()

	c.expectedSequenceToken = token
}

func (c *mockClient) waitForLogs(numberOfLogs int, timeout time.Duration) error {
	endTime := time.Now().Add(timeout)
	for {
		if c.numLogs() >= numberOfLogs {
			return nil
		}

		if time.Now().After(endTime) {
			return errors.New("ran out of time waiting for logs")
		}

		time.Sleep(time.Millisecond)
	}
}

func (c *mockClient) numLogs() int {
	c.RLock()
	defer c.RUnlock()

	return len(c.logEvents)
}

type exampleLog struct {
	Time, Message, Filename string
	Port                    uint16
}

type logsContainer struct {
	sync.Mutex
	logs []exampleLog
}

func (l *logsContainer) addLog(log exampleLog) {
	l.Lock()
	defer l.Unlock()

	l.logs = append(l.logs, log)
}

func (l *logsContainer) getLogEvents() ([]*cloudwatchlogs.InputLogEvent, error) {
	l.Lock()
	defer l.Unlock()

	var logEvents []*cloudwatchlogs.InputLogEvent
	for _, log := range l.logs {
		message, err := json.Marshal(log)
		if err != nil {
			return nil, errors.Wrap(err, "json.Marshal")
		}

		logEvents = append(logEvents, &cloudwatchlogs.InputLogEvent{
			Message: aws.String(string(message)),
			// Timestamps for CloudWatch Logs should be in milliseconds since the epoch.
			Timestamp: aws.Int64(time.Now().UTC().UnixNano() / int64(time.Millisecond)),
		})
	}
	return logEvents, nil
}

func helperWriteLogs(t *testing.T, writer io.Writer, logs ...interface{}) {
	for _, log := range logs {
		message, err := json.Marshal(log)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		_, err = writer.Write(message)
		if err != nil {
			t.Fatalf("writer.Write(%s): %v", string(message), err)
		}
	}
}

// assertEqualLogMessages asserts that the log messages are all the same, ignoring the timestamps.
func assertEqualLogMessages(t *testing.T, expectedLogs []*cloudwatchlogs.InputLogEvent, logs []*cloudwatchlogs.InputLogEvent) {
	if !assert.Equal(t, len(expectedLogs), len(logs), "expected to have the same number of logs") {
		return
	}

	for index, log := range logs {
		if log == nil {
			t.Fatalf("found nil log at index: %d", index)
		}
		if expectedLogs[index] == nil {
			t.Fatalf("found nil log in expectedLogs at index: %d", index)
		}
		assert.Equal(t, *expectedLogs[index].Message, *log.Message)
	}
}

func TestCloudWatchWriter(t *testing.T) {
	client := &mockClient{}

	cloudWatchWriter, err := cloudwatchwriter.NewWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	// give the queueMonitor goroutine time to start up
	time.Sleep(time.Millisecond)

	log1 := exampleLog{
		Time:     "2009-11-10T23:00:02.043123061Z",
		Message:  "Test message 1",
		Filename: "filename",
		Port:     666,
	}
	log2 := exampleLog{
		Time:     "2009-11-10T23:00:02.043123061Z",
		Message:  "Test message 2",
		Filename: "filename",
		Port:     666,
	}
	log3 := exampleLog{
		Time:     "2009-11-10T23:00:02.043123061Z",
		Message:  "Test message 3",
		Filename: "filename",
		Port:     666,
	}
	log4 := exampleLog{
		Time:     "2009-11-10T23:00:02.043123061Z",
		Message:  "Test message 4",
		Filename: "filename",
		Port:     666,
	}

	helperWriteLogs(t, cloudWatchWriter, log1, log2, log3, log4)

	logs := logsContainer{}
	logs.addLog(log1)
	logs.addLog(log2)
	logs.addLog(log3)
	logs.addLog(log4)

	expectedLogs, err := logs.getLogEvents()
	if err != nil {
		t.Fatal(err)
	}

	if err = client.waitForLogs(2, 210*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	assertEqualLogMessages(t, expectedLogs, client.getLogEvents())
}

func TestCloudWatchWriterBatchInterval(t *testing.T) {
	client := &mockClient{}

	cloudWatchWriter, err := cloudwatchwriter.NewWithClient(client, 5*time.Second, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	// can't set anything below 200 milliseconds
	err = cloudWatchWriter.SetBatchInterval(199 * time.Millisecond)
	if err == nil {
		t.Fatal("expected an error")
	}

	// setting it to a value greater than or equal to 200 is OK
	err = cloudWatchWriter.SetBatchInterval(201 * time.Millisecond)
	if err != nil {
		t.Fatalf("CloudWatchWriter.SetBatchInterval: %v", err)
	}

	// give the queueMonitor goroutine time to start up
	time.Sleep(time.Millisecond)

	aLog := exampleLog{
		Time:     "2009-11-10T23:00:02.043123061Z",
		Message:  "Test message",
		Filename: "filename",
		Port:     666,
	}

	helperWriteLogs(t, cloudWatchWriter, aLog)

	startTime := time.Now()
	if err := client.waitForLogs(1, 210*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	timeTaken := time.Since(startTime)
	if timeTaken < 190*time.Millisecond {
		t.Fatalf("expected batch interval time to be approximately 200 milliseconds, found: %dms", timeTaken.Milliseconds())
	}
}

// Hit the 1MB limit on batch size of logs to trigger an earlier batch
func TestCloudWatchWriterHit1MBLimit(t *testing.T) {
	client := &mockClient{}

	cloudWatchWriter, err := cloudwatchwriter.NewWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	// give the queueMonitor goroutine time to start up
	time.Sleep(time.Millisecond)

	logs := logsContainer{}
	numLogs := 9999
	for i := 0; i < numLogs; i++ {
		aLog := exampleLog{
			Time:     "2009-11-10T23:00:02.043123061Z",
			Message:  fmt.Sprintf("longggggggggggggggggggggggggggg test message %d", i),
			Filename: "/home/deadpool/src/github.com/superlongfilenameblahblahblahblah.txt",
			Port:     666,
		}
		helperWriteLogs(t, cloudWatchWriter, aLog)
		logs.addLog(aLog)
	}

	// Main assertion is that we are triggering a batch early as we're sending
	// so much data
	assert.True(t, client.numLogs() > 0)

	if err = client.waitForLogs(numLogs, 210*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	expectedLogs, err := logs.getLogEvents()
	if err != nil {
		t.Fatal(err)
	}

	assertEqualLogMessages(t, expectedLogs, client.getLogEvents())
}

// Hit the 10k limit on number of logs to trigger an earlier batch
func TestCloudWatchWriterHit10kLimit(t *testing.T) {
	client := &mockClient{}

	cloudWatchWriter, err := cloudwatchwriter.NewWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	// give the queueMonitor goroutine time to start up
	time.Sleep(time.Millisecond)

	var expectedLogs []*cloudwatchlogs.InputLogEvent
	numLogs := 10001
	for i := 0; i < numLogs; i++ {
		message := fmt.Sprintf("hello %d", i)
		_, err = cloudWatchWriter.Write([]byte(message))
		if err != nil {
			t.Fatalf("cloudWatchWriter.Write: %v", err)
		}
		expectedLogs = append(expectedLogs, &cloudwatchlogs.InputLogEvent{
			Message:   aws.String(message),
			Timestamp: aws.Int64(time.Now().UTC().UnixNano() / int64(time.Millisecond)),
		})
	}

	// give the queueMonitor goroutine time to catch-up (sleep is far less than
	// the minimum of 200 milliseconds)
	time.Sleep(10 * time.Millisecond)

	// Main assertion is that we are triggering a batch early as we're sending
	// so many logs
	assert.True(t, client.numLogs() > 0)

	if err = client.waitForLogs(numLogs, 210*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	assertEqualLogMessages(t, expectedLogs, client.getLogEvents())
}

func TestCloudWatchWriterParallel(t *testing.T) {
	client := &mockClient{}

	cloudWatchWriter, err := cloudwatchwriter.NewWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	logs := logsContainer{}
	numLogs := 8000
	for i := 0; i < numLogs; i++ {
		go func() {
			aLog := exampleLog{
				Time:     "2009-11-10T23:00:02.043123061Z",
				Message:  "Test message",
				Filename: "filename",
				Port:     666,
			}
			logs.addLog(aLog)
			helperWriteLogs(t, cloudWatchWriter, aLog)
		}()
	}

	// allow more time as there are a lot of goroutines to set off!
	if err = client.waitForLogs(numLogs, 4*time.Second); err != nil {
		t.Fatal(err)
	}

	expectedLogs, err := logs.getLogEvents()
	if err != nil {
		t.Fatal(err)
	}

	assertEqualLogMessages(t, expectedLogs, client.getLogEvents())
}

func TestCloudWatchWriterClose(t *testing.T) {
	client := &mockClient{}

	cloudWatchWriter, err := cloudwatchwriter.NewWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	numLogs := 99
	for i := 0; i < numLogs; i++ {
		aLog := exampleLog{
			Time:     "2009-11-10T23:00:02.043123061Z",
			Message:  fmt.Sprintf("Test message %d", i),
			Filename: "filename",
			Port:     666,
		}
		helperWriteLogs(t, cloudWatchWriter, aLog)
	}

	// The logs shouldn't have come through yet
	assert.Equal(t, 0, client.numLogs())

	// Close should block until the queue is empty
	cloudWatchWriter.Close()

	// The logs should have all come through now
	assert.Equal(t, numLogs, client.numLogs())
}

func TestCloudWatchWriterReportError(t *testing.T) {
	client := &mockClient{
		putLogEventsShouldError: true,
	}

	cloudWatchWriter, err := cloudwatchwriter.NewWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	// give the queueMonitor goroutine time to start up
	time.Sleep(time.Millisecond)

	log1 := exampleLog{
		Time:     "2009-11-10T23:00:02.043123061Z",
		Message:  "Test message 1",
		Filename: "filename",
		Port:     666,
	}

	helperWriteLogs(t, cloudWatchWriter, log1)

	// sleep until the batch should have been sent
	time.Sleep(211 * time.Millisecond)

	_, err = cloudWatchWriter.Write([]byte("hello world"))
	if err == nil {
		t.Fatal("expected the last error from PutLogEvents to appear here")
	}
}

func TestCloudWatchWriterReceiveInvalidSequenceTokenException(t *testing.T) {
	// Setup
	client := &mockClient{}

	cloudWatchWriter, err := cloudwatchwriter.NewWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	// give the queueMonitor goroutine time to start up
	time.Sleep(time.Millisecond)

	// At this point the cloudWatchWriter should have the normal sequence token.
	// So we change the mock cloudwatch client to expect a different sequence
	// token:
	client.setExpectedSequenceToken(aws.String("new sequence token"))

	// Action
	logs := logsContainer{}
	log := exampleLog{
		Time:     "2009-11-10T23:00:02.043123061Z",
		Message:  "Test message 1",
		Filename: "filename",
		Port:     666,
	}

	helperWriteLogs(t, cloudWatchWriter, log)
	logs.addLog(log)

	// Result
	if err = client.waitForLogs(1, 210*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	expectedLogs, err := logs.getLogEvents()
	if err != nil {
		t.Fatal(err)
	}
	assertEqualLogMessages(t, expectedLogs, client.getLogEvents())
}

type protectedObject struct {
	sync.Mutex
	haveBeenCalled bool
}

func (p *protectedObject) setCalled() {
	p.Lock()
	defer p.Unlock()

	p.haveBeenCalled = true
}

func (p *protectedObject) getCalled() bool {
	p.Lock()
	defer p.Unlock()

	return p.haveBeenCalled
}

func TestCloudWatchWriterErrorHandler(t *testing.T) {
	objectUnderObservation := protectedObject{}
	handler := func(error) {
		objectUnderObservation.setCalled()
	}

	client := &mockClient{
		putLogEventsShouldError: true,
	}

	cloudWatchWriter, err := cloudwatchwriter.NewWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	cloudWatchWriter.SetErrorHandler(handler)

	// give the queueMonitor goroutine time to start up
	time.Sleep(time.Millisecond)

	aLog := exampleLog{
		Time:     "2009-11-10T23:00:02.043123061Z",
		Message:  "Test message",
		Filename: "filename",
		Port:     666,
	}

	helperWriteLogs(t, cloudWatchWriter, aLog)

	// give the cloudWatchWriter time to call PutLogEvents
	time.Sleep(211 * time.Millisecond)

	assert.True(t, objectUnderObservation.getCalled())
}

// TestCloudWatchWriterCloseBug there seems to be a bug on closing the logger
// and it getting locked up. I'm trying to reproduce it.
func TestCloudWatchWriterCloseBug(t *testing.T) {
	client := &mockClient{}

	for i := 0; i < 100; i++ {
		cloudWatchWriter, err := cloudwatchwriter.NewWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
		if err != nil {
			t.Fatalf("NewWithClient: %v", err)
		}

		numLogs := 100
		expectedLogs := make([]*cloudwatchlogs.InputLogEvent, numLogs)
		for i := 0; i < numLogs; i++ {
			message := fmt.Sprintf("hello %d", i)
			_, err = cloudWatchWriter.Write([]byte(message))
			if err != nil {
				t.Fatalf("cloudWatchWriter.Write: %v", err)
			}
			expectedLogs = append(expectedLogs, &cloudwatchlogs.InputLogEvent{
				Message:   aws.String(message),
				Timestamp: aws.Int64(time.Now().UTC().UnixNano() / int64(time.Millisecond)),
			})
		}

		cloudWatchWriter.Close()
		assertEqualLogMessages(t, expectedLogs, client.getLogEvents())
	}
}
