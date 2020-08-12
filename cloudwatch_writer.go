package zerolog2cloudwatch

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
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
	client                                     CloudWatchLogsClient
	logGroupName, logStreamName, sequenceToken string
}

// Write implements the io.Writer interface.
func (c *CloudWatchWriter) Write(log []byte) (int, error) {
	var logEvents []*cloudwatchlogs.InputLogEvent
	logEvents = append(logEvents, &cloudwatchlogs.InputLogEvent{
		Message:   aws.String(string(log)),
		Timestamp: aws.Int64(time.Now().UTC().Unix()),
	})

	input := &cloudwatchlogs.PutLogEventsInput{
		LogEvents:     logEvents,
		LogGroupName:  aws.String(c.logGroupName),
		LogStreamName: aws.String(c.logStreamName),
	}
	if c.sequenceToken != "" {
		input.SequenceToken = aws.String(c.sequenceToken)
	}

	resp, err := c.client.PutLogEvents(input)
	if err != nil {
		return 0, errors.Wrap(err, "cloudwatchlogs.Client.PutLogEvents")
	}

	if resp != nil && resp.NextSequenceToken != nil {
		c.sequenceToken = *resp.NextSequenceToken
	}

	return len(log), nil
}

// NewWriter returns a pointer to a CloudWatchWriter struct, or an error.
func NewWriter(sess *session.Session, logGroupName, logStreamName string) (*CloudWatchWriter, error) {
	return NewWriterWithClient(cloudwatchlogs.New(sess), logGroupName, logStreamName)
}

// NewWriterWithClient returns a pointer to a CloudWatchWriter struct, or an error.
func NewWriterWithClient(client CloudWatchLogsClient, logGroupName, logStreamName string) (*CloudWatchWriter, error) {
	return &CloudWatchWriter{
		client: client,
	}, nil
}
