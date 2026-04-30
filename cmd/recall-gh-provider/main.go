package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	recallprovider "github.com/solodov/recall/provider"
	"github.com/solodov/recall/providers/gh"
)

func main() {
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var selectors selectorListFlag
	flags.Var(&selectors, "selector", "GitHub selector to enable: file:content, commit:content, issue:content, pr:content, or repo:name; repeatable and defaults to all")
	ghPath := flags.String("gh", "gh", "GitHub CLI executable path")
	flags.Parse(os.Args[1:])

	provider, err := gh.New(gh.Options{Selectors: selectors, GitHubPath: *ghPath})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := recallprovider.ServeSearchWithOptions(context.Background(), provider, recallprovider.ServeOptions{Args: flags.Args()}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type selectorListFlag []gh.Selector

func (values *selectorListFlag) String() string {
	return fmt.Sprint([]gh.Selector(*values))
}

func (values *selectorListFlag) Set(value string) error {
	selector, err := gh.ParseSelector(value)
	if err != nil {
		return err
	}
	*values = append(*values, selector)
	return nil
}
