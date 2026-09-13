package cmd

import (
	"fmt"

	"adaptive-downloader/testserver/server"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	port                    int
	fileSize                string
	concurrencyThreshold    int
	belowThresholdBandwidth string
	aboveThresholdBandwidth string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP test server",
	RunE: func(cmd *cobra.Command, args []string) error {
		fileSizeBytes, err := humanize.ParseBytes(fileSize)
		if err != nil {
			return fmt.Errorf("invalid file size %q: %w", fileSize, err)
		}

		belowThresholdBandwidthBytes, err := humanize.ParseBytes(belowThresholdBandwidth)
		if err != nil {
			return fmt.Errorf("invalid below-threshold bandwidth %q: %w", belowThresholdBandwidth, err)
		}

		aboveThresholdBandwidthBytes, err := humanize.ParseBytes(aboveThresholdBandwidth)
		if err != nil {
			return fmt.Errorf("invalid above-threshold bandwidth %q: %w", aboveThresholdBandwidth, err)
		}

		config := server.Config{
			Port:                    port,
			FileSize:                int64(fileSizeBytes),
			ConcurrencyThreshold:    concurrencyThreshold,
			BelowThresholdBandwidth: int64(belowThresholdBandwidthBytes),
			AboveThresholdBandwidth: int64(aboveThresholdBandwidthBytes),
		}

		if err := config.Validate(); err != nil {
			return err
		}

		srv := server.New(config)

		fmt.Printf(
			"Starting test server on :%d with file size %d bytes\n",
			config.Port,
			config.FileSize,
		)

		return srv.ListenAndServe(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().IntVarP(
		&port,
		"port",
		"p",
		server.DefaultPort,
		"HTTP server port",
	)

	serveCmd.Flags().StringVar(
		&fileSize,
		"size",
		server.DefaultFileSize,
		"Size of the test file in bytes",
	)

	serveCmd.Flags().IntVarP(
		&concurrencyThreshold,
		"concurrency-threshold",
		"N",
		server.DefaultConcurrencyThreshold,
		"Default concurrency threshold before throttling",
	)
	serveCmd.Flags().StringVar(&belowThresholdBandwidth, "below-threshold-bandwidth", server.DefaultBelowThresholdBandwidth, "Aggregate bandwidth at or below the concurrency threshold")
	serveCmd.Flags().StringVar(&aboveThresholdBandwidth, "above-threshold-bandwidth", server.DefaultAboveThresholdBandwidth, "Aggregate bandwidth above the concurrency threshold")
}
