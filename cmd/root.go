package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	verbose   bool
	logLevel  string
	logFormat string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "adaptive-downloader",
	Short: "Joint adaptive HTTP download manager using contextual bandits for dynamic control",
	Long: `Adaptive HTTP File Download Manager (Joint Adaptive Control)

This tool performs multi-threaded HTTP byte-range downloads while dynamically 
tuning request concurrency (N) and future byte-range chunk sizes (S) in real time. 
It leverages a Go-native LinUCB Contextual Bandit to respond to changing network 
conditions—including latency spikes, packet loss, and server-side per-connection throttling.

Complete Examples:
  # Start an adaptive download with default LinUCB policy and live dashboard
  adaptive-downloader download http://example.com/file.iso -o ./file.iso

  # Run a fixed baseline download (Fixed N=4, S=5MB) for ablation testing
  adaptive-downloader download http://example.com/file.iso -p fixed -n 4 -s 5242880

  # Execute an automated evaluation benchmark suite
  adaptive-downloader benchmark --url http://example.com/file.iso`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return setupLogger()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// GetRootCmd returns the base rootCmd for programmatic testing.
func GetRootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging (shorthand for --log-level=debug")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "text", "Log output format (text, json)")
}

func setupLogger() error {
	if !verbose && logLevel == "" {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return nil
	}

	var level slog.Level
	if verbose {
		level = slog.LevelDebug
	} else {
		switch strings.ToLower(logLevel) {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError

		default:
			return fmt.Errorf("invalid log level: %q", logLevel)
		}
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch strings.ToLower(logFormat) {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	default:
		return fmt.Errorf("invalid log format: %q (valid options: text, json)", logFormat)
	}

	slog.SetDefault(slog.New(handler))

	return nil
}
