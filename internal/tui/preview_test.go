package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solodov/recall/internal/render"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	"google.golang.org/protobuf/proto"
)

func TestDefaultPreviewRendersContextAroundFileTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\nfive\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	summary := render.ResultSummary{Target: &searchv1.OpenTarget{Target: &searchv1.OpenTarget_File{File: &searchv1.FileTarget{Path: path, Line: proto.Uint32(3)}}}}

	preview, err := DefaultPreview(context.Background(), summary, PreviewOptions{ContextLines: 1})
	if err != nil {
		t.Fatalf("DefaultPreview returned error: %v", err)
	}
	if !preview.Available {
		t.Fatal("preview is unavailable for file target")
	}
	for _, want := range []string{"    2 │ two", ">     3 │ three", "    4 │ four"} {
		if !strings.Contains(preview.Text, want) {
			t.Fatalf("preview %q does not contain %q", preview.Text, want)
		}
	}
}

func TestDefaultPreviewReportsUnavailableForURITarget(t *testing.T) {
	summary := render.ResultSummary{Target: &searchv1.OpenTarget{Target: &searchv1.OpenTarget_Uri{Uri: &searchv1.UriTarget{Uri: "https://example.invalid"}}}}

	preview, err := DefaultPreview(context.Background(), summary, PreviewOptions{})
	if err != nil {
		t.Fatalf("DefaultPreview returned error: %v", err)
	}
	if preview.Available {
		t.Fatal("URI target unexpectedly has a preview")
	}
	if !strings.Contains(preview.Text, "No preview available") {
		t.Fatalf("preview text = %q", preview.Text)
	}
}
