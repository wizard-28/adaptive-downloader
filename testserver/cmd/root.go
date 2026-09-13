package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	verbose   bool
	logLevel  string
	logFormat string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "testserver",
	Short: "Internal test server for adaptive-downloader",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return setupLogger()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	err := rootCmd.ExecuteContext(ctx)
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
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
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
