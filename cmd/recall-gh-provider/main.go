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
	var domains domainListFlag
	flags.Var(&domains, "domain", "GitHub search domain to enable: code, commit, issue, pr, or repo; repeatable and defaults to all")
	ghPath := flags.String("gh", "gh", "GitHub CLI executable path")
	flags.Parse(os.Args[1:])

	provider, err := gh.New(gh.Options{Domains: domains, GitHubPath: *ghPath})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := recallprovider.ServeSearchWithOptions(context.Background(), provider, recallprovider.ServeOptions{Args: flags.Args()}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type domainListFlag []gh.Domain

func (values *domainListFlag) String() string {
	return fmt.Sprint([]gh.Domain(*values))
}

func (values *domainListFlag) Set(value string) error {
	domain, err := gh.ParseDomain(value)
	if err != nil {
		return err
	}
	*values = append(*values, domain)
	return nil
}
