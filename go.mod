module github.com/claranet/zabbix-aws-deregister

go 1.13

require (
	github.com/AlekSi/zabbix v0.2.0
	github.com/aws/aws-lambda-go v1.13.3
	github.com/aws/aws-sdk-go v1.26.7
	github.com/sirupsen/logrus v1.4.2
	golang.org/x/net v0.0.0-20191209160850-c0dbc17a3553 // indirect
	gonuts.io/aleksi/reflector v0.0.0-00010101000000-000000000000 // indirect
)

replace gonuts.io/aleksi/reflector => github.com/AlekSi/reflector v0.4.1
