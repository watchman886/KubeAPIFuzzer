package run

import (
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// findSchemaRef locates a SchemaRef within a RequestBodyRef based on a dot-delimited path
// It returns the target SchemaRef or an error if the path cannot be resolved
// path needs to be in the format like "foo.bar.baz" or "foo[0].bar.baz".
func findSchemaRef(requestBodyRef *openapi3.RequestBodyRef, path string) (*openapi3.SchemaRef, error) {
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
	schemaRef := media.Schema

	// Split the target path into individual segments
	segments := strings.Split(path, ".")
	var seg string
	for _, seg = range segments {
		// Ensure schema value exists
		if schemaRef.Value == nil {
			return nil, fmt.Errorf("schema value is nil at segment %q", seg)
		}

		// Handle array indexing using string operations: "name[index]"
		if idx := strings.Index(seg, "["); idx != -1 {
			propName := seg[:idx]
			// Lookup the array property
			props := schemaRef.Value.Properties
			nextRef, exists := props[propName]
			if !exists {
				return nil, fmt.Errorf("property %q not found for array indexing", propName)
			}
			schemaRef = nextRef

			// Move into the item schema of the array
			if schemaRef.Value.Items == nil {
				return nil, fmt.Errorf("schema %q is not an array or missing items schema", propName)
			}
			schemaRef = schemaRef.Value.Items
			continue
		}

		// Handle object property lookup
		if props := schemaRef.Value.Properties; props != nil {
			if nextRef, exists := props[seg]; exists {
				schemaRef = nextRef
				continue
			}
		}

		// No matching segment found
		return nil, fmt.Errorf("segment %q not found in schema properties", seg)
	}

	// Return the final located schema reference
	return schemaRef, nil
}

func (c *FuzzCtx) addStatusCodeCount(code int) {
	if _, ok := c.httpStatusCodeCount[code]; ok {
		c.httpStatusCodeCount[code] += 1
	} else {
		c.httpStatusCodeCount[code] = 1
	}
}

func (c *FuzzCtx) randValueByFieldPath(requestBodyRef *openapi3.RequestBodyRef, fieldPath string) (any, error) {
	schemaRef, err := findSchemaRef(requestBodyRef, fieldPath)
	if err != nil {
		return nil, fmt.Errorf("cannot find schema ref %q: %v", fieldPath, err)
	}

	// get the last part of the field name
	parts := strings.Split(fieldPath, ".")
	fieldName := parts[len(parts)-1]

	return c.valueProvider.FillSchema(schemaRef, fieldName, 0), nil
}
