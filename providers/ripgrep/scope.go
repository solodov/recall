// Package ripgrep implements recall's first-party code search provider.
package ripgrep

import "fmt"

// Scope identifies a named provider-local search area.
type Scope string

const (
	// ScopeTest matches test files and conventional test directories.
	ScopeTest Scope = "test"
)

var excludedScopeGlobs = map[Scope][]string{
	ScopeTest: {
		"**/*_test.*",
		"**/*.test.*",
		"**/*.spec.*",
		"**/test/**",
		"**/tests/**",
		"**/__tests__/**",
	},
}

// ExcludedScopeGlobArgs translates provider query exclusions into ripgrep glob
// arguments. Unknown scopes fail here so query parsing can stay independent of
// ripgrep's argv shape.
func ExcludedScopeGlobArgs(scopes []Scope) ([]string, error) {
	seen := make(map[Scope]bool, len(scopes))
	args := []string{}
	for _, scope := range scopes {
		if seen[scope] {
			continue
		}
		seen[scope] = true

		patterns, ok := ExcludedScopeGlobs(scope)
		if !ok {
			return nil, fmt.Errorf("unsupported ripgrep exclusion scope %q", scope)
		}
		for _, pattern := range patterns {
			args = append(args, "--glob", "!"+pattern)
		}
	}
	return args, nil
}

// ExcludedScopeGlobs returns the ripgrep glob patterns for one exclusion scope.
func ExcludedScopeGlobs(scope Scope) ([]string, bool) {
	patterns, ok := excludedScopeGlobs[scope]
	if !ok {
		return nil, false
	}
	return append([]string{}, patterns...), true
}
