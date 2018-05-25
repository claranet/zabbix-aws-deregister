# Zabbix Deregister for AWS AutoScaling

Disable or Delete host from zabbix automatically when AWS autoscaling group terminates its corresponding instance:
Cloudwatch event rule catchs the scale down event from autoscaling group then run a lambda function which will connect to Zabbix API to update host.

## Zabbix configuration

### Administration

1/ Create a new User Group [Administration > User Groups > Create user group]

```
User group:
Group name: AWS AutoScaling Deregister
Frontend access: Disabled
Enabled: [x]
Permissions:
Host Group: [All Host Groups to manage]
Permission: Read-write
```

2/ Create a User [Administration > Users > Create user]

```
User:
Alias: aws-autoScaling-deregister
Name: AWS AutoScaling Deregister
Surname: aws-autoScaling-deregister
Groups: AWS AutoScaling Deregister (created in step 1)
Password/confirm: (Remember it, you will need it soon)
Permissions:
User type: Zabbix Admin
```

### Server configuration

1/ Create a new template [Configuration > Templates > Create template] named `AWS_Metadata`.

2/ Edit this template [Configuration > Templates > `AWS_Metadata` > Items > Create item] :

```
Name: InstanceID
Type: Zabbix Agent
Key: aws.metadata[instance-id]
Type of information: Character
Update interval (in sec): 90
Populates host inventory field: Alias
Description: AWS InstanceID from metadata
```

3/ Update all auto registration actions used for autoscaling hosts [Configuration > Actions > Auto registration] to add new operations:
* `Set host inventory mode` to `Automatic` (if not already done)
* `Link to templates` to `AWS_Metadata`

### Agent configuration

A new UserParameter needs to be added to retrieve the AWS instanceId:

```shell
echo "UserParameter=aws.metadata[*],bash -c 'if [[ \"\$1\" == *\"security-credentials\"* ]]; then echo \"permissions denied\"; else curl -s http://169.254.169.254/latest/meta-data/$1; fi'" >> /etc/zabbix/zabbix_agentd.d/aws.conf
service zabbix-agent restart
```

## Script preparation

Build and zip `zabbix-aws-deregister.go` :

    $ cd go/src/zabbix-aws-deregister && go get && go build && zip zabbix-aws-deregister.zip zabbix-aws-deregister

Or simply use [the zip provided in this repo](https://github.com/claranet/zabbix-aws-deregister/releases/download/v1.0.0/zabbix-aws-deregister_1.0.0_linux_amd64.zip)

## AWS configuration

The following resources should be created :

* IAM policy
* IAM role
* Lambda function
* Cloudwatch event rule
* SNS topic
* KMS key

1/ Create the IAM policy `zabbix-aws-deregister` [AWS console > IAM > Policies > Create policy > JSON]:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Action": [
                "logs:CreateLogGroup",
                "logs:DescribeLogStreams",
            ],
            "Resource": [
                "arn:aws:logs:eu-west-1:424242424242:*"
            ],
            "Effect": "Allow"
        },
        {
            "Action": [
                "logs:CreateLogStream",
                "logs:PutLogEvents",
            ],
            "Resource": [
                "arn:aws:logs:eu-west-1:424242424242:log-group:/aws/lambda/zabbix-aws-deregister:*:*"
            ],
            "Effect": "Allow"
        }
    ]
}
```

2/ Create the IAM role `zabbix-aws-deregister` [AWS console > IAM > Roles > Create role > AWS Service > Lambda] attaching the previously created policy `zabbix-aws-deregister`.

3/ Create the KMS key `zabbix-aws-deregister` in the same region of your lambda [AWS console > IAM > Ecryption Keys > Create Key]:

```
Alias and Description
    Alias: lambda/zabbix-aws-deregister
    Name: zabbix-aws-deregister
Key Administrative Permissions: add admin aws role(s)
Key Usage Permissions: add the previously created role
```

4/ Create the lambda function `zabbix-aws-deregister` [AWS console > Lambda > Functions > Create function > Author from scratch]:

```
Name: zabbix-aws-deregister
Runtime: Go 1.x
Role: Choose an existing role
Existing role: zabbix-aws-deregister
```

5/ Configure the lambda function `zabbix-aws-deregister` as following:

```
Function code:
    Code entry type: Upload a .ZIP file
    Function package: Upload the zip file previously created from "Script preparation" step
    Handler: zabbix-aws-deregister
Environment variables:
    ZABBIX_URL: https://zabbix.host.tld/api_jsonrpc.php
    ZABBIX_USER: previously created user from "Zabbix administration" step (aws-autoScaling-deregister)
    ZABBIX_PASS: previously created password from "Zabbix administration" step
    DELETING_HOST: true / false
    DEBUG: true / false
    Encryption configuration:
        Enable helpers for encryption in transit: [x]
        KMS key to encrypt in transit: select the previously created key

Execution role: set the previously created role

```

Then, click on `encrypt` button for both `ZABBIX_USER` and `ZABBIX_PASS` environment variables.

Notice: if `DELETING_HOST` is set to `false` so zabbix hosts are not deleted, only disabled.

6/ Create an sns topic and subscribe [AWS console > SNS > Topics > Create topic]

7/ Add subscription on sns topic to the previously created lambda

8/ Create the cloudwatch event rule [AWS console > Cloudwatch > Events > Rules > Create rule].

a/ Configure Event Source as following :

```
Event Pattern: [x]
Service Name: Auto Scaling
Event Type: Instance Launch and Terminate
Specific instance event(s): 
  EC2 Instance Terminate Successful
  EC2 Instance Terminate Unsuccessful
