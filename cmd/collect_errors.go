package cmd

import (
	"fmt"
	"k-fuzz/app/collect_errors"
	"k-fuzz/values"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	outputFile  string
	onlyMessage bool
)

var collectErrorsCmd = &cobra.Command{
	Use:   "collect-errors [file]",
	Short: "Collect error messages from JSON file where StatusCode is 500",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return cmd.Help()
		}

		filename := args[0]

		// prepare output (file or stdout)
		out, cleanup, err := prepareOutput(outputFile)
		if err != nil {
			return err
		}
		defer cleanup()

		// read and parse
		responsesV2, err := collect_errors.ReadErrorResponsesV2(filename)
		if err != nil {
			// try legacy format
			logrus.Warnf("failed to read as v2 format: %v, trying legacy format", err)
			responsesV1, err := collect_errors.ReadErrorResponsesV1(filename)
			if err != nil {
				return fmt.Errorf("failed to read as v1 format: %w", err)
			}

			return collect_errors.ProcessErrorResponseV1(responsesV1, out, onlyMessage)
		}

		return collect_errors.ProcessErrorResponseV2(responsesV2, out, onlyMessage)
	},
}

func init() {
	collectErrorsCmd.Flags().StringVarP(&outputFile, values.OutputFile, "o", "", "Write results to this file (must not already exist)")
	collectErrorsCmd.Flags().BoolVarP(&onlyMessage, values.Brief, "b", false, "Only output unique error messages instead of full responses")
	rootCmd.AddCommand(collectErrorsCmd)
}
