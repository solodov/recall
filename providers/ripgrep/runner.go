package ripgrep

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

const (
	defaultRipgrepBinary = "rg"
	maxRipgrepArgBytes   = 64 * 1024
)

// RunOptions describes one ripgrep invocation after query parsing and root
// resolution have already completed.
type RunOptions struct {
	Pattern     string
	Roots       []string
	Selectors   []SearchSelector
	FileTypes   []string
	PathFilters []PathFilter
	Limit       int
}

// RunResult contains structured ripgrep match events and non-fatal diagnostics.
type RunResult struct {
	Matches     []Match
	PathMatches []PathMatch
	Warnings    []*searchv1.SearchResponse_Warning
}

// Match is the provider-owned representation of one ripgrep JSON match event.
type Match struct {
	Path       string
	LineNumber uint64
	Line       string
	Submatches []Submatch
}

// Submatch identifies a matched byte range within Match.Line.
type Submatch struct {
	Text  string
	Start uint64
	End   uint64
}

// PathMatch identifies a file path whose root-relative path matched a path search.
type PathMatch struct {
	Path string
}

// Runner executes ripgrep without a shell and parses its output.
type Runner struct {
	Binary string
}

// BuildArgs returns the ripgrep argv for one content search. It is separated
// from Run so query-to-ripgrep translation can be tested without launching a process.
func BuildArgs(options RunOptions) ([]string, error) {
	pattern := strings.TrimSpace(options.Pattern)
	if pattern == "" {
		return nil, errors.New("ripgrep pattern is required")
	}
	if len(options.Roots) == 0 {
		return nil, errors.New("at least one ripgrep root is required")
	}

	args := []string{
		"--json",
		"--fixed-strings",
		"--line-number",
		"--column",
		"--with-filename",
		"--color=never",
		"--no-follow",
	}
	args, err := appendSharedFileSelectionArgs(args, options)
	if err != nil {
		return nil, err
	}
	args = append(args, pattern)
	args = append(args, options.Roots...)
	return args, nil
}

// BuildFilesArgs returns the ripgrep argv for listing searchable files.
func BuildFilesArgs(options RunOptions) ([]string, error) {
	if len(options.Roots) == 0 {
		return nil, errors.New("at least one ripgrep root is required")
	}
	args := []string{"--files", "--null", "--color=never", "--no-follow"}
	args, err := appendSharedFileSelectionArgs(args, options)
	if err != nil {
		return nil, err
	}
	args = append(args, options.Roots...)
	return args, nil
}

func appendSharedFileSelectionArgs(args []string, options RunOptions) ([]string, error) {
	typeArgs, err := FileTypeArgs(options.FileTypes)
	if err != nil {
		return nil, err
	}
	args = append(args, typeArgs...)
	return args, nil
}

// FileTypeArgs translates provider query type filters into ripgrep --type
// arguments. Duplicate types are ignored so repeated query terms do not change
// provider behavior.
func FileTypeArgs(fileTypes []string) ([]string, error) {
	seen := make(map[string]bool, len(fileTypes))
	args := []string{}
	for _, value := range fileTypes {
		fileType := strings.TrimSpace(value)
		if fileType == "" {
			return nil, errors.New("ripgrep file type is required")
		}
		if seen[fileType] {
			continue
		}
		seen[fileType] = true
		args = append(args, "--type", fileType)
	}
	return args, nil
}

// Run executes ripgrep and returns parsed content and path match events. Exit
// code 1 from content search means no matches and is reported as success.
func (runner Runner) Run(ctx context.Context, options RunOptions) (RunResult, error) {
	binary := strings.TrimSpace(runner.Binary)
	if binary == "" {
		binary = defaultRipgrepBinary
	}
	selectors := options.Selectors
	if len(selectors) == 0 {
		selectors = []SearchSelector{SearchSelectorFileContent}
	}

	var result RunResult
	var contentFiles []string
	var haveContentFiles bool
	if containsSearchSelector(selectors, SearchSelectorFileName) || hasIncludePathFilter(options.PathFilters) {
		files, warnings, err := runner.listFiles(ctx, binary, options)
		if err != nil {
			return RunResult{}, err
		}
		result.Warnings = append(result.Warnings, warnings...)
		if containsSearchSelector(selectors, SearchSelectorFileName) {
			pathFiles, err := filterPaths(files, options, true)
			if err != nil {
				return RunResult{}, err
			}
			pathLimit := remainingLimit(options.Limit, len(result.PathMatches)+len(result.Matches))
			result.PathMatches = append(result.PathMatches, pathMatches(pathFiles, pathLimit)...)
		}
		if hasIncludePathFilter(options.PathFilters) {
			contentFiles, err = filterPaths(files, options, false)
			if err != nil {
				return RunResult{}, err
			}
			haveContentFiles = true
		}
	}

	if containsSearchSelector(selectors, SearchSelectorFileContent) && !limitReached(options.Limit, result) {
		contentOptions := options
		contentOptions.Selectors = []SearchSelector{SearchSelectorFileContent}
		contentOptions.Limit = remainingLimit(options.Limit, len(result.PathMatches))
		if haveContentFiles {
			contentOptions.Roots = contentFiles
			if len(contentOptions.Roots) == 0 {
				return result, nil
			}
		}
		contentResult, err := runner.runContent(ctx, binary, contentOptions)
		if err != nil {
			return RunResult{}, err
		}
		contentResult.Matches, err = filterContentMatches(contentResult.Matches, options)
		if err != nil {
			return RunResult{}, err
		}
		result.Matches = append(result.Matches, contentResult.Matches...)
		result.Warnings = append(result.Warnings, contentResult.Warnings...)
	}
	return result, nil
}