Any group name: [x]
```

The event pattern should be :

```json
{
  "source": [
    "aws.autoscaling"
  ],
  "detail-type": [
    "EC2 Instance Terminate Successful",
    "EC2 Instance Terminate Unsuccessful"
  ]
}
```

b/ Add a new target `SNS topic`:

```
Topic: zabbix-aws-deregister (previously created sns topic)
Configure Input: Matched event
```

c/ Configure rule details:

```
Name: zabbix-aws-deregister
Description: Automatic instance deregistration from zabbix when scale down
State: Enabled
```

## Troubleshooting

To quicly test the lambda function and get result it is possible to create a test event [configure test events > Create a new test event],
it could be named `ScaleDown` and looks like the following json:

```json
{
  "Records": [
    {
      "EventVersion": "1.0",
      "EventSubscriptionArn": "arn:aws:sns:eu-west-1:424242424242:test-zabbix:76f12898-fd88-4c72-9fa0-fa0793a98acf",
      "EventSource": "aws:sns",
      "Sns": {
        "Signature": "sRgkFMVQwHihlUmLB4u6HkdSw2z8f2uUsFXW/fJOJ8pb07G/Gbn8d+DQujIgaXg2Mx+YNrh3iclG7Llcmo/11h3HFPQMJ5HYYzN9RH0H/hAYjByl8Sx1TwxR8+9AhO0IXCrmCNz9n5egpOdglH/B3oV1z4aEMXLoHh3C8CIuWy7uyWiCgWT3cd3fq891GtRbMofQeCORqqocGvBEYf6rFttPP/lMg/VtyOtSxRKa9QA9xSBqOuGZAdr2G1saYJ3y1Nr4vIWy12VZ0B4glnp7mEwjcwrrgXjqIUnQoGfICDaxaJNVf4PZtggUDICbfgqeqO9+g5PU7fCsLHbBfVkuIA==",
        "MessageId": "723a5c6d-33df-5057-a9b8-0fedcf8f6654",
        "Type": "Notification",
        "TopicArn": "arn:aws:sns:eu-west-1:424242424242:test-zabbix",
        "MessageAttributes": {},
        "SignatureVersion": "1",
        "Timestamp": "2018-05-24T17:44:48.887Z",
        "SigningCertUrl": "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-eaea6120e66ea12e88dcd8bcbddca752.pem",
        "Message": "{\"version\":\"0\",\"id\":\"c2cf5c1e-e2bd-30ef-2524-7ffb7d579931\",\"detail-type\":\"EC2 Instance Terminate Successful\",\"source\":\"aws.autoscaling\",\"account\":\"424242424242\",\"time\":\"2018-05-24T17:44:48Z\",\"region\":\"eu-west-1\",\"resources\":[\"arn:aws:autoscaling:eu-west-1:424242424242:autoScalingGroup:dde9d75e-bb6e-4718-8840-9a3dc6a0af80:autoScalingGroupName/as.datadog-sandbox.default.webfront\",\"arn:aws:ec2:eu-west-1:424242424242:instance/i-0926bd0a06518bf44\"],\"detail\":{\"Description\":\"Terminating EC2 instance: i-0926bd0a06518bf44\",\"Details\":{\"Subnet ID\":\"subnet-5767c533\",\"Availability Zone\":\"eu-west-1b\"},\"EndTime\":\"2018-05-24T17:44:48.562Z\",\"RequestId\":\"66259088-028e-46da-ab24-9dc2b68e2607\",\"ActivityId\":\"66259088-028e-46da-ab24-9dc2b68e2607\",\"Cause\":\"At 2018-05-24T17:43:54Z a user request update of AutoScalingGroup constraints to min: 0, max: 2, desired: 0 changing the desired capacity from 1 to 0.  At 2018-05-24T17:44:06Z an instance was taken out of service in response to a difference between desired and actual capacity, shrinking the capacity from 1 to 0.  At 2018-05-24T17:44:06Z instance i-0926bd0a06518bf44 was selected for termination.\",\"AutoScalingGroupName\":\"as.datadog-sandbox.default.webfront\",\"StartTime\":\"2018-05-24T17:44:06.609Z\",\"EC2InstanceId\":\"i-0926bd0a06518bf44\",\"StatusCode\":\"InProgress\",\"StatusMessage\":\"\"}}",
        "UnsubscribeUrl": "https://sns.eu-west-1.amazonaws.com/?Action=Unsubscribe&SubscriptionArn=arn:aws:sns:eu-west-1:424242424242:test-zabbix:76f12898-fd88-4c72-9fa0-fa0793a98acf",
        "Subject": ""
      }
    }
  ]
}
```

Then, you just have to click on `Test` button to fire the function, review result message and cloudwatch logs.

## References

* [Zabbix Manual](https://www.zabbix.com/documentation/3.4/start)
* [AWS Lambda](https://docs.aws.amazon.com/lambda/latest/dg/welcome.html)
* [AWS Lambda Golang support](https://aws.amazon.com/fr/blogs/compute/announcing-go-support-for-aws-lambda/)

## License

Copyright (c) 2018 Claranet. Available under the MIT License.
