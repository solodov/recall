package ripgrep

import (
	"errors"
	"fmt"
	"strings"
)

// Query is the provider-owned search shape produced from the user query text.
type Query struct {
	Pattern        string
	FileTypes      []string
	ExcludedScopes []Scope
}

// ParseQuery translates the initial ripgrep provider query language. Free text
// becomes a literal ripgrep pattern, type:foo restricts ripgrep file types, and
// -in:test excludes test files.
func ParseQuery(input string) (Query, error) {
	var query Query
	var patternTerms []string
	for _, token := range strings.Fields(input) {
		switch {
		case strings.HasPrefix(token, "-in:"):
			scope, err := parseScope(strings.TrimPrefix(token, "-in:"))
			if err != nil {
				return Query{}, err
			}
			query.ExcludedScopes = append(query.ExcludedScopes, scope)
		case strings.HasPrefix(token, "in:"):
			return Query{}, fmt.Errorf("positive in: scope is not supported yet: %s", token)
		case strings.HasPrefix(token, "type:"):
			fileType, err := parseFileType(strings.TrimPrefix(token, "type:"))
			if err != nil {
				return Query{}, err
			}
			query.FileTypes = append(query.FileTypes, fileType)
		case strings.HasPrefix(token, "-type:"):
			return Query{}, fmt.Errorf("negative type: file filter is not supported yet: %s", token)
		case strings.HasPrefix(token, "-"):
			return Query{}, fmt.Errorf("unsupported ripgrep query operator %q", token)
		default:
			patternTerms = append(patternTerms, token)
		}
	}
	query.Pattern = strings.TrimSpace(strings.Join(patternTerms, " "))
	if query.Pattern == "" {
		return Query{}, errors.New("ripgrep query must contain search text")
	}
	return query, nil
}

func parseScope(value string) (Scope, error) {
	scope := Scope(strings.TrimSpace(value))
	switch scope {
	case ScopeTest:
		return scope, nil
	default:
		return "", fmt.Errorf("unsupported ripgrep query scope %q", value)
	}
}

func parseFileType(value string) (string, error) {
	fileType := strings.TrimSpace(value)
	if fileType == "" {
		return "", errors.New("ripgrep file type is required")
	}
	return fileType, nil
}
