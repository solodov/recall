// Package gh implements a recall provider backed by the GitHub CLI.
package gh

import (
	"fmt"
	"strings"
)

// Selector is one provider-local GitHub search surface supported by this provider.
type Selector string

const (
	SelectorCode   Selector = "file:content"
	SelectorCommit Selector = "commit:content"
	SelectorIssue  Selector = "issue:content"
	SelectorPR     Selector = "pr:content"
	SelectorRepo   Selector = "repo:name"
)

var defaultSelectors = []Selector{SelectorCode, SelectorCommit, SelectorIssue, SelectorPR, SelectorRepo}

func normalizeSelectors(selectors []Selector) ([]Selector, error) {
	if len(selectors) == 0 {
		return append([]Selector{}, defaultSelectors...), nil
	}
	seen := map[Selector]bool{}
	normalized := make([]Selector, 0, len(selectors))
	for _, selector := range selectors {
		parsed, err := ParseSelector(string(selector))
		if err != nil {
			return nil, err
		}
		if seen[parsed] {
			continue
		}
		seen[parsed] = true
		normalized = append(normalized, parsed)
	}
	return normalized, nil
}

// ParseSelector validates a configured GitHub provider-local selector.
func ParseSelector(value string) (Selector, error) {
	switch selector := Selector(strings.TrimSpace(value)); selector {
	case SelectorCode, SelectorCommit, SelectorIssue, SelectorPR, SelectorRepo:
		return selector, nil
	case "":
		return "", fmt.Errorf("github selector is required")
	default:
		return "", fmt.Errorf("unsupported github selector %q", value)
	}
}
