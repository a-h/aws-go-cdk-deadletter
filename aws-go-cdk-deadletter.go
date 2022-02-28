package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/constructs-go/constructs/v10"
)

type AWSGoCDKDeadletterStackProps struct {
	awscdk.StackProps
}

func NewAWSGoCDKDeadletterStack(scope constructs.Construct, id string, props *AWSGoCDKDeadletterStackProps) awscdk.Stack {
	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}
	stack := awscdk.NewStack(scope, &id, &sprops)

	// Create EventBridge Bus.
	// Create Lambda function to process messages on the bus.
	//   It will crash when receiving a message with `{ "behaviour": "fail" }` in it and succeed with any other message.
	//   The message will log its execution.
	// Create EventBridge Subscription.
	// Test what happens when a message fails multiple times.
	// Implement a dead letter queue.
	//   Ensure that the dead letter queue is encrypted at rest.

	return stack
}

func main() {
	app := awscdk.NewApp(nil)
	NewAWSGoCDKDeadletterStack(app, "AWSGoCDKDeadletterStack", &AWSGoCDKDeadletterStackProps{
		awscdk.StackProps{
			Env: nil,
		},
	})
	app.Synth(nil)
}
