// Package config loads and validates recall's operator-owned provider registry.
package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	configv1 "github.com/solodov/recall/proto/recall/config/v1"

	"google.golang.org/protobuf/encoding/prototext"
)

const (
	configDirName  = "recall"
	configFileName = "config.txtpb"
)

var (
	providerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	envNamePattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// LoadedConfig pairs a validated registry with source locations discovered from
// the textproto file so terminal output can link sources back to their config.
type LoadedConfig struct {
	Config            *configv1.RecallConfig
	ProviderLocations map[string]Location
}

// Location identifies a 1-based file position in an operator-owned config.
type Location struct {
	Path   string
	Line   uint32
	Column uint32
}

// DefaultPath returns the operator-owned registry path in the XDG config
// hierarchy, falling back to $HOME/.config when XDG_CONFIG_HOME is unset.
func DefaultPath() (string, error) {
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, configDirName, configFileName), nil
	}

	home := os.Getenv("HOME")
	if home == "" {
		return "", errors.New("resolve recall config path: XDG_CONFIG_HOME and HOME are unset")
	}
	return filepath.Join(home, ".config", configDirName, configFileName), nil
}

// LoadDefault reads and validates the registry from the default XDG config path.
func LoadDefault() (*configv1.RecallConfig, error) {
	loaded, err := LoadDefaultWithLocations()
	if err != nil {
		return nil, err
	}
	return loaded.Config, nil
}

// LoadDefaultWithLocations reads the default registry and records provider block
// positions when the textproto layout can be mapped back to parsed providers.
func LoadDefaultWithLocations() (LoadedConfig, error) {
	path, err := DefaultPath()
	if err != nil {
		return LoadedConfig{}, err
	}
	return LoadFileWithLocations(path)
}

// LoadFile reads a textproto provider registry and validates it before use.
func LoadFile(path string) (*configv1.RecallConfig, error) {
	loaded, err := LoadFileWithLocations(path)
	if err != nil {
		return nil, err
	}
	return loaded.Config, nil
}

// LoadFileWithLocations reads a textproto provider registry and keeps provider
// block line numbers as best-effort UI metadata; config validity never depends
// on source-location discovery.
func LoadFileWithLocations(path string) (LoadedConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LoadedConfig{}, fmt.Errorf("recall config not found at %s; create %s before running recall", path, path)
		}
		return LoadedConfig{}, fmt.Errorf("read recall config %s: %w", path, err)
	}

	cfg := &configv1.RecallConfig{}
	unmarshalOptions := prototext.UnmarshalOptions{DiscardUnknown: false}
	if err := unmarshalOptions.Unmarshal(data, cfg); err != nil {
		return LoadedConfig{}, fmt.Errorf("parse recall config %s: %w", path, err)
	}
	if err := Validate(cfg); err != nil {
		return LoadedConfig{}, fmt.Errorf("validate recall config %s: %w", path, err)
	}

	locationPath := path
	if absolutePath, err := filepath.Abs(path); err == nil {
		locationPath = absolutePath
	}
	return LoadedConfig{
		Config:            cfg,
		ProviderLocations: locateProviderLocations(locationPath, data, cfg.GetProviders()),
	}, nil
}

// Validate enforces registry semantics that protobuf shape alone cannot encode.
func Validate(cfg *configv1.RecallConfig) error {
	if cfg == nil {
		return errors.New("recall config is nil")
	}

	var problems []error
	seenProviderIDs := make(map[string]struct{}, len(cfg.GetProviders()))
	for index, provider := range cfg.GetProviders() {
		location := fmt.Sprintf("providers[%d]", index)
		if provider == nil {
			problems = append(problems, fmt.Errorf("%s is nil", location))
			continue
		}

		id := provider.GetId()
		if id == "" {
			problems = append(problems, fmt.Errorf("%s.id is required", location))
		} else if !providerIDPattern.MatchString(id) {
			problems = append(problems, fmt.Errorf("%s.id %q must start with an ASCII letter or digit and contain only ASCII letters, digits, '.', '_', or '-'", location, id))
		} else if _, exists := seenProviderIDs[id]; exists {
			problems = append(problems, fmt.Errorf("%s.id %q is duplicated", location, id))
		} else {
			seenProviderIDs[id] = struct{}{}
		}

		if weight := provider.GetWeight(); weight <= 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
			problems = append(problems, fmt.Errorf("%s.weight must be positive", location))
		}
		if provider.GetTimeoutMs() == 0 {
			problems = append(problems, fmt.Errorf("%s.timeout_ms must be positive", location))
		}
		if provider.GetDefaultLimit() == 0 {
			problems = append(problems, fmt.Errorf("%s.default_limit must be positive", location))
		}

		transports := provider.GetTransports()
		if len(transports) == 0 {
			problems = append(problems, fmt.Errorf("%s.transports must contain at least one transport", location))
		}
		for transportIndex, transport := range transports {
			transportLocation := fmt.Sprintf("%s.transports[%d]", location, transportIndex)
			problems = append(problems, validateTransport(transportLocation, transport)...)
		}
	}

	seenOpenerIDs := make(map[string]struct{}, len(cfg.GetOpeners()))
	for index, opener := range cfg.GetOpeners() {
		location := fmt.Sprintf("openers[%d]", index)
		if opener == nil {
			problems = append(problems, fmt.Errorf("%s is nil", location))
			continue
		}
		problems = append(problems, validateOpener(location, opener, seenOpenerIDs)...)
	}

	return errors.Join(problems...)
}

