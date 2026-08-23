package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	outputPath string
	policyType string
	initialN   int
	initialS   int64
	sha256sum  string
)

// downloadCmd represents the download command
var downloadCmd = &cobra.Command{
	Use:   "download <url>",
	Short: "Start a download",
	Args:  cobra.ExactArgs(1),
	Long:  `Download a file. For example: download https://url/to/file`,
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]
		fmt.Printf("download called %v\n", url)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	const defaultN = 2
	const defaultS = 5 * 1024 * 1024

	downloadCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Path to downloaded file")
	downloadCmd.Flags().StringVarP(&policyType, "policy", "p", "linucb", "Policy: linucb | fixed")
	downloadCmd.Flags().IntVarP(&initialN, "initial-concurrency", "n", defaultN, "Initial concurrency limit N")
	downloadCmd.Flags().Int64VarP(&initialS, "initial-chunk-size", "s", defaultS, "Initial chunk size in bytes")
	downloadCmd.Flags().StringVarP(&sha256sum, "sha256sum", "h", "", "Sha256 checksum to verify against")
}
