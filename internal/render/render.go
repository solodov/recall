// Package render owns recall's provider-agnostic presentation rules.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"recall/internal/normalize"
	"recall/internal/orchestrator"
	searchv1 "recall/proto/recall/search/v1"

	"google.golang.org/protobuf/encoding/protojson"
)

// HumanOptions controls terminal-oriented rendering without changing provider
// response data.
type HumanOptions struct {
	// Grouped groups hits by source and provider-supplied group identity.
	Grouped bool
}

// WriteHuman renders normalized results with generic named-URI and grouping
// rules. Providers supply data only; recall decides how it is presented.
func WriteHuman(writer io.Writer, result *orchestrator.Result, options HumanOptions) error {
	if result == nil {
		return nil
	}
	if options.Grouped {
		return writeGroupedHuman(writer, result)
	}
	for _, hit := range ungroupedHits(result) {
		writeHumanHit(writer, hit, "")
	}
	for _, response := range result.Responses {
		writeHumanWarnings(writer, response.Warnings)
	}
	return nil
}

func writeGroupedHuman(writer io.Writer, result *orchestrator.Result) error {
	for _, response := range result.Responses {
		fmt.Fprintf(writer, "# %s\n", response.ProviderID)
		groups := groupHits(response.Hits)
		for _, group := range groups {
			fmt.Fprintf(writer, "## %s", group.title)
			if primary := firstURI(group.uris); primary != nil {
				fmt.Fprintf(writer, " <%s>", primary.GetUri())
			}
			fmt.Fprintln(writer)
			for _, hit := range group.hits {
				writeHumanHit(writer, hit, "  ")
			}
		}
		writeHumanWarnings(writer, response.Warnings)
	}
	return nil
}

func writeHumanHit(writer io.Writer, normalized normalize.Hit, indent string) {
	hit := normalized.Hit
	if hit == nil {
		return
	}
	fmt.Fprintf(writer, "%s[%s] %s", indent, normalized.ProviderID, singleLine(hit.GetTitle()))
	if primary := firstURI(hit.GetUris()); primary != nil {
		fmt.Fprintf(writer, " <%s>", primary.GetUri())
	}
	if kind := strings.TrimSpace(hit.GetKind()); kind != "" {
		fmt.Fprintf(writer, " (%s)", kind)
	}
	if occurredAt := hit.GetOccurredAt(); occurredAt != nil && occurredAt.IsValid() {
		fmt.Fprintf(writer, " %s", occurredAt.AsTime().UTC().Format(time.RFC3339))
	}
	fmt.Fprintln(writer)
	if snippet := strings.TrimSpace(hit.GetSnippet()); snippet != "" {
		fmt.Fprintf(writer, "%s  %s\n", indent, singleLine(snippet))
	}
	if actions := secondaryActions(hit.GetUris()); actions != "" {
		fmt.Fprintf(writer, "%s  actions: %s\n", indent, actions)
	}
}

func writeHumanWarnings(writer io.Writer, warnings []normalize.Warning) {
	for _, warning := range warnings {
		if warning.Warning == nil {
			continue
		}
		fmt.Fprintf(writer, "[%s] warning: %s\n", warning.ProviderID, singleLine(warning.Warning.GetMessage()))
	}
}

