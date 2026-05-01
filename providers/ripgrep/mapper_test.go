package ripgrep

import (
	"path/filepath"
	"reflect"
	"testing"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

func TestResultsFromMatchesMapsFileContentFields(t *testing.T) {
	root := t.TempDir()
	matchPath := filepath.Join(root, "src", "main.go")

	results := ResultsFromMatches([]Match{{
		Path:       matchPath,
		LineNumber: 42,
		Line:       `fmt.Println("foo")`,
		Submatches: []Submatch{{Text: "foo", Start: 13, End: 16}},
	}}, ResultOptions{Roots: []string{root}})

	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	result := results[0]
	if result.GetSelector() != SelectorFileContent {
		t.Fatalf("selector = %q, want %q", result.GetSelector(), SelectorFileContent)
	}
	if result.GetId() != "file_content:"+matchPath+":42:14" {
		t.Fatalf("id = %q", result.GetId())
	}
	if got := resultTextField(t, result, "path"); got != "src/main.go" {
		t.Fatalf("path field = %q, want relative file path", got)
	}
	if got := resultIntegerField(t, result, "line"); got != 42 {
		t.Fatalf("line field = %d, want 42", got)
	}
	if got := resultIntegerField(t, result, "column"); got != 14 {
		t.Fatalf("column field = %d, want 14", got)
	}
	if got := resultTextField(t, result, "snippet"); got != `fmt.Println("foo")` {
		t.Fatalf("snippet field = %q", got)
	}
	if !reflect.DeepEqual(result.GetFormat().GetTitleFields(), []string{"line", "snippet"}) {
		t.Fatalf("title fields = %#v, want line and snippet", result.GetFormat().GetTitleFields())
	}
	if !reflect.DeepEqual(result.GetFormat().GetDetailFields(), []string{"line", "snippet"}) {
		t.Fatalf("detail fields = %#v, want title fields suppressed from detail fallback", result.GetFormat().GetDetailFields())
	}
	if len(result.GetTargets()) != 1 || result.GetTargets()[0].GetFile().GetPath() != matchPath || result.GetTargets()[0].GetFile().GetLine() != 42 || result.GetTargets()[0].GetFile().GetColumn() != 14 {
		t.Fatalf("targets = %#v, want positioned file target", result.GetTargets())
	}
	if result.GetGroup().GetKey() != "src/main.go" || result.GetGroup().GetTitle() != "src/main.go" {
		t.Fatalf("group = %#v, want file group", result.GetGroup())
	}
	if len(result.GetGroup().GetTargets()) != 1 || result.GetGroup().GetTargets()[0].GetFile().GetPath() != matchPath {
		t.Fatalf("group targets = %#v, want file target", result.GetGroup().GetTargets())
	}
}

func TestPathResultsFromMatchesMapsFileAndDirectoryFields(t *testing.T) {
	root := t.TempDir()
	matchPath := filepath.Join(root, "providers", "ripgrep", "runner.go")

	results := PathResultsFromMatches([]PathMatch{{Path: matchPath}}, ResultOptions{Roots: []string{root}})

	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	result := results[0]
	if result.GetSelector() != SelectorFileName || resultTextField(t, result, "name") != "runner.go" {
		t.Fatalf("result = %#v, want path match name", result)
	}
	if result.GetId() != "file_name:"+matchPath {
		t.Fatalf("id = %q", result.GetId())
	}
	if got := resultTextField(t, result, "path"); got != "providers/ripgrep/runner.go" {
		t.Fatalf("path field = %q", got)
	}
	if got := resultTextField(t, result, "directory"); got != "providers/ripgrep" {
		t.Fatalf("directory field = %q", got)
	}
	if !reflect.DeepEqual(result.GetFormat().GetTitleFields(), []string{"name"}) {
		t.Fatalf("title fields = %#v, want name", result.GetFormat().GetTitleFields())
	}
	if len(result.GetTargets()) != 1 || result.GetTargets()[0].GetFile().GetPath() != matchPath {
		t.Fatalf("targets = %#v, want file target", result.GetTargets())
	}
	if result.GetGroup().GetKey() != "providers/ripgrep" || result.GetGroup().GetTitle() != "providers/ripgrep" {
		t.Fatalf("group = %#v, want parent directory group", result.GetGroup())
	}
	if result.GetGroup().GetTargets()[0].GetFile().GetPath() != filepath.Dir(matchPath) {
		t.Fatalf("group targets = %#v, want directory target", result.GetGroup().GetTargets())
	}
}

func TestResultsFromMatchesEmitsOneResultPerMatchingLine(t *testing.T) {
	root := t.TempDir()
	results := ResultsFromMatches([]Match{{
		Path:       filepath.Join(root, "main.go"),
		LineNumber: 3,
		Line:       "foo foo",
		Submatches: []Submatch{
			{Text: "foo", Start: 0, End: 3},
			{Text: "foo", Start: 4, End: 7},
		},
	}}, ResultOptions{Roots: []string{root}})

	if len(results) != 1 {
		t.Fatalf("result count = %d, want one result per matching line", len(results))
	}
	if got := resultIntegerField(t, results[0], "line"); got != 3 {
		t.Fatalf("line field = %d, want first match line", got)
	}
	if got := resultIntegerField(t, results[0], "column"); got != 1 {
		t.Fatalf("column field = %d, want first match column", got)
	}
}

func TestResultsFromMatchesWithoutSubmatchesStillEmitsLineResult(t *testing.T) {
	root := t.TempDir()
	results := ResultsFromMatches([]Match{{
		Path:       filepath.Join(root, "main.go"),
		LineNumber: 7,
		Line:       "matched line",
	}}, ResultOptions{Roots: []string{root}})

	if len(results) != 1 {
		t.Fatalf("result count = %d, want fallback line result", len(results))
	}
	if got := resultIntegerField(t, results[0], "line"); got != 7 {
		t.Fatalf("line field = %d, want 7", got)
	}
	if got := resultIntegerField(t, results[0], "column"); got != 1 {
		t.Fatalf("column field = %d, want column one fallback", got)
	}
}

func TestMatchesToSearchResponseClonesWarnings(t *testing.T) {
	warning := &searchv1.SearchResponse_Warning{Message: "missing", Code: proto.String(WarningRootMissing)}
	response := MatchesToSearchResponse(nil, []*searchv1.SearchResponse_Warning{warning}, ResultOptions{})
	warning.Message = "mutated"

	if len(response.GetWarnings()) != 1 {
		t.Fatalf("warning count = %d, want 1", len(response.GetWarnings()))
	}
	if response.GetWarnings()[0].GetMessage() != "missing" || response.GetWarnings()[0].GetCode() != WarningRootMissing {
		t.Fatalf("warnings = %#v, want cloned missing-root warning", response.GetWarnings())
	}
}

func resultTextField(t *testing.T, result *searchv1.SearchResponse_Result, key string) string {
	t.Helper()
	for _, field := range result.GetFields() {
		if field.GetKey() == key {
			return field.GetText()
		}
	}
	t.Fatalf("missing text field %q in %#v", key, result.GetFields())
	return ""
}

func resultIntegerField(t *testing.T, result *searchv1.SearchResponse_Result, key string) int64 {
	t.Helper()
	for _, field := range result.GetFields() {
		if field.GetKey() == key {
			return field.GetInteger()
		}
	}
	t.Fatalf("missing integer field %q in %#v", key, result.GetFields())
	return 0
}
