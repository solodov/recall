package main

import (
	"context"
	"fmt"
	"os"

	"github.com/solodov/recall/internal/cli"
)

func main() {
	app := cli.App{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
