package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"encoding/base64"

	"github.com/AlekSi/zabbix"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	log "github.com/sirupsen/logrus"
)

// AutoscalingEvent structure to serialize json from Message attribute of CloudwatchEvent
type AutoscalingEvent struct {
	InstanceID           string `json:"EC2InstanceId"`
	EndTime              string `json:"EndTime"`
	StartTime            string `json:"StartTime"`
	AutoScalingGroupName string `json:"AutoScalingGroupName"`
	Cause                string `json:"Cause"`
	Description          string `json:"Description"`
	StatusCode           string `json:"StatusCode"`
}

// Configuration structure to store Config for the lambda function
type Configuration struct {
	URL      string
	User     string
	Password string
}

// Config Global configuration structure
var Config Configuration

func decrypt(requestLogger log.Entry, encrypted string, variable string) string {
	kmsClient := kms.New(session.New())
	decodedBytes, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		requestLogger.WithFields(log.Fields{"variable": variable}).WithError(err).Panic("Failed to decode string")
	}
	input := &kms.DecryptInput{
		CiphertextBlob: decodedBytes,
	}
	response, err := kmsClient.Decrypt(input)
	if err != nil {
		requestLogger.WithFields(log.Fields{"variable": variable}).WithError(err).Panic("Failed to decrypt bytes from kms key")
	}

	requestLogger.WithFields(log.Fields{"variable": variable}).Debug("Configuration set")

	// Plaintext is a byte array, so convert to string
	return string(response.Plaintext[:])
}

// init function to setup environment
func init() {
	var ok bool
	var encryptedUser string
	var encryptedPass string

	log.SetFormatter(&log.JSONFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)

	requestLogger := log.WithFields(log.Fields{"stage": "config"})

	debugString := os.Getenv("DEBUG")
	if debugString != "" {
		debug, err := strconv.ParseBool(debugString)
		if err != nil {
			requestLogger.WithFields(log.Fields{"variable": "DEBUG"}).WithError(err).Error("Failed to parse boolean")
		} else if debug {
			log.SetLevel(log.DebugLevel)
			requestLogger.WithFields(log.Fields{"variable": "DEBUG", "value": true}).Debug("Configuration set")
		}
	}

	Config.URL, ok = os.LookupEnv("ZABBIX_URL")
	if !ok {
		requestLogger.WithFields(log.Fields{"variable": "ZABBIX_URL"}).Panic("Environment variable not set")
	}

	encryptedUser, ok = os.LookupEnv("ZABBIX_USER")
	if !ok {
		requestLogger.WithFields(log.Fields{"variable": "ZABBIX_USER"}).Panic("Environment variable not set")
	}
	encryptedPass, ok = os.LookupEnv("ZABBIX_PASS")
	if !ok {
		requestLogger.WithFields(log.Fields{"variable": "ZABBIX_PASS"}).Panic("Environment variable not set")
	}

	Config.User = decrypt(*requestLogger, encryptedUser, "ZABBIX_USER")
	Config.Password = decrypt(*requestLogger, encryptedPass, "ZABBIX_PASS")
}

