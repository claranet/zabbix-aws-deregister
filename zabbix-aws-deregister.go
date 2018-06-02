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

func decrypt(encrypted string, variable string) string {
	kmsClient := kms.New(session.New())
	decodedBytes, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		log.WithFields(log.Fields{
			"stage":    "config",
			"variable": variable,
			"error":    err,
		}).Panic("Failed to decode string")
	}
	input := &kms.DecryptInput{
		CiphertextBlob: decodedBytes,
	}
	response, err := kmsClient.Decrypt(input)
	if err != nil {
		log.WithFields(log.Fields{
			"stage":    "config",
			"variable": variable,
			"error":    err,
		}).Panic("Failed to decrypt bytes from kms key")
	}

	log.WithFields(log.Fields{
		"stage":    "config",
		"variable": variable,
	}).Debug("Configuration set")

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

	debugString := os.Getenv("DEBUG")
	if debugString != "" {
		debug, err := strconv.ParseBool(debugString)
		if err != nil {
			log.WithFields(log.Fields{
				"stage":    "config",
				"error":    err,
				"variable": "DEBUG",
			}).Error("Failed to parse boolean")
		} else if debug {
			log.SetLevel(log.DebugLevel)
			log.WithFields(log.Fields{
				"stage":    "config",
				"variable": "DEBUG",
				"value":    true,
			}).Debug("Configuration set")
		}
	}

	Config.URL, ok = os.LookupEnv("ZABBIX_URL")
	if !ok {
		log.WithFields(log.Fields{
			"stage":    "config",
			"variable": "ZABBIX_URL",
		}).Panic("Environment variable not set")
	}

	encryptedUser, ok = os.LookupEnv("ZABBIX_USER")
	if !ok {
		log.WithFields(log.Fields{
			"stage":    "config",
			"variable": "ZABBIX_USER",
		}).Panic("Environment variable not set")
	}
	encryptedPass, ok = os.LookupEnv("ZABBIX_PASS")
	if !ok {
		log.WithFields(log.Fields{
			"stage":    "config",
			"variable": "ZABBIX_PASS",
		}).Panic("Environment variable not set")
	}

	Config.User = decrypt(encryptedUser, "ZABBIX_USER")
	Config.Password = decrypt(encryptedPass, "ZABBIX_PASS")
}

// HandleRequest hot start lambda function start point
func HandleRequest(snsEvents events.SNSEvent) (string, error) {
	const zabbixHostDisable = 1
	const getIP = "http://ip.clara.net"
	var err error

	log.WithFields(log.Fields{
		"stage": "init",
		"event": "sns",
		"value": &snsEvents,
	}).Debug("Catching event")

	var cloudwatchEvent events.CloudWatchEvent
	err = json.Unmarshal([]byte(snsEvents.Records[0].SNS.Message), &cloudwatchEvent)
	if err != nil {
		log.WithFields(log.Fields{
			"stage": "init",
			"event": "sns",
			"err":   err,
		}).Error("Failed unmarshal json event")
		return "", err
	}

	log.WithFields(log.Fields{
		"stage": "init",
		"event": "cloudwatch",
		"value": &cloudwatchEvent,
	}).Debug("Catching event")

	var autoscalingEvent AutoscalingEvent
	err = json.Unmarshal(cloudwatchEvent.Detail, &autoscalingEvent)
	if err != nil {
		log.WithFields(log.Fields{
			"stage": "init",
			"event": "autoscaling",
			"err":   err,
		}).Error("Failed unmarshal json event")
		return "", err
	}

	log.WithFields(log.Fields{
		"stage": "init",
		"event": "autoscaling",
		"value": &autoscalingEvent,
	}).Debug("Catching event")

	searchInventory := make(map[string]string)
	searchInventory["alias"] = autoscalingEvent.InstanceID

	if log.GetLevel() == log.DebugLevel {
		resp, err := http.Get(getIP)
		if err != nil {
			log.WithFields(log.Fields{
				"stage": "info",
				"err":   err,
			}).Error("Failed retrieve internet IP address")
			return "", err
		}
		defer resp.Body.Close()
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		bodyString := string(bodyBytes)
		log.WithFields(log.Fields{
			"stage": "info",
			"value": bodyString,
		}).Debug("Retrieving internet IP address")
	}

	log.WithFields(log.Fields{
		"stage": "connection",
		"url":   Config.URL,
	}).Debug("Connecting to zabbix API")
	api := zabbix.NewAPI(Config.URL)

	log.WithFields(log.Fields{
		"stage": "connection",
		"url":   Config.URL,
		"user":  Config.User,
	}).Debug("Loging to zabbix API")
	_, err = api.Login(Config.User, Config.Password)
	if err != nil {
		log.WithFields(log.Fields{
			"stage": "connection",
			"url":   Config.URL,
			"user":  Config.User,
			"error": err,
		}).Error("Failed to logging on zabbix API")
		return "", err
	}

	log.WithFields(log.Fields{
		"stage":    "get",
		"instance": autoscalingEvent.InstanceID,
	}).Debug("Searching for corresponding zabbix host")
	res, err := api.HostsGet(zabbix.Params{
		"output":          []string{"host"},
		"selectInventory": []string{"alias"},
		"searchInventory": searchInventory,
	})

	if err != nil {
		log.WithFields(log.Fields{
			"stage":    "get",
			"instance": autoscalingEvent.InstanceID,
			"error":    err,
		}).Error("Failed searching for corresponding zabbix host")
		return "", err
	}

	if len(res) < 1 {
		log.WithFields(log.Fields{
			"stage":    "get",
			"instance": autoscalingEvent.InstanceID,
		}).Warn("Zabbix host not found, do nothing")
		return fmt.Sprintf("host not found"), nil
	} else if len(res) > 1 {
		log.WithFields(log.Fields{
			"stage":    "get",
			"instance": autoscalingEvent.InstanceID,
		}).Error("More than one zabbix host found")
		return "", fmt.Errorf("more than one hosts found")
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
		descriptionJSON, err := json.Marshal(descriptionStruct)
		if err != nil {
			log.WithFields(log.Fields{
				"stage":       "update",
				"description": descriptionStruct,
				"host":        res[0].HostId,
				"error":       err,
			}).Error("Failed marshal json from description struct")
			return "", err
		}

		log.WithFields(log.Fields{
			"stage":       "update",
			"instance":    autoscalingEvent.InstanceID,
			"host":        res[0].HostId,
			"description": string(descriptionJSON),
			"cur_name":    res[0].Host,
			"new_name":    name,
			"enabled":     false,
		}).Debug("Updating zabbix host")
		_, err = api.CallWithError("host.update", zabbix.Params{
			"hostid":      res[0].HostId,
			"host":        name,
			"name":        name,
			"description": string(descriptionJSON),
			"status":      zabbixHostDisable,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"stage":       "update",
				"instance":    autoscalingEvent.InstanceID,
				"host":        res[0].HostId,
				"description": string(descriptionJSON),
				"cur_name":    res[0].Host,
				"new_name":    name,
				"enabled":     false,
				"error":       err,
			}).Error("Updating zabbix host")
			return "", err
		}

	}

	log.WithFields(log.Fields{
		"stage":    "success",
		"instance": autoscalingEvent.InstanceID,
		"host":     res[0].HostId,
	}).Debug("Function finished successfully")
	return fmt.Sprintf(autoscalingEvent.InstanceID), nil
}

func main() {
	lambda.Start(HandleRequest)
}
