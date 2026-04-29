// Package normalize validates provider SearchResponse payloads and attaches
// recall-owned metadata before ranking or rendering.
package normalize

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

// ProviderResponse is a provider response after semantic validation. Hits and
// warnings carry recall's configured provider ID because source identity is not
// provider-owned data.
type ProviderResponse struct {
	ProviderID string
	Hits       []Hit
	Warnings   []Warning

	// Raw preserves the validated provider payload for future machine-readable
	// renderers without making ranking or rendering depend on unannotated data.
	Raw *searchv1.SearchResponse
}

// Hit is one validated result annotated with its source provider and local rank.
type Hit struct {
	ProviderID   string
	ProviderRank int
	Hit          *searchv1.SearchHit
}

// Warning is one validated non-fatal diagnostic annotated with its source.
type Warning struct {
	ProviderID string
	Warning    *searchv1.Warning
}

// SearchResponse validates response semantics that protobuf cannot express and
// returns annotated copies of hits and warnings.
func SearchResponse(providerID string, response *searchv1.SearchResponse) (ProviderResponse, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return ProviderResponse{}, errors.New("provider id is required")
	}
	if response == nil {
		return ProviderResponse{}, errors.New("response is nil")
	}

	var problems []error
	for index, hit := range response.GetHits() {
		problems = append(problems, validateHit(fmt.Sprintf("hits[%d]", index), hit)...)
	}
	for index, warning := range response.GetWarnings() {
		problems = append(problems, validateWarning(fmt.Sprintf("warnings[%d]", index), warning)...)
	}
	if err := errors.Join(problems...); err != nil {
		return ProviderResponse{}, err
	}

	normalized := ProviderResponse{
		ProviderID: providerID,
		Hits:       make([]Hit, 0, len(response.GetHits())),
		Warnings:   make([]Warning, 0, len(response.GetWarnings())),
		Raw:        proto.Clone(response).(*searchv1.SearchResponse),
	}
	for index, hit := range response.GetHits() {
		normalized.Hits = append(normalized.Hits, Hit{
			ProviderID:   providerID,
			ProviderRank: index + 1,
			Hit:          proto.Clone(hit).(*searchv1.SearchHit),
		})
	}
	for _, warning := range response.GetWarnings() {
		normalized.Warnings = append(normalized.Warnings, Warning{
			ProviderID: providerID,
			Warning:    proto.Clone(warning).(*searchv1.Warning),
		})
	}
	return normalized, nil
}

// FilterKinds keeps only hits whose provider-native kind appears in kindFilter.
// An empty filter returns response unchanged because recall treats --kind as a
// presentation/orchestration control, not part of the provider SearchRequest.
func FilterKinds(response ProviderResponse, kindFilter map[string]bool) ProviderResponse {
	if len(kindFilter) == 0 {
		return response
	}
	filtered := ProviderResponse{
		ProviderID: response.ProviderID,
		Hits:       make([]Hit, 0, len(response.Hits)),
		Warnings:   append([]Warning{}, response.Warnings...),
	}
	filtered.Raw = &searchv1.SearchResponse{Warnings: make([]*searchv1.Warning, 0, len(response.Warnings))}
	for _, hit := range response.Hits {
		if hit.Hit == nil || !kindFilter[hit.Hit.GetKind()] {
			continue
		}
		filtered.Hits = append(filtered.Hits, hit)
		filtered.Raw.Hits = append(filtered.Raw.Hits, proto.Clone(hit.Hit).(*searchv1.SearchHit))
	}
	for _, warning := range response.Warnings {
		if warning.Warning != nil {
			filtered.Raw.Warnings = append(filtered.Raw.Warnings, proto.Clone(warning.Warning).(*searchv1.Warning))
		}
	}
	return filtered
}

func validateHit(location string, hit *searchv1.SearchHit) []error {
	if hit == nil {
		return []error{fmt.Errorf("%s is nil", location)}
	}

	var problems []error
	if strings.TrimSpace(hit.GetId()) == "" {
		problems = append(problems, fmt.Errorf("%s.id is required", location))
	}
	if strings.TrimSpace(hit.GetKind()) == "" {
		problems = append(problems, fmt.Errorf("%s.kind is required", location))
	}
	if strings.TrimSpace(hit.GetTitle()) == "" {
		problems = append(problems, fmt.Errorf("%s.title is required", location))
	}
	if hit.Score != nil {
		score := hit.GetScore()
		if math.IsNaN(score) || math.IsInf(score, 0) {
			problems = append(problems, fmt.Errorf("%s.score must be finite", location))
		}
	}
	for index, uri := range hit.GetUris() {
		problems = append(problems, validateNamedURI(fmt.Sprintf("%s.uris[%d]", location, index), uri)...)
	}
	if group := hit.GetGroup(); group != nil {
		problems = append(problems, validateGroup(location+".group", group)...)
	}
	if occurredAt := hit.GetOccurredAt(); occurredAt != nil {
		if err := occurredAt.CheckValid(); err != nil {
			problems = append(problems, fmt.Errorf("%s.occurred_at is invalid: %w", location, err))
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
	for index, uri := range group.GetUris() {
		problems = append(problems, validateNamedURI(fmt.Sprintf("%s.uris[%d]", location, index), uri)...)
	}
	return problems
}

func validateNamedURI(location string, namedURI *searchv1.NamedUri) []error {
	if namedURI == nil {
		return []error{fmt.Errorf("%s is nil", location)}
	}

	var problems []error
	if strings.TrimSpace(namedURI.GetName()) == "" {
		problems = append(problems, fmt.Errorf("%s.name is required", location))
	}
	uriValue := strings.TrimSpace(namedURI.GetUri())
	if uriValue == "" {
		problems = append(problems, fmt.Errorf("%s.uri is required", location))
		return problems
	}
	parsed, err := url.Parse(uriValue)
	if err != nil {
		problems = append(problems, fmt.Errorf("%s.uri is malformed: %w", location, err))
	} else if parsed.Scheme == "" {
		problems = append(problems, fmt.Errorf("%s.uri must include a scheme", location))
	}
	return problems
}

func validateWarning(location string, warning *searchv1.Warning) []error {
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
