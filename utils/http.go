package utils

import (
	"bytes"
	"crypto/tls"
	"io"
	"k-fuzz/values"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jtacoma/uritemplates"
	"github.com/spf13/viper"
)

func BuildURL(base, p string, params map[string]string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}

	u.Path = path.Join(u.Path, p)

	// concat query
	q := u.Query()
	for name, val := range params {
		q.Set(name, val)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func ExpandPathRFC6570(tpl string, params map[string]any) (string, error) {
	tmpl, err := uritemplates.Parse(tpl)
	if err != nil {
		return "", err
	}
	expanded, err := tmpl.Expand(params)
	if err != nil {
		return "", err
	}
	return expanded, nil
}

// HasWatchSegment returns true if the path of the given URL
// contains an independent segment equal to "watch".
func HasWatchSegment(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		// If the URL is invalid, treat it as not matching.
		return false
	}
	// Split the path into segments by "/"
	parts := strings.Split(u.Path, "/")
	for _, seg := range parts {
		if seg == "watch" {
			return true
		}
	}
	return false
}

type RoundTripFunc func(req *http.Request) (*http.Response, error)

func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func NewTestClient(fn RoundTripFunc) *http.Client {
	return &http.Client{
		Transport: fn,
	}
}

func NewClient() *http.Client {
	if viper.GetBool(values.DryRun) {
		return newMockClient()
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: viper.GetBool(values.SkipTlsVerify),
		},
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   time.Duration(viper.GetInt64(values.Timeout)) * time.Second,
	}

	return client
}

func newMockClient() *http.Client {
	fakeBody := `{"status":"ok"}`
	client := NewTestClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(fakeBody)),
			Header:     make(http.Header),
		}, nil
	})

	return client
}
