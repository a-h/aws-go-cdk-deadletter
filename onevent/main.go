package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"go.uber.org/zap"
)

func main() {
	var err error
	log, err = zap.NewProduction()
	if err != nil {
		panic(err)
	}
	lambda.Start(Handler)
}

var log *zap.Logger

type Fields struct {
	ShouldFail bool `json:"shouldFail"`
}

func Handler(ctx context.Context, event events.CloudWatchEvent) (err error) {
	log := log.With(zap.String("source", event.Source), zap.String("type", event.DetailType))
	var fields Fields
	err = json.Unmarshal(event.Detail, &fields)
	if err != nil {
		log.Error("failed to unmarshal message", zap.Error(err))
		return
	}
	log.Info("message received", zap.Bool("shouldFail", fields.ShouldFail))
	if fields.ShouldFail {
		return errors.New("failed to process message")
	}
	return nil
}
