// Package exampleprovider implements recall's deterministic stdio RPC reference provider.
package exampleprovider

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	recallprovider "github.com/solodov/recall/provider"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Provider searches a small built-in fixture so provider authors can exercise
// the real SearchProvider contract without setting up an index or credentials.
type Provider struct {
	documents []fixtureDocument
}

// New returns the deterministic reference provider used by the example binary.
func New() *Provider {
	return &Provider{documents: defaultFixtureDocuments()}
}

// Serve handles exactly one recall stdio RPC call for the example provider.
func (provider *Provider) Serve(ctx context.Context, stdin io.Reader, stdout io.Writer, args []string) error {
	return recallprovider.ServeSearchWithOptions(ctx, provider, recallprovider.ServeOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Args:   args,
	})
}

// ListCapabilities advertises the deterministic fixture selectors.
func (provider *Provider) ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	return &searchv1.ListCapabilitiesResponse{Surfaces: []*searchv1.SearchSurface{
		{Selector: "note:content", Title: "Fixture notes", Description: "Search synthetic note titles and body text"},
		{Selector: "event:content", Title: "Fixture events", Description: "Search synthetic event titles and body text"},
	}}, nil
}

// Search returns best-first fixture results matching all query terms.
func (provider *Provider) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("search request is nil")
	}
	query := strings.TrimSpace(request.GetQuery())
	if query == "" {
		return nil, fmt.Errorf("query must be non-empty")
	}
	limit, _ := recallprovider.RequestedLimit(request)
	selectorHints := recallprovider.RequestedSelectors(request)

	terms := strings.Fields(strings.ToLower(query))
	results := make([]*searchv1.SearchResponse_Result, 0, len(provider.documents))
	for _, document := range provider.documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !document.matches(terms) || !matchesSelectorHint(document.selector, selectorHints) {
			continue
		}
		results = append(results, document.result())
		if limit > 0 && len(results) >= limit {
			break
		}
	}

	return &searchv1.SearchResponse{
		Results: results,
		Warnings: []*searchv1.SearchResponse_Warning{{
			Message: "example provider uses a built-in deterministic fixture",
			Code:    proto.String("example_fixture"),
		}},
	}, nil
}

// ServeDefault handles one stdio RPC call using process streams and environment.
func ServeDefault(ctx context.Context) error {
	return New().Serve(ctx, os.Stdin, os.Stdout, os.Args[1:])
}

type fixtureDocument struct {
	id       string
	selector string
	fields   []*searchv1.SearchResponse_Result_Field
	format   *searchv1.SearchResponse_Result_Format
	score    float64
	targets  []*searchv1.OpenTarget
	group    *searchv1.SearchGroup
}

func matchesSelectorHint(selector string, hints map[string]bool) bool {
	if len(hints) == 0 {
		return true
	}
	for hint := range hints {
		if selector == hint || strings.HasPrefix(selector, hint+":") {
			return true
		}
	}
	return false
}

