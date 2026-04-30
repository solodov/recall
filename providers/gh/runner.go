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
func (runner Runner) Search(ctx context.Context, domain Domain, query string, limit int) ([]Item, error) {
	binary := strings.TrimSpace(runner.Binary)
	if binary == "" {
		binary = defaultGitHubBinary
	}
	args, err := BuildArgs(domain, query, limit)
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
		return nil, fmt.Errorf("gh search %s failed: %w%s", domain, err, stderrSuffix(stderr.String()))
	}
	return ParseItems(stdout.Bytes())
}

// BuildArgs returns the gh argv for one search domain.
func BuildArgs(domain Domain, query string, limit int) ([]string, error) {
	endpoint, err := endpointForDomain(domain)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("github search query is required")
	}
	args := []string{"api", "-X", "GET", endpoint, "-f", "q=" + query, "-F", "per_page=" + strconv.Itoa(apiLimit(limit))}
	switch domain {
	case DomainCode:
		args = append(args, "-H", "Accept: application/vnd.github.text-match+json")
	case DomainCommit:
		args = append(args, "-H", "Accept: application/vnd.github+json")
	}
	return args, nil
}

func endpointForDomain(domain Domain) (string, error) {
	switch domain {
	case DomainCode:
		return "search/code", nil
	case DomainCommit:
		return "search/commits", nil
	case DomainIssue, DomainPR:
		return "search/issues", nil
	case DomainRepo:
		return "search/repositories", nil
	default:
		return "", fmt.Errorf("unsupported github search domain %q", domain)
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
