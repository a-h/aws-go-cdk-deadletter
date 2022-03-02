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
	"github.com/aws/aws-cdk-go/awscdk/v2/awslogs"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssns"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssqs"
	awsapigatewayv2 "github.com/aws/aws-cdk-go/awscdkapigatewayv2alpha/v2"
	awsapigatewayv2integrations "github.com/aws/aws-cdk-go/awscdkapigatewayv2integrationsalpha/v2"
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

	// Create a dead letter queue for the onEventHandler function.
	onEventHandlerDLQ := awssqs.NewQueue(stack, jsii.String("EventHandlerDLQ"), &awssqs.QueueProps{
		Encryption:      awssqs.QueueEncryption_KMS_MANAGED,
		RetentionPeriod: awscdk.Duration_Days(jsii.Number(14)),
	})
	// Add an alarm to the queue.
	addDLQAlarm(stack, jsii.String("EventHandlerDLQAlarm"), onEventHandlerDLQ, alarmTopic)

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
		DeadLetterQueue:        onEventHandlerDLQ,
		DeadLetterQueueEnabled: jsii.Bool(true),
	})
	// Scrape JSON logs for errors and alert if any are found.
	addErrorsLoggedAlarm(stack, jsii.String("OnEventHandlerErrorsLogged"), onEventHandler, 1, alarmTopic)
	// Alert if over 40% of requests within a 5 minute window throw errors.
	addLambdaErrorsAlarm(stack, jsii.String("OnEventHandlerLambdaErrors"), onEventHandler, 0.4, alarmTopic)

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
				DeadLetterQueue: onEventHandlerDLQ,
			}),
		},
	})

	// Create API Gateway.
	f := awslambdago.NewGoFunction(stack, jsii.String("Handler"), &awslambdago.GoFunctionProps{
		Runtime:    awslambda.Runtime_GO_1_X(),
		Entry:      jsii.String("./http"),
		Bundling:   bundlingOptions,
		MemorySize: jsii.Number(1024),
		Timeout:    awscdk.Duration_Millis(jsii.Number(15000)),
		Tracing:    awslambda.Tracing_ACTIVE,
		Environment: &map[string]*string{
			"AWS_XRAY_CONTEXT_MISSING": jsii.String("IGNORE_ERROR"),
		},
	})
	// Scrape JSON logs for errors and alert if any are found.
	addErrorsLoggedAlarm(stack, jsii.String("HandlerErrorsLogged"), f, 1, alarmTopic)
	// Alert if over 40% of requests within a 5 minute window throw errors.
	addLambdaErrorsAlarm(stack, jsii.String("HandlerLambdaErrors"), f, 0.4, alarmTopic)

	// Send all paths to the same handler.
	fi := awsapigatewayv2integrations.NewHttpLambdaIntegration(jsii.String("DefaultHandlerIntegration"), f, &awsapigatewayv2integrations.HttpLambdaIntegrationProps{})
	endpoint := awsapigatewayv2.NewHttpApi(stack, jsii.String("ApiGatewayV2API"), &awsapigatewayv2.HttpApiProps{
		DefaultIntegration: fi,
	})
	awscdk.NewCfnOutput(stack, jsii.String("Url"), &awscdk.CfnOutputProps{
		ExportName: jsii.String("Url"),
		Value:      endpoint.Url(),
	})
	// Alert on > 40% server errors within a 5 minute window.
	addAPIGatewayErrorsAlarm(stack, jsii.String("HTTPApi500Errors"), endpoint, 0.4, alarmTopic)

	return stack
}

// addAlarmSNSTopic creates a shared SNS topic to attach all alarm notifications to.
// In a production stack, you might expect to import this from a stack that contains
// infrastructure used by multiple projects.
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

// addDLQAlarm creates an alarm against the dead letter queue when it contains any messages.
func addDLQAlarm(stack awscdk.Stack, id *string, dlq awssqs.IQueue, alarmTopic awssns.ITopic) {
	alarm := awscloudwatch.NewAlarm(stack, id, &awscloudwatch.AlarmProps{
		AlarmDescription: jsii.String("Queue depth alarm for DLQ."),
		AlarmName:        id,
		Metric: dlq.Metric(jsii.String("ApproximateNumberOfMessagesVisible"), &awscloudwatch.MetricOptions{
			Statistic: jsii.String("Maximum"),                  // The Max ApproximateNumberOfMessagesVisible within a
			Period:    awscdk.Duration_Minutes(jsii.Number(5)), // 5 minute period.
		}),
		EvaluationPeriods:  jsii.Number(1),                                                      // If, in the last "1" of those periods
		DatapointsToAlarm:  jsii.Number(1),                                                      // There's more than one datapoint
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_OR_EQUAL_TO_THRESHOLD, // Where the metric >= to
		Threshold:          jsii.Number(1),                                                      // The value of 1... then
		ActionsEnabled:     jsii.Bool(true),                                                     // Do the actions.
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,                        // And ignore any missing data.
	})
	alarm.AddAlarmAction(awscloudwatchactions.NewSnsAction(alarmTopic))
}

