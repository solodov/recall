// Package gh implements a recall provider backed by the GitHub CLI.
package gh

import (
	"fmt"
	"strings"
)

// Domain is one GitHub search entity family supported by this provider.
type Domain string

const (
	DomainCode   Domain = "code"
	DomainCommit Domain = "commit"
	DomainIssue  Domain = "issue"
	DomainPR     Domain = "pr"
	DomainRepo   Domain = "repo"
)

var defaultDomains = []Domain{DomainCode, DomainCommit, DomainIssue, DomainPR, DomainRepo}

func normalizeDomains(domains []Domain) ([]Domain, error) {
	if len(domains) == 0 {
		return append([]Domain{}, defaultDomains...), nil
	}
	seen := map[Domain]bool{}
	normalized := make([]Domain, 0, len(domains))
	for _, domain := range domains {
		parsed, err := ParseDomain(string(domain))
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

// ParseDomain validates a provider-configured GitHub search domain.
func ParseDomain(value string) (Domain, error) {
	switch domain := Domain(strings.TrimSpace(value)); domain {
	case DomainCode, DomainCommit, DomainIssue, DomainPR, DomainRepo:
		return domain, nil
	case "":
		return "", fmt.Errorf("github search domain is required")
	default:
		return "", fmt.Errorf("unsupported github search domain %q", value)
	}
}
