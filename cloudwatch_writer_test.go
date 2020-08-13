package zerolog2cloudwatch_test

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
	"github.com/mec07/zerolog2cloudwatch"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

type mockClient struct {
	sync.RWMutex
	logEvents     []*cloudwatchlogs.InputLogEvent
	logGroupName  *string
	logStreamName *string
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
			LogStreamName: c.logStreamName,
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

	if putLogEvents == nil {
		return nil, errors.New("received nil *cloudwatchlogs.PutLogEventsInput")
	}

	c.logEvents = append(c.logEvents, putLogEvents.LogEvents...)
	output := &cloudwatchlogs.PutLogEventsOutput{
		NextSequenceToken: aws.String("next sequence token"),
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

func (c *mockClient) waitForLogs(numberOfLogs int, timeout time.Duration) {
	endTime := time.Now().Add(timeout)
	for {
		if c.numLogs() >= numberOfLogs {
			return
		}

		if time.Now().After(endTime) {
			return
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

	cloudWatchWriter, err := zerolog2cloudwatch.NewWriterWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWriterWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

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

	client.waitForLogs(2, 201*time.Millisecond)

	assertEqualLogMessages(t, expectedLogs, client.getLogEvents())
}

func TestCloudWatchWriterBatchDuration(t *testing.T) {
	client := &mockClient{}

	cloudWatchWriter, err := zerolog2cloudwatch.NewWriterWithClient(client, 5*time.Second, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWriterWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	// can't set anything below 200 milliseconds
	err = cloudWatchWriter.SetBatchDuration(199 * time.Millisecond)
	if err == nil {
		t.Fatal("expected an error")
	}

	// setting it to a value greater than or equal to 200 is OK
	err = cloudWatchWriter.SetBatchDuration(200 * time.Millisecond)
	if err != nil {
		t.Fatalf("CloudWatchWriter.SetBatchDuration: %v", err)
	}

	aLog := exampleLog{
		Time:     "2009-11-10T23:00:02.043123061Z",
		Message:  "Test message",
		Filename: "filename",
		Port:     666,
	}

	helperWriteLogs(t, cloudWatchWriter, aLog)

	// The client shouldn't have received any logs at this time
	assert.Equal(t, 0, client.numLogs())

	// Still no logs after 100 milliseconds
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 0, client.numLogs())

	// The client should have received the log after another 101 milliseconds
	// (as that is a total sleep time of 201 milliseconds)
	time.Sleep(101 * time.Millisecond)
	assert.Equal(t, 1, client.numLogs())
}

// Hit the 10,000 log limit to trigger an earlier batch
func TestCloudWatchWriterBulk(t *testing.T) {
	client := &mockClient{}

	cloudWatchWriter, err := zerolog2cloudwatch.NewWriterWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWriterWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	logs := logsContainer{}
	numLogs := 10000
	for i := 0; i < numLogs; i++ {
		aLog := exampleLog{
			Time:     "2009-11-10T23:00:02.043123061Z",
			Message:  fmt.Sprintf("Test message %d", i),
			Filename: "filename",
			Port:     666,
		}
		helperWriteLogs(t, cloudWatchWriter, aLog)
		logs.addLog(aLog)
	}

	client.waitForLogs(numLogs, 200*time.Millisecond)

	expectedLogs, err := logs.getLogEvents()
	if err != nil {
		t.Fatal(err)
	}

	assertEqualLogMessages(t, expectedLogs, client.getLogEvents())
}

func TestCloudWatchWriterParallel(t *testing.T) {
	client := &mockClient{}

	cloudWatchWriter, err := zerolog2cloudwatch.NewWriterWithClient(client, 200*time.Millisecond, "logGroup", "logStream")
	if err != nil {
		t.Fatalf("NewWriterWithClient: %v", err)
	}
	defer cloudWatchWriter.Close()

	logs := logsContainer{}
	numLogs := 10000
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

	// allow for more time as there are a lot of goroutines to set off!
	client.waitForLogs(numLogs, time.Second)

	expectedLogs, err := logs.getLogEvents()
	if err != nil {
		t.Fatal(err)
	}

	assertEqualLogMessages(t, expectedLogs, client.getLogEvents())
}

// ADD TEST FOR CLOSE BLOCKING UNTIL THE QUEUE IS EMPTY
