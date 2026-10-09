package run

import (
	"encoding/json"
	"fmt"
	"k-fuzz/utils"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

func (c *FuzzCtx) MutateBody(body []byte, requestBodySchema *openapi3.RequestBodyRef) []byte {
	mutatedBody := make([]byte, len(body))
	copy(mutatedBody, body)

	// generate a random number from 1 to 32
	for n := 1 + utils.R.IntN(32); n > 0; n -= 1 {
		// randomly choose a mutation type
		switch utils.R.IntN(4) {
		case 0:
			if nextBody, err := c.mutateRemoveField(mutatedBody); err == nil {
				mutatedBody = nextBody
			}
		case 1, 2:
			if nextBody, err := c.mutateAddField(requestBodySchema, mutatedBody); err == nil {
				mutatedBody = nextBody
			}
		case 3:
			if nextBody, err := c.mutateChangeFieldToRandomType(mutatedBody); err == nil {
				mutatedBody = nextBody
			}
		}
	}

	return mutatedBody
}

// mutateRemoveField removes one field from the request body JSON.
func (c *FuzzCtx) mutateRemoveField(reqBodyJSON []byte) ([]byte, error) {
	// Unmarshal the request body JSON into a generic map
	// FIXME: reqBody may not be in JSON format
	var reqMap map[string]any
	if err := json.Unmarshal(reqBodyJSON, &reqMap); err != nil {
		return nil, fmt.Errorf("unmarshal request body: %w", err)
	}

	leafPaths := utils.CollectLeafPaths(reqMap)
	if len(leafPaths) == 0 {
		return reqBodyJSON, nil
	}

	choice := leafPaths[utils.R.IntN(len(leafPaths))]
	parent := utils.GetLeafParent(reqMap, choice)

	if key, ok := choice[len(choice)-1].(string); ok {
		if m, ok2 := parent.(map[string]any); ok2 {
			delete(m, key)
		}
	}

	mutatedJSON, err := json.MarshalIndent(reqMap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal mutated body: %w", err)
	}
	return mutatedJSON, nil
}

func (c *FuzzCtx) mutateAddField(requestBodyRef *openapi3.RequestBodyRef, reqBodyJSON []byte) ([]byte, error) {
	// Unmarshal the request body JSON into a generic map
	var reqMap map[string]any
	if err := json.Unmarshal(reqBodyJSON, &reqMap); err != nil {
		return nil, fmt.Errorf("unmarshal request body: %w", err)
	}

	// Validate the input RequestBodyRef
	if requestBodyRef == nil || requestBodyRef.Value == nil {
		return nil, fmt.Errorf("requestBodyRef is nil")
	}

	// Select the media type schema
	media, ok := requestBodyRef.Value.Content["application/json"]
	if !ok || media.Schema == nil {
		media, ok = requestBodyRef.Value.Content["*/*"]
		if !ok || media.Schema == nil {
			return nil, fmt.Errorf("no 'application/json' schema defined")
		}
	}

	// Start traversal from the root schema
	rootSchema := media.Schema

	// Traversing reqMap and schema at the same time，collect all object nodes
	type node struct {
		path   []string
		schema *openapi3.SchemaRef
		value  any
	}
	var nodes []node

	var traverse func(path []string, schema *openapi3.SchemaRef, v any)
	traverse = func(path []string, schema *openapi3.SchemaRef, v any) {
		if schema == nil || schema.Value == nil {
			return
		}

		switch schemaKind(schema.Value) {
		case "object":
			m, ok := v.(map[string]any)
			if !ok {
				return
			}
			// collect this object node
			nodes = append(nodes, node{path: path, schema: schema, value: m})
			// traverse the fields that already exist in the map
			for prop, childVal := range m {
				var childSchema *openapi3.SchemaRef
				if ps, found := schema.Value.Properties[prop]; found {
					childSchema = ps
				} else if schema.Value.AdditionalProperties.Has != nil && *schema.Value.AdditionalProperties.Has {
					childSchema = schema.Value.AdditionalProperties.Schema
				}
				if childSchema != nil {
					traverse(append(path, prop), childSchema, childVal)
				}
			}
		case "array":
			arr, ok := v.([]any)
			if !ok || schema.Value.Items == nil {
				return
			}
			for idx, item := range arr {
				traverse(append(path, fmt.Sprintf("%d", idx)), schema.Value.Items, item)
			}
		}
	}
	traverse([]string{}, rootSchema, reqMap)

	if len(nodes) == 0 {
		return nil, fmt.Errorf("no object nodes found to insert into")
	}

	// Collect candidates for insertion points
	var candidates [][]string
	for _, n := range nodes {
		m := n.value.(map[string]any)
		props := n.schema.Value.Properties

		// same level properties that are defined in the schema but not yet in the map
		for prop := range props {
			if _, exists := m[prop]; !exists {
				candidates = append(candidates, append(append([]string{}, n.path...), prop))
			}
		}

		// child properties that are defined in the schema but not yet in the map
		for prop, childVal := range m {
			if childSchema, found := props[prop]; found &&
				childSchema != nil &&
				schemaKind(childSchema.Value) == "object" {
				if childMap, ok := childVal.(map[string]any); ok {
					for sub := range childSchema.Value.Properties {
						if _, exists := childMap[sub]; !exists {
							candidates = append(candidates,
								append(append(append([]string{}, n.path...), prop), sub))
						}
					}
				}
			}
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no valid insertion points according to schema")
	}

	// Randomly select one candidate path, join it to form fieldName, and generate a value
	pick := candidates[utils.R.IntN(len(candidates))]
	fieldName := strings.Join(pick, ".")

	newVal, err := c.randValueByFieldPath(requestBodyRef, fieldName)
	if err != nil {
		return nil, fmt.Errorf("generate random value for field %s: %w", fieldName, err)
	}

	err = setField(reqMap, fieldName, newVal)
	if err != nil {
		return nil, fmt.Errorf("set field %s: %w", fieldName, err)
	}

	mutatedJSON, err := json.MarshalIndent(reqMap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal mutated body: %w", err)
	}
	return mutatedJSON, nil
}

func schemaKind(schema *openapi3.Schema) string {
	if schema == nil {
		return ""
	}

	if typ := utils.GetAType(schema.Type); typ != "" {
		return typ
	}

	if len(schema.Properties) > 0 || schema.AdditionalProperties.Schema != nil ||
		(schema.AdditionalProperties.Has != nil && *schema.AdditionalProperties.Has) {
		return "object"
	}

	if schema.Items != nil {
		return "array"
	}

	return ""
}

func (c *FuzzCtx) mutateChangeFieldToRandomType(reqBodyJSON []byte) ([]byte, error) {
	var reqMap map[string]any
	if err := json.Unmarshal(reqBodyJSON, &reqMap); err != nil {
		return nil, fmt.Errorf("unmarshal request body: %w", err)
	}

	paths := utils.CollectLeafPaths(reqMap)
	if len(paths) == 0 {
		return reqBodyJSON, nil
	}

	chosen := paths[utils.R.IntN(len(paths))]
	parent := utils.GetLeafParent(reqMap, chosen)
	leafKey := chosen[len(chosen)-1]

	switch p := parent.(type) {
	case map[string]any:
		if k, ok := leafKey.(string); ok {
			p[k] = c.randFillNode()
		} else {
			return nil, fmt.Errorf("leaf key is not a string: %v", leafKey)
		}
	case []any:
		if k, ok := leafKey.(int); ok {
			p[k] = c.randFillNode()
		} else {
			return nil, fmt.Errorf("leaf key is not an int: %v", leafKey)
		}
	default:
		// this means something is wrong
		c.unexpectedLogger.Errorf("unhandled type %T, requestJson: %s", parent, string(reqBodyJSON))
	}

	return json.Marshal(reqMap)
}

func (c *FuzzCtx) randFillNode() any {
	switch utils.R.IntN(4) {
	case 0:
		return c.valueProvider.FillString([]string{}, 0, nil)
	case 1:
		return c.valueProvider.FillDouble(nil, nil)
	case 2:
		return c.valueProvider.FillInteger(nil, nil)
	case 3:
		return c.valueProvider.FillBoolean()
	}

	// unreachable
	return nil
}
