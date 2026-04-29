package ripgrep

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	recallprovider "github.com/solodov/recall/provider"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestProviderSearchPassesExistingRootsQueryAndLimitToRunner(t *testing.T) {
	root := t.TempDir()
	runner := &recordingRunner{result: RunResult{Matches: []Match{{
		Path:       filepath.Join(root, "main.go"),
		LineNumber: 8,
		Line:       "foo",
		Submatches: []Submatch{{Text: "foo", Start: 0, End: 3}},
	}}}}
	provider := New(Options{Roots: []string{root}, Runner: runner})

	response, err := provider.Search(context.Background(), &searchv1.SearchRequest{Query: "foo type:go -in:test", Limit: proto.Uint32(2)})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !runner.called {
		t.Fatal("runner was not called")
	}
	if runner.options.Pattern != "foo" {
		t.Fatalf("pattern = %q, want foo", runner.options.Pattern)
	}
	if !reflect.DeepEqual(runner.options.Roots, []string{root}) {
		t.Fatalf("roots = %#v, want %q", runner.options.Roots, root)
	}
	if !reflect.DeepEqual(runner.options.FileTypes, []string{"go"}) {
		t.Fatalf("file types = %#v, want go", runner.options.FileTypes)
	}
	if !reflect.DeepEqual(runner.options.ExcludedScopes, []Scope{ScopeTest}) {
		t.Fatalf("excluded scopes = %#v, want test", runner.options.ExcludedScopes)
	}
	if runner.options.Limit != 2 {
		t.Fatalf("limit = %d, want 2", runner.options.Limit)
	}
	if len(response.GetHits()) != 1 || response.GetHits()[0].GetTitle() != "main.go:8:1" {
		t.Fatalf("hits = %#v, want mapped code hit", response.GetHits())
	}
}

func TestProviderSearchSkipsMissingRootsAndPreservesWarnings(t *testing.T) {
	base := t.TempDir()
	existing := filepath.Join(base, "src")
	createDir(t, existing)
	missing := filepath.Join(base, "missing")
	runner := &recordingRunner{}
	provider := New(Options{Roots: []string{existing, missing}, Runner: runner})

	response, err := provider.Search(context.Background(), &searchv1.SearchRequest{Query: "foo"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !runner.called {
		t.Fatal("runner was not called for existing root")
	}
	if !reflect.DeepEqual(runner.options.Roots, []string{existing}) {
		t.Fatalf("roots = %#v, want only existing root", runner.options.Roots)
	}
	if len(response.GetWarnings()) != 1 || response.GetWarnings()[0].GetCode() != WarningRootMissing {
		t.Fatalf("warnings = %#v, want missing-root warning", response.GetWarnings())
	}
}

func TestProviderSearchAllMissingRootsReturnsWarningsWithoutRunner(t *testing.T) {
	provider := New(Options{Roots: []string{filepath.Join(t.TempDir(), "missing")}, Runner: &recordingRunner{}})

	response, err := provider.Search(context.Background(), &searchv1.SearchRequest{Query: "foo"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(response.GetHits()) != 0 {
		t.Fatalf("hits = %#v, want none", response.GetHits())
	}
	if len(response.GetWarnings()) != 1 || response.GetWarnings()[0].GetCode() != WarningRootMissing {
		t.Fatalf("warnings = %#v, want missing-root warning", response.GetWarnings())
	}
}

func TestProviderSearchWithoutLimitReturnsAllRunnerMatches(t *testing.T) {
	root := t.TempDir()
	runner := &recordingRunner{result: RunResult{Matches: []Match{
		{Path: filepath.Join(root, "one.go"), LineNumber: 1, Line: "foo", Submatches: []Submatch{{Start: 0, End: 3}}},
		{Path: filepath.Join(root, "two.go"), LineNumber: 2, Line: "foo", Submatches: []Submatch{{Start: 0, End: 3}}},
	}}}
	provider := New(Options{Roots: []string{root}, Runner: runner})

	response, err := provider.Search(context.Background(), &searchv1.SearchRequest{Query: "foo"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if runner.options.Limit != 0 {
		t.Fatalf("limit = %d, want 0 for absent request limit", runner.options.Limit)
	}
	if len(response.GetHits()) != 2 {
		t.Fatalf("hit count = %d, want all runner matches", len(response.GetHits()))
	}
}

func TestProviderServesThroughSDKWithTextproto(t *testing.T) {
	root := t.TempDir()
	runner := &recordingRunner{result: RunResult{Matches: []Match{{
		Path:       filepath.Join(root, "main.go"),
		LineNumber: 3,
		Line:       "foo",
		Submatches: []Submatch{{Text: "foo", Start: 0, End: 3}},
	}}}}
	provider := New(Options{Roots: []string{root}, Runner: runner})
	var stdout bytes.Buffer

	err := recallprovider.ServeSearchWithOptions(context.Background(), provider, recallprovider.ServeOptions{
		Stdin:  bytes.NewReader([]byte("query: \"foo type:go -in:test\"\nlimit: 1\n")),
		Stdout: &stdout,
		Args:   []string{searchv1.SearchProviderSearchPath},
	})
	if err != nil {
		t.Fatalf("ServeSearchWithOptions returned error: %v", err)
	}
	if runner.options.Limit != 1 || !reflect.DeepEqual(runner.options.FileTypes, []string{"go"}) || !reflect.DeepEqual(runner.options.ExcludedScopes, []Scope{ScopeTest}) {
		t.Fatalf("runner options = %#v, want limit, go type, and test exclusion", runner.options)
	}
	response := &searchv1.SearchResponse{}
	if err := prototext.Unmarshal(stdout.Bytes(), response); err != nil {
		t.Fatalf("response was not textproto: %v", err)
	}
	if len(response.GetHits()) != 1 || response.GetHits()[0].GetKind() != KindCodeMatch {
		t.Fatalf("response hits = %#v, want code match", response.GetHits())
	}
}

type recordingRunner struct {
	called  bool
	options RunOptions
	result  RunResult
	err     error
}

func (runner *recordingRunner) Run(_ context.Context, options RunOptions) (RunResult, error) {
	runner.called = true
	runner.options = RunOptions{
		Pattern:        options.Pattern,
		Roots:          append([]string{}, options.Roots...),
		FileTypes:      append([]string{}, options.FileTypes...),
		ExcludedScopes: append([]Scope{}, options.ExcludedScopes...),
		Limit:          options.Limit,
	}
	return runner.result, runner.err
}

func createDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("create dir %q: %v", path, err)
	}
}