func (runner Runner) listFiles(ctx context.Context, binary string, options RunOptions) ([]string, []*searchv1.SearchResponse_Warning, error) {
	args, err := BuildFilesArgs(options)
	if err != nil {
		return nil, nil, err
	}
	stdout, stderr, err := runBuffered(ctx, binary, args)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}
		if warnings, ok := missingPathWarnings(stderr); ok {
			return parseNullSeparatedPaths(stdout), warnings, nil
		}
		return nil, nil, fmt.Errorf("ripgrep --files failed: %w%s", err, stderrSuffix(stderr))
	}
	return parseNullSeparatedPaths(stdout), nil, nil
}

func (runner Runner) runContent(ctx context.Context, binary string, options RunOptions) (RunResult, error) {
	chunks, err := splitContentOptions(options)
	if err != nil {
		return RunResult{}, err
	}
	var result RunResult
	for _, chunk := range chunks {
		chunk.Limit = remainingLimit(options.Limit, len(result.Matches))
		chunkResult, err := runner.runContentChunk(ctx, binary, chunk)
		if err != nil {
			return RunResult{}, err
		}
		result.Matches = append(result.Matches, chunkResult.Matches...)
		result.Warnings = append(result.Warnings, chunkResult.Warnings...)
		if limitReached(options.Limit, result) {
			break
		}
	}
	return result, nil
}

