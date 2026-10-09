package collect_errors

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

type ErrorResponseV2 struct {
	Req struct {
		Data struct {
			Method      string `json:"Method"`
			Path        string `json:"Path"`
			Params      any    `json:"Params"`
			BodyBytes   any    `json:"BodyBytes"`
			ContentType string `json:"ContentType"`
		} `json:"Data"`
	} `json:"Req"`
	Resp struct {
		StatusCode int    `json:"StatusCode"`
		Body       []byte `json:"Body"`
	} `json:"Resp"`
}

type BodyMessageV2 struct {
	Message string `json:"message"`
}

func ReadErrorResponsesV2(filename string) ([]ErrorResponseV2, error) {
	startTime := time.Now()
	logrus.Infof("Reading error responses from file %s", filename)

	content, err := os.ReadFile(filename)
	logrus.Infof("Read error responses took %fs", time.Since(startTime).Seconds())
	if err != nil {
		return nil, err
	}

	var responses []ErrorResponseV2
	startTime = time.Now()
	logrus.Infof("Unmarshaling error responses from file %s", filename)
	if err := json.Unmarshal(content, &responses); err != nil {
		return nil, err
	}
	logrus.Infof("Unmarshal error responses took %fs", time.Since(startTime).Seconds())
	return responses, nil
}

func ProcessErrorResponseV2(responses []ErrorResponseV2, out io.Writer, onlyMessage bool) error {
	// filter 5xx errors
	filtered := filter5xxErrorsV2(responses)

	if len(filtered) == 0 {
		_, err := fmt.Fprintln(out, "No status code 5xx errors found")
		if err != nil {
			return err
		}
		return nil
	}

	if onlyMessage {
		return outputMessagesV2(filtered, out)
	}
	return outputFullResponsesV2(filtered, out)
}

func filter5xxErrorsV2(responses []ErrorResponseV2) []ErrorResponseV2 {
	var filtered []ErrorResponseV2
	for _, resp := range responses {
		if resp.Resp.StatusCode >= http.StatusInternalServerError && resp.Resp.StatusCode < 600 {
			filtered = append(filtered, resp)
		}
	}
	return filtered
}

func outputMessagesV2(filtered []ErrorResponseV2, out io.Writer) error {
	uniqueMessages := make(map[string]string)

	for _, resp := range filtered {
		var bodyMessage BodyMessageV2

		// unmarshall resp.Resp.Body
		if err := json.Unmarshal(resp.Resp.Body, &bodyMessage); err != nil {
			logrus.Warnf("failed to unmarshal response body: %v", err)
			logrus.Warnf("response body length: %v", len(resp.Resp.Body))
			logrus.Warnf("request: %+v", resp.Req)
			continue
		}

		if bodyMessage.Message != "" {
			hitPrefix := getHitPrefix(bodyMessage.Message)

			if hitPrefix == "" {
				uniqueMessages[bodyMessage.Message] = bodyMessage.Message
			} else {
				uniqueMessages[hitPrefix] = bodyMessage.Message
			}
		}
	}

	if len(uniqueMessages) == 0 {
		_, err := fmt.Fprintln(out, "No status code 5xx error messages found")
		if err != nil {
			return err
		}
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"count": len(uniqueMessages),
	}).Info("Found unique error messages:")

	for _, v := range uniqueMessages {
		_, err := fmt.Fprintln(out, v)
		if err != nil {
			return err
		}
	}
	return nil
}

func outputFullResponsesV2(filtered []ErrorResponseV2, out io.Writer) error {
	// convert v2 to v1
	var converted []ErrorResponseV1
	for _, resp := range filtered {
		v1, err := convertV2ToOutput(resp)
		if err != nil {
			logrus.Warnf("failed to convert v2 to output: %v", err)
			continue
		}
		converted = append(converted, v1)
	}

	logrus.WithFields(logrus.Fields{
		"count": len(converted),
	}).Info("Found error responses with status code 5xx:")

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(converted)
}
