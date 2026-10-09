package run

import (
	"k-fuzz/utils"
	"net/http"

	"github.com/sirupsen/logrus"
)

func (c *FuzzCtx) Request(client *http.Client, schema utils.RequestSchema, data utils.RequestData) (*http.Response, []byte, error) {

	// print all request info into requestLog
	c.requestLogger.WithFields(logrus.Fields{
		"method":       *data.Method,
		"path":         *data.Path,
		"params":       data.Params,
		"content_type": *data.ContentType,
		"body_bytes":   data.BodyBytes,
	}).Info()

	resp, respBody, err := utils.SendRequest(client, data)
	if resp != nil {
		c.addStatusCodeCount(resp.StatusCode)
		if c.features.EnableStateAwareness &&
			http.StatusOK <= resp.StatusCode &&
			resp.StatusCode < http.StatusMultipleChoices &&
			data.PathTemplate != nil {
			requestKind := ""
			if data.Kind != nil {
				requestKind = *data.Kind
			}
			c.pathValueCache.Observe(*data.PathTemplate, requestKind, respBody)
		}
	}

	if !c.features.EnableCoverageFeedback {
		if c.seedCorpus != nil {
			c.seedCorpus.Enqueue(utils.Request{
				Data:   &data,
				Schema: &schema,
			})
		}
		return resp, respBody, err
	}

	hasNewPath, compareErr := c.CompareCoverages()
	if compareErr != nil {
		logrus.Errorf("failed to compare coverages: %v", compareErr)
	} else if hasNewPath && c.seedCorpus != nil {
		c.seedCorpus.Enqueue(utils.Request{
			Data:   &data,
			Schema: &schema,
		})
	}

	return resp, respBody, err
}
