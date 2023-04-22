package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type LokiMessage struct {
	Timestamp int64 // nanosecond
	Message   string
}

const (
	LokiURL = "http://domain:3100/loki/"
)

func main() {
	lambda.Start(func(ctx context.Context, event events.CloudwatchLogsEvent) (string, error) {
		err := Run(ctx, event)
		if err != nil {
			return "ERROR", fmt.Errorf("ERROR: %+v", err)
		}

		return "OK", nil
	})
}

// Run execute the downstream process
func Run(ctx context.Context, event events.CloudwatchLogsEvent) error {
	fmt.Println("Loki shipper start.....")
	// Decode
	decodedData, _ := event.AWSLogs.Parse()
	err := processLog(ctx, decodedData)
	if err != nil {
		return err
	}

	return nil
}

func processLog(ctx context.Context, event events.CloudwatchLogsData) error {
	if len(event.LogEvents) == 0 {
		return fmt.Errorf("LogEvents is empty")
	}
	fmt.Println("log group: ", event.LogGroup)

	var (
		label, labelValue string
		lokiMessages      []*LokiMessage
	)

	for _, le := range event.LogEvents {
		label, labelValue = getLabelFromLogGroupName(event.LogGroup)
		if json.Valid([]byte(le.Message)) || strings.Contains(le.Message, "panic") {
			lokiMessages = append(lokiMessages, &LokiMessage{
				Timestamp: le.Timestamp * 1000000,
				Message:   le.Message,
			})
		}
	}

	if len(lokiMessages) > 0 {
		if err := pushLoki(ctx, lokiMessages, label, labelValue); err != nil {
			fmt.Println("failed to push log to Loki: ", err)
		}
	}
	return nil
}

func getLabelFromLogGroupName(lg string) (svcName, funcName string) {
	if strings.Contains(lg, "/") {
		splittedLg := strings.Split(lg, "/")
		funcName = splittedLg[len(splittedLg)-1]
		if strings.Contains(funcName, "-") {
			splitedName := strings.Split(funcName, "-")
			svcName = splitedName[0]
		}
	}
	return
}

func pushLoki(ctx context.Context, messages []*LokiMessage, label, labelValue string) error {
	lokiURL := LokiURL + "api/v1/push"

	//Encode the data
	var values []*[]string
	for _, msg := range messages {
		v := []string{
			fmt.Sprintf("%v", msg.Timestamp), msg.Message,
		}
		values = append(values, &v)
	}
	postBody, _ := json.Marshal(map[string]interface{}{
		"streams": []map[string]interface{}{
			{
				"stream": map[string]interface{}{
					label: labelValue,
				},
				"values": values,
			},
		},
	})

	requestBody := bytes.NewBuffer(postBody)
	resp, err := http.Post(lokiURL, "application/json", requestBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
