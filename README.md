# cloudwatchwriter
Package to enable sending logs from zerolog to AWS CloudWatch.

## Usage

This library assumes that you have IAM credentials to allow you to talk to AWS CloudWatch Logs.
The specific permissions that are required are:
- CreateLogGroup,
- CreateLogStream,
- DescribeLogStreams,
- PutLogEvents.

If these permissions aren't assigned to the user who's IAM credentials you're using then this package will not work.
There are two exceptions to that:
- if the log group already exists, then you don't need permission to CreateLogGroup;
- if the log stream already exists, then you don't need permission to CreateLogStream.

### Standard use case
If you want zerolog to send all logs to CloudWatch then do the following:
```
import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/mec07/cloudwatchwriter"
	"github.com/rs/zerolog/log"
)

const (
    region = "eu-west-2"
    logGroupName = "log-group-name"
    logStreamName = "log-stream-name"
)

// setupZerolog sets up the main zerolog logger to write to CloudWatch instead
// of stdout. It returns an error or the CloudWatchWriter.Close method which
// blocks until all the logs have been processed.
func setupZerolog(accessKeyID, secretKey string) (func(), error) {
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKeyID, secretKey, ""),
	})
	if err != nil {
		return nil, fmt.Errorf("session.NewSession: %w", err)
	}

	cloudWatchWriter, err := cloudwatchwriter.New(sess, logGroupName, logStreamName)
	if err != nil {
		return nil, fmt.Errorf("cloudwatchwriter.New: %w", err)
	}

	log.Logger = log.Output(cloudWatchWriter)
	return cloudWatchWriter.Close, nil
}
```
If you want to ensure that all your logs are sent to CloudWatch during the shut down sequence of your program then you can `defer` the `cloudWatchWriter.Close()` function in main.
The `Close()` function blocks until all the logs have been processed.
If you prefer to use AWS IAM credentials that are saved in the usual location on your computer then you don't have to specify the credentials, e.g.:
```
sess, err := session.NewSession(&aws.Config{
    Region:      aws.String(region),
})
```
For more details, see: https://docs.aws.amazon.com/sdk-for-go/api/aws/session/.
See the example directory for a working example.

### Write to CloudWatch and the console
What I personally prefer is to write to both CloudWatch and the console, e.g.
```
cloudWatchWriter, err := cloudwatchwriter.New(sess, logGroupName, logStreamName)
if err != nil {
    return fmt.Errorf("cloudwatchwriter.New: %w", err)
}
consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
log.Logger = log.Output(zerolog.MultiLevelWriter(consoleWriter, cloudWatchWriter))
```

### Create a new zerolog Logger
Of course, you can create a new `zerolog.Logger` using this too:
```
cloudWatchWriter, err := cloudwatchwriter.New(sess, logGroupName, logStreamName)
if err != nil {
    return fmt.Errorf("cloudwatchwriter.New: %w", err)
}
logger := zerolog.New(cloudWatchWriter).With().Timestamp().Logger()
```
and of course you can create a new `zerolog.Logger` which can write to both CloudWatch and the console:
```
cloudWatchWriter, err := cloudwatchwriter.New(sess, logGroupName, logStreamName)
if err != nil {
    return fmt.Errorf("cloudwatchwriter.New: %w", err)
}
consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
logger := zerolog.New(zerolog.MultiLevelWriter(consoleWriter, cloudWatchWriter)).With().Timestamp().Logger()
```

### Changing the default settings

#### Batch interval
The logs are sent in batches because AWS has a maximum of 5 PutLogEvents requests per second per log stream (https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_PutLogEvents.html).
The default value of the batch period is 5 seconds, which means it will send the a batch of logs at least once every 5 seconds.
Batches of logs will be sent earlier if the size of the collected logs exceeds 1MB (another AWS restriction).
To change the batch frequency, you can set the time interval between batches to a smaller or larger value, e.g. 1 second:
```
err := cloudWatchWriter.SetBatchInterval(time.Second)
```
If you set it below 200 milliseconds it will return an error.
The batch interval is not guaranteed as two things can alter how often the batches get delivered:
- as soon as 1MB of logs or 10k logs have accumulated, they are sent (due to AWS restrictions on batch size);
- we have to send the batches in sequence (an AWS restriction) so a long running request to CloudWatch can delay the next batch.

## Acknowledgements
Much thanks has to go to the creator of `zerolog` (https://github.com/rs/zerolog), for creating such a good logger.
Thanks must go to the writer of `logrus-cloudwatchlogs` (https://github.com/kdar/logrus-cloudwatchlogs) as I found it a helpful resource for interfacing with `cloudwatchlogs`.
Thanks also goes to the writer of this: https://gist.github.com/asdine/f821abe6189a04250ae61b77a3048bd9, which I also found helpful for extracting logs from `zerolog`.