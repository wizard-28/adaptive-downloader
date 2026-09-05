package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	outputPath  string
	policyType  string
	concurrency int
	chunkSize   int64
	sha256sum   string
	noProgress  bool
)

// downloadCmd represents the download command.
var downloadCmd = &cobra.Command{
	Use:          "download <url>",
	Short:        "Start a download",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if concurrency <= 0 {
			return fmt.Errorf(
				"concurrency must be greater than 0, got %d",
				concurrency,
			)
		}

		if chunkSize <= 0 {
			return fmt.Errorf(
				"chunk size must be greater than 0, got %d",
				chunkSize,
			)
		}

		return runDownload(cmd.Context(), args[0])
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	const defaultN = 2
	const defaultS = 5 * 1024 * 1024

	downloadCmd.Flags().Bool(
		"help",
		false,
		"help for download",
	)

	downloadCmd.Flags().StringVarP(
		&outputPath,
		"output",
		"o",
		"",
		"Path to downloaded file",
	)

	downloadCmd.Flags().StringVarP(
		&policyType,
		"policy",
		"p",
		"fixed",
		"Policy: fixed | linucb",
	)

	downloadCmd.Flags().IntVarP(
		&concurrency,
		"concurrency",
		"n",
		defaultN,
		"Concurrency limit N",
	)

	downloadCmd.Flags().Int64VarP(
		&chunkSize,
		"chunk-size",
		"s",
		defaultS,
		"Chunk size S in bytes",
	)

	downloadCmd.Flags().StringVarP(
		&sha256sum,
		"sha256",
		"h",
		"",
		"Sha256 checksum to verify against",
	)

	downloadCmd.Flags().BoolVar(
		&noProgress,
		"no-progress",
		false,
		"Disable progress bar output",
	)

	downloadCmd.Flags().IntVar(
		&concurrency,
		"initial-concurrency",
		defaultN,
		"Initial concurrency limit N",
	)

	downloadCmd.Flags().Int64Var(
		&chunkSize,
		"initial-chunk-size",
		defaultS,
		"Initial chunk size in bytes",
	)

	downloadCmd.Flags().StringVar(
		&sha256sum,
		"sha256sum",
		"",
		"Sha256 checksum to verify against",
	)

	_ = downloadCmd.Flags().MarkHidden("initial-concurrency")
	_ = downloadCmd.Flags().MarkHidden("initial-chunk-size")
	_ = downloadCmd.Flags().MarkHidden("sha256sum")
}
