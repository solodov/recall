// Package openers dispatches recall:// open targets to operator-configured commands.
package openers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	configv1 "github.com/solodov/recall/proto/recall/config/v1"
)

const (
	RecallScheme = "recall"
	OpenHost     = "open"

	TargetTypeFile = "file"
	TargetTypeURI  = "uri"
)

var placeholderPattern = regexp.MustCompile(`\{([A-Za-z0-9_.-]+)\}`)

// Target is the typed open payload carried by a recall:// URL.
type Target struct {
	Source string
	Kind   string
	Type   string

	URI       string
	URIScheme string
	Path      string

	Line      uint32
	HasLine   bool
	Column    uint32
	HasColumn bool
}

// Runner executes one local opener command without a shell.
type Runner func(context.Context, string, []string) error

// Options controls opener dispatch side effects for production and tests.
type Options struct {
	Runner          Runner
	FallbackCommand string
}

// ParseRecallURL decodes the recall:// URL emitted by terminal human output.
func ParseRecallURL(raw string) (Target, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Target{}, fmt.Errorf("parse recall open URL: %w", err)
	}
	if parsed.Scheme != RecallScheme {
		return Target{}, fmt.Errorf("open URL scheme %q is not %q", parsed.Scheme, RecallScheme)
	}
	if parsed.Host != OpenHost {
		return Target{}, fmt.Errorf("open URL host %q is not %q", parsed.Host, OpenHost)
	}

	query := parsed.Query()
	if version := query.Get("v"); version != "" && version != "1" {
		return Target{}, fmt.Errorf("unsupported recall open URL version %q", version)
	}
	target := Target{
		Source: strings.TrimSpace(query.Get("source")),
		Kind:   strings.TrimSpace(query.Get("kind")),
		Type:   strings.TrimSpace(query.Get("type")),
	}
	switch target.Type {
	case TargetTypeFile:
		target.Path = strings.TrimSpace(query.Get("path"))
		if target.Path == "" {
			return Target{}, errors.New("recall file open URL requires path")
		}
		if err := parseLocation(query, &target); err != nil {
			return Target{}, err
		}
	case TargetTypeURI:
		target.URI = strings.TrimSpace(query.Get("uri"))
		if target.URI == "" {
			return Target{}, errors.New("recall URI open URL requires uri")
		}
		uri, err := url.Parse(target.URI)
		if err != nil {
			return Target{}, fmt.Errorf("parse original URI: %w", err)
		}
		if uri.Scheme == "" {
			return Target{}, errors.New("original URI requires scheme")
		}
		target.URIScheme = uri.Scheme
	default:
		return Target{}, fmt.Errorf("unsupported recall open target type %q", target.Type)
	}
	return target, nil
}

// Open chooses a configured opener for targetURL and executes it. If no opener
// matches, it falls back to the platform open command with the original target.
func Open(ctx context.Context, cfg *configv1.RecallConfig, targetURL string, options Options) error {
	target, err := ParseRecallURL(targetURL)
	if err != nil {
		return err
	}
	runner := options.Runner
	if runner == nil {
		runner = runCommand
	}

	for _, opener := range cfg.GetOpeners() {
		if !matchesOpener(opener, target) {
			continue
		}
		args, ok, err := expandArgs(opener.GetArgs(), target)
		if err != nil {
			return fmt.Errorf("opener %q: %w", opener.GetId(), err)
		}
		if !ok {
			continue
		}
		command := strings.TrimSpace(opener.GetCommand())
		if command == "" {
			return fmt.Errorf("opener %q command is empty", opener.GetId())
		}
		return runner(ctx, command, args)
	}

	command, args := fallbackInvocation(target, options)
	return runner(ctx, command, args)
}

func parseLocation(query url.Values, target *Target) error {
	line, hasLine, err := parseOptionalUint32(query.Get("line"), "line")
	if err != nil {
		return err
	}
	column, hasColumn, err := parseOptionalUint32(query.Get("column"), "column")
	if err != nil {
		return err
	}
	if hasColumn && !hasLine {
		return errors.New("recall file open URL column requires line")
	}
	target.Line = line
	target.HasLine = hasLine
	target.Column = column
	target.HasColumn = hasColumn
	return nil
}

func parseOptionalUint32(value string, name string) (uint32, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return 0, false, fmt.Errorf("%s must be a positive uint32", name)
	}
	return uint32(parsed), true, nil
}

func matchesOpener(opener *configv1.Opener, target Target) bool {
	if opener == nil {
		return false
	}
	return matchesFilter(opener.GetSources(), target.Source) &&
		matchesFilter(opener.GetKinds(), target.Kind) &&
		matchesFilter(opener.GetTargetTypes(), target.Type) &&
		matchesFilter(opener.GetUriSchemes(), target.URIScheme)
}

func matchesFilter(values []string, actual string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if strings.TrimSpace(value) == actual {
			return true
		}
	}
	return false
}

func expandArgs(templates []string, target Target) ([]string, bool, error) {
	args := make([]string, 0, len(templates))
	for _, template := range templates {
		arg, ok, err := expandTemplate(template, target)
		if err != nil || !ok {
			return nil, ok, err
		}
		args = append(args, arg)
	}
	return args, true, nil
}

func expandTemplate(template string, target Target) (string, bool, error) {
	missing := false
	var invalid error
	expanded := placeholderPattern.ReplaceAllStringFunc(template, func(match string) string {
		if invalid != nil {
			return match
		}
		name := match[1 : len(match)-1]
		value, ok, err := placeholderValue(name, target)
		if err != nil {
			invalid = err
			return match
		}
		if !ok {
			missing = true
			return match
		}
		return value
	})
	if invalid != nil {
		return "", false, invalid
	}
	if missing {
		return "", false, nil
	}
	return expanded, true, nil
}

func placeholderValue(name string, target Target) (string, bool, error) {
	switch name {
	case "source":
		return optionalValue(target.Source)
	case "kind":
		return optionalValue(target.Kind)
	case "type":
		return target.Type, true, nil
	case "scheme":
		return optionalValue(target.URIScheme)
	case "uri":
		return optionalValue(target.URI)
	case "path":
		return optionalValue(target.Path)
	case "line":
		if !target.HasLine {
			return "", false, nil
		}
		return strconv.FormatUint(uint64(target.Line), 10), true, nil
	case "column":
		if !target.HasColumn {
			return "", false, nil
		}
		return strconv.FormatUint(uint64(target.Column), 10), true, nil
	default:
		return "", false, fmt.Errorf("unknown placeholder {%s}", name)
	}
}

func optionalValue(value string) (string, bool, error) {
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

func fallbackInvocation(target Target, options Options) (string, []string) {
	value := fallbackValue(target)
	if command := strings.TrimSpace(options.FallbackCommand); command != "" {
		return command, []string{value}
	}
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{value}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", value}
	default:
		return "xdg-open", []string{value}
	}
}

func fallbackValue(target Target) string {
	if target.Type == TargetTypeFile {
		return target.Path
	}
	return target.URI
}

func runCommand(ctx context.Context, command string, args []string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
