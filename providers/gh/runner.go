package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	defaultGitHubBinary = "gh"
	defaultAPILimit     = 100
	maxAPILimit         = 100
)

// Runner executes GitHub searches through the gh CLI without a shell.
type Runner struct {
	Binary string
}

// Search calls GitHub's REST search endpoints via gh api and parses one page of results.
func (runner Runner) Search(ctx context.Context, selector Selector, query string, limit int) ([]Item, error) {
	binary := strings.TrimSpace(runner.Binary)
	if binary == "" {
		binary = defaultGitHubBinary
	}
	args, err := BuildArgs(selector, query, limit)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("gh search %s failed: %w%s", selector, err, stderrSuffix(stderr.String()))
	}
	return ParseItems(stdout.Bytes())
}

// BuildArgs returns the gh argv for one search selector.
func BuildArgs(selector Selector, query string, limit int) ([]string, error) {
	endpoint, err := endpointForSelector(selector)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("github search query is required")
	}
	args := []string{"api", "-X", "GET", endpoint, "-f", "q=" + query, "-F", "per_page=" + strconv.Itoa(apiLimit(limit))}
	switch selector {
	case SelectorCode:
		args = append(args, "-H", "Accept: application/vnd.github.text-match+json")
	case SelectorCommit:
		args = append(args, "-H", "Accept: application/vnd.github+json")
	}
	return args, nil
}

func endpointForSelector(selector Selector) (string, error) {
	switch selector {
	case SelectorCode:
		return "search/code", nil
	case SelectorCommit:
		return "search/commits", nil
	case SelectorIssue, SelectorPR:
		return "search/issues", nil
	case SelectorRepo:
		return "search/repositories", nil
	default:
		return "", fmt.Errorf("unsupported github selector %q", selector)
	}
}

func apiLimit(limit int) int {
	if limit <= 0 {
		return defaultAPILimit
	}
	if limit > maxAPILimit {
		return maxAPILimit
	}
	return limit
}

// ParseItems decodes the JSON envelope returned by GitHub search APIs.
func ParseItems(data []byte) ([]Item, error) {
	var envelope struct {
		Items []Item `json:"items"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode gh search response: %w", err)
	}
	return envelope.Items, nil
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
