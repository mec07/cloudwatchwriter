package main

import (
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/mec07/cloudwatchwriter"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	region        = "eu-west-2"
	logGroupName  = "cloudwatchwriter"
	logStreamName = "this-computer"
)

func main() {
	accessKeyID := os.Getenv("ACCESS_KEY_ID")
	secretKey := os.Getenv("SECRET_ACCESS_KEY")

	logger, close, err := newCloudWatchLogger(accessKeyID, secretKey)
	if err != nil {
		log.Error().Err(err).Msg("newCloudWatchLogger")
		return
	}
	defer close()

	for i := 0; i < 10000; i++ {
		logger.Info().Str("package", "cloudwatchwriter").Msg(fmt.Sprintf("Log %d", i))
	}
}

func newCloudWatchLogger(accessKeyID, secretKey string) (zerolog.Logger, func(), error) {
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKeyID, secretKey, ""),
	})
	if err != nil {
		return log.Logger, nil, fmt.Errorf("session.NewSession: %w", err)
	}

	cloudWatchWriter, err := cloudwatchwriter.New(sess, logGroupName, logStreamName)
	if err != nil {
		return log.Logger, nil, fmt.Errorf("cloudwatchwriter.New: %w", err)
	}

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
	logger := zerolog.New(zerolog.MultiLevelWriter(consoleWriter, cloudWatchWriter)).With().Timestamp().Logger()

	return logger, cloudWatchWriter.Close, nil
}
