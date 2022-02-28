package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	// "github.com/aws/aws-cdk-go/awscdk/v2/awssqs"
	"github.com/aws/constructs-go/constructs/v10"
	// "github.com/aws/jsii-runtime-go"
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

	// The code that defines your stack goes here

	// example resource
	// queue := awssqs.NewQueue(stack, jsii.String("AwsGoCdkDeadletterQueue"), &awssqs.QueueProps{
	// 	VisibilityTimeout: awscdk.Duration_Seconds(jsii.Number(300)),
	// })

	return stack
}

func main() {
	app := awscdk.NewApp(nil)
	NewAWSGoCDKDeadletterStack(app, "AwsGoCdkDeadletterStack", &AWSGoCDKDeadletterStackProps{
		awscdk.StackProps{
			Env: nil,
		},
	})
	app.Synth(nil)
}