func locateProviderLocations(path string, data []byte, providers []*configv1.Provider) map[string]Location {
	startLines := messageFieldStartLines(data, "providers")
	if len(startLines) == 0 {
		return nil
	}
	locations := make(map[string]Location, len(providers))
	for index, provider := range providers {
		if provider == nil || index >= len(startLines) {
			continue
		}
		id := strings.TrimSpace(provider.GetId())
		if id == "" {
			continue
		}
		locations[id] = Location{Path: path, Line: startLines[index], Column: 1}
	}
	return locations
}

func messageFieldStartLines(data []byte, field string) []uint32 {
	tokens := scanTextprotoTokens(data)
	lines := []uint32{}
	for index, token := range tokens {
		if token.value != field {
			continue
		}
		next := index + 1
		if next < len(tokens) && tokens[next].value == ":" {
			next++
		}
		if next < len(tokens) && (tokens[next].value == "{" || tokens[next].value == "<") {
			lines = append(lines, token.line)
		}
	}
	return lines
}

type textprotoToken struct {
	value string
	line  uint32
}

func scanTextprotoTokens(data []byte) []textprotoToken {
	tokens := []textprotoToken{}
	line := uint32(1)
	for index := 0; index < len(data); {
		char := data[index]
		switch {
		case char == '\n':
			line++
			index++
		case isTextprotoSpace(char):
			index++
		case char == '#':
			index = skipLineComment(data, index)
		case char == '/' && index+1 < len(data) && data[index+1] == '/':
			index = skipLineComment(data, index+2)
		case char == '/' && index+1 < len(data) && data[index+1] == '*':
			var nextLine uint32
			index, nextLine = skipBlockComment(data, index+2, line)
			line = nextLine
		case char == '"' || char == '\'':
			var nextLine uint32
			index, nextLine = skipQuotedString(data, index+1, char, line)
			line = nextLine
		case isTextprotoIdentStart(char):
			start := index
			startLine := line
			index++
			for index < len(data) && isTextprotoIdentPart(data[index]) {
				index++
			}
			tokens = append(tokens, textprotoToken{value: string(data[start:index]), line: startLine})
		case char == ':' || char == '{' || char == '}' || char == '<' || char == '>':
			tokens = append(tokens, textprotoToken{value: string(char), line: line})
			index++
		default:
			index++
		}
	}
	return tokens
}

func skipLineComment(data []byte, index int) int {
	for index < len(data) && data[index] != '\n' {
		index++
	}
	return index
}

func skipBlockComment(data []byte, index int, line uint32) (int, uint32) {
	for index < len(data) {
		if data[index] == '\n' {
			line++
			index++
			continue
		}
		if data[index] == '*' && index+1 < len(data) && data[index+1] == '/' {
			return index + 2, line
		}
		index++
	}
	return index, line
}

func skipQuotedString(data []byte, index int, quote byte, line uint32) (int, uint32) {
	escaped := false
	for index < len(data) {
		char := data[index]
		if char == '\n' {
			line++
		}
		index++
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == quote {
			return index, line
		}
	}
	return index, line
}

func isTextprotoSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\r' || char == '\f'
}

