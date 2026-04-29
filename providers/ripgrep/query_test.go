package ripgrep

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseQueryKeepsLiteralSearchText(t *testing.T) {
	query, err := ParseQuery("foo bar")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	if query.Pattern != "foo bar" {
		t.Fatalf("pattern = %q, want literal text", query.Pattern)
	}
	if len(query.ExcludedScopes) != 0 {
		t.Fatalf("excluded scopes = %#v, want none", query.ExcludedScopes)
	}
}

func TestParseQueryExcludesTestScope(t *testing.T) {
	query, err := ParseQuery("foo -in:test")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	if query.Pattern != "foo" {
		t.Fatalf("pattern = %q, want foo", query.Pattern)
	}
	if !reflect.DeepEqual(query.ExcludedScopes, []Scope{ScopeTest}) {
		t.Fatalf("excluded scopes = %#v, want test", query.ExcludedScopes)
	}
}

func TestParseQueryIncludesFileTypes(t *testing.T) {
	query, err := ParseQuery("foo type:go type:ts -in:test")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	if query.Pattern != "foo" {
		t.Fatalf("pattern = %q, want foo", query.Pattern)
	}
	if !reflect.DeepEqual(query.FileTypes, []string{"go", "ts"}) {
		t.Fatalf("file types = %#v, want go and ts", query.FileTypes)
	}
	if !reflect.DeepEqual(query.ExcludedScopes, []Scope{ScopeTest}) {
		t.Fatalf("excluded scopes = %#v, want test", query.ExcludedScopes)
	}
}

func TestParseQueryRejectsMissingSearchText(t *testing.T) {
	_, err := ParseQuery("-in:test")
	if err == nil || !strings.Contains(err.Error(), "search text") {
		t.Fatalf("ParseQuery error = %v, want missing search text", err)
	}
}

func TestParseQueryRejectsUnsupportedScopeAndOperator(t *testing.T) {
	_, err := ParseQuery("foo -in:docs")
	if err == nil || !strings.Contains(err.Error(), "docs") {
		t.Fatalf("unsupported scope error = %v", err)
	}

	_, err = ParseQuery("foo -kind:go")
	if err == nil || !strings.Contains(err.Error(), "-kind:go") {
		t.Fatalf("unsupported operator error = %v", err)
	}
}

func TestParseQueryRejectsInvalidFileTypes(t *testing.T) {
	_, err := ParseQuery("foo type:")
	if err == nil || !strings.Contains(err.Error(), "file type") {
		t.Fatalf("empty type error = %v", err)
	}

	_, err = ParseQuery("foo -type:go")
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("negative type error = %v", err)
	}
}

func TestParseQueryRejectsPositiveInScopeForNow(t *testing.T) {
	_, err := ParseQuery("foo in:test")
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("positive scope error = %v, want unsupported", err)
	}
}