func splitContentOptions(options RunOptions) ([]RunOptions, error) {
	if len(options.Roots) == 0 {
		return nil, errors.New("at least one ripgrep root is required")
	}
	var chunks []RunOptions
	chunk := options
	chunk.Roots = nil
	for _, root := range options.Roots {
		candidate := chunk
		candidate.Roots = append(append([]string{}, chunk.Roots...), root)
		args, err := BuildArgs(candidate)
		if err != nil {
			return nil, err
		}
		if len(chunk.Roots) > 0 && estimateArgBytes(args) > maxRipgrepArgBytes {
			chunks = append(chunks, chunk)
			chunk = options
			chunk.Roots = []string{root}
			continue
		}
		chunk.Roots = candidate.Roots
	}
	if len(chunk.Roots) > 0 {
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

func estimateArgBytes(args []string) int {
	total := 0
	for _, arg := range args {
		total += len(arg) + 1
	}
	return total
}

func (runner Runner) runContentChunk(ctx context.Context, binary string, options RunOptions) (RunResult, error) {
	args, err := BuildArgs(options)
	if err != nil {
		return RunResult{}, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("open ripgrep stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return RunResult{}, fmt.Errorf("start ripgrep: %w", err)
	}
	matches, limitReached, parseErr := parseMatches(stdout, options.Limit, cancel)
	waitErr := cmd.Wait()
	if parseErr != nil {
		return RunResult{}, parseErr
	}
	if waitErr != nil {
		if limitReached {
			return RunResult{Matches: matches}, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return RunResult{}, ctxErr
		}
		if exitCode(waitErr) == 1 {
			return RunResult{Matches: nil}, nil
		}
		if warnings, ok := missingPathWarnings(stderr.String()); ok {
			return RunResult{Matches: matches, Warnings: warnings}, nil
		}
		return RunResult{}, fmt.Errorf("ripgrep failed: %w%s", waitErr, stderrSuffix(stderr.String()))
	}
	return RunResult{Matches: matches}, nil
}

func runBuffered(ctx context.Context, binary string, args []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func parseNullSeparatedPaths(stdout string) []string {
	parts := strings.Split(stdout, "\x00")
	paths := make([]string, 0, len(parts))
	for _, path := range parts {
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func filterPaths(paths []string, options RunOptions, applyPattern bool) ([]string, error) {
	filters, err := compilePathFilters(options.PathFilters)
	if err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		absolutePath := absoluteMatchPath(path, options.Roots)
		displayPath := displayMatchPath(absolutePath, options.Roots)
		if applyPattern && !pathContainsPattern(displayPath, options.Pattern) {
			continue
		}
		if !matchesPathFilters(displayPath, filters) {
			continue
		}
		cleaned := filepath.Clean(absolutePath)
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		filtered = append(filtered, cleaned)
	}
	return filtered, nil
}

type compiledPathFilter struct {
	include bool
	pattern *regexp.Regexp
}

func compilePathFilters(filters []PathFilter) ([]compiledPathFilter, error) {
	compiled := make([]compiledPathFilter, 0, len(filters))
	for _, filter := range filters {
		pattern, err := compilePathPattern(filter.Pattern)
		if err != nil {
			return nil, fmt.Errorf("compile path filter %q: %w", filter.Pattern, err)
		}
		compiled = append(compiled, compiledPathFilter{include: filter.Include, pattern: pattern})
	}
	return compiled, nil
}

func pathContainsPattern(path string, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true
	}
	return strings.Contains(strings.ToLower(path), strings.ToLower(pattern))
}

func matchesPathFilters(path string, filters []compiledPathFilter) bool {
	for _, filter := range filters {
		matched := filter.pattern.MatchString(path)
		if filter.include && !matched {
			return false
		}
		if !filter.include && matched {
			return false
		}
	}
	return true
}

func filterContentMatches(matches []Match, options RunOptions) ([]Match, error) {
	if len(options.PathFilters) == 0 {
		return matches, nil
	}
	filters, err := compilePathFilters(options.PathFilters)
	if err != nil {
		return nil, err
	}
	filtered := make([]Match, 0, len(matches))
	for _, match := range matches {
		absolutePath := absoluteMatchPath(match.Path, options.Roots)
		displayPath := displayMatchPath(absolutePath, options.Roots)
		if matchesPathFilters(displayPath, filters) {
			filtered = append(filtered, match)
		}
	}
	return filtered, nil
}

func pathMatches(paths []string, limit int) []PathMatch {
	matches := make([]PathMatch, 0, len(paths))
	for _, path := range paths {
		if limit > 0 && len(matches) >= limit {
			break
		}
		matches = append(matches, PathMatch{Path: path})
	}
	return matches
}

func remainingLimit(limit int, used int) int {
	if limit <= 0 {
		return 0
	}
	remaining := limit - used
	if remaining < 0 {
		return 0
	}
	return remaining
}

func limitReached(limit int, result RunResult) bool {
	return limit > 0 && len(result.PathMatches)+len(result.Matches) >= limit
}

func parseMatches(reader io.Reader, limit int, onLimit func()) ([]Match, bool, error) {
	decoder := json.NewDecoder(reader)
	matches := []Match{}
	for {
		var event ripgrepEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				return matches, false, nil
			}
			return nil, false, fmt.Errorf("decode ripgrep JSON: %w", err)
		}
		if event.Type != "match" {
			continue
		}
		match, err := event.toMatch()
		if err != nil {
			return nil, false, err
		}
		matches = append(matches, match)
		if limit > 0 && len(matches) >= limit {
			if onLimit != nil {
				onLimit()
			}
			return matches, true, nil
		}
	}
}

type ripgrepEvent struct {
	Type string           `json:"type"`
	Data ripgrepEventData `json:"data"`
}

type ripgrepEventData struct {
	Path       ripgrepText  `json:"path"`
	Lines      ripgrepText  `json:"lines"`
	LineNumber uint64       `json:"line_number"`
	Submatches []rgSubmatch `json:"submatches"`
}

type rgSubmatch struct {
	Match ripgrepText `json:"match"`
	Start uint64      `json:"start"`
	End   uint64      `json:"end"`
}

type ripgrepText struct {
	Text string `json:"text"`
}

func (event ripgrepEvent) toMatch() (Match, error) {
	if event.Data.Path.Text == "" {
		return Match{}, errors.New("ripgrep match event missing path text")
	}
	match := Match{
		Path:       event.Data.Path.Text,
		LineNumber: event.Data.LineNumber,
		Line:       strings.TrimRight(event.Data.Lines.Text, "\r\n"),
		Submatches: make([]Submatch, 0, len(event.Data.Submatches)),
	}
	for _, submatch := range event.Data.Submatches {
		match.Submatches = append(match.Submatches, Submatch{
			Text:  submatch.Match.Text,
			Start: submatch.Start,
			End:   submatch.End,
		})
	}
	return match, nil
}

func missingPathWarnings(stderr string) ([]*searchv1.SearchResponse_Warning, bool) {
	var warnings []*searchv1.SearchResponse_Warning
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !isMissingPathDiagnostic(line) {
			return nil, false
		}
		warnings = append(warnings, &searchv1.SearchResponse_Warning{
			Message: line,
			Code:    proto.String(WarningPathMissing),
		})
	}
	return warnings, len(warnings) > 0
}

func isMissingPathDiagnostic(line string) bool {
	return strings.HasPrefix(line, "rg: ") && strings.Contains(line, ": No such file or directory (os error 2)")
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func stderrSuffix(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	if len(stderr) > 4096 {
		stderr = stderr[:4096] + "…"
	}
	return "; stderr: " + stderr
}
