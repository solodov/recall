package ripgrep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildArgsIncludesJSONFixedStringSearchRootsTypesAndExclusions(t *testing.T) {
	args, err := BuildArgs(RunOptions{
		Pattern:        "foo",
		Roots:          []string{"/repo"},
		FileTypes:      []string{"go"},
		ExcludedScopes: []Scope{ScopeTest},
	})
	if err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}

	want := []string{
		"--json",
		"--fixed-strings",
		"--line-number",
		"--column",
		"--with-filename",
		"--color=never",
		"--type", "go",
		"--glob", "!**/*_test.*",
		"--glob", "!**/*.test.*",
		"--glob", "!**/*.spec.*",
		"--glob", "!**/test/**",
		"--glob", "!**/tests/**",
		"--glob", "!**/__tests__/**",
		"foo",
		"/repo",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestBuildArgsRejectsMissingPatternOrRoots(t *testing.T) {
	_, err := BuildArgs(RunOptions{Pattern: "", Roots: []string{"/repo"}})
	if err == nil || !strings.Contains(err.Error(), "pattern") {
		t.Fatalf("missing pattern error = %v", err)
	}

	_, err = BuildArgs(RunOptions{Pattern: "foo"})
	if err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("missing roots error = %v", err)
	}
}

func TestFileTypeArgsDeduplicatesAndRejectsEmptyTypes(t *testing.T) {
	args, err := FileTypeArgs([]string{"go", "ts", "go"})
	if err != nil {
		t.Fatalf("FileTypeArgs returned error: %v", err)
	}
	want := []string{"--type", "go", "--type", "ts"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}

	_, err = FileTypeArgs([]string{"go", ""})
	if err == nil || !strings.Contains(err.Error(), "file type") {
		t.Fatalf("empty file type error = %v", err)
	}
}

func TestParseMatchesIgnoresNonMatchEventsAndDecodesMatch(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"begin","data":{"path":{"text":"main.go"}}}`,
		`{"type":"match","data":{"path":{"text":"main.go"},"lines":{"text":"fmt.Println(\"foo\")\n"},"line_number":12,"submatches":[{"match":{"text":"foo"},"start":13,"end":16}]}}`,
		`{"type":"summary","data":{}}`,
	}, "\n")

	matches, limitReached, err := parseMatches(strings.NewReader(input), 0, nil)
	if err != nil {
		t.Fatalf("parseMatches returned error: %v", err)
	}
	if limitReached {
		t.Fatal("limitReached = true, want false")
	}
	if len(matches) != 1 {
		t.Fatalf("match count = %d, want 1", len(matches))
	}
	match := matches[0]
	if match.Path != "main.go" || match.LineNumber != 12 || match.Line != `fmt.Println("foo")` {
		t.Fatalf("match = %#v", match)
	}
	if len(match.Submatches) != 1 || match.Submatches[0].Text != "foo" || match.Submatches[0].Start != 13 || match.Submatches[0].End != 16 {
		t.Fatalf("submatches = %#v", match.Submatches)
	}
}

func TestParseMatchesStopsAtLimit(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"match","data":{"path":{"text":"one.go"},"lines":{"text":"foo\n"},"line_number":1,"submatches":[]}}`,
		`{"type":"match","data":{"path":{"text":"two.go"},"lines":{"text":"foo\n"},"line_number":2,"submatches":[]}}`,
	}, "\n")
	calledCancel := false

	matches, limitReached, err := parseMatches(strings.NewReader(input), 1, func() { calledCancel = true })
	if err != nil {
		t.Fatalf("parseMatches returned error: %v", err)
	}
	if !limitReached || !calledCancel {
		t.Fatalf("limitReached=%v calledCancel=%v, want both true", limitReached, calledCancel)
	}
	if len(matches) != 1 || matches[0].Path != "one.go" {
		t.Fatalf("matches = %#v, want only first match", matches)
	}
}

func TestRunnerTreatsExitCodeOneAsNoMatches(t *testing.T) {
	runner := Runner{Binary: writeFakeRG(t, "exit 1\n")}

	result, err := runner.Run(context.Background(), RunOptions{Pattern: "foo", Roots: []string{"/repo"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Matches) != 0 {
		t.Fatalf("matches = %#v, want none", result.Matches)
	}
}

func TestRunnerReturnsRipgrepStderrForOtherFailures(t *testing.T) {
	runner := Runner{Binary: writeFakeRG(t, "echo 'bad pattern' >&2\nexit 2\n")}

	_, err := runner.Run(context.Background(), RunOptions{Pattern: "foo", Roots: []string{"/repo"}})
	if err == nil {
		t.Fatal("Run succeeded despite failing ripgrep")
	}
	if !strings.Contains(err.Error(), "bad pattern") {
		t.Fatalf("error = %q, want stderr details", err.Error())
	}
}

func TestRunnerStopsAfterLimit(t *testing.T) {
	script := `
printf '%s\n' '{"type":"match","data":{"path":{"text":"one.go"},"lines":{"text":"foo\\n"},"line_number":1,"submatches":[]}}'
sleep 10
printf '%s\n' '{"type":"match","data":{"path":{"text":"two.go"},"lines":{"text":"foo\\n"},"line_number":2,"submatches":[]}}'
`
	runner := Runner{Binary: writeFakeRG(t, script)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runner.Run(ctx, RunOptions{Pattern: "foo", Roots: []string{"/repo"}, Limit: 1})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Path != "one.go" {
		t.Fatalf("matches = %#v, want only first match", result.Matches)
	}
}

func TestRunnerReturnsContextCancellation(t *testing.T) {
	runner := Runner{Binary: writeFakeRG(t, "sleep 10\n")}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := runner.Run(ctx, RunOptions{Pattern: "foo", Roots: []string{"/repo"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context deadline", err)
	}
}

func writeFakeRG(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rg")
	contents := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatalf("write fake rg: %v", err)
	}
	return path
}
