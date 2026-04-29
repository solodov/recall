package ripgrep

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

const (
	// WarningRootMissing identifies a configured ripgrep root that does not exist.
	WarningRootMissing = "ripgrep_root_missing"
)

// RootResolver validates configured roots at search time so stale optional
// paths are skipped without failing the provider.
type RootResolver struct {
	WorkDir string
	Stat    func(string) (os.FileInfo, error)
}

// RootResolution contains existing roots to search and non-fatal diagnostics
// for configured roots that were skipped.
type RootResolution struct {
	Roots    []string
	Warnings []*searchv1.Warning
}

// ResolveRoots returns existing file or directory roots. Missing roots become
// warnings, and an empty configured root list defaults to the resolver work dir.
func (resolver RootResolver) ResolveRoots(roots []string) (RootResolution, error) {
	candidates := roots
	if len(candidates) == 0 {
		candidates = []string{"."}
	}

	stat := resolver.Stat
	if stat == nil {
		stat = os.Stat
	}

	var resolution RootResolution
	for _, root := range candidates {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}

		resolved, err := resolver.resolvePath(root)
		if err != nil {
			return RootResolution{}, err
		}
		if _, err := stat(resolved); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				resolution.Warnings = append(resolution.Warnings, missingRootWarning(resolved))
				continue
			}
			return RootResolution{}, fmt.Errorf("stat ripgrep root %q: %w", resolved, err)
		}
		resolution.Roots = append(resolution.Roots, resolved)
	}
	return resolution, nil
}

func (resolver RootResolver) resolvePath(root string) (string, error) {
	if filepath.IsAbs(root) {
		return filepath.Clean(root), nil
	}
	base := resolver.WorkDir
	if strings.TrimSpace(base) == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve ripgrep working directory: %w", err)
		}
	}
	return filepath.Abs(filepath.Join(base, root))
}

func missingRootWarning(root string) *searchv1.Warning {
	return &searchv1.Warning{
		Message: fmt.Sprintf("ripgrep root does not exist: %s", root),
		Code:    proto.String(WarningRootMissing),
	}
}
