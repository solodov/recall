// Package normalize validates provider SearchResponse payloads and attaches
// recall-owned metadata before ranking or rendering.
package normalize

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strings"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

// ProviderResponse is a provider response after semantic validation. Results
// and warnings carry recall's configured provider ID because source identity is
// not provider-owned data.
type ProviderResponse struct {
	ProviderID string
	Results    []Result
	Warnings   []Warning

	// Raw preserves the validated provider payload for future machine-readable
	// renderers without making ranking or rendering depend on unannotated data.
	Raw *searchv1.SearchResponse
}

// Result is one validated result annotated with its source provider and local rank.
type Result struct {
	ProviderID   string
	ProviderRank int
	Result       *searchv1.SearchResponse_Result
}

// Warning is one validated non-fatal diagnostic annotated with its source.
type Warning struct {
	ProviderID string
	Warning    *searchv1.SearchResponse_Warning
}

// SearchResponse validates response semantics that protobuf cannot express and
// returns annotated copies of results and warnings.
func SearchResponse(providerID string, response *searchv1.SearchResponse) (ProviderResponse, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return ProviderResponse{}, errors.New("provider id is required")
	}
	if response == nil {
		return ProviderResponse{}, errors.New("response is nil")
	}

	var problems []error
	for index, result := range response.GetResults() {
		problems = append(problems, validateResult(fmt.Sprintf("results[%d]", index), result)...)
	}
	for index, warning := range response.GetWarnings() {
		problems = append(problems, validateWarning(fmt.Sprintf("warnings[%d]", index), warning)...)
	}
	if err := errors.Join(problems...); err != nil {
		return ProviderResponse{}, err
	}

	normalized := ProviderResponse{
		ProviderID: providerID,
		Results:    make([]Result, 0, len(response.GetResults())),
		Warnings:   make([]Warning, 0, len(response.GetWarnings())),
		Raw:        proto.Clone(response).(*searchv1.SearchResponse),
	}
	for index, result := range response.GetResults() {
		normalized.Results = append(normalized.Results, Result{
			ProviderID:   providerID,
			ProviderRank: index + 1,
			Result:       proto.Clone(result).(*searchv1.SearchResponse_Result),
		})
	}
	for _, warning := range response.GetWarnings() {
		normalized.Warnings = append(normalized.Warnings, Warning{
			ProviderID: providerID,
			Warning:    proto.Clone(warning).(*searchv1.SearchResponse_Warning),
		})
	}
	return normalized, nil
}

// FilterSelectors keeps only results matching one of the provider-local selector
// filters. An empty filter returns response unchanged because a provider-only
// selector searches every surface from that provider.
func FilterSelectors(response ProviderResponse, selectors []string) ProviderResponse {
	if len(selectors) == 0 {
		return response
	}
	filtered := ProviderResponse{
		ProviderID: response.ProviderID,
		Results:    make([]Result, 0, len(response.Results)),
		Warnings:   append([]Warning{}, response.Warnings...),
	}
	filtered.Raw = &searchv1.SearchResponse{Warnings: make([]*searchv1.SearchResponse_Warning, 0, len(response.Warnings))}
	for _, result := range response.Results {
		if result.Result == nil || !matchesSelectorFilter(result.Result.GetSelector(), selectors) {
			continue
		}
		filtered.Results = append(filtered.Results, result)
		filtered.Raw.Results = append(filtered.Raw.Results, proto.Clone(result.Result).(*searchv1.SearchResponse_Result))
	}
	for _, warning := range response.Warnings {
		if warning.Warning != nil {
			filtered.Raw.Warnings = append(filtered.Raw.Warnings, proto.Clone(warning.Warning).(*searchv1.SearchResponse_Warning))
		}
	}
	return filtered
}

func matchesSelectorFilter(selector string, filters []string) bool {
	selector = strings.TrimSpace(selector)
	for _, filter := range filters {
		filter = strings.TrimSpace(filter)
		if filter == "" {
			continue
		}
		if selector == filter || strings.HasPrefix(selector, filter+":") {
			return true
		}
	}
	return false
}

func validateResult(location string, result *searchv1.SearchResponse_Result) []error {
	if result == nil {
		return []error{fmt.Errorf("%s is nil", location)}
	}

	var problems []error
	if strings.TrimSpace(result.GetId()) == "" {
		problems = append(problems, fmt.Errorf("%s.id is required", location))
	}
	if strings.TrimSpace(result.GetSelector()) == "" {
		problems = append(problems, fmt.Errorf("%s.selector is required", location))
	}
	if result.Score != nil {
		score := result.GetScore()
		if math.IsNaN(score) || math.IsInf(score, 0) {
			problems = append(problems, fmt.Errorf("%s.score must be finite", location))
		}
	}
	problems = append(problems, validateFields(location+".fields", result.GetFields())...)
	for index, target := range result.GetTargets() {
		problems = append(problems, validateOpenTarget(fmt.Sprintf("%s.targets[%d]", location, index), target)...)
	}
	if group := result.GetGroup(); group != nil {
		problems = append(problems, validateGroup(location+".group", group)...)
	}
	return problems
}

