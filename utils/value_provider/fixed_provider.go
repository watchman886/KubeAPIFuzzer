package value_provider

import (
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/sirupsen/logrus"
)

// FixedProvider implements ValueProvider by always returning deterministic values.
type FixedProvider struct {
	resourceKind string
	apiVersion   string
	maxDepth     uint
}

// NewFixedProvider returns a new FixedProvider with default settings.
func NewFixedProvider(kind string, apiVersion string, maxDepth uint) ValueProvider {
	return &FixedProvider{resourceKind: kind, apiVersion: apiVersion, maxDepth: maxDepth}
}

// FillSchema returns a fixed value matching the schema:
// - enum: the first enum value
// - string: a string of length MinLength (or 1) filled with 'x'
// - number: the minimum value (or 0)
// - boolean: always false
// - array: a slice of length MinItems, elements filled recursively
// - object: a map with each property filled recursively
func (p *FixedProvider) FillSchema(ref *openapi3.SchemaRef, propertyName string, depth uint) any {
	if depth > p.maxDepth || ref == nil || ref.Value == nil {
		return nil
	}
	s := ref.Value

	if special, ok := specialObjectValue(p.resourceKind, p.apiVersion, propertyName); ok {
		return special
	}

	typ := schemaType(ref)

	// Handle oneOf/anyOf by always picking the first variant
	if typ == "" {
		if len(s.OneOf) > 0 {
			return p.FillSchema(s.OneOf[0], propertyName, depth+1)
		}
		if len(s.AnyOf) > 0 {
			return p.FillSchema(s.AnyOf[0], propertyName, depth+1)
		}
		if len(s.AllOf) > 0 {
			return p.FillSchema(s.AllOf[0], propertyName, depth+1)
		}

		switch ref.Ref {
		case "#/components/schemas/io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.JSON":
			return fixedValue.Json
		}

		logrus.Warnf("schema type is nil, and met unexpected ref: %s", ref.Ref)
		return fixedValue.String
	}

	// Phase 1: for some special cases, we need to return a fixed value
	const AppName = "my-app"
	switch propertyName {
	case "schedule":
		return "*/5 * * * *"
	case "selector":
		return map[string]any{
			"matchLabels": map[string]string{
				"app": AppName,
			},
		}

	case "labels":
		if p.resourceKind == "ReplicaSet" || p.resourceKind == "DaemonSet" || p.resourceKind == "StatefulSet" || p.resourceKind == "Deployment" {
			return map[string]string{
				"app": AppName,
			}
		}
	}

	// Phase 2: for all other cases, we need to return a value based on the type
	switch typ {
	case "string":
		if formatted, ok := formattedStringValue(s.Format); ok {
			return formatted
		}
		return p.FillString(convertAnySliceToStringSlice(s.Enum), s.MinLength, s.MaxLength)

	case "integer":
		switch s.Format {
		case "int32", "int64":
			return p.FillInteger(s.Min, s.Max)
		case "double":
			return p.FillDouble(s.Min, s.Max)
		default:
			return p.FillInteger(s.Min, s.Max)
		}

	case "boolean":
		return p.FillBoolean()

	case "array":
		minLen := intOrDefault(s.MinItems, 1)
		arr := make([]any, minLen)
		for i := range arr {
			arr[i] = p.FillSchema(s.Items, propertyName, depth+1)
		}
		return arr

	case "object":
		selectedNames := objectPropertyNames(s, false)
		obj := make(map[string]any, len(selectedNames))
		for _, name := range selectedNames {
			if prop, ok := s.Properties[name]; ok {
				obj[name] = p.FillSchema(prop, name, depth+1)
			}
		}
		return obj

	default:
		return nil
	}
}

// FillParameter only fills parameters, does not consider "required" field
func (p *FixedProvider) FillParameter(ref *openapi3.ParameterRef) string {
	if ref == nil || ref.Value == nil {
		return fixedValue.String
	}
	if val, ok := preferredParameterValue(ref.Value.Name); ok {
		return val
	}

	s := ref.Value.Schema
	if s == nil || s.Value == nil {
		return fixedValue.String
	}

	val := p.FillSchema(s, ref.Value.Name, 0)
	return fmt.Sprint(val)
}

// ContentType always selects the first media type.
func (p *FixedProvider) ContentType(content openapi3.Content) string {
	preferred := preferredContentTypes(content)
	if len(preferred) > 0 {
		return preferred[0]
	}
	return ""
}

func (p *FixedProvider) FillString(stringEnum []string, stringMinLength uint64, stringMaxLength *uint64) string {
	return fixedValue.String
}

func (p *FixedProvider) FillDouble(doubleMin *float64, doubleMax *float64) float64 {
	return fixedValue.Double
}

func (p *FixedProvider) FillInteger(min *float64, max *float64) int64 {
	return fixedValue.Int
}

func (p *FixedProvider) FillBoolean() bool {
	return fixedValue.Bool
}
