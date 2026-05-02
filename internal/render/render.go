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

	// ProviderConfigTargets links grouped source labels back to provider registry
	// blocks so operators can inspect the source behind a result without adding
	// more terminal rows.
	ProviderConfigTargets map[string]*searchv1.OpenTarget
}

// ResultSummary is the compact provider-independent presentation shape shared
// by interactive frontends that need one row per blended search result.
type ResultSummary struct {
	ProviderID string
	Selector   string
	GroupTitle string
	Title      string
	Details    []FieldSummary
	Target     *searchv1.OpenTarget

	Normalized normalize.Result
}

// FieldSummary is one display-ready detail fact for a summarized search result.
type FieldSummary struct {
	Key   string
	Label string
	Value string
}

// SummarizeResults returns blended result rows using the same field layout rules
// as human output while leaving grouping and styling to the caller.
func SummarizeResults(result *orchestrator.Result) []ResultSummary {
	if result == nil {
		return nil
	}
	normalizedResults := ungroupedResults(result)
	summaries := make([]ResultSummary, 0, len(normalizedResults))
	for _, normalized := range normalizedResults {
		searchResult := normalized.Result
		if searchResult == nil {
			continue
		}
		layout := layoutResult(searchResult)
		details := make([]FieldSummary, 0, len(layout.detailFields))
		for _, field := range layout.detailFields {
			value := fieldValue(field)
			if value == "" {
				continue
			}
			details = append(details, FieldSummary{Key: field.Key, Label: humanizeFieldKey(field.Key), Value: value})
		}

		groupTitle := ""
		if group := searchResult.GetGroup(); group != nil {
			groupTitle = singleLine(group.GetTitle())
			if groupTitle == "" {
				groupTitle = singleLine(group.GetKey())
			}
		}

		summaries = append(summaries, ResultSummary{
			ProviderID: normalized.ProviderID,
			Selector:   strings.TrimSpace(searchResult.GetSelector()),
			GroupTitle: groupTitle,
			Title:      titleText(layout.titleFields, searchResult.GetId()),
			Details:    details,
			Target:     firstTarget(searchResult.GetTargets()),
			Normalized: normalized,
		})
	}
	return summaries
}

// WriteHuman renders normalized structured results with compact terminal rules.
// Providers choose fields and suggested order; recall owns grouping, labels,
// timestamp localization, terminal links, secondary actions, and fallbacks.
func WriteHuman(writer io.Writer, result *orchestrator.Result, options HumanOptions) error {
	if result == nil {
		return nil
	}
	if options.Ungrouped {
		for _, normalized := range ungroupedResults(result) {
			writeHumanResult(writer, normalized, "")
		}
		for _, response := range result.Responses {
			writeHumanWarnings(writer, response.Warnings)
		}
		return nil
	}
	return writeGroupedHuman(writer, result, options)
}

func writeGroupedHuman(writer io.Writer, result *orchestrator.Result, options HumanOptions) error {
	wroteGroup := false
	for _, response := range result.Responses {
		groups := groupResults(response.Results)
		for _, group := range groups {
			if wroteGroup {
				fmt.Fprintln(writer)
			}
			fmt.Fprintf(writer, "%s %s\n", linkedGroupHeaderLabel(response.ProviderID, group, options.ProviderConfigTargets), linkedGroupTitle(response.ProviderID, group))
			for _, normalized := range group.results {
				writeGroupedResult(writer, response.ProviderID, group, normalized)
			}
			wroteGroup = true
		}
		writeHumanWarnings(writer, response.Warnings)
	}
	return nil
}

func writeHumanResult(writer io.Writer, normalized normalize.Result, indent string) {
	result := normalized.Result
	if result == nil {
		return
	}
	layout := layoutResult(result)
	fmt.Fprintf(writer, "%s[%s] %s", indent, normalized.ProviderID, linkedTitle(normalized.ProviderID, result, titleText(layout.titleFields, result.GetId())))
	if selector := strings.TrimSpace(result.GetSelector()); selector != "" {
		fmt.Fprintf(writer, " (%s)", selector)
	}
	fmt.Fprintln(writer)
	writeDetailRows(writer, indent+"  ", layout.detailFields)
	if actions := secondaryActions(normalized.ProviderID, result.GetSelector(), result.GetTargets()); actions != "" {
		fmt.Fprintf(writer, "%s  actions: %s\n", indent, actions)
	}
}