// addAPIGatewayErrorsAlarm creates an alarm that triggers when the HTTP API receives more than the rate percentage (expressed as a value between 0.0 and 1.0) of 500 errors.
func addAPIGatewayErrorsAlarm(stack awscdk.Stack, id *string, endpoint awsapigatewayv2.HttpApi, ratePercentage float64, alarmTopic awssns.ITopic) {
	errorsAlarm := awscloudwatch.NewAlarm(stack, id, &awscloudwatch.AlarmProps{
		AlarmDescription: jsii.String("API Gateway 500 errors."),
		AlarmName:        id,
		Metric: endpoint.MetricServerError(&awscloudwatch.MetricOptions{
			Statistic: jsii.String("Average"),                  // The Average of 500 errors (vs standard requests) within a
			Period:    awscdk.Duration_Minutes(jsii.Number(5)), // 5 minute period.
		}),
		EvaluationPeriods:  jsii.Number(1),                                          // If, in the last "1" of those periods
		DatapointsToAlarm:  jsii.Number(1),                                          // There's more than one datapoint
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_THRESHOLD, // Where the metric >= to
		Threshold:          &ratePercentage,                                         // The value of 0.4 would be 40% then
		ActionsEnabled:     jsii.Bool(true),                                         // Do the actions.
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,            // And ignore any missing data.
	})
	errorsAlarm.AddAlarmAction(awscloudwatchactions.NewSnsAction(alarmTopic))
}

// addLambdaErrorsAlarm creates an alarm that triggers when the given Lambda function is erroring at a rate above the expected percentage. The rate is useful since
// some integration endpoints may have a < 0.01 (1%) error rate, and have a dead letter queue attached.
func addLambdaErrorsAlarm(stack awscdk.Stack, id *string, f awslambdago.GoFunction, ratePercentage float64, alarmTopic awssns.ITopic) {
	errorsAlarm := awscloudwatch.NewAlarm(stack, id, &awscloudwatch.AlarmProps{
		AlarmDescription: jsii.String("Error logged by service."),
		AlarmName:        id,
		Metric: f.MetricErrors(&awscloudwatch.MetricOptions{
			Statistic: jsii.String("Average"),                  // The Average of Lambda errors (vs successful requests) within a
			Period:    awscdk.Duration_Minutes(jsii.Number(5)), // 5 minute period.
		}),
		EvaluationPeriods:  jsii.Number(1),                                          // If, in the last "1" of those periods
		DatapointsToAlarm:  jsii.Number(1),                                          // There's more than one datapoint
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_THRESHOLD, // Where the metric >= to
		Threshold:          &ratePercentage,                                         // The value of 40% then
		ActionsEnabled:     jsii.Bool(true),                                         // Do the actions.
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,            // And ignore any missing data.
	})
	errorsAlarm.AddAlarmAction(awscloudwatchactions.NewSnsAction(alarmTopic))
}

// addErrorsLoggedAlarm creates an alarm that triggers when the number of allowed error logs within a 5 minute is exceeded. It's expected that the
// usual value will be 0, i.e. alert on any error.
func addErrorsLoggedAlarm(stack awscdk.Stack, id *string, f awslambdago.GoFunction, errorsIn5MinuteWindow int, alarmTopic awssns.ITopic) {
	metricNamespace := f.Stack().StackName()
	awslogs.NewMetricFilter(stack, jsii.String(*id+"_MF"), &awslogs.MetricFilterProps{
		LogGroup:        f.LogGroup(),
		MetricNamespace: metricNamespace,
		MetricName:      jsii.String("errorsLogged"),
		FilterPattern: awslogs.FilterPattern_Any(
			awslogs.FilterPattern_StringValue(jsii.String("$.level"), jsii.String("="), jsii.String("error")),
			awslogs.FilterPattern_StringValue(jsii.String("$.level"), jsii.String("="), jsii.String("ERROR")),
		),
		MetricValue: jsii.String("1"),
	})
	errorsAlarm := awscloudwatch.NewAlarm(stack, jsii.String(*id+"_Alarm"), &awscloudwatch.AlarmProps{
		AlarmDescription: jsii.String("Error logged by service."),
		AlarmName:        jsii.String(*id + "_ErrorsLoggedAlarm"),
		Metric: awscloudwatch.NewMetric(&awscloudwatch.MetricProps{
			MetricName: jsii.String("errorsLogged"),
			Namespace:  metricNamespace,
			Statistic:  jsii.String("sum"),                      // The sum of errors over a
			Period:     awscdk.Duration_Minutes(jsii.Number(5)), // 5 minute period.
		}),
		EvaluationPeriods:  jsii.Number(1),                                          // If, in the last "1" of those periods
		DatapointsToAlarm:  jsii.Number(1),                                          // There's more than one datapoint
		ComparisonOperator: awscloudwatch.ComparisonOperator_GREATER_THAN_THRESHOLD, // Where the metric >= to
		Threshold:          jsii.Number(float64(errorsIn5MinuteWindow)),             // The max errors in 5 minute window then
		ActionsEnabled:     jsii.Bool(true),                                         // Do the actions.
		TreatMissingData:   awscloudwatch.TreatMissingData_NOT_BREACHING,            // And ignore any missing data.
	})
	errorsAlarm.AddAlarmAction(awscloudwatchactions.NewSnsAction(alarmTopic))
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
