package run

import (
	"encoding/json"
	"fmt"
	"k-fuzz/utils"
	vp "k-fuzz/utils/value_provider"
	"k-fuzz/values"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Coverage struct {
	Dir                      string
	previousCoverageFileName string
}

type FuzzCtx struct {
	resourceKind        string
	apiVersion          string
	features            FeatureFlags
	pathValueCache      *PathValueCache
	valueProvider       vp.ValueProvider
	httpStatusCodeCount map[int]int
	coverage            *Coverage
	seedCorpus          *utils.RequestQueue
	requestLogger       *logrus.Logger
	unexpectedLogger    *logrus.Logger
}

var pathParamPattern = regexp.MustCompile(`\{([^}/]+)\}`)

func NewFuzz(kind string, apiVersion string, pathValueCache *PathValueCache, httpStatusCodeCount map[int]int, coverage *Coverage, seedCorpus *utils.RequestQueue, requestLogger *logrus.Logger, unexpectedLogger *logrus.Logger, useRandomProvider bool, features FeatureFlags) FuzzCtx {
	var valueProvider vp.ValueProvider

	if useRandomProvider || viper.GetBool(values.RandomFill) {
		valueProvider = vp.NewRandomProvider(kind, apiVersion, viper.GetUint(values.MaxSchemaDepth))
	} else {
		valueProvider = vp.NewFixedProvider(kind, apiVersion, viper.GetUint(values.MaxSchemaDepth))
	}

	ctx := FuzzCtx{
		resourceKind:        kind,
		apiVersion:          apiVersion,
		features:            features,
		pathValueCache:      pathValueCache,
		valueProvider:       valueProvider,
		coverage:            coverage,
		seedCorpus:          seedCorpus,
		httpStatusCodeCount: httpStatusCodeCount,
		requestLogger:       requestLogger,
		unexpectedLogger:    unexpectedLogger,
	}

	return ctx
}

func (c *FuzzCtx) FuzzPath(tpl string) (string, error) {
	params := make(map[string]any)
	for _, match := range pathParamPattern.FindAllStringSubmatch(tpl, -1) {
		if len(match) < 2 {
			continue
		}
		name := match[1]
		params[name] = c.pathParamValue(tpl, name)
	}

	filledPath, err := utils.ExpandPathRFC6570(tpl, params)
	if err != nil {
		logrus.Errorf("failed to expand path %s with params %v: %v", tpl, params, err)
		return "", err
	}

	return filledPath, nil

}

func (c *FuzzCtx) pathParamValue(tpl string, name string) any {
	switch name {
	case "namespace":
		return "default"
	case "container":
		return utils.StringFixed
	}

	if c.features.EnableStateAwareness && c.pathValueCache != nil {
		if value, ok := c.pathValueCache.Lookup(tpl, name, c.resourceKind); ok {
			return value
		}
	}

	switch {
	case strings.Contains(tpl, "/services/{name}"):
		return "kubernetes"
	case strings.Contains(tpl, "/serviceaccounts/{name}"):
		return "default"
	case strings.Contains(tpl, "/namespaces/{name}"):
		return "default"
	case strings.Contains(tpl, "/configmaps/{name}"):
		return "kube-root-ca.crt"
	case strings.Contains(tpl, "/endpoints/{name}"):
		return "kubernetes"
	default:
		return utils.StringFixed
	}
}

// FuzzParams returns the fuzz result of the request params
func (c *FuzzCtx) FuzzParams(op *openapi3.Operation, method string, pathTemplate string) map[string]string {
	params := make(map[string]string)

	for i, pRef := range op.Parameters {
		if pRef == nil || pRef.Value == nil {
			logrus.Warnf("skipping nil parameter reference at index %d", i)
			continue
		}
		p := pRef.Value

		if p.In != openapi3.ParameterInQuery {
			continue
		}

		if !p.Required && !shouldPopulateOptionalQuery(method, p.Name) {
			continue
		}

		if p.Schema == nil || p.Schema.Value == nil {
			logrus.Warnf("parameter %q has no Schema; skipping", p.Name)
			continue
		}

		if c.features.EnableStateAwareness && c.pathValueCache != nil {
			if value, ok := c.pathValueCache.Lookup(pathTemplate, p.Name, c.resourceKind); ok {
				params[p.Name] = value
				continue
			}
		}

		val := c.valueProvider.FillSchema(p.Schema, p.Name, 0)
		params[p.Name] = fmt.Sprint(val)
	}

	return params
}

func shouldPopulateOptionalQuery(method string, name string) bool {
	switch name {
	case "dryRun":
		return method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
	case "fieldManager", "fieldValidation":
		return method == "POST" || method == "PUT"
	case "watch", "allowWatchBookmarks":
		return method == "GET"
	default:
		return false
	}
}

// FuzzRequestBody picks a random requestBody content type from the spec,
// generates a random object for its schema, serializes it appropriately,
// and returns the bytes plus the Content-Type header value.
func (c *FuzzCtx) FuzzRequestBody(op *openapi3.Operation) (body []byte, contentType string, err error) {
	if op.RequestBody == nil {
		return nil, "", nil
	}

	contentType = c.valueProvider.ContentType(op.RequestBody.Value.Content)
	media := op.RequestBody.Value.Content.Get(contentType)

	if media == nil || media.Schema == nil {
		opJson, _ := json.MarshalIndent(op, "", "  ")
		return nil, "", fmt.Errorf("no schema for content type %q, request body: %v", contentType, string(opJson))
	}

	serializedContentType := contentType
	if serializedContentType == "" || serializedContentType == "*/*" {
		serializedContentType = "application/json"
	}

	// generate a Go value for that schema
	obj := c.valueProvider.FillSchema(media.Schema, "", 0)
	obj = utils.PruneNil(obj)

	if obj == nil {
		obj = make(map[string]any)
	}

	// serialize based on contentType
	switch {
	case contentType == "*/*" || strings.Contains(contentType, "json"):
		body, err = json.Marshal(obj)

	case strings.Contains(contentType, "yaml"):
		// body, err = yaml.Marshal(obj)
		logrus.Errorf("We don't support yaml yet")
		return nil, "", fmt.Errorf("yaml not supported yet")

	case strings.Contains(contentType, "cbor"):
		// body, err = cbor.Marshal(obj)
		logrus.Errorf("We don't support cbor yet")
		return nil, "", fmt.Errorf("cbor not supported yet")

	default:
		return nil, "", fmt.Errorf("unsupported request content type %q", contentType)
	}
	return body, serializedContentType, err
}
