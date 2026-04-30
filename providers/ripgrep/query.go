package ripgrep

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// SearchKind selects which ripgrep-backed result family a query should emit.
type SearchKind string

const (
	SearchKindContent SearchKind = SelectorFileContent
	SearchKindPath    SearchKind = SelectorFileName
)

// PathFilter matches root-relative slash-normalized file paths before a search.
type PathFilter struct {
	Include bool
	Pattern string
}

// Query is the provider-owned search shape produced from the user query text.
type Query struct {
	Pattern     string
	Kinds       []SearchKind
	FileTypes   []string
	PathFilters []PathFilter
}

// ParseQuery translates the ripgrep provider query language. Free text becomes
// literal content search text and a path-name substring, type:foo restricts
// ripgrep file types, and in:regex / -in:regex filter root-relative file paths.
// Recall-level selectors are sent separately as advisory SearchRequest hints.
func ParseQuery(input string) (Query, error) {
	var query Query
	var patternTerms []string
	tokens := strings.Fields(input)
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		switch {
		case strings.HasPrefix(token, "-in:"):
			filter, err := parsePathFilter(false, strings.TrimPrefix(token, "-in:"))
			if err != nil {
				return Query{}, err
			}
			query.PathFilters = append(query.PathFilters, filter)
		case strings.HasPrefix(token, "in:"):
			filter, err := parsePathFilter(true, strings.TrimPrefix(token, "in:"))
			if err != nil {
				return Query{}, err
			}
			query.PathFilters = append(query.PathFilters, filter)
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
	query.Kinds = defaultSearchKinds(query)
	if err := validateQuery(query); err != nil {
		return Query{}, err
	}
	return query, nil
}

func defaultSearchKinds(query Query) []SearchKind {
	if query.Pattern == "" && hasIncludePathFilter(query.PathFilters) {
		return []SearchKind{SearchKindPath}
	}
	return []SearchKind{SearchKindPath, SearchKindContent}
}

func hasIncludePathFilter(filters []PathFilter) bool {
	for _, filter := range filters {
		if filter.Include {
			return true
		}
	}
	return false
}

func parsePathFilter(include bool, value string) (PathFilter, error) {
	pattern := strings.TrimSpace(value)
	if pattern == "" {
		return PathFilter{}, errors.New("ripgrep path filter is required")
	}
	if _, err := compilePathPattern(pattern); err != nil {
		return PathFilter{}, fmt.Errorf("invalid ripgrep path filter %q: %w", pattern, err)
	}
	return PathFilter{Include: include, Pattern: pattern}, nil
}

func compilePathPattern(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile("(?i:" + pattern + ")")
}

func validateQuery(query Query) error {
	if query.Pattern != "" {
		return nil
	}
	if containsSearchKind(query.Kinds, SearchKindContent) {
		return errors.New("ripgrep content search must contain search text")
	}
	if containsSearchKind(query.Kinds, SearchKindPath) && hasIncludePathFilter(query.PathFilters) {
		return nil
	}
	return errors.New("ripgrep query must contain search text or an in:regex path selector")
}

func containsSearchKind(kinds []SearchKind, kind SearchKind) bool {
	for _, existing := range kinds {
		if existing == kind {
			return true
		}
	}
	return false
}

func parseFileType(value string) (string, error) {
	fileType := strings.TrimSpace(value)
	if fileType == "" {
		return "", errors.New("ripgrep file type is required")
	}
	return fileType, nil
}
