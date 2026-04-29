package main

import (
	"context"
	"fmt"
	"os"

	"github.com/solodov/recall/examples/exampleprovider"
)

func main() {
	if err := exampleprovider.ServeDefault(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
