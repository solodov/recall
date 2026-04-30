package ripgrep

import (
	"fmt"
	"path/filepath"
	"strings"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

const (
	// SelectorFileContent is emitted for ripgrep file content matches.
	SelectorFileContent = "file:content"
	// SelectorFileName is emitted for ripgrep file name/path matches.
	SelectorFileName = "file:name"
)

// HitOptions controls how ripgrep match paths are presented in recall hits.
type HitOptions struct {
	Roots []string
}

// MatchesToSearchResponse converts ripgrep content matches plus provider warnings into
// a recall SearchResponse. Each matching line becomes one hit so repeated terms
// on the same line do not crowd out distinct search results.
func MatchesToSearchResponse(matches []Match, warnings []*searchv1.Warning, options HitOptions) *searchv1.SearchResponse {
	return SearchResponseFromRunResult(RunResult{Matches: matches}, warnings, options)
}

// SearchResponseFromRunResult maps one runner result into recall hits and warnings.
func SearchResponseFromRunResult(result RunResult, warnings []*searchv1.Warning, options HitOptions) *searchv1.SearchResponse {
	hits := PathHitsFromMatches(result.PathMatches, options)
	hits = append(hits, HitsFromMatches(result.Matches, options)...)
	return &searchv1.SearchResponse{
		Hits:     hits,
		Warnings: cloneWarnings(warnings),
	}
}

// HitsFromMatches converts parsed ripgrep match events into recall-friendly
// hits grouped by source file for the default grouped renderer.
func HitsFromMatches(matches []Match, options HitOptions) []*searchv1.SearchHit {
	hits := make([]*searchv1.SearchHit, 0, len(matches))
	for _, match := range matches {
		hits = append(hits, hitFromMatch(match, options))
	}
	return hits
}

// PathHitsFromMatches converts path matches into openable file hits grouped by directory.
func PathHitsFromMatches(matches []PathMatch, options HitOptions) []*searchv1.SearchHit {
	hits := make([]*searchv1.SearchHit, 0, len(matches))
	for _, match := range matches {
		hits = append(hits, pathHitFromMatch(match, options))
	}
	return hits
}

func pathHitFromMatch(match PathMatch, options HitOptions) *searchv1.SearchHit {
	absolutePath := absoluteMatchPath(match.Path, options.Roots)
	displayPath := displayMatchPath(absolutePath, options.Roots)
	displayDir := displayMatchPath(filepath.Dir(absolutePath), options.Roots)
	return &searchv1.SearchHit{
		Id:       fmt.Sprintf("file_name:%s", absolutePath),
		Selector: SelectorFileName,
		Title:    filepath.Base(displayPath),
		Targets: []*searchv1.OpenTarget{
			fileTarget(absolutePath, 0, 0),
		},
		Group: &searchv1.SearchGroup{
			Key:   displayDir,
			Title: displayDir,
			Targets: []*searchv1.OpenTarget{
				fileTarget(filepath.Dir(absolutePath), 0, 0),
			},
		},
	}
}

func hitFromMatch(match Match, options HitOptions) *searchv1.SearchHit {
	absolutePath := absoluteMatchPath(match.Path, options.Roots)
	displayPath := displayMatchPath(absolutePath, options.Roots)
	column := firstSubmatchColumn(match)
	lineNumber := match.LineNumber
	if lineNumber == 0 {
		lineNumber = 1
	}

	return &searchv1.SearchHit{
		Id:       fmt.Sprintf("file_content:%s:%d:%d", absolutePath, lineNumber, column),
		Selector: SelectorFileContent,
		Title:    fmt.Sprintf("%s:%d:%d", displayPath, lineNumber, column),
		Snippet:  proto.String(match.Line),
		Targets: []*searchv1.OpenTarget{
			fileTarget(absolutePath, lineNumber, column),
		},
		Group: &searchv1.SearchGroup{
			Key:   displayPath,
			Title: displayPath,
			Targets: []*searchv1.OpenTarget{
				fileTarget(absolutePath, 0, 0),
			},
		},
	}
}

func firstSubmatchColumn(match Match) uint64 {
	if len(match.Submatches) == 0 {
		return 1
	}
	return match.Submatches[0].Start + 1
}

func absoluteMatchPath(path string, roots []string) string {
	path = filepath.Clean(path)
	if filepath.IsAbs(path) {
		return path
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		return filepath.Clean(filepath.Join(root, path))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absolute
}

func displayMatchPath(absolutePath string, roots []string) string {
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if rel, ok := relativePath(root, absolutePath); ok {
			return rel
		}
	}
	return filepath.ToSlash(absolutePath)
}

func relativePath(root string, path string) (string, bool) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(absoluteRoot, absolutePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	if relative == "." {
		relative = filepath.Base(absolutePath)
	}
	return filepath.ToSlash(relative), true
}

func fileTarget(path string, line uint64, column uint64) *searchv1.OpenTarget {
	target := &searchv1.FileTarget{Path: path}
	if line > 0 && line <= maxUint32 {
		target.Line = proto.Uint32(uint32(line))
		if column > 0 && column <= maxUint32 {
			target.Column = proto.Uint32(uint32(column))
		}
	}
	return &searchv1.OpenTarget{Target: &searchv1.OpenTarget_File{File: target}}
}

const maxUint32 = uint64(^uint32(0))

func cloneWarnings(warnings []*searchv1.Warning) []*searchv1.Warning {
	cloned := make([]*searchv1.Warning, 0, len(warnings))
	for _, warning := range warnings {
		if warning == nil {
			continue
		}
		cloned = append(cloned, proto.Clone(warning).(*searchv1.Warning))
	}
	return cloned
}
