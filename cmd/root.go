package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var sharedParams struct {
	showVersion   bool
	outPutFile    string
	host          string
	bearerToken   string
	skipTlsVerify bool
}

var rootCmd = &cobra.Command{
	Use:   "kf",
	Short: "k-fuzz is a Kubernetes fuzzer",
	Long:  `k-fuzz is a Kubernetes fuzzer that helps you find bugs in your Kubernetes cluster.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if sharedParams.showVersion {
			fmt.Println("alpha")
			return nil
		}

		return cmd.Help()
	},
}

func init() {
	rootCmd.Flags().BoolVar(&sharedParams.showVersion, "version", false, "show current version")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
