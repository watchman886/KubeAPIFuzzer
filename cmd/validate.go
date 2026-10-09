package cmd

import (
	"context"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate [file]",
	Short: "Validate OpenAPI v3 file spec",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return cmd.Help()
		}

		filename := args[0]
		loader := openapi3.NewLoader()

		logrus.Infof("Loading openapi v3 schema from file %s", filename)
		doc, err := loader.LoadFromFile(filename)
		if err != nil {
			return err
		}
		logrus.Infof("Successfully loaded openapi v3 schema from file %s", filename)

		if err := doc.Validate(context.Background()); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
