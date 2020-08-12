# zerolog2cloudwatch
Package to enable sending logs from zerolog to AWS CloudWatch.

## Usage

### Standard use case
If you want zerolog to send all logs to CloudWatch then do the following:
```
import (

)

func setupLogs() error {
    sess, err := session.NewSession(session.Options{
        Region:      aws.String(region),
        Credentials: credentials.NewStaticCredentials(accessKeyID, secretKey, sessionToken),
    })
    if err != nil {
        return fmt.Errorf("session.NewSession: %w", err)
    }

    cloudwatchWriter, err := zerolog2cloudwatch.NewWriter(sess, logGroupName, logStreamName)
    if err != nil {
        return fmt.Errorf("zerolog2cloudwatch.NewWriter: %w", err)
    }

    log.Logger = log.Output(cloudwatchWriter)
}
```
If you prefer to use AWS IAM credentials that are saved in the usual location on your computer then you don't have to specify the credentials, e.g.:
```
sess, err := session.NewSession(session.Options{
    Region:      aws.String(region),
})
```
For more details, see: https://docs.aws.amazon.com/sdk-for-go/api/aws/session/.

### Write to CloudWatch and the console
What I personally prefer is to write to both CloudWatch and the console, e.g.
```
cloudwatchWriter, err := zerolog2cloudwatch.NewWriter(sess, logGroupName, logStreamName)
if err != nil {
    return fmt.Errorf("zerolog2cloudwatch.NewWriter: %w", err)
}
consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
log.Logger = log.Output(zerolog.MultiLevelWriter(consoleWriter, cloudwatchWriter))
```

### Create a new zerolog Logger
Of course, you can create a new `zerolog.Logger` using this too:
```
cloudwatchWriter, err := zerolog2cloudwatch.NewWriter(sess, logGroupName, logStreamName)
if err != nil {
    return fmt.Errorf("zerolog2cloudwatch.NewWriter: %w", err)
}
logger := zerolog.New(cloudwatchWriter)
```
and of course you can create a new `zerolog.Logger` which can write to both CloudWatch and the console:
```
cloudwatchWriter, err := zerolog2cloudwatch.NewWriter(sess, logGroupName, logStreamName)
if err != nil {
    return fmt.Errorf("zerolog2cloudwatch.NewWriter: %w", err)
}
consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
logger := zerolog.New(zerolog.MultiLevelWriter(consoleWriter, cloudwatchWriter))
```