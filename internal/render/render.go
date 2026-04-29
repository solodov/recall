// Package render owns recall's provider-independent presentation rules.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/solodov/recall/internal/normalize"
	"github.com/solodov/recall/internal/orchestrator"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/encoding/protojson"
)

// HumanOptions controls terminal-oriented rendering without changing provider
// response data. The zero value uses grouped terminal output.
type HumanOptions struct {
	// Ungrouped renders a flat result list for debugging or compatibility.
	Ungrouped bool
}

// WriteHuman renders normalized results with compact terminal-oriented rules.
// Providers supply data only; recall chooses how to display common result kinds,
// open targets, snippets, groups, and warnings.
func WriteHuman(writer io.Writer, result *orchestrator.Result, options HumanOptions) error {
	if result == nil {
		return nil
	}
	if options.Ungrouped {
		for _, hit := range ungroupedHits(result) {
			writeHumanHit(writer, hit, "")
		}
		for _, response := range result.Responses {
			writeHumanWarnings(writer, response.Warnings)
		}
		return nil
	}
	return writeGroupedHuman(writer, result)
}

func writeGroupedHuman(writer io.Writer, result *orchestrator.Result) error {
	wroteGroup := false
	for _, response := range result.Responses {
		groups := groupHits(response.Hits)
		for _, group := range groups {
			if wroteGroup {
				fmt.Fprintln(writer)
			}
			fmt.Fprintf(writer, "[%s] %s\n", response.ProviderID, linkedGroupTitle(response.ProviderID, group))
			for _, hit := range group.hits {
				writeGroupedHit(writer, response.ProviderID, group, hit)
			}
			wroteGroup = true
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
	if isCodeMatch(hit) {
		writeCompactCodeHit(writer, normalized, indent)
		return
	}
	title := linkedTitle(normalized.ProviderID, hit)
	fmt.Fprintf(writer, "%s[%s] %s", indent, normalized.ProviderID, title)
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
	if actions := secondaryActions(normalized.ProviderID, hit.GetKind(), hit.GetTargets()); actions != "" {
		fmt.Fprintf(writer, "%s  actions: %s\n", indent, actions)
	}
}

func writeCompactCodeHit(writer io.Writer, normalized normalize.Hit, indent string) {
	hit := normalized.Hit
	fmt.Fprintf(writer, "%s[%s] %s", indent, normalized.ProviderID, linkedTitle(normalized.ProviderID, hit))
	if occurredAt := hit.GetOccurredAt(); occurredAt != nil && occurredAt.IsValid() {
		fmt.Fprintf(writer, " %s", occurredAt.AsTime().UTC().Format(time.RFC3339))
	}
	fmt.Fprintln(writer)
	if snippet := strings.TrimSpace(hit.GetSnippet()); snippet != "" {
		fmt.Fprintf(writer, "%s  %s\n", indent, singleLine(snippet))
	}
	if actions := secondaryActions(normalized.ProviderID, hit.GetKind(), hit.GetTargets()); actions != "" {
		fmt.Fprintf(writer, "%s  actions: %s\n", indent, actions)
	}
}

func writeGroupedHit(writer io.Writer, providerID string, group groupedHits, normalized normalize.Hit) {
	hit := normalized.Hit
	if hit == nil {
		return
	}
	if fileTarget, target, ok := groupedLineTarget(group, hit); ok {
		label := groupedHitLabel(hit)
		fmt.Fprintf(writer, "  %5d: %s", fileTarget.GetLine(), terminalLink(label, recallOpenURL(providerID, hit.GetKind(), target)))
		fmt.Fprintln(writer)
		if actions := groupedSecondaryActions(providerID, hit.GetKind(), group, hit.GetTargets()); actions != "" {
			fmt.Fprintf(writer, "         actions: %s\n", actions)
		}
		return
	}

	fmt.Fprintf(writer, "  %s", linkedTitle(providerID, hit))
	if occurredAt := hit.GetOccurredAt(); occurredAt != nil && occurredAt.IsValid() {
		fmt.Fprintf(writer, " %s", occurredAt.AsTime().UTC().Format(time.RFC3339))
	}
	fmt.Fprintln(writer)
	if snippet := groupedSnippet(hit); snippet != "" {
		fmt.Fprintf(writer, "    %s\n", snippet)
	}
	if actions := groupedSecondaryActions(providerID, hit.GetKind(), group, hit.GetTargets()); actions != "" {
		fmt.Fprintf(writer, "    actions: %s\n", actions)
	}
}

func linkedTitle(providerID string, hit *searchv1.SearchHit) string {
	title := singleLine(hit.GetTitle())
	if target := firstTarget(hit.GetTargets()); target != nil {
		return terminalLink(title, recallOpenURL(providerID, hit.GetKind(), target))
	}
	return title
}

func linkedGroupTitle(providerID string, group groupedHits) string {
	title := singleLine(group.title)
	if target := firstTarget(group.targets); target != nil {
		return terminalLink(title, recallOpenURL(providerID, commonGroupKind(group), target))
	}
	return title
}

func commonGroupKind(group groupedHits) string {
	var kind string
	for _, normalized := range group.hits {
		hit := normalized.Hit
		if hit == nil {
			continue
		}
		hitKind := strings.TrimSpace(hit.GetKind())
		if hitKind == "" {
			continue
		}
		if kind == "" {
			kind = hitKind
			continue
		}
		if kind != hitKind {
			return ""
		}
	}
	return kind
}

func groupedLineTarget(group groupedHits, hit *searchv1.SearchHit) (*searchv1.FileTarget, *searchv1.OpenTarget, bool) {
	groupFile := fileTargetForOpen(firstTarget(group.targets))
	if groupFile == nil {
		return nil, nil, false
	}
	target := firstTarget(hit.GetTargets())
	hitFile := fileTargetForOpen(target)
	if hitFile == nil || hitFile.Line == nil {
		return nil, nil, false
	}
	if !sameFilePath(groupFile.GetPath(), hitFile.GetPath()) {
		return nil, nil, false
	}
	return hitFile, target, true
}

func fileTargetForOpen(target *searchv1.OpenTarget) *searchv1.FileTarget {
	if target == nil {
		return nil
	}
	return target.GetFile()
}

func sameFilePath(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func groupedHitLabel(hit *searchv1.SearchHit) string {
	if snippet := strings.TrimSpace(hit.GetSnippet()); snippet != "" {
		return singleLine(snippet)
	}
	return singleLine(hit.GetTitle())
}

func groupedSnippet(hit *searchv1.SearchHit) string {
	snippet := singleLine(hit.GetSnippet())
	if snippet == "" || snippet == singleLine(hit.GetTitle()) {
		return ""
	}
	return snippet
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

const codeMatchKind = "code_match"

type groupedHits struct {
	key     string
	title   string
	targets []*searchv1.OpenTarget
	hits    []normalize.Hit
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
		title := "Results"
		var targets []*searchv1.OpenTarget
		if group != nil && strings.TrimSpace(group.GetKey()) != "" {
			key = group.GetKey()
			title = group.GetTitle()
			targets = group.GetTargets()
		}
		index, exists := indexes[key]
		if !exists {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, groupedHits{key: key, title: title, targets: targets})
		}
		groups[index].hits = append(groups[index].hits, hit)
	}
	return groups
}

func isCodeMatch(hit *searchv1.SearchHit) bool {
	return strings.TrimSpace(hit.GetKind()) == codeMatchKind
}

func firstTarget(targets []*searchv1.OpenTarget) *searchv1.OpenTarget {
	for _, target := range targets {
		if target == nil || target.GetTarget() == nil {
			continue
		}
		return target
	}
	return nil
}

func secondaryActions(providerID string, kind string, targets []*searchv1.OpenTarget) string {
	return secondaryActionsFiltered(providerID, kind, targets, nil)
}

func groupedSecondaryActions(providerID string, kind string, group groupedHits, targets []*searchv1.OpenTarget) string {
	return secondaryActionsFiltered(providerID, kind, targets, func(target *searchv1.OpenTarget) bool {
		return isRedundantGroupAction(group, target)
	})
}

func secondaryActionsFiltered(providerID string, kind string, targets []*searchv1.OpenTarget, skip func(*searchv1.OpenTarget) bool) string {
	if len(targets) <= 1 {
		return ""
	}
	actions := make([]string, 0, len(targets)-1)
	for _, target := range targets[1:] {
		if target == nil || target.GetTarget() == nil || (skip != nil && skip(target)) {
			continue
		}
		label := targetLabel(target)
		actions = append(actions, terminalLink(label, recallOpenURL(providerID, kind, target)))
	}
	return strings.Join(actions, " ")
}

func isRedundantGroupAction(group groupedHits, target *searchv1.OpenTarget) bool {
	groupTarget := firstTarget(group.targets)
	if groupFile, targetFile := fileTargetForOpen(groupTarget), fileTargetForOpen(target); groupFile != nil && targetFile != nil {
		return sameFilePath(groupFile.GetPath(), targetFile.GetPath())
	}
	groupURI := uriTargetForOpen(groupTarget)
	targetURI := uriTargetForOpen(target)
	return groupURI != nil && targetURI != nil && strings.TrimSpace(groupURI.GetUri()) == strings.TrimSpace(targetURI.GetUri())
}

func uriTargetForOpen(target *searchv1.OpenTarget) *searchv1.UriTarget {
	if target == nil {
		return nil
	}
	return target.GetUri()
}

func targetLabel(target *searchv1.OpenTarget) string {
	if target.GetFile() != nil {
		return "file"
	}
	if uriTarget := target.GetUri(); uriTarget != nil {
		parsed, err := url.Parse(uriTarget.GetUri())
		if err == nil && parsed.Scheme != "" {
			return parsed.Scheme
		}
		return "uri"
	}
	return "target"
}

func recallOpenURL(providerID string, kind string, target *searchv1.OpenTarget) string {
	values := url.Values{}
	values.Set("v", "1")
	if providerID = strings.TrimSpace(providerID); providerID != "" {
		values.Set("source", providerID)
	}
	if kind = strings.TrimSpace(kind); kind != "" {
		values.Set("kind", kind)
	}
	if fileTarget := target.GetFile(); fileTarget != nil {
		values.Set("type", "file")
		values.Set("path", fileTarget.GetPath())
		if fileTarget.Line != nil {
			values.Set("line", strconv.FormatUint(uint64(fileTarget.GetLine()), 10))
		}
		if fileTarget.Column != nil {
			values.Set("column", strconv.FormatUint(uint64(fileTarget.GetColumn()), 10))
		}
	} else if uriTarget := target.GetUri(); uriTarget != nil {
		values.Set("type", "uri")
		values.Set("uri", uriTarget.GetUri())
	} else {
		return ""
	}
	return (&url.URL{Scheme: "recall", Host: "open", RawQuery: values.Encode()}).String()
}

func terminalLink(label string, targetURL string) string {
	if targetURL == "" {
		return label
	}
	return "\x1b]8;;" + targetURL + "\x1b\\" + label + "\x1b]8;;\x1b\\"
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
