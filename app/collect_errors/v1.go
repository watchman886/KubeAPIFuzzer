package collect_errors

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

type ErrorResponseV1 struct {
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
		StatusCode int `json:"StatusCode"`
		Body       struct {
			Message string `json:"message"`
		} `json:"Body"`
	} `json:"Resp"`
}

func ReadErrorResponsesV1(filename string) ([]ErrorResponseV1, error) {
	startTime := time.Now()
	logrus.Infof("Reading error responses from file %s", filename)

	content, err := os.ReadFile(filename)
	logrus.Infof("Read error responses took %fs", time.Since(startTime).Seconds())
	if err != nil {
		return nil, err
	}

	var responses []ErrorResponseV1
	startTime = time.Now()
	logrus.Infof("Unmarshaling error responses from file %s", filename)
	if err := json.Unmarshal(content, &responses); err != nil {
		return nil, err
	}
	logrus.Infof("Unmarshal error responses took %fs", time.Since(startTime).Seconds())
	return responses, nil
}

func ProcessErrorResponseV1(responses []ErrorResponseV1, out io.Writer, onlyMessage bool) error {
	// filter 500 errors
	filtered := filter500ErrorsV1(responses)

	if len(filtered) == 0 {
		_, err := fmt.Fprintln(out, "No status code 500 errors found")
		if err != nil {
			return err
		}
		return nil
	}

	if onlyMessage {
		return outputMessagesV1(filtered, out)
	}
	return outputFullResponsesV1(filtered, out)
}

func filter500ErrorsV1(responses []ErrorResponseV1) []ErrorResponseV1 {
	var filtered []ErrorResponseV1
	for _, resp := range responses {
		if resp.Resp.StatusCode == 500 {
			filtered = append(filtered, resp)
		}
	}
	return filtered
}

func outputMessagesV1(filtered []ErrorResponseV1, out io.Writer) error {
	uniqueMessages := make(map[string]string)

	for _, resp := range filtered {
		if resp.Resp.Body.Message != "" {
			hitPrefix := ""
			for prefix := range prefixSet {
				if len(resp.Resp.Body.Message) >= len(prefix) &&
					resp.Resp.Body.Message[:len(prefix)] == prefix {
					hitPrefix = prefix
					break
				}
			}

			if hitPrefix == "" {
				uniqueMessages[resp.Resp.Body.Message] = resp.Resp.Body.Message
			} else {
				uniqueMessages[hitPrefix] = resp.Resp.Body.Message
			}
		}
	}

	if len(uniqueMessages) == 0 {
		_, err := fmt.Fprintln(out, "No status code 500 error messages found")
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

func outputFullResponsesV1(filtered []ErrorResponseV1, out io.Writer) error {
	logrus.WithFields(logrus.Fields{
		"count": len(filtered),
	}).Info("Found error responses with status code 500:")

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(filtered)
}
