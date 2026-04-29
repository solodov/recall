package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/solodov/recall/internal/config"
	"github.com/solodov/recall/internal/openers"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
)

func main() {
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	configPath := flags.String("config", "", "provider registry path")
	flags.Parse(os.Args[1:])
	if flags.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [--config PATH] recall://open?...\n", os.Args[0])
		os.Exit(2)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := openers.Open(context.Background(), cfg, flags.Arg(0), openers.Options{}); err != nil {
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
