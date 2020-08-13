package main

import (
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/mec07/zerolog2cloudwatch"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	region        = "eu-west-2"
	logGroupName  = "zerolog2cloudwatch"
	logStreamName = "this-computer"
)

func main() {
	accessKeyID := os.Getenv("ACCESS_KEY_ID")
	secretKey := os.Getenv("SECRET_ACCESS_KEY")

	logger, err := newLogger(accessKeyID, secretKey)
	if err != nil {
		log.Error().Err(err).Msg("newLogger")
		return
	}

	logger.Info().Str("name", "zerolog2cloudwatch").Msg("Log to test out this package")
}

func newLogger(accessKeyID, secretKey string) (zerolog.Logger, error) {
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKeyID, secretKey, ""),
	})
	if err != nil {
		return log.Logger, fmt.Errorf("session.NewSession: %w", err)
	}

	cloudwatchWriter, err := zerolog2cloudwatch.NewWriter(sess, logGroupName, logStreamName)
	if err != nil {
		return log.Logger, fmt.Errorf("zerolog2cloudwatch.NewWriter: %w", err)
	}

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
	logger := zerolog.New(zerolog.MultiLevelWriter(consoleWriter, cloudwatchWriter)).With().Timestamp().Logger()

	return logger, nil
}
