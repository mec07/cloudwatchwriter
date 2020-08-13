package zerolog2cloudwatch

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/pkg/errors"
	"gopkg.in/oleiade/lane.v1"
)

const (
	// minBatchDuration is 200 ms as the maximum rate of PutLogEvents is 5
	// requests per second.
	minBatchDuration time.Duration = 200000000
	// defaultBatchDuration is 5 seconds.
	defaultBatchDuration time.Duration = 5000000000
	// megabyte is the number of bytes in a megabyte (it's also the batch size limit imposed by AWS CloudWatch Logs).
	megabyte = 1048576
	// maxNumLogsToSend is the maximum number of logs to send to CloudWatch at
	// once (primarily because any more than this will likely cause us to go
	// over the 1MB batch limit.)
	maxNumLogsToSend = 10000
)

// CloudWatchLogsClient represents the AWS cloudwatchlogs client that we need to talk to CloudWatch
type CloudWatchLogsClient interface {
	DescribeLogStreams(*cloudwatchlogs.DescribeLogStreamsInput) (*cloudwatchlogs.DescribeLogStreamsOutput, error)
	CreateLogGroup(*cloudwatchlogs.CreateLogGroupInput) (*cloudwatchlogs.CreateLogGroupOutput, error)
	CreateLogStream(*cloudwatchlogs.CreateLogStreamInput) (*cloudwatchlogs.CreateLogStreamOutput, error)
	PutLogEvents(*cloudwatchlogs.PutLogEventsInput) (*cloudwatchlogs.PutLogEventsOutput, error)
}

// CloudWatchWriter can be inserted into zerolog to send logs to CloudWatch.
type CloudWatchWriter struct {
	client            CloudWatchLogsClient
	batchDuration     time.Duration
	queue             *lane.Queue
	err               error
	logGroupName      *string
	logStreamName     *string
	nextSequenceToken *string
}

// NewWriter returns a pointer to a CloudWatchWriter struct, or an error.
func NewWriter(sess *session.Session, logGroupName, logStreamName string) (*CloudWatchWriter, error) {
	return NewWriterWithClient(cloudwatchlogs.New(sess), defaultBatchDuration, logGroupName, logStreamName)
}

// NewWriterWithClient returns a pointer to a CloudWatchWriter struct, or an
// error.
func NewWriterWithClient(client CloudWatchLogsClient, batchDuration time.Duration, logGroupName, logStreamName string) (*CloudWatchWriter, error) {
	writer := &CloudWatchWriter{
		client:        client,
		queue:         lane.NewQueue(),
		logGroupName:  aws.String(logGroupName),
		logStreamName: aws.String(logStreamName),
	}

	err := writer.SetBatchDuration(batchDuration)
	if err != nil {
		return nil, errors.Wrapf(err, "set batch duration: %v", batchDuration)
	}

	logStream, err := writer.getOrCreateLogStream()
	if err != nil {
		return nil, err
	}
	writer.nextSequenceToken = logStream.UploadSequenceToken

	go writer.batchSender()

	return writer, nil
}

// SetBatchDuration sets the maximum time between batches of logs sent to
// CloudWatch.
func (c *CloudWatchWriter) SetBatchDuration(duration time.Duration) error {
	if duration < minBatchDuration {
		return errors.New("supplied batch duration is less than the minimum")
	}

	c.batchDuration = duration
	return nil
}

// Write implements the io.Writer interface.
func (c *CloudWatchWriter) Write(log []byte) (int, error) {
	event := &cloudwatchlogs.InputLogEvent{
		Message: aws.String(string(log)),
		// Timestamp has to be in milliseconds since the epoch
		Timestamp: aws.Int64(time.Now().UTC().UnixNano() / int64(time.Millisecond)),
	}
	c.queue.Enqueue(event)

	// report last sending error
	if c.err != nil {
		lastErr := c.err
		c.err = nil
		return 0, lastErr
	}
	return len(log), nil
}

func (c *CloudWatchWriter) batchSender() {
	var batch []*cloudwatchlogs.InputLogEvent
	batchSize := 0
	nextSendTime := time.Now().Add(c.batchDuration)

	for {
		if time.Now().After(nextSendTime) {
			c.sendBatch(batch)
			batch = nil
			batchSize = 0
			nextSendTime.Add(c.batchDuration)
		}

		item := c.queue.Dequeue()
		if item == nil {
			// Empty queue, means no logs to process, so just sleep.
			time.Sleep(time.Millisecond)
			continue
		}

		logEvent, ok := item.(*cloudwatchlogs.InputLogEvent)
		if !ok || logEvent.Message == nil {
			// This should not happen!
			continue
		}

		// Send the batch if the next message would push it over the 1MB limit
		// on batch size.
		messageSize := len(*logEvent.Message) + 36
		if batchSize+messageSize > megabyte {
			c.sendBatch(batch)
			batch = nil
			batchSize = 0
		}

		batch = append(batch, logEvent)
		batchSize += messageSize

		if len(batch) >= maxNumLogsToSend {
			c.sendBatch(batch)
			batch = nil
			batchSize = 0
		}
	}
}

func (c *CloudWatchWriter) sendBatch(batch []*cloudwatchlogs.InputLogEvent) {
	if len(batch) == 0 {
		return
	}

	input := &cloudwatchlogs.PutLogEventsInput{
		LogEvents:     batch,
		LogGroupName:  c.logGroupName,
		LogStreamName: c.logStreamName,
		SequenceToken: c.nextSequenceToken,
	}

	output, err := c.client.PutLogEvents(input)
	if err != nil {
		c.err = err
		return
	}
	c.nextSequenceToken = output.NextSequenceToken
}

// Close blocks until the writer has completed writing the logs to CloudWatch.
func (c *CloudWatchWriter) Close() {}

// getOrCreateLogStream gets info on the log stream for the log group and log
// stream we're interested in -- primarily for the purpose of finding the value
// of the next sequence token. If the log group doesn't exist, then we create
// it, if the log stream doesn't exist, then we create it.
func (c *CloudWatchWriter) getOrCreateLogStream() (*cloudwatchlogs.LogStream, error) {
	// Get the log streams that match our log group name and log stream
	output, err := c.client.DescribeLogStreams(&cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName:        c.logGroupName,
		LogStreamNamePrefix: c.logStreamName,
	})
	if err != nil || output == nil {
		awserror, ok := err.(awserr.Error)
		// i.e. the log group does not exist
		if ok && awserror.Code() == cloudwatchlogs.ErrCodeResourceNotFoundException {
			_, err = c.client.CreateLogGroup(&cloudwatchlogs.CreateLogGroupInput{
				LogGroupName: c.logGroupName,
			})
			if err != nil {
				return nil, errors.Wrap(err, "cloudwatchlog.Client.CreateLogGroup")
			}
			return c.getOrCreateLogStream()
		}

		return nil, errors.Wrap(err, "cloudwatchlogs.Client.DescribeLogStreams")
	}

	if len(output.LogStreams) > 0 {
		return output.LogStreams[0], nil
	}

	// No matching log stream, so we need to create it
	_, err = c.client.CreateLogStream(&cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  c.logGroupName,
		LogStreamName: c.logStreamName,
	})
	if err != nil {
		return nil, errors.Wrap(err, "cloudwatchlogs.Client.CreateLogStream")
	}

	// We can just return an empty log stream as the initial sequence token would be nil anyway.
	return &cloudwatchlogs.LogStream{}, nil
}
