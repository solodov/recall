package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	"github.com/solodov/recall/providers/ripgrep"

	"google.golang.org/protobuf/encoding/prototext"
)

func TestRipgrepProviderBinarySmokeUsesFakeRG(t *testing.T) {
	if os.Getenv("RECALL_RIPGREP_PROVIDER_TEST_HELPER") == "1" {
		return
	}

	root := t.TempDir()
	fakeArgsPath := filepath.Join(t.TempDir(), "rg-args")
	fakeRG := writeFakeRipgrep(t, fakeArgsPath)

	cmd := exec.Command(os.Args[0], "-test.run=TestRipgrepProviderBinaryEntrypoint")
	cmd.Env = append(os.Environ(),
		"RECALL_RIPGREP_PROVIDER_TEST_HELPER=1",
		"RECALL_RIPGREP_PROVIDER_TEST_ROOT="+root,
		"RECALL_RIPGREP_PROVIDER_TEST_RG="+fakeRG,
	)
	cmd.Stdin = strings.NewReader("query: \"foo type:go -in:test\"\nlimit: 1\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("provider helper failed: %v\nstderr: %s", err, stderr.String())
	}

	response := &searchv1.SearchResponse{}
	if err := prototext.Unmarshal(stdout.Bytes(), response); err != nil {
		t.Fatalf("decode provider response: %v\n%s", err, stdout.String())
	}
	if len(response.GetHits()) != 1 {
		t.Fatalf("hit count = %d, want 1", len(response.GetHits()))
	}
	hit := response.GetHits()[0]
	if hit.GetSelector() != ripgrep.KindCodeMatch || hit.GetTitle() != "main.go:4:1" || hit.GetSnippet() != "foo()" {
		t.Fatalf("hit = %#v, want mapped code match", hit)
	}

	args, err := os.ReadFile(fakeArgsPath)
	if err != nil {
		t.Fatalf("read fake rg args: %v", err)
	}
	argLog := string(args)
	for _, want := range []string{"--json", "--fixed-strings", "--type\ngo", "foo", root} {
		if !strings.Contains(argLog, want) {
			t.Fatalf("fake rg args %q do not contain %q", argLog, want)
		}
	}
}

func TestRipgrepProviderBinaryEntrypoint(t *testing.T) {
	if os.Getenv("RECALL_RIPGREP_PROVIDER_TEST_HELPER") != "1" {
		return
	}
	os.Args = []string{
		os.Args[0],
		"--root", os.Getenv("RECALL_RIPGREP_PROVIDER_TEST_ROOT"),
		"--rg", os.Getenv("RECALL_RIPGREP_PROVIDER_TEST_RG"),
		searchv1.SearchProviderSearchPath,
	}
	main()
	os.Exit(0)
}

func writeFakeRipgrep(t *testing.T, argsPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rg")
	body := `#!/bin/sh
if [ "$1" = "--files" ]; then
  printf '%s\0' 'main.go'
  exit 0
fi
printf '%s\n' "$@" > "$RECALL_RIPGREP_PROVIDER_TEST_ARGS"
printf '%s\n' '{"type":"match","data":{"path":{"text":"main.go"},"lines":{"text":"foo()\n"},"line_number":4,"submatches":[{"match":{"text":"foo"},"start":0,"end":3}]}}'
`
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake ripgrep: %v", err)
	}
	t.Setenv("RECALL_RIPGREP_PROVIDER_TEST_ARGS", argsPath)
	return path
}
