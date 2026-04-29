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
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return LoadFile(path)
}

// LoadFile reads a textproto provider registry and validates it before use.
func LoadFile(path string) (*configv1.RecallConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("recall config not found at %s; create %s before running recall", path, path)
		}
		return nil, fmt.Errorf("read recall config %s: %w", path, err)
	}

	cfg := &configv1.RecallConfig{}
	unmarshalOptions := prototext.UnmarshalOptions{DiscardUnknown: false}
	if err := unmarshalOptions.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse recall config %s: %w", path, err)
	}
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validate recall config %s: %w", path, err)
	}
	return cfg, nil
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

		switch transport := provider.GetTransport().(type) {
		case *configv1.Provider_Stdio:
			problems = append(problems, validateStdioTransport(location, transport.Stdio)...)
		case *configv1.Provider_Grpc:
			problems = append(problems, validateGrpcTransport(location, transport.Grpc)...)
		case nil:
			problems = append(problems, fmt.Errorf("%s.transport must set exactly one of stdio or grpc", location))
		default:
			problems = append(problems, fmt.Errorf("%s.transport has unsupported type %T", location, transport))
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
	problems = append(problems, validateFilterList(location+".kinds", opener.GetKinds(), nil)...)
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
