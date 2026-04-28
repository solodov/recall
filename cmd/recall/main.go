package main

import (
	"context"
	"fmt"
	"os"

	"recall/internal/cli"
)

func main() {
	app := cli.App{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
