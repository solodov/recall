package ripgrep

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRootsKeepsExistingFilesAndDirectories(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "src")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("create dir root: %v", err)
	}
	file := filepath.Join(base, "README.md")
	if err := os.WriteFile(file, []byte("docs"), 0o600); err != nil {
		t.Fatalf("create file root: %v", err)
	}

	resolution, err := (RootResolver{WorkDir: base}).ResolveRoots([]string{"src", "README.md"})
	if err != nil {
		t.Fatalf("ResolveRoots returned error: %v", err)
	}

	want := []string{dir, file}
	if len(resolution.Roots) != len(want) {
		t.Fatalf("roots = %#v, want %#v", resolution.Roots, want)
	}
	for index := range want {
		if resolution.Roots[index] != want[index] {
			t.Fatalf("roots = %#v, want %#v", resolution.Roots, want)
		}
	}
	if len(resolution.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", resolution.Warnings)
	}
}

func TestResolveRootsDefaultsToWorkDir(t *testing.T) {
	base := t.TempDir()

	resolution, err := (RootResolver{WorkDir: base}).ResolveRoots(nil)
	if err != nil {
		t.Fatalf("ResolveRoots returned error: %v", err)
	}
	if len(resolution.Roots) != 1 || resolution.Roots[0] != base {
		t.Fatalf("roots = %#v, want work dir %q", resolution.Roots, base)
	}
}

func TestResolveRootsSkipsMissingRootsWithWarnings(t *testing.T) {
	base := t.TempDir()
	existing := filepath.Join(base, "src")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatalf("create existing root: %v", err)
	}
	missing := filepath.Join(base, "missing")

	resolution, err := (RootResolver{WorkDir: base}).ResolveRoots([]string{"src", "missing"})
	if err != nil {
		t.Fatalf("ResolveRoots returned error: %v", err)
	}
	if len(resolution.Roots) != 1 || resolution.Roots[0] != existing {
		t.Fatalf("roots = %#v, want only %q", resolution.Roots, existing)
	}
	if len(resolution.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want one missing-root warning", resolution.Warnings)
	}
	warning := resolution.Warnings[0]
	if warning.GetCode() != WarningRootMissing || !strings.Contains(warning.GetMessage(), missing) {
		t.Fatalf("warning = %#v, want missing-root diagnostic for %q", warning, missing)
	}
}

func TestResolveRootsAllMissingIsNoOpWithWarnings(t *testing.T) {
	base := t.TempDir()

	resolution, err := (RootResolver{WorkDir: base}).ResolveRoots([]string{"missing-a", "missing-b"})
	if err != nil {
		t.Fatalf("ResolveRoots returned error: %v", err)
	}
	if len(resolution.Roots) != 0 {
		t.Fatalf("roots = %#v, want no roots", resolution.Roots)
	}
	if len(resolution.Warnings) != 2 {
		t.Fatalf("warnings = %#v, want one warning per missing root", resolution.Warnings)
	}
}

func TestResolveRootsPropagatesNonMissingStatErrors(t *testing.T) {
	permissionErr := errors.New("permission denied")
	resolver := RootResolver{
		WorkDir: t.TempDir(),
		Stat: func(string) (os.FileInfo, error) {
			return nil, permissionErr
		},
	}

	_, err := resolver.ResolveRoots([]string{"src"})
	if !errors.Is(err, permissionErr) {
		t.Fatalf("ResolveRoots error = %v, want permission error", err)
	}
}
