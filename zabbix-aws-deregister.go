package main

import (
	"fmt"
	"log"
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
	const zabbixHostDisable = 1

	log.Print("Initializing environement")

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
		return "Error parsing boolean value from DELETING_HOST environment variable", err
	}

	searchInventory := make(map[string]string)
	searchInventory["alias"] = event.Detail.InstanceId

	log.Print("Connecting to zabbix api")
	api := zabbix.NewAPI(configuration.Url)
	log.Print("Authentificating to zabbix api")
	_, err = api.Login(configuration.User, configuration.Password)
	if err != nil {
		return "Error loging to zabbix api", err
	}
	log.Printf("Getting zabbix host corresponding to instanceid %s", event.Detail.InstanceId)
	res, err := api.HostsGet(zabbix.Params{
		"output":          []string{"host"},
		"selectInventory": []string{"alias"},
		"searchInventory": searchInventory,
	})
	if err != nil {
		return "Error getting hosts from zabbix api", err
	}
	if len(res) < 1 {
		return "Error analyzing hosts list value", fmt.Errorf("Zabbix host not found for instanceid %s", event.Detail.InstanceId)
	} else if len(res) > 1 {
		return "Error analyzing hosts list value", fmt.Errorf("More than one host found for instanceid %s", event.Detail.InstanceId)
	} else {
		if configuration.Deleting {
			log.Printf("Deleting zabbix host %s", res[0].HostId)
			_, err := api.CallWithError("host.delete", []string{res[0].HostId})
			if err != nil {
				return "Error deleting host from zabbix api", err
			}
		} else {
			log.Printf("Disabling zabbix host %s", res[0].HostId)
			_, err := api.CallWithError("host.update", zabbix.Params{
				"hostid": res[0].HostId,
				"status": zabbixHostDisable,
			})
			if err != nil {
				return "Error disabling host from zabbix api", err

			}
		}
	}

	log.Print("Function finished successfully")

	return fmt.Sprintf("Zabbix host corresponding to AWS instanceid %s has been scaled down", event.Detail.InstanceId), nil
}

func main() {
	lambda.Start(HandleRequest)
}
