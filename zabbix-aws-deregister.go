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
	Deleting bool
}

// Value corresponding to zabbix host disabled
const ZabbixHostDisable = 1

// Global configuration structure
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

func init() {
	var ok bool
	var err error
	var encryptedUser string
	var encryptedPass string

	log.SetFormatter(&log.JSONFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)

	debug := os.Getenv("DEBUG")
	if debug != "" {
		_, err = strconv.ParseBool(debug)
		if err != nil {
			log.WithFields(log.Fields{
				"stage":    "config",
				"error":    err,
				"variable": "DEBUG",
			}).Error("Failed to parse boolean")
		} else {
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
	deletingHost := os.Getenv("DELETING_HOST")
	if deletingHost != "" {
		Config.Deleting, err = strconv.ParseBool(deletingHost)
		if err != nil {
			log.WithFields(log.Fields{
				"stage":    "config",
				"error":    err,
				"variable": "DELETING_HOST",
			}).Error("Failed to parse boolean")
		}
	} else {
		Config.Deleting = false
	}
	log.WithFields(log.Fields{
		"stage":    "config",
		"variable": "DELETING_HOST",
		"value":    Config.Deleting,
	}).Debug("Configuration set")

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
		}).Panic("Failed unmarshal json event")
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
		}).Panic("Failed unmarshal json event")
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
		resp, err := http.Get("http://ip.clara.net")
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
		"stage": "init",
		"url":   Config.URL,
	}).Debug("Connecting to zabbix API")
	api := zabbix.NewAPI(Config.URL)

	log.WithFields(log.Fields{
		"stage": "init",
		"url":   Config.URL,
		"user":  Config.User,
	}).Debug("Loging to zabbix API")
	_, err = api.Login(Config.User, Config.Password)
	if err != nil {
		log.WithFields(log.Fields{
			"stage": "init",
			"url":   Config.URL,
			"user":  Config.User,
			"error": err,
		}).Panic("Failed to logging on zabbix API")
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
		}).Panic("Failed searching for corresponding zabbix host")
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
		if Config.Deleting {
			log.WithFields(log.Fields{
				"stage":    "set",
				"instance": autoscalingEvent.InstanceID,
				"host":     res[0].HostId,
			}).Debug("Deleting zabbix host")
			_, err := api.CallWithError("host.delete", []string{res[0].HostId})
			if err != nil {
				log.Print("Error deleting host from zabbix api:")
				log.WithFields(log.Fields{
					"stage":    "set",
					"instance": autoscalingEvent.InstanceID,
					"host":     res[0].HostId,
					"error":    err,
				}).Error("Deleting zabbix host")
				return "", err
			}
		} else {
			name := strings.Join([]string{"ZDTP", res[0].Host}, "_")
			description := strings.Join([]string{"Automatically edited from Zabbix Deregister:", time.Now().String(), "This host needs to be purged:", res[0].Host}, "\n")

			log.WithFields(log.Fields{
				"stage":       "set",
				"instance":    autoscalingEvent.InstanceID,
				"host":        res[0].HostId,
				"description": description,
				"cur_name":    res[0].Host,
				"new_name":    name,
				"enabled":     false,
			}).Debug("Updating zabbix host")
			_, err := api.CallWithError("host.update", zabbix.Params{
				"hostid":      res[0].HostId,
				"host":        name,
				"name":        name,
				"description": description,
				"status":      ZabbixHostDisable,
			})
			if err != nil {
				log.WithFields(log.Fields{
					"stage":       "set",
					"instance":    autoscalingEvent.InstanceID,
					"host":        res[0].HostId,
					"description": description,
					"cur_name":    res[0].Host,
					"new_name":    name,
					"enabled":     false,
					"error":       err,
				}).Error("Updating zabbix host")
				return "", err
			}
		}
	}

	log.WithFields(log.Fields{
		"stage":    "end",
		"instance": autoscalingEvent.InstanceID,
		"host":     res[0].HostId,
	}).Debug("Function finished successfully")
	return fmt.Sprintf(autoscalingEvent.InstanceID), nil
}

func main() {
	lambda.Start(HandleRequest)
}