// WriteJSON renders a machine-readable result while preserving each validated
// provider SearchResponse under its configured provider identity.
func WriteJSON(writer io.Writer, result *orchestrator.Result) error {
	machineResult := machineResult{}
	if result != nil {
		machineResult.Responses = make([]machineProviderResponse, 0, len(result.Responses))
		for _, response := range result.Responses {
			responseJSON, err := marshalProviderResponse(response)
			if err != nil {
				return err
			}
			machineResult.Responses = append(machineResult.Responses, machineProviderResponse{
				ProviderID: response.ProviderID,
				Response:   responseJSON,
			})
		}
		machineResult.BlendedHits = make([]machineBlendedHit, 0, len(result.BlendedHits))
		for _, hit := range result.BlendedHits {
			hitJSON, err := marshalSearchHit(hit.Normalized.Hit)
			if err != nil {
				return err
			}
			machineResult.BlendedHits = append(machineResult.BlendedHits, machineBlendedHit{
				ProviderID:   hit.Normalized.ProviderID,
				ProviderRank: hit.Normalized.ProviderRank,
				BlendedScore: hit.BlendedScore,
				Hit:          hitJSON,
			})
		}
		for _, failure := range result.Failures {
			if failure.Err == nil {
				continue
			}
			machineResult.Failures = append(machineResult.Failures, machineFailure{
				ProviderID: failure.ProviderID,
				Error:      failure.Err.Error(),
			})
		}
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(machineResult)
}

func marshalProviderResponse(response orchestrator.ProviderResponse) (json.RawMessage, error) {
	providerResponse := response.Raw
	if providerResponse == nil {
		providerResponse = synthesizeRawResponse(response)
	}
	payload, err := protojson.MarshalOptions{
		EmitUnpopulated: true,
		UseProtoNames:   true,
	}.Marshal(providerResponse)
	if err != nil {
		return nil, fmt.Errorf("marshal provider %q response as JSON: %w", response.ProviderID, err)
	}
	return json.RawMessage(payload), nil
}

func marshalSearchHit(hit *searchv1.SearchHit) (json.RawMessage, error) {
	if hit == nil {
		return json.RawMessage("null"), nil
	}
	payload, err := protojson.MarshalOptions{
		EmitUnpopulated: true,
		UseProtoNames:   true,
	}.Marshal(hit)
	if err != nil {
		return nil, fmt.Errorf("marshal blended hit as JSON: %w", err)
	}
	return json.RawMessage(payload), nil
}

func synthesizeRawResponse(response orchestrator.ProviderResponse) *searchv1.SearchResponse {
	raw := &searchv1.SearchResponse{
		Hits:     make([]*searchv1.SearchHit, 0, len(response.Hits)),
		Warnings: make([]*searchv1.Warning, 0, len(response.Warnings)),
	}
	for _, hit := range response.Hits {
		if hit.Hit != nil {
			raw.Hits = append(raw.Hits, hit.Hit)
		}
	}
	for _, warning := range response.Warnings {
		if warning.Warning != nil {
			raw.Warnings = append(raw.Warnings, warning.Warning)
		}
	}
	return raw
}

type groupedHits struct {
	key   string
	title string
	uris  []*searchv1.NamedUri
	hits  []normalize.Hit
}

func ungroupedHits(result *orchestrator.Result) []normalize.Hit {
	if len(result.BlendedHits) == 0 {
		hits := []normalize.Hit{}
		for _, response := range result.Responses {
			hits = append(hits, response.Hits...)
		}
		return hits
	}

	hits := make([]normalize.Hit, 0, len(result.BlendedHits))
	for _, blended := range result.BlendedHits {
		hits = append(hits, blended.Normalized)
	}
	return hits
}

func groupHits(hits []normalize.Hit) []groupedHits {
	groups := []groupedHits{}
	indexes := map[string]int{}
	for _, hit := range hits {
		group := hit.Hit.GetGroup()
		key := "__ungrouped__"
		title := "Ungrouped"
		var uris []*searchv1.NamedUri
		if group != nil && strings.TrimSpace(group.GetKey()) != "" {
			key = group.GetKey()
			title = group.GetTitle()
			uris = group.GetUris()
		}
		index, exists := indexes[key]
		if !exists {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, groupedHits{key: key, title: title, uris: uris})
		}
		groups[index].hits = append(groups[index].hits, hit)
	}
	return groups
}

func firstURI(uris []*searchv1.NamedUri) *searchv1.NamedUri {
	for _, uri := range uris {
		if uri != nil && strings.TrimSpace(uri.GetUri()) != "" {
			return uri
		}
	}
	return nil
}

func secondaryActions(uris []*searchv1.NamedUri) string {
	if len(uris) <= 1 {
		return ""
	}
	actions := make([]string, 0, len(uris)-1)
	for _, uri := range uris[1:] {
		if uri == nil || strings.TrimSpace(uri.GetName()) == "" || strings.TrimSpace(uri.GetUri()) == "" {
			continue
		}
		actions = append(actions, fmt.Sprintf("%s=%s", uri.GetName(), uri.GetUri()))
	}
	return strings.Join(actions, " ")
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

type machineResult struct {
	Responses   []machineProviderResponse `json:"responses"`
	BlendedHits []machineBlendedHit       `json:"blended_hits,omitempty"`
	Failures    []machineFailure          `json:"failures,omitempty"`
}

type machineProviderResponse struct {
	ProviderID string          `json:"provider_id"`
	Response   json.RawMessage `json:"response"`
}

type machineBlendedHit struct {
	ProviderID   string          `json:"provider_id"`
	ProviderRank int             `json:"provider_rank"`
	BlendedScore float64         `json:"blended_score"`
	Hit          json.RawMessage `json:"hit"`
}

type machineFailure struct {
	ProviderID string `json:"provider_id"`
	Error      string `json:"error"`
}
