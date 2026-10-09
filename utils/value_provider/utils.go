package value_provider

import (
	"k-fuzz/utils"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

var fixedValue = struct {
	Int    int64
	Double float64
	String string
	Bool   bool
	Json   string
}{
	Int:    1,
	Double: 1.0,
	String: utils.StringFixed,
	Bool:   true,
	Json:   "{\"kind\":\"fixed json\"}",
}

// ValueProvider defines methods for populating an OpenAPI schema
// and selecting a Content-Type.
type ValueProvider interface {
	FillSchema(ref *openapi3.SchemaRef, propertyName string, depth uint) any
	FillParameter(ref *openapi3.ParameterRef) string
	ContentType(content openapi3.Content) string

	FillString(stringEnum []string, stringMinLength uint64, stringMaxLength *uint64) string
	FillDouble(doubleMin *float64, doubleMax *float64) float64
	FillInteger(min *float64, max *float64) int64
	FillBoolean() bool
}

// intOrDefault returns the integer value of val if non-nil, otherwise def.
func intOrDefault[T interface{ uint64 | *uint64 }](val T, def int) int {
	switch v := any(val).(type) {
	case *uint64:
		if v != nil {
			return int(*v)
		}
		return def
	case uint64:
		if v != 0 {
			return int(v)
		}
		return def
	}

	panic("unreachable")
}

// float64OrDefault returns the float64 value of val if non-nil, otherwise def.
func float64OrDefault(val *float64, def float64) float64 {
	if val != nil {
		return *val
	}
	return def
}

func convertAnySliceToStringSlice(arr []any) []string {
	stringArr := make([]string, 0, len(arr))
	for _, v := range arr {
		if str, ok := v.(string); ok {
			stringArr = append(stringArr, str)
		}
	}

	return stringArr
}

func schemaType(ref *openapi3.SchemaRef) string {
	if ref == nil || ref.Value == nil {
		return ""
	}

	if typ := utils.GetAType(ref.Value.Type); typ != "" {
		return typ
	}

	if len(ref.Value.Properties) > 0 || ref.Value.AdditionalProperties.Schema != nil ||
		(ref.Value.AdditionalProperties.Has != nil && *ref.Value.AdditionalProperties.Has) {
		return "object"
	}

	if ref.Value.Items != nil {
		return "array"
	}

	return ""
}

func preferredContentTypes(content openapi3.Content) []string {
	jsonCandidates := make([]string, 0, len(content))
	fallbackCandidates := make([]string, 0, len(content))
	hasWildcard := false

	for mt := range content {
		switch {
		case mt == "*/*":
			hasWildcard = true
		case strings.Contains(mt, "json") || strings.HasSuffix(mt, "+json"):
			jsonCandidates = append(jsonCandidates, mt)
		default:
			fallbackCandidates = append(fallbackCandidates, mt)
		}
	}

	sort.Slice(jsonCandidates, func(i, j int) bool {
		return contentTypePriority(jsonCandidates[i]) < contentTypePriority(jsonCandidates[j])
	})
	sort.Slice(fallbackCandidates, func(i, j int) bool {
		return contentTypePriority(fallbackCandidates[i]) < contentTypePriority(fallbackCandidates[j])
	})

	candidates := append(jsonCandidates, fallbackCandidates...)
	if len(candidates) == 0 && hasWildcard {
		return []string{"*/*"}
	}

	return candidates
}

func contentTypePriority(mt string) int {
	switch mt {
	case "application/json":
		return 0
	case "application/merge-patch+json":
		return 1
	case "application/strategic-merge-patch+json":
		return 2
	case "*/*":
		return 3
	default:
		if strings.HasSuffix(mt, "+json") {
			return 4
		}
		if strings.Contains(mt, "json") {
			return 5
		}
		return 100
	}
}

func preferredParameterValue(name string) (string, bool) {
	switch name {
	case "dryRun":
		return "All", true
	case "fieldManager":
		return "k-fuzz", true
	case "fieldValidation":
		return "Warn", true
	case "pretty", "watch", "allowWatchBookmarks":
		return "false", true
	default:
		return "", false
	}
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

func specialObjectValue(resourceKind string, apiVersion string, propertyName string) (any, bool) {
	switch propertyName {
	case "kind":
		if resourceKind != "" {
			return resourceKind, true
		}
	case "apiVersion":
		if apiVersion != "" {
			return apiVersion, true
		}
	case "metadata":
		return map[string]any{
			"name": defaultObjectName(resourceKind),
			"labels": map[string]string{
				"app": defaultObjectName(resourceKind),
			},
		}, true
	case "name":
		return defaultObjectName(resourceKind), true
	case "generateName":
		return defaultObjectName(resourceKind) + "-", true
	case "namespace", "serviceAccountName":
		return "default", true
	case "eventTime":
		return time.Now().UTC().Format(time.RFC3339Nano), true
	default:
		return nil, false
	}
	return nil, false
}

func formattedStringValue(format string) (string, bool) {
	switch format {
	case "date-time":
		return time.Now().UTC().Format(time.RFC3339Nano), true
	case "date":
		return time.Now().UTC().Format(time.DateOnly), true
	case "duration":
		return "1s", true
	case "byte":
		return "Zml4ZWQ=", true
	case "uuid":
		return "11111111-1111-1111-1111-111111111111", true
	default:
		return "", false
	}
}

func defaultObjectName(resourceKind string) string {
	var b strings.Builder
	b.WriteString("kfuzz")

	for _, r := range strings.ToLower(resourceKind) {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if r == '-' {
			b.WriteRune(r)
		}
	}

	if b.Len() == len("kfuzz") {
		b.WriteString("object")
	}

	return b.String()
}

func objectPropertyNames(schema *openapi3.Schema, random bool) []string {
	if schema == nil || len(schema.Properties) == 0 {
		return nil
	}

	selected := make([]string, 0, len(schema.Required)+2)
	selectedSet := make(map[string]bool, len(schema.Required)+2)

	for _, name := range schema.Required {
		if _, ok := schema.Properties[name]; ok {
			selected = append(selected, name)
			selectedSet[name] = true
		}
	}

	for _, preferred := range []string{"metadata", "spec", "selector", "template"} {
		if _, ok := schema.Properties[preferred]; ok && !selectedSet[preferred] {
			selected = append(selected, preferred)
			selectedSet[preferred] = true
			if !random && len(selected) >= 2 {
				return selected
			}
		}
	}

	if len(selected) == 0 {
		keys := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		selected = append(selected, keys[0])
		selectedSet[keys[0]] = true
	}

	if random {
		keys := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			if !selectedSet[name] {
				keys = append(keys, name)
			}
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			selected = append(selected, keys[utils.R.IntN(len(keys))])
		}
	}

	return selected
}
