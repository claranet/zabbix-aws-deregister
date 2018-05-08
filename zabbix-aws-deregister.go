package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/AlekSi/zabbix"
	"github.com/aws/aws-lambda-go/lambda"
)

type AutoscalingEventDetails struct {
	InstanceId           string `json:"EC2InstanceId"`
	EndTime              string `json:"EndTime"`
	StartTime            string `json:"StartTime"`
	AutoScalingGroupName string `json:"AutoScalingGroupName"`
	Cause                string `json:"Cause"`
	Description          string `json:"Description"`
	StatusCode           string `json:"StatusCode"`
}
type AutoscalingEvent struct {
	DetailType string                  `json:"detail-type"`
	Source     string                  `json:"source"`
	Account    string                  `json:"account"`
	Time       string                  `json:"time"`
	Region     string                  `json:"region"`
	Detail     AutoscalingEventDetails `json:"detail"`
}

type Configuration struct {
	Url      string
	User     string
	Password string
	Deleting bool
}

func HandleRequest(event AutoscalingEvent) (string, error) {
	var ok bool
	var err error
	configuration := Configuration{}
	configuration.Url, ok = os.LookupEnv("ZABBIX_URL")
	if !ok {
		panic("ZABBIX_URL not set")
	}
	configuration.User, ok = os.LookupEnv("ZABBIX_USER")
	if !ok {
		panic("ZABBIX_USER not set")
	}
	configuration.Password, ok = os.LookupEnv("ZABBIX_PASS")
	if !ok {
		panic("ZABBIX_PASS not set")
	}
	configuration.Deleting, err = strconv.ParseBool(os.Getenv("DELETING_HOST"))
	if err != nil {
		panic(err)
	}

	searchInventory := make(map[string]string)
	searchInventory["alias"] = event.Detail.InstanceId

	api := zabbix.NewAPI(configuration.Url)
	_, err = api.Login(configuration.User, configuration.Password)
	if err != nil {
		panic(err)
	}
	res, err := api.HostsGet(zabbix.Params{
		"output":          []string{"host"},
		"selectInventory": []string{"alias"},
		"searchInventory": searchInventory,
	})
	if err != nil {
		panic(err)
	}
	for _, host := range res {
		if configuration.Deleting {
			_, err := api.CallWithError("host.delete", []string{host.HostId})
			if err != nil {
				panic(err)
			}
		} else {
			_, err := api.CallWithError("host.update", zabbix.Params{
				"hostid": host.HostId,
				"status": 1,
			})
			if err != nil {
				panic(err)
			}
		}
	}

	return fmt.Sprintf("Zabbix host correspondig to AWS instanceid %s has been disabled", event.Detail.InstanceId), nil
}

func main() {
	lambda.Start(HandleRequest)
}
