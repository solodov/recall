package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/solodov/recall/internal/config"
	"github.com/solodov/recall/internal/logging"
	"github.com/solodov/recall/internal/openers"
	runctx "github.com/solodov/recall/internal/runtime"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
)

func main() {
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	configPath := flags.String("config", "", "provider registry path")
	logPath := flags.String("log-path", "", "main rotated log path")
	logLevel := flags.String("log-level", "off", "also print logs to stderr at level: debug|info|warn|error|off")
	flags.Parse(os.Args[1:])
	if flags.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [--config PATH] [--log-path PATH] [--log-level LEVEL] recall://open?...\n", os.Args[0])
		os.Exit(2)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logger, err := newLogger(*logPath, *logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := openers.Open(context.Background(), cfg, flags.Arg(0), openers.Options{Logger: logger}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadConfig(path string) (*configv1.RecallConfig, error) {
	if path != "" {
		return config.LoadFile(path)
	}
	return config.LoadDefault()
}

// newLogger creates the same rotated main log used by recall search commands.
func newLogger(path string, stderrLevel string) (*slog.Logger, error) {
	if strings.TrimSpace(path) == "" {
		logPaths, err := runctx.DefaultLogPaths()
		if err != nil {
			return nil, err
		}
		path = logPaths.Main
	}
	return logging.New(path, stderrLevel)
}