func isTextprotoIdentStart(char byte) bool {
	return (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || char == '_'
}

func isTextprotoIdentPart(char byte) bool {
	return isTextprotoIdentStart(char) || (char >= '0' && char <= '9')
}

func validateTransport(location string, transport *configv1.Transport) []error {
	if transport == nil {
		return []error{fmt.Errorf("%s is nil", location)}
	}

	switch transport := transport.GetTransport().(type) {
	case *configv1.Transport_Stdio:
		return validateStdioTransport(location, transport.Stdio)
	case *configv1.Transport_Grpc:
		return validateGrpcTransport(location, transport.Grpc)
	case nil:
		return []error{fmt.Errorf("%s must set exactly one of stdio or grpc", location)}
	default:
		return []error{fmt.Errorf("%s has unsupported type %T", location, transport)}
	}
}

func validateStdioTransport(location string, transport *configv1.StdioTransport) []error {
	if transport == nil {
		return []error{fmt.Errorf("%s.stdio is nil", location)}
	}

	var problems []error
	if strings.TrimSpace(transport.GetCommand()) == "" {
		problems = append(problems, fmt.Errorf("%s.stdio.command is required", location))
	}

	envKeys := make([]string, 0, len(transport.GetEnv()))
	for key := range transport.GetEnv() {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	for _, key := range envKeys {
		if key == "" {
			problems = append(problems, fmt.Errorf("%s.stdio.env contains an empty key", location))
			continue
		}
		if !envNamePattern.MatchString(key) {
			problems = append(problems, fmt.Errorf("%s.stdio.env[%q] is not a valid environment variable name", location, key))
		}
	}

	return problems
}

func validateGrpcTransport(location string, transport *configv1.GrpcTransport) []error {
	if transport == nil {
		return []error{fmt.Errorf("%s.grpc is nil", location)}
	}
	if strings.TrimSpace(transport.GetEndpoint()) == "" {
		return []error{fmt.Errorf("%s.grpc.endpoint is required", location)}
	}
	return nil
}

func validateOpener(location string, opener *configv1.Opener, seen map[string]struct{}) []error {
	var problems []error
	id := strings.TrimSpace(opener.GetId())
	if id == "" {
		problems = append(problems, fmt.Errorf("%s.id is required", location))
	} else if !providerIDPattern.MatchString(id) {
		problems = append(problems, fmt.Errorf("%s.id %q must start with an ASCII letter or digit and contain only ASCII letters, digits, '.', '_', or '-'", location, id))
	} else if _, exists := seen[id]; exists {
		problems = append(problems, fmt.Errorf("%s.id %q is duplicated", location, id))
	} else {
		seen[id] = struct{}{}
	}

	if strings.TrimSpace(opener.GetCommand()) == "" {
		problems = append(problems, fmt.Errorf("%s.command is required", location))
	}
	problems = append(problems, validateFilterList(location+".sources", opener.GetSources(), providerIDPattern)...)
	problems = append(problems, validateFilterList(location+".selectors", opener.GetSelectors(), nil)...)
	problems = append(problems, validateTargetTypes(location+".target_types", opener.GetTargetTypes())...)
	problems = append(problems, validateFilterList(location+".uri_schemes", opener.GetUriSchemes(), nil)...)
	return problems
}

func validateFilterList(location string, values []string, pattern *regexp.Regexp) []error {
	var problems []error
	seen := map[string]struct{}{}
	for index, value := range values {
		value = strings.TrimSpace(value)
		itemLocation := fmt.Sprintf("%s[%d]", location, index)
		if value == "" {
			problems = append(problems, fmt.Errorf("%s must be non-empty", itemLocation))
			continue
		}
		if pattern != nil && !pattern.MatchString(value) {
			problems = append(problems, fmt.Errorf("%s %q is invalid", itemLocation, value))
		}
		if _, exists := seen[value]; exists {
			problems = append(problems, fmt.Errorf("%s %q is duplicated", itemLocation, value))
		}
		seen[value] = struct{}{}
	}
	return problems
}

func validateTargetTypes(location string, values []string) []error {
	var problems []error
	for index, value := range values {
		value = strings.TrimSpace(value)
		switch value {
		case "file", "uri":
		case "":
			problems = append(problems, fmt.Errorf("%s[%d] must be non-empty", location, index))
		default:
			problems = append(problems, fmt.Errorf("%s[%d] %q must be file or uri", location, index, value))
		}
	}
	problems = append(problems, validateFilterList(location, values, nil)...)
	return problems
}
