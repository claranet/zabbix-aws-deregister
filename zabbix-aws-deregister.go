package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
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

type SnsEvent struct {
	Type             string `json:"Type"`
	TopicArn         string `json:"TopicArn"`
	MessageId        string `json:"MessageId"`
	Message          string `json:"Message"`
	Timestamp        string `json:"Timestamp"`
	SignatureVersion string `json:"SignatureVersion"`
	Signature        string `json:"Signature"`
	SigningCertURL   string `json:"SigningCertURL"`
	UnsubscribeURL   string `json:"UnsubscribeURL"`
}

type Configuration struct {
	Url      string
	User     string
	Password string
	Deleting bool
	Debug    bool
}

func HandleRequest(snsEvent SnsEvent) (string, error) {

	var ok bool
	var err error
	const zabbixHostDisable = 1

	log.Print("Initializing environment")

	configuration := Configuration{}
	configuration.Url, ok = os.LookupEnv("ZABBIX_URL")
	if !ok {
		return "Error parsing ZABBIX_URL environment variable", fmt.Errorf("ZABBIX_URL environement variable not set")
	}
	configuration.User, ok = os.LookupEnv("ZABBIX_USER")
	if !ok {
		return "Error parsing ZABBIX_USER environment variable", fmt.Errorf("ZABBIX_USER environement variable not set")
	}
	configuration.Password, ok = os.LookupEnv("ZABBIX_PASS")
	if !ok {
		return "Error parsing ZABBIX_PASS environment variable", fmt.Errorf("ZABBIX_PASS environement variable not set")
	}
	deletingHost := os.Getenv("DELETING_HOST")
	if deletingHost != "" {
		configuration.Deleting, err = strconv.ParseBool(deletingHost)
		if err != nil {
			return "Error parsing boolean value from DELETING_HOST environment variable", err
		}
	} else {
		configuration.Deleting = false
	}
	debug := os.Getenv("DEBUG")
	if debug != "" {
		configuration.Debug, err = strconv.ParseBool(debug)
		if err != nil {
			return "Error parsing boolean value from DEBUG environment variable", err
		}
	} else {
		configuration.Debug = false
	}

	if configuration.Debug {
		log.Print("Catching SNS Event:")
		log.Print(snsEvent)
	}

	autoscalingEvent := &AutoscalingEvent{}
	err = json.Unmarshal([]byte(snsEvent.Message), autoscalingEvent)
	if err != nil {
		return "Error cannot unmarshal message from sns event", err
	}

	if configuration.Debug {
		log.Print("Parsing autoscale event from sns event:")
		log.Print(autoscalingEvent)
	}

	searchInventory := make(map[string]string)
	searchInventory["alias"] = autoscalingEvent.Detail.InstanceId

	if configuration.Debug {
		resp, err := http.Get("http://ip.clara.net")
		if err != nil {
			return "Error getting internet ip address", err
		}
		defer resp.Body.Close()
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		bodyString := string(bodyBytes)
		log.Printf("Lambda outbound traffic from : %s", bodyString)
	}

	log.Print("Connecting to zabbix api")
	api := zabbix.NewAPI(configuration.Url)
	log.Print("Authentificating to zabbix api")
	_, err = api.Login(configuration.User, configuration.Password)
	if err != nil {
		return "Error loging to zabbix api", err
	}
	log.Printf("Getting zabbix host corresponding to instanceid %s", autoscalingEvent.Detail.InstanceId)
	res, err := api.HostsGet(zabbix.Params{
		"output":          []string{"host"},
		"selectInventory": []string{"alias"},
		"searchInventory": searchInventory,
	})
	if err != nil {
		return "Error getting hosts from zabbix api", err
	}
	if len(res) < 1 {
		return fmt.Sprintf("Zabbix host not found for instanceid %s", autoscalingEvent.Detail.InstanceId), nil
	} else if len(res) > 1 {
		return "Error analyzing hosts list value", fmt.Errorf("More than one host found for instanceid %s", autoscalingEvent.Detail.InstanceId)
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

	return fmt.Sprintf("Zabbix host corresponding to AWS instanceid %s has been scaled down", autoscalingEvent.Detail.InstanceId), nil
}

func main() {
	lambda.Start(HandleRequest)
}