func validateFields(location string, fields []*searchv1.SearchResponse_Result_Field) []error {
	if len(fields) == 0 {
		return []error{fmt.Errorf("%s must contain at least one field", location)}
	}
	var problems []error
	seen := map[string]int{}
	for index, field := range fields {
		fieldLocation := fmt.Sprintf("%s[%d]", location, index)
		if field == nil {
			problems = append(problems, fmt.Errorf("%s is nil", fieldLocation))
			continue
		}
		key := strings.TrimSpace(field.GetKey())
		if key == "" {
			problems = append(problems, fmt.Errorf("%s.key is required", fieldLocation))
		} else if previous, exists := seen[key]; exists {
			problems = append(problems, fmt.Errorf("%s.key %q duplicates %s[%d]", fieldLocation, key, location, previous))
		} else {
			seen[key] = index
		}
		switch value := field.GetValue().(type) {
		case *searchv1.SearchResponse_Result_Field_Text:
		case *searchv1.SearchResponse_Result_Field_Integer:
		case *searchv1.SearchResponse_Result_Field_Timestamp:
			if value.Timestamp == nil {
				problems = append(problems, fmt.Errorf("%s.timestamp is required", fieldLocation))
			} else if err := value.Timestamp.CheckValid(); err != nil {
				problems = append(problems, fmt.Errorf("%s.timestamp is invalid: %w", fieldLocation, err))
			}
		case nil:
			problems = append(problems, fmt.Errorf("%s.value is required", fieldLocation))
		default:
			problems = append(problems, fmt.Errorf("%s.value has unsupported type %T", fieldLocation, value))
		}
	}
	return problems
}

func validateGroup(location string, group *searchv1.SearchGroup) []error {
	if group == nil {
		return []error{fmt.Errorf("%s is nil", location)}
	}

	var problems []error
	if strings.TrimSpace(group.GetKey()) == "" {
		problems = append(problems, fmt.Errorf("%s.key is required", location))
	}
	if strings.TrimSpace(group.GetTitle()) == "" {
		problems = append(problems, fmt.Errorf("%s.title is required", location))
	}
	for index, target := range group.GetTargets() {
		problems = append(problems, validateOpenTarget(fmt.Sprintf("%s.targets[%d]", location, index), target)...)
	}
	return problems
}

func validateOpenTarget(location string, target *searchv1.OpenTarget) []error {
	if target == nil {
		return []error{fmt.Errorf("%s is nil", location)}
	}

	switch value := target.GetTarget().(type) {
	case *searchv1.OpenTarget_Uri:
		return validateURITarget(location+".uri", value.Uri)
	case *searchv1.OpenTarget_File:
		return validateFileTarget(location+".file", value.File)
	case nil:
		return []error{fmt.Errorf("%s.target is required", location)}
	default:
		return []error{fmt.Errorf("%s.target has unsupported type %T", location, value)}
	}
}

func validateURITarget(location string, target *searchv1.UriTarget) []error {
	if target == nil {
		return []error{fmt.Errorf("%s is nil", location)}
	}
	uriValue := strings.TrimSpace(target.GetUri())
	if uriValue == "" {
		return []error{fmt.Errorf("%s.uri is required", location)}
	}
	parsed, err := url.Parse(uriValue)
	if err != nil {
		return []error{fmt.Errorf("%s.uri is malformed: %w", location, err)}
	}
	if parsed.Scheme == "" {
		return []error{fmt.Errorf("%s.uri must include a scheme", location)}
	}
	return nil
}

func validateFileTarget(location string, target *searchv1.FileTarget) []error {
	if target == nil {
		return []error{fmt.Errorf("%s is nil", location)}
	}

	var problems []error
	path := strings.TrimSpace(target.GetPath())
	if path == "" {
		problems = append(problems, fmt.Errorf("%s.path is required", location))
	} else if !filepath.IsAbs(path) {
		problems = append(problems, fmt.Errorf("%s.path must be absolute", location))
	}
	if target.Line != nil && target.GetLine() == 0 {
		problems = append(problems, fmt.Errorf("%s.line must be positive when present", location))
	}
	if target.Column != nil {
		if target.GetColumn() == 0 {
			problems = append(problems, fmt.Errorf("%s.column must be positive when present", location))
		}
		if target.Line == nil {
			problems = append(problems, fmt.Errorf("%s.column requires line", location))
		}
	}
	return problems
}

func validateWarning(location string, warning *searchv1.SearchResponse_Warning) []error {
	if warning == nil {
		return []error{fmt.Errorf("%s is nil", location)}
	}

	var problems []error
	if strings.TrimSpace(warning.GetMessage()) == "" {
		problems = append(problems, fmt.Errorf("%s.message is required", location))
	}
	if warning.Code != nil && strings.TrimSpace(warning.GetCode()) == "" {
		problems = append(problems, fmt.Errorf("%s.code must be non-empty when present", location))
	}
	return problems
}
