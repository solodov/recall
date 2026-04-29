package ripgrep

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

const defaultRipgrepBinary = "rg"

// RunOptions describes one ripgrep invocation after query parsing and root
// resolution have already completed.
type RunOptions struct {
	Pattern        string
	Roots          []string
	FileTypes      []string
	ExcludedScopes []Scope
	Limit          int
}

// RunResult contains structured ripgrep match events and non-fatal diagnostics.
type RunResult struct {
	Matches  []Match
	Warnings []*searchv1.Warning
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

// Runner executes ripgrep without a shell and parses its JSON output.
type Runner struct {
	Binary string
}

// BuildArgs returns the ripgrep argv for one search. It is separated from Run so
// query-to-ripgrep translation can be tested without launching a process.
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
	typeArgs, err := FileTypeArgs(options.FileTypes)
	if err != nil {
		return nil, err
	}
	args = append(args, typeArgs...)

	globArgs, err := ExcludedScopeGlobArgs(options.ExcludedScopes)
	if err != nil {
		return nil, err
	}
	args = append(args, globArgs...)
	args = append(args, pattern)
	args = append(args, options.Roots...)
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

// Run executes ripgrep and returns parsed JSON match events. Exit code 1 means
// ripgrep found no matches and is reported as an empty successful result.
func (runner Runner) Run(ctx context.Context, options RunOptions) (RunResult, error) {
	args, err := BuildArgs(options)
	if err != nil {
		return RunResult{}, err
	}
	binary := strings.TrimSpace(runner.Binary)
	if binary == "" {
		binary = defaultRipgrepBinary
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

func missingPathWarnings(stderr string) ([]*searchv1.Warning, bool) {
	var warnings []*searchv1.Warning
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !isMissingPathDiagnostic(line) {
			return nil, false
		}
		warnings = append(warnings, &searchv1.Warning{
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