func writeGroupedResult(writer io.Writer, providerID string, group groupedResults, normalized normalize.Result) {
	result := normalized.Result
	if result == nil {
		return
	}
	layout := layoutResult(result)
	if line, label, target, ok := groupedLineTitle(group, result, layout.titleFields); ok {
		lineLabel := styleLineNumber(fmt.Sprintf("%5d:", line))
		fmt.Fprintf(writer, "  %s %s\n", lineLabel, terminalLink(label, OpenURL(providerID, result.GetSelector(), target)))
		writeDetailRows(writer, "    ", layout.detailFields)
		if actions := groupedSecondaryActions(providerID, result.GetSelector(), group, result.GetTargets()); actions != "" {
			fmt.Fprintf(writer, "    actions: %s\n", actions)
		}
		return
	}
	if timestamp, label, target, ok := groupedTimestampTitle(result, layout.titleFields); ok {
		timeLabel := styleLineNumber(formatTimestampLabel(timestamp) + ":")
		fmt.Fprintf(writer, "  %s %s\n", timeLabel, terminalLink(label, OpenURL(providerID, result.GetSelector(), target)))
		writeDetailRows(writer, "    ", layout.detailFields)
		if actions := groupedSecondaryActions(providerID, result.GetSelector(), group, result.GetTargets()); actions != "" {
			fmt.Fprintf(writer, "    actions: %s\n", actions)
		}
		return
	}

	fmt.Fprintf(writer, "  %s\n", linkedTitle(providerID, result, titleText(layout.titleFields, result.GetId())))
	writeDetailRows(writer, "    ", layout.detailFields)
	if actions := groupedSecondaryActions(providerID, result.GetSelector(), group, result.GetTargets()); actions != "" {
		fmt.Fprintf(writer, "    actions: %s\n", actions)
	}
}

func linkedTitle(providerID string, result *searchv1.SearchResponse_Result, label string) string {
	label = singleLine(label)
	if label == "" {
		label = singleLine(result.GetId())
	}
	if target := firstTarget(result.GetTargets()); target != nil {
		return terminalLink(label, OpenURL(providerID, result.GetSelector(), target))
	}
	return label
}

func linkedGroupTitle(providerID string, group groupedResults) string {
	title := styleGroupTitle(singleLine(group.title))
	if target := firstTarget(group.targets); target != nil {
		return terminalLink(title, OpenURL(providerID, commonGroupSelector(group), target))
	}
	return title
}

func linkedGroupHeaderLabel(providerID string, group groupedResults, configTargets map[string]*searchv1.OpenTarget) string {
	label := styleGroupLabel("[" + groupHeaderLabel(providerID, group) + "]")
	if target := configTargets[providerID]; target != nil {
		return terminalLink(label, OpenURL(providerID, commonGroupSelector(group), target))
	}
	return label
}

func groupHeaderLabel(providerID string, group groupedResults) string {
	if selector := commonGroupSelector(group); selector != "" {
		return providerID + ":" + selector
	}
	return providerID
}

func styleGroupLabel(label string) string {
	return styleANSI(label, groupLabelStyle)
}

func styleGroupTitle(title string) string {
	return styleANSI(title, groupTitleStyle)
}

func styleLineNumber(lineNumber string) string {
	return styleANSI(lineNumber, lineNumberStyle)
}

func styleANSI(text string, style string) string {
	if text == "" {
		return ""
	}
	return style + text + resetStyle
}

func commonGroupSelector(group groupedResults) string {
	var selector string
	for _, normalized := range group.results {
		result := normalized.Result
		if result == nil {
			continue
		}
		resultSelector := strings.TrimSpace(result.GetSelector())
		if resultSelector == "" {
			continue
		}
		if selector == "" {
			selector = resultSelector
			continue
		}
		if selector != resultSelector {
			return ""
		}
	}
	return selector
}

func groupedLineTitle(group groupedResults, result *searchv1.SearchResponse_Result, titleFields []normalize.Field) (int64, string, *searchv1.OpenTarget, bool) {
	if len(titleFields) < 2 || strings.TrimSpace(titleFields[0].Key) != "line" || titleFields[0].Kind != normalize.FieldKindInteger || titleFields[0].Integer <= 0 {
		return 0, "", nil, false
	}
	groupFile := fileTargetForOpen(firstTarget(group.targets))
	if groupFile == nil {
		return 0, "", nil, false
	}
	target := firstTarget(result.GetTargets())
	resultFile := fileTargetForOpen(target)
	if resultFile == nil || resultFile.Line == nil || !sameFilePath(groupFile.GetPath(), resultFile.GetPath()) {
		return 0, "", nil, false
	}
	label := titleText(titleFields[1:], result.GetId())
	if label == "" {
		return 0, "", nil, false
	}
	return titleFields[0].Integer, label, target, true
}

