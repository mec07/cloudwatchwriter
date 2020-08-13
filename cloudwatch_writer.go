package zerolog2cloudwatch

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/pkg/errors"
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
	client        CloudWatchLogsClient
	logGroupName  *string
	logStreamName *string
	sequenceToken *string
}

// NewWriter returns a pointer to a CloudWatchWriter struct, or an error.
func NewWriter(sess *session.Session, logGroupName, logStreamName string) (*CloudWatchWriter, error) {
	return NewWriterWithClient(cloudwatchlogs.New(sess), logGroupName, logStreamName)
}

// NewWriterWithClient returns a pointer to a CloudWatchWriter struct, or an error.
func NewWriterWithClient(client CloudWatchLogsClient, logGroupName, logStreamName string) (*CloudWatchWriter, error) {
	writer := &CloudWatchWriter{
		client:        client,
		logGroupName:  aws.String(logGroupName),
		logStreamName: aws.String(logStreamName),
	}

	logStream, err := writer.getOrCreateLogStream()
	if err != nil {
		return nil, err
	}

	writer.sequenceToken = logStream.UploadSequenceToken

	return writer, nil
}

// Write implements the io.Writer interface.
func (c *CloudWatchWriter) Write(log []byte) (int, error) {
	var logEvents []*cloudwatchlogs.InputLogEvent
	logEvents = append(logEvents, &cloudwatchlogs.InputLogEvent{
		Message: aws.String(string(log)),
		// Timestamp has to be in milliseconds since the epoch
		Timestamp: aws.Int64(time.Now().UTC().UnixNano() / int64(time.Millisecond)),
	})

	input := &cloudwatchlogs.PutLogEventsInput{
		LogEvents:     logEvents,
		LogGroupName:  c.logGroupName,
		LogStreamName: c.logStreamName,
		SequenceToken: c.sequenceToken,
	}

	resp, err := c.client.PutLogEvents(input)
	if err != nil {
		return 0, errors.Wrap(err, "cloudwatchlogs.Client.PutLogEvents")
	}

	if resp != nil {
		c.sequenceToken = resp.NextSequenceToken
	}

	return len(log), nil
}

// getOrCreateLogStream gets info on the log stream for the log group and log
// stream we're interested in -- primarily for the purpose of finding the value
// of the next sequence token. If the log group doesn't exist, then we create
// it, if the log stream doesn't exist, then we create it.
func (c *CloudWatchWriter) getOrCreateLogStream() (*cloudwatchlogs.LogStream, error) {
	// Get the log streams that match our log group name and log group stream
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
