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

// ResultOptions controls how ripgrep match paths are represented in structured
// recall results while open targets keep absolute local paths.
type ResultOptions struct {
	Roots []string
}

// MatchesToSearchResponse converts ripgrep content matches plus provider warnings into
// a recall SearchResponse. Each matching line becomes one result so repeated
// terms on the same line do not crowd out distinct search results.
func MatchesToSearchResponse(matches []Match, warnings []*searchv1.SearchResponse_Warning, options ResultOptions) *searchv1.SearchResponse {
	return SearchResponseFromRunResult(RunResult{Matches: matches}, warnings, options)
}

// SearchResponseFromRunResult maps one runner result into recall results and warnings.
func SearchResponseFromRunResult(result RunResult, warnings []*searchv1.SearchResponse_Warning, options ResultOptions) *searchv1.SearchResponse {
	results := PathResultsFromMatches(result.PathMatches, options)
	results = append(results, ResultsFromMatches(result.Matches, options)...)
	return &searchv1.SearchResponse{
		Results:  results,
		Warnings: cloneWarnings(warnings),
	}
}

// ResultsFromMatches converts parsed ripgrep match events into structured
// results grouped by source file for the default grouped renderer.
func ResultsFromMatches(matches []Match, options ResultOptions) []*searchv1.SearchResponse_Result {
	results := make([]*searchv1.SearchResponse_Result, 0, len(matches))
	for _, match := range matches {
		results = append(results, resultFromMatch(match, options))
	}
	return results
}

// PathResultsFromMatches converts path matches into openable file results grouped by directory.
func PathResultsFromMatches(matches []PathMatch, options ResultOptions) []*searchv1.SearchResponse_Result {
	results := make([]*searchv1.SearchResponse_Result, 0, len(matches))
	for _, match := range matches {
		results = append(results, pathResultFromMatch(match, options))
	}
	return results
}

func pathResultFromMatch(match PathMatch, options ResultOptions) *searchv1.SearchResponse_Result {
	absolutePath := absoluteMatchPath(match.Path, options.Roots)
	displayPath := displayMatchPath(absolutePath, options.Roots)
	displayDir := displayMatchPath(filepath.Dir(absolutePath), options.Roots)
	return &searchv1.SearchResponse_Result{
		Id:       fmt.Sprintf("file_name:%s", absolutePath),
		Selector: SelectorFileName,
		Fields: []*searchv1.SearchResponse_Result_Field{
			textField("name", filepath.Base(displayPath)),
			textField("path", displayPath),
			textField("directory", displayDir),
		},
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
		Format: resultFormat([]string{"name"}, []string{"name"}),
	}
}

func resultFromMatch(match Match, options ResultOptions) *searchv1.SearchResponse_Result {
	absolutePath := absoluteMatchPath(match.Path, options.Roots)
	displayPath := displayMatchPath(absolutePath, options.Roots)
	column := firstSubmatchColumn(match)
	lineNumber := match.LineNumber
	if lineNumber == 0 {
		lineNumber = 1
	}

	return &searchv1.SearchResponse_Result{
		Id:       fmt.Sprintf("file_content:%s:%d:%d", absolutePath, lineNumber, column),
		Selector: SelectorFileContent,
		Fields: []*searchv1.SearchResponse_Result_Field{
			textField("path", displayPath),
			integerField("line", int64(lineNumber)),
			integerField("column", int64(column)),
			textField("snippet", match.Line),
		},
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
		Format: resultFormat([]string{"line", "snippet"}, []string{"line", "snippet"}),
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

func textField(key string, value string) *searchv1.SearchResponse_Result_Field {
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Text{Text: value},
	}
}

func integerField(key string, value int64) *searchv1.SearchResponse_Result_Field {
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Integer{Integer: value},
	}
}

func resultFormat(titleFields []string, detailFields []string) *searchv1.SearchResponse_Result_Format {
	return &searchv1.SearchResponse_Result_Format{TitleFields: titleFields, DetailFields: detailFields}
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

func cloneWarnings(warnings []*searchv1.SearchResponse_Warning) []*searchv1.SearchResponse_Warning {
	cloned := make([]*searchv1.SearchResponse_Warning, 0, len(warnings))
	for _, warning := range warnings {
		if warning == nil {
			continue
		}
		cloned = append(cloned, proto.Clone(warning).(*searchv1.SearchResponse_Warning))
	}
	return cloned
}