func groupedTimestampTitle(result *searchv1.SearchResponse_Result, titleFields []normalize.Field) (time.Time, string, *searchv1.OpenTarget, bool) {
	if len(titleFields) < 2 || titleFields[0].Kind != normalize.FieldKindTimestamp {
		return time.Time{}, "", nil, false
	}
	target := firstTarget(result.GetTargets())
	if target == nil {
		return time.Time{}, "", nil, false
	}
	label := titleText(titleFields[1:], result.GetId())
	if label == "" {
		return time.Time{}, "", nil, false
	}
	return titleFields[0].Timestamp, label, target, true
}

func formatTimestampLabel(timestamp time.Time) string {
	return timestamp.In(time.Local).Format("2006-01-02 15:04:05")
}

func formatInlineTimestamp(timestamp time.Time) string {
	return timestamp.In(time.Local).Format("2006-01-02T15:04:05")
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

func writeDetailRows(writer io.Writer, indent string, fields []normalize.Field) {
	for _, field := range fields {
		fmt.Fprintf(writer, "%s%s: %s\n", indent, humanizeFieldKey(field.Key), fieldValue(field))
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
		machineResult.BlendedResults = make([]machineBlendedResult, 0, len(result.BlendedResults))
		for _, blended := range result.BlendedResults {
			resultJSON, err := marshalSearchResult(blended.Normalized.Result)
			if err != nil {
				return err
			}
			machineResult.BlendedResults = append(machineResult.BlendedResults, machineBlendedResult{
				ProviderID:   blended.Normalized.ProviderID,
				ProviderRank: blended.Normalized.ProviderRank,
				BlendedScore: blended.BlendedScore,
				Result:       resultJSON,
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

func marshalSearchResult(result *searchv1.SearchResponse_Result) (json.RawMessage, error) {
	if result == nil {
		return json.RawMessage("null"), nil
	}
	payload, err := protojson.MarshalOptions{
		EmitUnpopulated: true,
		UseProtoNames:   true,
	}.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal blended result as JSON: %w", err)
	}
	return json.RawMessage(payload), nil
}

func synthesizeRawResponse(response orchestrator.ProviderResponse) *searchv1.SearchResponse {
	raw := &searchv1.SearchResponse{
		Results:  make([]*searchv1.SearchResponse_Result, 0, len(response.Results)),
		Warnings: make([]*searchv1.SearchResponse_Warning, 0, len(response.Warnings)),
	}
	for _, result := range response.Results {
		if result.Result != nil {
			raw.Results = append(raw.Results, result.Result)
		}
	}
	for _, warning := range response.Warnings {
		if warning.Warning != nil {
			raw.Warnings = append(raw.Warnings, warning.Warning)
		}
	}
	return raw
}

const (
	resetStyle      = "\x1b[0m"
	groupLabelStyle = "\x1b[2;38;2;150;139;125m"
	groupTitleStyle = "\x1b[1m"
	lineNumberStyle = "\x1b[38;2;135;125;112m"
)

type groupedResults struct {
	key     string
	title   string
	targets []*searchv1.OpenTarget
	results []normalize.Result
}

type resultLayout struct {
	titleFields  []normalize.Field
	detailFields []normalize.Field
}

func ungroupedResults(result *orchestrator.Result) []normalize.Result {
	if len(result.BlendedResults) == 0 {
		results := []normalize.Result{}
		for _, response := range result.Responses {
			results = append(results, response.Results...)
		}
		return results
	}

	results := make([]normalize.Result, 0, len(result.BlendedResults))
	for _, blended := range result.BlendedResults {
		results = append(results, blended.Normalized)
	}
	return results
}

func groupResults(results []normalize.Result) []groupedResults {
	groups := []groupedResults{}
	indexes := map[string]int{}
	for _, normalized := range results {
		result := normalized.Result
		if result == nil {
			continue
		}
		group := result.GetGroup()
		key := "__ungrouped__"
		title := "Results"
		var targets []*searchv1.OpenTarget
		if group != nil && strings.TrimSpace(group.GetKey()) != "" {
			key = group.GetKey()
			title = strings.TrimSpace(group.GetTitle())
			if title == "" {
				title = group.GetKey()
			}
			targets = group.GetTargets()
		}
		index, exists := indexes[key]
		if !exists {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, groupedResults{key: key, title: title, targets: targets})
		}
		groups[index].results = append(groups[index].results, normalized)
	}
	return groups
}

func layoutResult(result *searchv1.SearchResponse_Result) resultLayout {
	fields := visibleFields(normalize.Fields(result))
	if len(fields) == 0 {
		return resultLayout{}
	}
	fieldByKey := make(map[string]normalize.Field, len(fields))
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key != "" {
			fieldByKey[key] = field
		}
	}
	used := map[string]bool{}
	format := result.GetFormat()
	layout := resultLayout{}
	if format != nil {
		layout.titleFields = selectFormatFields(format.GetTitleFields(), fieldByKey, used)
	}
	if len(layout.titleFields) == 0 {
		layout.titleFields = []normalize.Field{fields[0]}
		used[strings.TrimSpace(fields[0].Key)] = true
	}
	if format != nil && len(format.GetDetailFields()) > 0 {
		layout.detailFields = selectFormatFields(format.GetDetailFields(), fieldByKey, used)
	} else {
		for _, field := range fields {
			key := strings.TrimSpace(field.Key)
			if key == "" || used[key] {
				continue
			}
			layout.detailFields = append(layout.detailFields, field)
			used[key] = true
		}
	}
	return layout
}

// visibleFields keeps machine-readable-only fields out of normal human layout
// while preserving provider order for every field that remains eligible.
func visibleFields(fields []normalize.Field) []normalize.Field {
	visible := make([]normalize.Field, 0, len(fields))
	for _, field := range fields {
		if !field.Hidden {
			visible = append(visible, field)
		}
	}
	return visible
}

func selectFormatFields(keys []string, fieldByKey map[string]normalize.Field, used map[string]bool) []normalize.Field {
	selected := []normalize.Field{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || used[key] {
			continue
		}
		field, exists := fieldByKey[key]
		if !exists {
			continue
		}
		selected = append(selected, field)
		used[key] = true
	}
	return selected
}

func titleText(fields []normalize.Field, fallback string) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		if value := fieldValue(field); value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return singleLine(fallback)
	}
	return strings.Join(parts, " ")
}

func fieldValue(field normalize.Field) string {
	switch field.Kind {
	case normalize.FieldKindText:
		return singleLine(field.Text)
	case normalize.FieldKindInteger:
		return strconv.FormatInt(field.Integer, 10)
	case normalize.FieldKindTimestamp:
		return formatInlineTimestamp(field.Timestamp)
	default:
		return ""
	}
}

func humanizeFieldKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, "_", " ")
	key = strings.ReplaceAll(key, "-", " ")
	return key
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

