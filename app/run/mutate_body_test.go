package run

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	vp "k-fuzz/utils/value_provider"
)

func TestMutateAddFieldHandlesSchemaWithoutType(t *testing.T) {
	stringType := openapi3.Types{"string"}
	requestBody := &openapi3.RequestBodyRef{
		Value: &openapi3.RequestBody{
			Content: openapi3.Content{
				"application/json": &openapi3.MediaType{
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{
							Properties: openapi3.Schemas{
								"name": &openapi3.SchemaRef{
									Value: &openapi3.Schema{
										Type: &stringType,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	ctx := FuzzCtx{
		valueProvider: vp.NewFixedProvider("", "", 20),
	}

	mutated, err := ctx.mutateAddField(requestBody, []byte(`{}`))
	if err != nil {
		t.Fatalf("mutateAddField returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(mutated, &got); err != nil {
		t.Fatalf("mutated body is not valid JSON: %v", err)
	}
	if _, ok := got["name"]; !ok {
		t.Fatalf("mutated body = %s, want inserted name field", string(mutated))
	}
}
