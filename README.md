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
echo 'UserParameter=aws.metadata[*],curl -s http://169.254.169.254/latest/meta-data/$1' > /etc/zabbix/zabbix_agentd.d/aws.conf
service zabbix-agent restart
```

## Script preparation

Build and zip `zabbix-aws-deregister.go` :

    $ cd go/src/zabbix-aws-deregister && go get && go build && zip zabbix-aws-deregister.zip zabbix-aws-deregister

Or simply use [the zip provided in this repo](https://bitbucket.org/morea/zabbix/downloads/zabbix-aws-deregister.zip)

## AWS configuration

The following resources should be created :

* IAM policy
* IAM role
* Lambda function
* Cloudwatch event rule

1/ Create the IAM policy `zabbix-aws-deregister` [AWS console > IAM > Policies > Create policy > JSON]:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Action": [
                "logs:CreateLogStream"
            ],
            "Resource": [
                "arn:aws:logs:eu-west-1:424242424242:log-group:/aws/lambda/zabbix-aws-deregister:*"
            ],
            "Effect": "Allow"
        },
        {
            "Action": [
                "logs:PutLogEvents"
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

3/ Create the lambda function `zabbix-aws-deregister` [AWS console > Lambda > Functions > Create function > Author from scratch]:

```
Name: zabbix-aws-deregister
Runtime: Go 1.x
Role: Choose an existing role
Existing role: zabbix-aws-deregister
```

4/ Configure the lambda function `zabbix-aws-deregister` as following:

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
```

Notice: if `DELETING_HOST` is set to `false` so zabbix hosts are not deleted, only disabled.

5/ Create the cloudwatch event rule [AWS console > Cloudwatch > Events > Rules > Create rule].

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

b/ Add a new target `Lambda function`:

```
Function: zabbix-aws-deregister (previously created lambda function)
Configure Input: 
    Part of the matched event: $.detail
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
  "Description": "Terminating EC2 instance: i-070fc7ff625f55529",
  "Details": {
    "Subnet ID": "subnet-5c8fb338",
    "Availability Zone": "eu-west-1b"
  },
  "EndTime": "2018-03-06T19:12:44.047Z",
  "RequestId": "0a2fb0a9-18ee-44f2-bace-47309ef8ab79",
  "ActivityId": "0a2fb0a9-18ee-44f2-bace-47309ef8ab79",
  "Cause": "At 2018-03-06T19:11:33Z a user request update of AutoScalingGroup constraints to min: 0, max: 2, desired: 0 changing the desired capacity from 1 to 0.  At 2018-03-06T19:11:42Z an instance was taken out of service in response to a difference between desired and actual capacity, shrinking the capacity from 1 to 0.  At 2018-03-06T19:11:42Z instance i-070fc7ff625f55529 was selected for termination.",
  "AutoScalingGroupName": "test-asg",
  "StartTime": "2018-03-06T19:11:42.182Z",
  "EC2InstanceId": "i-070fc7ff625f55529",
  "StatusCode": "InProgress",
  "StatusMessage": ""
}
```

Then, you just have to click on `Test` button to fire the function, review result message and cloudwatch logs.

## References

* [Zabbix Manual](https://www.zabbix.com/documentation/3.4/start)
* [AWS Lambda](https://docs.aws.amazon.com/lambda/latest/dg/welcome.html)
* [AWS Lambda Golang support](https://aws.amazon.com/fr/blogs/compute/announcing-go-support-for-aws-lambda/)

## License

Copyright (c) 2018 Claranet. Available under the MIT License.
