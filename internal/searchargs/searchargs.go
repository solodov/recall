// Package searchargs parses recall search arguments for command and TUI frontends.
package searchargs

import (
	"fmt"
	"io"
	"strings"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/pflag"
)

// Search contains recall-owned routing flags plus the provider-native query.
type Search struct {
	Query     string
	Selectors []string
	Limit     uint32
}

// ParsePrompt shell-splits an interactive prompt and parses recall search flags.
func ParsePrompt(prompt string) (Search, error) {
	args, err := shellquote.Split(prompt)
	if err != nil {
		return Search{}, fmt.Errorf("parse prompt: %w", err)
	}
	return Parse(args)
}

// Parse extracts recall search flags from args and leaves the remaining words as
// the provider-owned query. Flag parsing stops at the first positional word so
// provider operators like -in:test remain part of the query.
func Parse(args []string) (Search, error) {
	var selectors stringListFlag
	var limit uint32
	format := "human"
	grouped := true

	flags := pflag.NewFlagSet("recall search", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.VarP(&selectors, "selector", "s", "comma-separated selectors to query")
	flags.Uint32VarP(&limit, "limit", "l", 0, "override per-provider result limit")
	flags.BoolVarP(&grouped, "grouped", "g", true, "accepted for CLI prompt compatibility")
	flags.StringVarP(&format, "format", "f", "human", "accepted when set to human")
	flags.SetInterspersed(false)
	if err := flags.Parse(args); err != nil {
		return Search{}, err
	}
	if format != "" && format != "human" {
		return Search{}, fmt.Errorf("interactive search does not support --format %q", format)
	}

	query := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if query == "" {
		return Search{}, fmt.Errorf("missing query")
	}
	return Search{Query: query, Selectors: append([]string{}, selectors...), Limit: limit}, nil
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func (values *stringListFlag) Type() string {
	return "stringList"
}
