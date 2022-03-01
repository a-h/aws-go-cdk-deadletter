package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatch"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscloudwatchactions"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsevents"
	"github.com/aws/aws-cdk-go/awscdk/v2/awseventstargets"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsiam"
	"github.com/aws/aws-cdk-go/awscdk/v2/awskms"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssns"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssqs"
	awslambdago "github.com/aws/aws-cdk-go/awscdklambdagoalpha/v2"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
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

	bundlingOptions := &awslambdago.BundlingOptions{
		GoBuildFlags: &[]*string{jsii.String(`-ldflags "-s -w"`)},
	}

	// Create a shared SNS topic to send alerts to.
	alarmTopic := addAlarmSNSTopic(stack)

	// Create a shared dead letter queue with appropriate encryption settings.
	dlq := awssqs.NewQueue(stack, jsii.String("EventHandlerDLQ"), &awssqs.QueueProps{
		Encryption:      awssqs.QueueEncryption_KMS_MANAGED,
		RetentionPeriod: awscdk.Duration_Days(jsii.Number(14)),
	})
	// Add an alarm to the queue.
	addDLQAlarm(stack, jsii.String("EventHandlerDLQAlarm"), dlq, alarmTopic)

	// Create a Lambda function to process messages on the bus.
	// The Lambda function will return an error if a message with `{ "shoudlFail": true }` is sent.
	//
	// Any other message will succeed.
	//
	// The function simply logs execution.
	onEventHandler := awslambdago.NewGoFunction(stack, jsii.String("OnEventHandler"), &awslambdago.GoFunctionProps{
		MemorySize: jsii.Number(1024),
		Timeout:    awscdk.Duration_Seconds(jsii.Number(60)),
		Entry:      jsii.String("./onevent"),
		Bundling:   bundlingOptions,
		Runtime:    awslambda.Runtime_GO_1_X(),
		// Dead letter handling configuration.
		RetryAttempts:          jsii.Number(2),
		DeadLetterQueue:        dlq,
		DeadLetterQueueEnabled: jsii.Bool(true),
	})

	// Create an EventBridge Bus to send input messages to.
	eventBus := awsevents.NewEventBus(stack, jsii.String("EventBus"), &awsevents.EventBusProps{})

	// Subscribe the Lambda function to EventBridge.
	awsevents.NewRule(stack, jsii.String("OnEventRule"), &awsevents.RuleProps{
		EventBus: eventBus,
		// Listen for all the events.
		EventPattern: &awsevents.EventPattern{
			Source:     nil,
			DetailType: nil,
			Region:     jsii.Strings(*eventBus.Env().Region),
		},
		Targets: &[]awsevents.IRuleTarget{
			// Configure the EventBridge target dead letter queue.
			awseventstargets.NewLambdaFunction(onEventHandler, &awseventstargets.LambdaFunctionProps{
				DeadLetterQueue: dlq,
			}),
		},
	})

	return stack
}

func addAlarmSNSTopic(stack awscdk.Stack) awssns.Topic {
	alarmEncryptionKey := awskms.NewKey(stack, jsii.String("AlarmTopicKey"), &awskms.KeyProps{})
	alarmEncryptionKey.AddToResourcePolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Actions: &[]*string{
			jsii.String("kms:Decrypt"),
			jsii.String("kms:GenerateDataKey"),
		},
		Effect: awsiam.Effect_ALLOW,
		Principals: &[]awsiam.IPrincipal{
			awsiam.NewServicePrincipal(jsii.String("cloudwatch.amazonaws.com"), &awsiam.ServicePrincipalOpts{}),
		},
		Resources: &[]*string{jsii.String("*")},
	}), jsii.Bool(true))
	topic := awssns.NewTopic(stack, jsii.String("AlarmTopic"), &awssns.TopicProps{
		DisplayName: jsii.String("alarmTopic"),
		MasterKey:   alarmEncryptionKey,
	})
	topic.AddToResourcePolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Actions: &[]*string{jsii.String("sns:Publish")},
		Effect:  awsiam.Effect_ALLOW,
		Principals: &[]awsiam.IPrincipal{
			awsiam.NewServicePrincipal(jsii.String("cloudwatch.amazonaws.com"), &awsiam.ServicePrincipalOpts{}),
		},
		Resources: &[]*string{topic.TopicArn()},
	}))
	awscdk.NewCfnOutput(stack, jsii.String("AlarmTopicArn"), &awscdk.CfnOutputProps{
		ExportName: jsii.String("alarm-topic-arn"),
		Value:      jsii.String(*topic.TopicArn()),
	})
	awscdk.NewCfnOutput(stack, jsii.String("AlarmTopicName"), &awscdk.CfnOutputProps{
		ExportName: jsii.String("alarm-topic-name"),
		Value:      jsii.String(*topic.TopicName()),
	})
	return topic
}

func addDLQAlarm(stack awscdk.Stack, id *string, dlq awssqs.IQueue, alarmTopic awssns.ITopic) {
	m := dlq.Metric(jsii.String("ApproximateNumberOfMessagesVisible"), &awscloudwatch.MetricOptions{
		Statistic: jsii.String("Maximum"),                  // The Max ApproximateNumberOfMessagesVisible within a
		Period:    awscdk.Duration_Minutes(jsii.Number(5)), // 5 minute period.
	})
	alarm := awscloudwatch.NewAlarm(stack, id, &awscloudwatch.AlarmProps{
		AlarmDescription:   jsii.String("Queue depth alarm for DLQ."),
		AlarmName:          jsii.String("QueueDepthAlarm-" + *dlq.QueueName()),
		Metric:             m,                                                                   // The metric is...
		EvaluationPeriods:  jsii.Number(1),                                                      // If, in the last "1" of those periods
		DatapointsToAlarm:  jsii.Number(1),                                                      // There's more than one datapoint
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_OR_EQUAL_TO_THRESHOLD, // Where the metric >= to
		Threshold:          jsii.Number(1),                                                      // The value of 1... then
		ActionsEnabled:     jsii.Bool(true),                                                     // Do the actions.
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,                        // And ignore any missing data.
	})
	alarm.AddAlarmAction(awscloudwatchactions.NewSnsAction(alarmTopic))
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
