package utils

import (
	"bytes"
	"fmt"
	"io"
	"k-fuzz/values"
	"net/http"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func SendRequest(client *http.Client, data RequestData) (*http.Response, []byte, error) {

	url, err := BuildURL(viper.GetString(values.Host), *data.Path, data.Params)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build URL: %v", err)
	}

	req, _ := http.NewRequest(*data.Method, url, bytes.NewReader(data.BodyBytes))

	if *data.ContentType != "" {
		req.Header.Set("Content-Type", *data.ContentType)
	}

	if viper.GetString(values.BearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+viper.GetString(values.BearerToken))
	}

	resp, err := client.Do(req)

	if err != nil {
		return nil, nil, err
	}

	if resp == nil {
		return nil, nil, fmt.Errorf("response is nil")
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logrus.Errorf("failed to close response body: %v", err)
		}
	}(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, fmt.Errorf("failed to read response body: %v", err)
	}

	logrus.Infof("[%s] %s → %d\n", *data.Method, url, resp.StatusCode)
	// log response body
	if len(respBody) > 0 {
		logrus.Debugf("Response body: %s", string(respBody))
	}

	// log request body
	if data.BodyBytes != nil {
		logrus.Debugf("Request body: %s", string(data.BodyBytes))
	}

	return resp, respBody, nil
}
