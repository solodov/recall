package tui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/solodov/recall/internal/render"
)

const (
	defaultPreviewContextLines = 4
	defaultPreviewHeadLines    = 80
	defaultPreviewMaxBytes     = 256 * 1024
	maxPreviewLineBytes        = 256 * 1024
)

// PreviewOptions controls local preview cost and context around file matches.
type PreviewOptions struct {
	ContextLines int
	HeadLines    int
	MaxBytes     int64
}

// Preview is display-ready text for the selected result. Available is false
// when a result has no previewable target, which is a normal provider outcome.
type Preview struct {
	Text      string
	Available bool
}

// DefaultPreview renders file targets with nearby line context and reports other
// target kinds as unavailable until providers grow their own preview capability.
func DefaultPreview(ctx context.Context, summary render.ResultSummary, options PreviewOptions) (Preview, error) {
	if summary.Target == nil || summary.Target.GetFile() == nil {
		return Preview{Text: "No preview available for this result.", Available: false}, nil
	}
	fileTarget := summary.Target.GetFile()
	path := strings.TrimSpace(fileTarget.GetPath())
	if path == "" {
		return Preview{Text: "No preview available for this result.", Available: false}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return Preview{}, fmt.Errorf("open preview file: %w", err)
	}
	defer file.Close()

	options = previewOptionsWithDefaults(options)
	targetLine := int(fileTarget.GetLine())
	startLine, endLine := previewLineRange(targetLine, options)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxPreviewLineBytes)
	lines := []string{}
	lineNumber := 0
	var bytesRead int64
	truncated := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return Preview{}, err
		}
		lineNumber++
		text := strings.TrimRight(scanner.Text(), "\r")
		bytesRead += int64(len(text) + 1)
		if bytesRead > options.MaxBytes {
			truncated = true
			break
		}
		if lineNumber < startLine {
			continue
		}
		if lineNumber > endLine {
			break
		}
		lines = append(lines, formatPreviewLine(lineNumber, targetLine, text))
	}
	if err := scanner.Err(); err != nil {
		return Preview{}, fmt.Errorf("read preview file: %w", err)
	}
	if len(lines) == 0 {
		if truncated {
			return Preview{Text: "Preview exceeded the configured byte limit before the selected line.", Available: true}, nil
		}
		if targetLine > lineNumber {
			return Preview{Text: fmt.Sprintf("Line %d is beyond the end of the file.", targetLine), Available: true}, nil
		}
		return Preview{Text: "File is empty.", Available: true}, nil
	}
	if truncated {
		lines = append(lines, "… preview truncated")
	}
	return Preview{Text: strings.Join(lines, "\n"), Available: true}, nil
}

func previewOptionsWithDefaults(options PreviewOptions) PreviewOptions {
	if options.ContextLines <= 0 {
		options.ContextLines = defaultPreviewContextLines
	}
	if options.HeadLines <= 0 {
		options.HeadLines = defaultPreviewHeadLines
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = defaultPreviewMaxBytes
	}
	return options
}

func previewLineRange(targetLine int, options PreviewOptions) (int, int) {
	if targetLine <= 0 {
		return 1, options.HeadLines
	}
	startLine := targetLine - options.ContextLines
	if startLine < 1 {
		startLine = 1
	}
	return startLine, targetLine + options.ContextLines
}

func formatPreviewLine(lineNumber int, targetLine int, text string) string {
	marker := " "
	if targetLine > 0 && lineNumber == targetLine {
		marker = ">"
	}
	return fmt.Sprintf("%s %5d │ %s", marker, lineNumber, text)
}
