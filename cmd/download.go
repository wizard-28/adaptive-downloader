package cmd

import (
	"context"
	"fmt"
	"log/slog"

	"adaptive-downloader/pkg/downloader"
	"adaptive-downloader/pkg/policy"

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

var downloadCmd = &cobra.Command{
	Use:          "download <url>",
	Short:        "Start a download",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		config, err := buildDownloadConfig()
		if err != nil {
			return err
		}

		return runDownload(
			cmd.Context(),
			args[0],
			config,
		)
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	downloadCmd.Flags().Bool("help", false, "help for download command")

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
		"Policy: fixed | bandit | rl",
	)

	downloadCmd.Flags().IntVarP(
		&concurrency,
		"concurrency",
		"n",
		downloader.DefaultConcurrency,
		"Initial concurrency limit N",
	)

	downloadCmd.Flags().Int64VarP(
		&chunkSize,
		"chunk-size",
		"s",
		downloader.DefaultChunkSize,
		"Initial chunk size S in bytes",
	)

	downloadCmd.Flags().StringVarP(
		&sha256sum,
		"sha256",
		"h",
		"",
		"SHA-256 checksum to verify against",
	)

	downloadCmd.Flags().BoolVar(
		&noProgress,
		"no-progress",
		false,
		"Disable progress bar output",
	)
}

func buildDownloadConfig() (downloader.Config, error) {
	if concurrency <= 0 {
		return downloader.Config{}, fmt.Errorf(
			"concurrency must be greater than 0, got %d",
			concurrency,
		)
	}

	if chunkSize <= 0 {
		return downloader.Config{}, fmt.Errorf(
			"chunk size must be greater than 0, got %d",
			chunkSize,
		)
	}

	selectedPolicy, err := selectPolicy(
		policyType,
		policy.Action{
			Concurrency: concurrency,
			ChunkSize:   chunkSize,
		},
	)
	if err != nil {
		return downloader.Config{}, fmt.Errorf(
			"initialize %s policy: %w",
			policyType,
			err,
		)
	}

	return downloader.Config{
		OutputPath:  outputPath,
		Policy:      selectedPolicy,
		Concurrency: concurrency,
		ChunkSize:   chunkSize,
		SHA256:      sha256sum,
		NoProgress:  noProgress,
	}, nil
}

func selectPolicy(
	policyType string,
	initialAction policy.Action,
) (policy.Policy, error) {
	if !initialAction.Valid() {
		return nil, fmt.Errorf(
			"invalid initial action: concurrency=%d chunk_size=%d",
			initialAction.Concurrency,
			initialAction.ChunkSize,
		)
	}

	switch policyType {
	case "fixed":
		return policy.NewFixed(initialAction)

	case "bandit":
		return nil, fmt.Errorf(
			"bandit policy is not implemented yet",
		)

	case "rl":
		return nil, fmt.Errorf(
			"rl policy is not implemented yet",
		)

	default:
		return nil, fmt.Errorf(
			"unknown policy %q: expected fixed, bandit, or rl",
			policyType,
		)
	}
}

func runDownload(
	ctx context.Context,
	targetURL string,
	config downloader.Config,
) error {
	slog.Debug(
		"Starting download",
		slog.String("url", targetURL),
		slog.String("policy", policyType),
	)

	d, err := downloader.New(config)
	if err != nil {
		return err
	}

	return d.Download(ctx, targetURL)
}