func (document fixtureDocument) matches(terms []string) bool {
	text := strings.ToLower(strings.Join(document.searchText(), " "))
	for _, term := range terms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

func (document fixtureDocument) searchText() []string {
	values := []string{document.id, document.selector}
	for _, field := range document.fields {
		if field == nil {
			continue
		}
		values = append(values, field.GetKey(), field.GetText())
	}
	if document.group != nil {
		values = append(values, document.group.GetTitle())
	}
	return values
}

func (document fixtureDocument) result() *searchv1.SearchResponse_Result {
	return &searchv1.SearchResponse_Result{
		Id:       document.id,
		Selector: document.selector,
		Fields:   cloneFields(document.fields),
		Score:    proto.Float64(document.score),
		Targets:  cloneOpenTargets(document.targets),
		Group:    cloneGroup(document.group),
		Format:   cloneFormat(document.format),
	}
}

func defaultFixtureDocuments() []fixtureDocument {
	documents := []fixtureDocument{
		{
			id:       "rollout-note",
			selector: "note:content",
			fields: []*searchv1.SearchResponse_Result_Field{
				textField("title", "Sample rollout note"),
				textField("snippet", "Checklist for staged rollouts, fallback commands, and verification steps."),
				timestampField("updated_at", time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC)),
			},
			format: resultFormat([]string{"title"}, []string{"updated_at", "snippet"}),
			score:  0.98,
			targets: []*searchv1.OpenTarget{
				fileTarget("/tmp/recall-example/rollout-note.md", 0, 0),
				uriTarget("https://example.invalid/recall/rollout-note"),
			},
			group: &searchv1.SearchGroup{
				Key:     "fixture:procedures",
				Title:   "Procedure notes",
				Targets: []*searchv1.OpenTarget{fileTarget("/tmp/recall-example/procedures", 0, 0)},
			},
		},
		{
			id:       "planning-session",
			selector: "event:content",
			fields: []*searchv1.SearchResponse_Result_Field{
				textField("summary", "Fixture planning session"),
				textField("description", "Synthetic calendar event covering risks, owners, and follow-up notes."),
				timestampField("start_at", time.Date(2026, 4, 27, 16, 0, 0, 0, time.UTC)),
			},
			format: resultFormat([]string{"summary"}, []string{"start_at", "description"}),
			score:  0.91,
			targets: []*searchv1.OpenTarget{
				uriTarget("https://calendar.example.invalid/event/planning-session"),
				fileTarget("/tmp/recall-example/planning-session.md", 0, 0),
			},
			group: &searchv1.SearchGroup{
				Key:     "fixture:schedule",
				Title:   "Schedule",
				Targets: []*searchv1.OpenTarget{uriTarget("https://calendar.example.invalid")},
			},
		},
		{
			id:       "recall-design",
			selector: "note:content",
			fields: []*searchv1.SearchResponse_Result_Field{
				textField("title", "Recall provider design"),
				textField("snippet", "Federated search design using protobuf SearchProvider and stdio RPC."),
				timestampField("updated_at", time.Date(2026, 4, 26, 11, 15, 0, 0, time.UTC)),
			},
			format: resultFormat([]string{"title"}, []string{"updated_at", "snippet"}),
			score:  0.87,
			targets: []*searchv1.OpenTarget{
				fileTarget("/tmp/recall-example/recall-provider-design.md", 0, 0),
			},
			group: &searchv1.SearchGroup{
				Key:     "fixture:design",
				Title:   "Design notes",
				Targets: []*searchv1.OpenTarget{fileTarget("/tmp/recall-example/design", 0, 0)},
			},
		},
	}
	sort.SliceStable(documents, func(left, right int) bool {
		return documents[left].score > documents[right].score
	})
	return documents
}

func textField(key string, value string) *searchv1.SearchResponse_Result_Field {
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Text{Text: value},
	}
}

func timestampField(key string, value time.Time) *searchv1.SearchResponse_Result_Field {
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Timestamp{Timestamp: timestamppb.New(value)},
	}
}

func resultFormat(titleFields []string, detailFields []string) *searchv1.SearchResponse_Result_Format {
	return &searchv1.SearchResponse_Result_Format{TitleFields: titleFields, DetailFields: detailFields}
}

func uriTarget(uri string) *searchv1.OpenTarget {
	return &searchv1.OpenTarget{Target: &searchv1.OpenTarget_Uri{Uri: &searchv1.UriTarget{Uri: uri}}}
}

func fileTarget(path string, line uint32, column uint32) *searchv1.OpenTarget {
	target := &searchv1.FileTarget{Path: path}
	if line > 0 {
		target.Line = proto.Uint32(line)
	}
	if column > 0 {
		target.Column = proto.Uint32(column)
	}
	return &searchv1.OpenTarget{Target: &searchv1.OpenTarget_File{File: target}}
}

func cloneFields(fields []*searchv1.SearchResponse_Result_Field) []*searchv1.SearchResponse_Result_Field {
	cloned := make([]*searchv1.SearchResponse_Result_Field, 0, len(fields))
	for _, field := range fields {
		cloned = append(cloned, proto.Clone(field).(*searchv1.SearchResponse_Result_Field))
	}
	return cloned
}

func cloneOpenTargets(targets []*searchv1.OpenTarget) []*searchv1.OpenTarget {
	cloned := make([]*searchv1.OpenTarget, 0, len(targets))
	for _, target := range targets {
		cloned = append(cloned, proto.Clone(target).(*searchv1.OpenTarget))
	}
	return cloned
}

func cloneGroup(group *searchv1.SearchGroup) *searchv1.SearchGroup {
	if group == nil {
		return nil
	}
	return proto.Clone(group).(*searchv1.SearchGroup)
}

func cloneFormat(format *searchv1.SearchResponse_Result_Format) *searchv1.SearchResponse_Result_Format {
	if format == nil {
		return nil
	}
	return proto.Clone(format).(*searchv1.SearchResponse_Result_Format)
}
