package utils

import (
	"bytes"
	"net/http"
	"regexp"

	"github.com/sirupsen/logrus"
)

var sensitiveKeywords = []string{
	"panic",
	"exception",
	"failure",
}

// stackTracePattern is used to match go-style stack trace, e.g. "path/to/file.go:123"
var stackTracePattern = regexp.MustCompile(`\S+\.go:\d+`)

func ScoreResponse(resp *http.Response, respBody []byte) uint {
	totalScore := uint(0)

	if resp == nil {
		return 0
	}

	totalScore += scoreStatusCode(resp.StatusCode)
	totalScore += scoreSensitiveContent(respBody)

	return totalScore

}

func scoreStatusCode(statusCode int) uint {
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		return 1
	} else if statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError {
		return 10
	} else if statusCode >= http.StatusInternalServerError {
		return 100
	} else {
		logrus.Warnf("Unexpected status code: %d", statusCode)
		return 0
	}
}

func scoreSensitiveContent(body []byte) uint {
	sensitiveContentScore := uint(0)

	lowerBody := bytes.ToLower(body)
	// check keyword
	for _, kw := range sensitiveKeywords {
		if bytes.Contains(lowerBody, []byte(kw)) {
			sensitiveContentScore += 50
		}
	}
	// check stack trace pattern
	if stackTracePattern.Match(body) {
		sensitiveContentScore += 100
	}
	return sensitiveContentScore
}
