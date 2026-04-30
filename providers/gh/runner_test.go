package gh

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildArgsUsesGitHubSearchEndpoint(t *testing.T) {
	args, err := BuildArgs(DomainPR, "parser repo:example/project type:pr", 25)
	if err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}
	want := []string{"api", "-X", "GET", "search/issues", "-f", "q=parser repo:example/project type:pr", "-F", "per_page=25"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestBuildArgsCapsAPILimit(t *testing.T) {
	args, err := BuildArgs(DomainCode, "parser", 500)
	if err != nil {
		t.Fatalf("BuildArgs returned error: %v", err)
	}
	if got := strings.Join(args, "\n"); !strings.Contains(got, "per_page=100") {
		t.Fatalf("args = %#v, want capped per_page", args)
	}
}

func TestRunnerSearchUsesFakeGH(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args")
	fakeGH := writeFakeGH(t, argsPath, `{"items":[{"number":3,"title":"Fix parser","state":"open","html_url":"https://github.com/example/project/issues/3","repository_url":"https://api.github.com/repos/example/project"}]}`)
	runner := Runner{Binary: fakeGH}

	items, err := runner.Search(context.Background(), DomainIssue, "parser type:issue", 7)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(items) != 1 || items[0].Number != 3 || items[0].Title != "Fix parser" {
		t.Fatalf("items = %#v, want issue", items)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	argText := string(args)
	for _, want := range []string{"api", "search/issues", "q=parser type:issue", "per_page=7"} {
		if !strings.Contains(argText, want) {
			t.Fatalf("args %q do not contain %q", argText, want)
		}
	}
}

func writeFakeGH(t *testing.T, argsPath string, stdout string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gh")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$RECALL_GH_TEST_ARGS\"\nprintf '%s\\n' '" + stdout + "'\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("RECALL_GH_TEST_ARGS", argsPath)
	return path
}
