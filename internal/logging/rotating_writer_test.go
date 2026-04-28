package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriterRotatesAndKeepsBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recall.log")
	writer, err := NewRotatingWriter(path, 8, 1)
	if err != nil {
		t.Fatalf("NewRotatingWriter returned error: %v", err)
	}

	for _, payload := range []string{"first\n", "second\n", "third\n"} {
		if _, err := writer.Write([]byte(payload)); err != nil {
			t.Fatalf("write %q: %v", payload, err)
		}
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	if !strings.Contains(string(current), "third") {
		t.Fatalf("current log = %q, want latest write", string(current))
	}
	backups, err := filepath.Glob(path + ".*")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("backup count = %d, want 1", len(backups))
	}
}
