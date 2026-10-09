package run

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func testFeatureFlags() FeatureFlags {
	return FeatureFlags{
		Enable422Repair:        true,
		EnableStateAwareness:   true,
		EnableCoverageFeedback: true,
	}
}

func TestFuzzRequestBodyWildcardContentTypeUsesJSONHeader(t *testing.T) {
	objectType := openapi3.Types{"object"}
	ctx := NewFuzz("ConfigMap", "v1", nil, nil, nil, nil, nil, nil, false, testFeatureFlags())
	body, contentType, err := ctx.FuzzRequestBody(&openapi3.Operation{
		RequestBody: &openapi3.RequestBodyRef{
			Value: &openapi3.RequestBody{
				Content: openapi3.Content{
					"*/*": &openapi3.MediaType{
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: &objectType,
								Properties: openapi3.Schemas{
									"metadata": &openapi3.SchemaRef{
										Value: &openapi3.Schema{Type: &objectType},
									},
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("FuzzRequestBody returned error: %v", err)
	}

	if contentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", contentType)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body is not valid json: %v", err)
	}
	if got == nil {
		t.Fatalf("body = %s, want json object", string(body))
	}
}

func TestShouldPopulateOptionalQuery(t *testing.T) {
	tests := []struct {
		method string
		name   string
		want   bool
	}{
		{method: "POST", name: "fieldValidation", want: true},
		{method: "PUT", name: "fieldManager", want: true},
		{method: "PATCH", name: "fieldValidation", want: false},
		{method: "PATCH", name: "fieldManager", want: false},
		{method: "GET", name: "sendInitialEvents", want: false},
		{method: "GET", name: "watch", want: true},
	}

	for _, tt := range tests {
		if got := shouldPopulateOptionalQuery(tt.method, tt.name); got != tt.want {
			t.Fatalf("shouldPopulateOptionalQuery(%q, %q) = %t, want %t", tt.method, tt.name, got, tt.want)
		}
	}
}

func TestFuzzPathUsesObservedListName(t *testing.T) {
	cache := NewPathValueCache()
	cache.Observe("/api/v1/namespaces/{namespace}/pods", "Pod", []byte(`{
		"kind":"PodList",
		"items":[
			{"metadata":{"name":"real-pod"}}
		]
	}`))

	ctx := NewFuzz("Pod", "v1", cache, nil, nil, nil, nil, nil, false, testFeatureFlags())
	got, err := ctx.FuzzPath("/api/v1/namespaces/{namespace}/pods/{name}/proxy")
	if err != nil {
		t.Fatalf("FuzzPath returned error: %v", err)
	}

	want := "/api/v1/namespaces/default/pods/real-pod/proxy"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestFuzzPathUsesObservedNameForNamedPlaceholder(t *testing.T) {
	cache := NewPathValueCache()
	cache.Observe("/api/v1/namespaces/{namespace}/pods", "Pod", []byte(`{
		"kind":"PodList",
		"items":[
			{"metadata":{"name":"real-pod"}}
		]
	}`))

	ctx := NewFuzz("Pod", "v1", cache, nil, nil, nil, nil, nil, false, testFeatureFlags())
	got, err := ctx.FuzzPath("/api/v1/namespaces/{namespace}/pods/{pod}/proxy")
	if err != nil {
		t.Fatalf("FuzzPath returned error: %v", err)
	}

	want := "/api/v1/namespaces/default/pods/real-pod/proxy"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestFuzzParamsUsesObservedContainerName(t *testing.T) {
	stringType := openapi3.Types{"string"}
	cache := NewPathValueCache()
	cache.Observe("/api/v1/namespaces/{namespace}/pods", "Pod", []byte(`{
		"kind":"PodList",
		"items":[
			{
				"metadata":{"name":"real-pod"},
				"spec":{"containers":[{"name":"main-container"}]}
			}
		]
	}`))

	ctx := NewFuzz("Pod", "v1", cache, nil, nil, nil, nil, nil, false, testFeatureFlags())
	params := ctx.FuzzParams(&openapi3.Operation{
		Parameters: openapi3.Parameters{
			&openapi3.ParameterRef{
				Value: &openapi3.Parameter{
					Name:     "container",
					In:       openapi3.ParameterInQuery,
					Required: true,
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{Type: &stringType},
					},
				},
			},
		},
	}, "GET", "/api/v1/namespaces/{namespace}/pods/{name}/log")

	if params["container"] != "main-container" {
		t.Fatalf("container = %q, want main-container", params["container"])
	}
}
