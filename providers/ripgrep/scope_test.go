package ripgrep

import (
	"reflect"
	"strings"
	"testing"
)

func TestExcludedScopeGlobArgsTranslatesTestScope(t *testing.T) {
	args, err := ExcludedScopeGlobArgs([]Scope{ScopeTest})
	if err != nil {
		t.Fatalf("ExcludedScopeGlobArgs returned error: %v", err)
	}

	want := []string{
		"--glob", "!**/*_test.*",
		"--glob", "!**/*.test.*",
		"--glob", "!**/*.spec.*",
		"--glob", "!**/test/**",
		"--glob", "!**/tests/**",
		"--glob", "!**/__tests__/**",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestExcludedScopeGlobArgsDeduplicatesScopes(t *testing.T) {
	args, err := ExcludedScopeGlobArgs([]Scope{ScopeTest, ScopeTest})
	if err != nil {
		t.Fatalf("ExcludedScopeGlobArgs returned error: %v", err)
	}
	if got, want := len(args), 12; got != want {
		t.Fatalf("arg count = %d, want %d", got, want)
	}
}

func TestExcludedScopeGlobArgsRejectsUnknownScope(t *testing.T) {
	_, err := ExcludedScopeGlobArgs([]Scope{"docs"})
	if err == nil {
		t.Fatal("ExcludedScopeGlobArgs succeeded with unknown scope")
	}
	if !strings.Contains(err.Error(), "docs") {
		t.Fatalf("error = %q, want unknown scope name", err.Error())
	}
}

func TestExcludedScopeGlobsReturnsCopy(t *testing.T) {
	first, ok := ExcludedScopeGlobs(ScopeTest)
	if !ok {
		t.Fatal("ScopeTest was not mapped")
	}
	first[0] = "mutated"

	second, ok := ExcludedScopeGlobs(ScopeTest)
	if !ok {
		t.Fatal("ScopeTest was not mapped on second call")
	}
	if second[0] == "mutated" {
		t.Fatal("ExcludedScopeGlobs returned mutable internal slice")
	}
}
