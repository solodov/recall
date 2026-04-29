package ripgrep

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseQueryKeepsLiteralSearchTextAndDefaultsToPathAndContent(t *testing.T) {
	query, err := ParseQuery("foo bar")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	if query.Pattern != "foo bar" {
		t.Fatalf("pattern = %q, want literal text", query.Pattern)
	}
	if !reflect.DeepEqual(query.Kinds, []SearchKind{SearchKindPath, SearchKindContent}) {
		t.Fatalf("kinds = %#v, want path and content", query.Kinds)
	}
}

func TestParseQueryAcceptsKindSelector(t *testing.T) {
	query, err := ParseQuery("foo -k path -k content -k path")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	if !reflect.DeepEqual(query.Kinds, []SearchKind{SearchKindPath, SearchKindContent}) {
		t.Fatalf("kinds = %#v, want deduplicated path and content", query.Kinds)
	}
	if query.Pattern != "foo" {
		t.Fatalf("pattern = %q, want foo", query.Pattern)
	}
}

func TestParseQueryIncludesPathFilters(t *testing.T) {
	query, err := ParseQuery("foo in:internal/.*/config -in:generated")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	want := []PathFilter{{Include: true, Pattern: "internal/.*/config"}, {Include: false, Pattern: "generated"}}
	if !reflect.DeepEqual(query.PathFilters, want) {
		t.Fatalf("path filters = %#v, want %#v", query.PathFilters, want)
	}
}

func TestPathFiltersAreCaseInsensitive(t *testing.T) {
	pattern, err := compilePathPattern("router")
	if err != nil {
		t.Fatalf("compilePathPattern returned error: %v", err)
	}
	if !pattern.MatchString("Source/Router.go") {
		t.Fatal("path filter did not match path case-insensitively")
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
	if !reflect.DeepEqual(query.PathFilters, []PathFilter{{Include: false, Pattern: "test"}}) {
		t.Fatalf("path filters = %#v, want test exclusion regex", query.PathFilters)
	}
}

func TestParseQueryAllowsPathOnlyFilterWithoutSearchText(t *testing.T) {
	query, err := ParseQuery("in:router")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	if query.Pattern != "" || !reflect.DeepEqual(query.Kinds, []SearchKind{SearchKindPath}) {
		t.Fatalf("query = %#v, want path-only filter query", query)
	}
}

func TestParseQueryRejectsMissingSearchText(t *testing.T) {
	_, err := ParseQuery("-in:test")
	if err == nil || !strings.Contains(err.Error(), "search text") {
		t.Fatalf("ParseQuery error = %v, want missing positive selector", err)
	}

	_, err = ParseQuery("-k path")
	if err == nil || !strings.Contains(err.Error(), "search text") {
		t.Fatalf("ParseQuery error = %v, want missing path search text", err)
	}
}

func TestParseQueryRejectsUnsupportedKindAndOperator(t *testing.T) {
	_, err := ParseQuery("foo -k symbol")
	if err == nil || !strings.Contains(err.Error(), "symbol") {
		t.Fatalf("unsupported kind error = %v", err)
	}

	_, err = ParseQuery("foo -kind:go")
	if err == nil || !strings.Contains(err.Error(), "-kind:go") {
		t.Fatalf("unsupported operator error = %v", err)
	}
}

func TestParseQueryRejectsInvalidFileTypesAndPathFilters(t *testing.T) {
	_, err := ParseQuery("foo type:")
	if err == nil || !strings.Contains(err.Error(), "file type") {
		t.Fatalf("empty type error = %v", err)
	}

	_, err = ParseQuery("foo -type:go")
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("negative type error = %v", err)
	}

	_, err = ParseQuery("foo in:(")
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("invalid path filter error = %v", err)
	}
}
