package searchargs

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePromptSearchFlagsAndQuery(t *testing.T) {
	parsed, err := ParsePrompt(`-s code:file:content --selector notes:page:content -l 7 "foo bar" -in:test`)
	if err != nil {
		t.Fatalf("ParsePrompt returned error: %v", err)
	}
	if parsed.Query != "foo bar -in:test" {
		t.Fatalf("query = %q, want shell-split provider query", parsed.Query)
	}
	if !reflect.DeepEqual(parsed.Selectors, []string{"code:file:content", "notes:page:content"}) {
		t.Fatalf("selectors = %#v", parsed.Selectors)
	}
	if parsed.Limit != 7 {
		t.Fatalf("limit = %d, want 7", parsed.Limit)
	}
}

func TestParseStopsFlagsAtFirstQueryWord(t *testing.T) {
	parsed, err := Parse([]string{"foo", "-in:test", "-s", "not-a-selector"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if parsed.Query != "foo -in:test -s not-a-selector" {
		t.Fatalf("query = %q, want dash-prefixed terms preserved", parsed.Query)
	}
	if len(parsed.Selectors) != 0 {
		t.Fatalf("selectors = %#v, want none", parsed.Selectors)
	}
}

func TestParseRejectsMissingQuery(t *testing.T) {
	_, err := ParsePrompt("-s code")
	if err == nil || !strings.Contains(err.Error(), "missing query") {
		t.Fatalf("ParsePrompt error = %v, want missing query", err)
	}
}

func TestParseRejectsMachineFormat(t *testing.T) {
	_, err := ParsePrompt("-f json query")
	if err == nil || !strings.Contains(err.Error(), "does not support --format") {
		t.Fatalf("ParsePrompt error = %v, want unsupported format", err)
	}
}
