package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	recallprovider "github.com/solodov/recall/provider"
	"github.com/solodov/recall/providers/ripgrep"
)

func main() {
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var roots stringListFlag
	flags.Var(&roots, "root", "file or directory root to search; repeatable and defaults to the current directory")
	rgPath := flags.String("rg", "rg", "ripgrep executable path")
	flags.Parse(os.Args[1:])

	provider := ripgrep.New(ripgrep.Options{Roots: roots, RipgrepPath: *rgPath})
	if err := recallprovider.ServeSearchWithOptions(context.Background(), provider, recallprovider.ServeOptions{Args: flags.Args()}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return fmt.Sprint([]string(*values))
}

func (values *stringListFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}
