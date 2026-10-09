package cmd

import (
	"context"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var checkOpenAPIObjectCmd = &cobra.Command{
	Use:   "check-openapi-object [file]",
	Short: "Check OpenAPI v3 spec for objects without defined properties",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filename := args[0]
		loader := &openapi3.Loader{Context: context.Background(), IsExternalRefsAllowed: true}

		doc, err := loader.LoadFromFile(filename)
		if err != nil {
			return err
		}
		if err := doc.Validate(context.Background()); err != nil {
			// Not required, but ensures $ref has been properly resolved
			logrus.Warnf("Warning: OpenAPI document validation issue: %v", err)
		}

		visited := map[*openapi3.SchemaRef]bool{}
		for name, schemaRef := range doc.Components.Schemas {
			checkSchema(schemaRef, "#/components/schemas/"+name, visited)
		}

		for path, item := range doc.Paths.Map() {
			for method, op := range item.Operations() {
				if op.RequestBody != nil {
					for mt, media := range op.RequestBody.Value.Content {
						checkSchema(media.Schema, fmt.Sprintf("%s %s requestBody[%s]", method, path, mt), visited)
					}
				}
				for code, resp := range op.Responses.Map() {
					for mt, media := range resp.Value.Content {
						checkSchema(media.Schema, fmt.Sprintf("%s %s response[%s][%s]", method, path, code, mt), visited)
					}
				}
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(checkOpenAPIObjectCmd)
}

func checkSchema(ref *openapi3.SchemaRef, path string, visited map[*openapi3.SchemaRef]bool) {
	if ref == nil || visited[ref] {
		return
	}
	visited[ref] = true

	s := ref.Value
	if s == nil {
		return
	}

	// Check target condition
	// Type is *Types (pointer to []string), needs proper checking
	//if s.Type != nil && s.Type.Includes("object") && s.Items != nil && (s.Items.Value == nil || len(s.Items.Value.Properties) == 0) {
	//	fmt.Printf("Issue found: %s defines type=object and items is empty\n", path)
	//}

	if s.Type != nil && s.Type.Includes("object") && s.Properties == nil && s.AdditionalProperties.Schema == nil {
		fmt.Printf("Issue found: %s defines type=object and properties is empty\n", path)
	}

	// Recursively check sub-structures
	if s.Properties != nil {
		for name, p := range s.Properties {
			checkSchema(p, path+"/properties/"+name, visited)
		}
	}
	if s.Items != nil {
		checkSchema(s.Items, path+"/items", visited)
	}
	if s.AdditionalProperties.Has != nil && s.AdditionalProperties.Schema != nil {
		checkSchema(s.AdditionalProperties.Schema, path+"/additionalProperties", visited)
	}
	if s.AllOf != nil {
		for i, sub := range s.AllOf {
			checkSchema(sub, fmt.Sprintf("%s/allOf[%d]", path, i), visited)
		}
	}
	if s.OneOf != nil {
		for i, sub := range s.OneOf {
			checkSchema(sub, fmt.Sprintf("%s/oneOf[%d]", path, i), visited)
		}
	}
	if s.AnyOf != nil {
		for i, sub := range s.AnyOf {
			checkSchema(sub, fmt.Sprintf("%s/anyOf[%d]", path, i), visited)
		}
	}
}
