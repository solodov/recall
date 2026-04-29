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

// Search returns best-first fixture hits matching all query terms.
func (provider *Provider) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("search request is nil")
	}
	query := strings.TrimSpace(request.GetQuery())
	if query == "" {
		return nil, fmt.Errorf("query must be non-empty")
	}
	limit, _ := recallprovider.RequestedLimit(request)

	terms := strings.Fields(strings.ToLower(query))
	hits := make([]*searchv1.SearchHit, 0, len(provider.documents))
	for _, document := range provider.documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !document.matches(terms) {
			continue
		}
		hits = append(hits, document.hit())
		if limit > 0 && len(hits) >= limit {
			break
		}
	}

	return &searchv1.SearchResponse{
		Hits: hits,
		Warnings: []*searchv1.Warning{{
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
	id         string
	kind       string
	title      string
	snippet    string
	score      float64
	targets    []*searchv1.OpenTarget
	group      *searchv1.SearchGroup
	occurredAt time.Time
}

func (document fixtureDocument) matches(terms []string) bool {
	text := strings.ToLower(strings.Join([]string{
		document.id,
		document.kind,
		document.title,
		document.snippet,
		document.group.GetTitle(),
	}, " "))
	for _, term := range terms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

func (document fixtureDocument) hit() *searchv1.SearchHit {
	return &searchv1.SearchHit{
		Id:         document.id,
		Kind:       document.kind,
		Title:      document.title,
		Snippet:    proto.String(document.snippet),
		Score:      proto.Float64(document.score),
		Targets:    cloneOpenTargets(document.targets),
		Group:      cloneGroup(document.group),
		OccurredAt: timestamppb.New(document.occurredAt),
	}
}

func defaultFixtureDocuments() []fixtureDocument {
	documents := []fixtureDocument{
		{
			id:      "example:rollout-note",
			kind:    "note",
			title:   "Sample rollout note",
			snippet: "Checklist for staged rollouts, fallback commands, and verification steps.",
			score:   0.98,
			targets: []*searchv1.OpenTarget{
				fileTarget("/tmp/recall-example/rollout-note.md", 0, 0),
				uriTarget("https://example.invalid/recall/rollout-note"),
			},
			group: &searchv1.SearchGroup{
				Key:     "fixture:procedures",
				Title:   "Procedure notes",
				Targets: []*searchv1.OpenTarget{fileTarget("/tmp/recall-example/procedures", 0, 0)},
			},
			occurredAt: time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC),
		},
		{
			id:      "example:planning-session",
			kind:    "event",
			title:   "Fixture planning session",
			snippet: "Synthetic calendar event covering risks, owners, and follow-up notes.",
			score:   0.91,
			targets: []*searchv1.OpenTarget{
				uriTarget("https://calendar.example.invalid/event/planning-session"),
				fileTarget("/tmp/recall-example/planning-session.md", 0, 0),
			},
			group: &searchv1.SearchGroup{
				Key:     "fixture:schedule",
				Title:   "Schedule",
				Targets: []*searchv1.OpenTarget{uriTarget("https://calendar.example.invalid")},
			},
			occurredAt: time.Date(2026, 4, 27, 16, 0, 0, 0, time.UTC),
		},
		{
			id:      "example:recall-design",
			kind:    "note",
			title:   "Recall provider design",
			snippet: "Federated search design using protobuf SearchProvider and stdio RPC.",
			score:   0.87,
			targets: []*searchv1.OpenTarget{
				fileTarget("/tmp/recall-example/recall-provider-design.md", 0, 0),
			},
			group: &searchv1.SearchGroup{
				Key:     "fixture:design",
				Title:   "Design notes",
				Targets: []*searchv1.OpenTarget{fileTarget("/tmp/recall-example/design", 0, 0)},
			},
			occurredAt: time.Date(2026, 4, 26, 11, 15, 0, 0, time.UTC),
		},
	}
	sort.SliceStable(documents, func(left, right int) bool {
		return documents[left].score > documents[right].score
	})
	return documents
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
