package value_provider

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestFixedProviderContentTypePrefersJSON(t *testing.T) {
	provider := NewFixedProvider("", "", 8)

	contentType := provider.ContentType(openapi3.Content{
		"application/cbor": &openapi3.MediaType{},
		"application/json": &openapi3.MediaType{},
		"*/*":              &openapi3.MediaType{},
	})

	if contentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", contentType)
	}
}

func TestFixedProviderContentTypeKeepsWildcardOnlyAsFallback(t *testing.T) {
	provider := NewFixedProvider("", "", 8)

	contentType := provider.ContentType(openapi3.Content{
		"*/*": &openapi3.MediaType{},
	})

	if contentType != "*/*" {
		t.Fatalf("content type = %q, want */*", contentType)
	}
}

func TestFixedProviderInfersObjectSchemaWithoutType(t *testing.T) {
	provider := NewFixedProvider("", "", 8)
	stringType := openapi3.Types{"string"}

	got := provider.FillSchema(&openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Properties: openapi3.Schemas{
				"name": &openapi3.SchemaRef{
					Value: &openapi3.Schema{Type: &stringType},
				},
			},
		},
	}, "", 0)

	obj, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("value type = %T, want map[string]any", got)
	}
	if obj["name"] != defaultObjectName("") {
		t.Fatalf("name = %v, want %q", obj["name"], defaultObjectName(""))
	}
}

func TestFixedProviderUsesKubernetesFriendlyParameterDefaults(t *testing.T) {
	provider := NewFixedProvider("", "", 8)
	stringType := openapi3.Types{"string"}

	value := provider.FillParameter(&openapi3.ParameterRef{
		Value: &openapi3.Parameter{
			Name: "dryRun",
			Schema: &openapi3.SchemaRef{
				Value: &openapi3.Schema{Type: &stringType},
			},
		},
	})

	if value != "All" {
		t.Fatalf("dryRun = %q, want All", value)
	}
}