func secondaryActions(providerID string, selector string, targets []*searchv1.OpenTarget) string {
	return secondaryActionsFiltered(providerID, selector, targets, nil)
}

func groupedSecondaryActions(providerID string, selector string, group groupedResults, targets []*searchv1.OpenTarget) string {
	return secondaryActionsFiltered(providerID, selector, targets, func(target *searchv1.OpenTarget) bool {
		return isRedundantGroupAction(group, target)
	})
}

func secondaryActionsFiltered(providerID string, selector string, targets []*searchv1.OpenTarget, skip func(*searchv1.OpenTarget) bool) string {
	if len(targets) <= 1 {
		return ""
	}
	actions := make([]string, 0, len(targets)-1)
	for _, target := range targets[1:] {
		if target == nil || target.GetTarget() == nil || (skip != nil && skip(target)) {
			continue
		}
		label := targetLabel(target)
		actions = append(actions, terminalLink(label, OpenURL(providerID, selector, target)))
	}
	return strings.Join(actions, " ")
}

func isRedundantGroupAction(group groupedResults, target *searchv1.OpenTarget) bool {
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

// OpenURL encodes a structured open target into the recall:// URL understood by
// recall-open and interactive frontends.
func OpenURL(providerID string, selector string, target *searchv1.OpenTarget) string {
	if target == nil {
		return ""
	}
	values := url.Values{}
	values.Set("v", "1")
	if providerID = strings.TrimSpace(providerID); providerID != "" {
		values.Set("source", providerID)
	}
	if selector = strings.TrimSpace(selector); selector != "" {
		values.Set("selector", selector)
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
	Responses      []machineProviderResponse `json:"responses"`
	BlendedResults []machineBlendedResult    `json:"blended_results,omitempty"`
	Failures       []machineFailure          `json:"failures,omitempty"`
}

type machineProviderResponse struct {
	ProviderID string          `json:"provider_id"`
	Response   json.RawMessage `json:"response"`
}

type machineBlendedResult struct {
	ProviderID   string          `json:"provider_id"`
	ProviderRank int             `json:"provider_rank"`
	BlendedScore float64         `json:"blended_score"`
	Result       json.RawMessage `json:"result"`
}

type machineFailure struct {
	ProviderID string `json:"provider_id"`
	Error      string `json:"error"`
}
