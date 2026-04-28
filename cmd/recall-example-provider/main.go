package main

import (
	"context"
	"fmt"
	"os"

	"recall/internal/exampleprovider"
)

func main() {
	if err := exampleprovider.ServeDefault(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