// HandleRequest hot start lambda function start point
func HandleRequest(snsEvents events.SNSEvent) (string, error) {
	const zabbixHostDisable = 1
	const getIP = "http://ip.clara.net"
	var err error

	requestLogger := log.WithFields(log.Fields{"stage": "init"})

	requestLogger.WithFields(log.Fields{"event": "sns", "value": &snsEvents}).Debug("Catching event")

	var cloudwatchEvent events.CloudWatchEvent
	err = json.Unmarshal([]byte(snsEvents.Records[0].SNS.Message), &cloudwatchEvent)
	if err != nil {
		requestLogger.WithFields(log.Fields{"event": "sns"}).WithError(err).Error("Failed unmarshal json event")
		return "", err
	}

	requestLogger.WithFields(log.Fields{"event": "cloudwatch", "value": &cloudwatchEvent}).Debug("Catching event")

	var autoscalingEvent AutoscalingEvent
	err = json.Unmarshal(cloudwatchEvent.Detail, &autoscalingEvent)
	if err != nil {
		requestLogger.WithFields(log.Fields{"event": "autoscaling"}).WithError(err).Error("Failed unmarshal json event")
		return "", err
	}

	requestLogger.WithFields(log.Fields{"event": "autoscaling", "value": &autoscalingEvent}).Debug("Catching event")

	searchInventory := make(map[string]string)
	searchInventory["alias"] = autoscalingEvent.InstanceID

	requestLogger = log.WithFields(log.Fields{"stage": "info"})

	if log.GetLevel() == log.DebugLevel {
		resp, err := http.Get(getIP)
		if err != nil {
			requestLogger.WithError(err).Error("Failed retrieve internet IP address")
			return "", err
		}
		defer resp.Body.Close()
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		bodyString := string(bodyBytes)
		requestLogger.WithFields(log.Fields{"value": bodyString}).Debug("Retrieving internet IP address")
	}

	requestLogger = log.WithFields(log.Fields{"stage": "connection"})

	requestLogger.WithFields(log.Fields{"url": Config.URL}).Debug("Connecting to zabbix API")
	api := zabbix.NewAPI(Config.URL)

	requestLogger.WithFields(log.Fields{"url": Config.URL, "user": Config.User}).Debug("Loging to zabbix API")
	_, err = api.Login(Config.User, Config.Password)
	if err != nil {
		requestLogger.WithFields(log.Fields{"url": Config.URL, "user": Config.User}).WithError(err).Error("Failed to logging on zabbix API")
		return "", err
	}

	requestLogger = log.WithFields(log.Fields{"stage": "get"})

	requestLogger.WithFields(log.Fields{"instance": autoscalingEvent.InstanceID}).Debug("Searching for corresponding zabbix host")
	res, err := api.HostsGet(zabbix.Params{
		"output":          []string{"host"},
		"selectInventory": []string{"alias"},
		"searchInventory": searchInventory,
	})

	if err != nil {
		requestLogger.WithFields(log.Fields{"instance": autoscalingEvent.InstanceID}).WithError(err).Error("Failed searching for corresponding zabbix host")
		return "", err
	}

	if len(res) < 1 {
		requestLogger.WithFields(log.Fields{"instance": autoscalingEvent.InstanceID}).Warn("Zabbix host not found, do nothing")
		return fmt.Sprintf("host not found"), nil
	} else if len(res) > 1 {
		requestLogger.WithFields(log.Fields{"instance": autoscalingEvent.InstanceID}).Error("More than one zabbix host found")
		return "", fmt.Errorf("more than one hosts found")
	} else if strings.HasPrefix(res[0].Host, "ZDTP_") {
		requestLogger.WithFields(log.Fields{"instance": autoscalingEvent.InstanceID}).Warn("Zabbix host already updated, do nothing")
		return fmt.Sprintf("host already updated"), nil
	} else {

		type description struct {
			Time    time.Time
			Action  string
			Pending []string
			Name    string
		}

		name := strings.Join([]string{"ZDTP", res[0].Host}, "_")

		descriptionStruct := description{
			Time:    time.Now(),
			Action:  "deregistered by zabbix aws deregister",
			Pending: []string{"purge"},
			Name:    res[0].Host,
		}

		requestLogger = log.WithFields(log.Fields{"stage": "update", "host": res[0].HostId})

		descriptionJSON, err := json.Marshal(descriptionStruct)
		if err != nil {
			requestLogger.WithFields(log.Fields{"description": descriptionStruct}).WithError(err).Error("Failed marshal json from description struct")
			return "", err
		}

		requestLogger.WithFields(log.Fields{"description": string(descriptionJSON), "cur_name": res[0].Host, "new_name": name, "enabled": false}).Debug("Updating zabbix host")
		_, err = api.CallWithError("host.update", zabbix.Params{
			"hostid":      res[0].HostId,
			"host":        name,
			"name":        name,
			"description": string(descriptionJSON),
			"status":      zabbixHostDisable,
		})
		if err != nil {
			requestLogger.WithFields(log.Fields{"description": string(descriptionJSON), "cur_name": res[0].Host, "new_name": name, "enabled": false}).WithError(err).Error("Updating zabbix host")
			return "", err
		}

	}

	requestLogger.WithFields(log.Fields{"stage": "success", "instance": autoscalingEvent.InstanceID, "host": res[0].HostId}).Info("Function finished successfully")
	return fmt.Sprintf(autoscalingEvent.InstanceID), nil
}

func main() {
	lambda.Start(HandleRequest)
}
