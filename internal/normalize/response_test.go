package normalize

import (
	"math"
	"strings"
	"testing"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSearchResponseAnnotatesValidatedHitsAndWarnings(t *testing.T) {
	response := &searchv1.SearchResponse{
		Hits: []*searchv1.SearchHit{
			{
				Id:       "hit-1",
				Selector: "note:content",
				Title:    "First hit",
				Snippet:  proto.String("matched context"),
				Score:    proto.Float64(1.23),
				Targets: []*searchv1.OpenTarget{
					fileTarget("/tmp/first.md", 12, 3),
				},
				Group: &searchv1.SearchGroup{
					Key:     "group-1",
					Title:   "Group one",
					Targets: []*searchv1.OpenTarget{fileTarget("/tmp", 0, 0)},
				},
				OccurredAt: timestamppb.Now(),
			},
		},
		Warnings: []*searchv1.Warning{{
			Message: "stale index",
			Code:    proto.String("stale_index"),
		}},
	}

	normalized, err := SearchResponse("example", response)
	if err != nil {
		t.Fatalf("SearchResponse returned error: %v", err)
	}

	if normalized.ProviderID != "example" {
		t.Fatalf("ProviderID = %q, want example", normalized.ProviderID)
	}
	if len(normalized.Hits) != 1 {
		t.Fatalf("hit count = %d, want 1", len(normalized.Hits))
	}
	if normalized.Hits[0].ProviderID != "example" || normalized.Hits[0].ProviderRank != 1 {
		t.Fatalf("normalized hit metadata = %#v", normalized.Hits[0])
	}
	if normalized.Hits[0].Hit == response.Hits[0] {
		t.Fatal("normalized hit reused provider-owned pointer")
	}
	if len(normalized.Warnings) != 1 || normalized.Warnings[0].ProviderID != "example" {
		t.Fatalf("normalized warnings = %#v", normalized.Warnings)
	}
	if normalized.Raw == response {
		t.Fatal("normalized raw response reused provider-owned pointer")
	}
}

func TestSearchResponseAllowsZeroHits(t *testing.T) {
	normalized, err := SearchResponse("example", &searchv1.SearchResponse{})
	if err != nil {
		t.Fatalf("SearchResponse returned error for empty success: %v", err)
	}
	if len(normalized.Hits) != 0 || len(normalized.Warnings) != 0 {
		t.Fatalf("normalized empty response = %#v", normalized)
	}
}

func TestSearchResponseRejectsMalformedHitsGroupsTargetsAndWarnings(t *testing.T) {
	response := &searchv1.SearchResponse{
		Hits: []*searchv1.SearchHit{
			{
				Id:       "",
				Selector: "",
				Title:    "",
				Targets: []*searchv1.OpenTarget{
					{Target: &searchv1.OpenTarget_Uri{Uri: &searchv1.UriTarget{Uri: "relative/path"}}},
					{Target: &searchv1.OpenTarget_Uri{Uri: &searchv1.UriTarget{Uri: "https://example.invalid", Timestamp: &timestamppb.Timestamp{Seconds: 253402300800}}}},
					{Target: &searchv1.OpenTarget_File{File: &searchv1.FileTarget{Path: "relative/path", Column: proto.Uint32(2)}}},
				},
				Group: &searchv1.SearchGroup{
					Key:     "",
					Title:   "",
					Targets: []*searchv1.OpenTarget{{Target: &searchv1.OpenTarget_Uri{Uri: &searchv1.UriTarget{Uri: ""}}}},
				},
				OccurredAt: &timestamppb.Timestamp{Seconds: 253402300800},
			},
		},
		Warnings: []*searchv1.Warning{{Message: "", Code: proto.String("")}},
	}

	err := firstError(SearchResponse("example", response))
	if err == nil {
		t.Fatal("SearchResponse succeeded for malformed response")
	}
	message := err.Error()
	for _, want := range []string{
		"hits[0].id",
		"hits[0].selector",
		"hits[0].title",
		"hits[0].targets[0].uri.uri must include a scheme",
		"hits[0].targets[1].uri.timestamp is invalid",
		"hits[0].targets[2].file.path must be absolute",
		"hits[0].targets[2].file.column requires line",
		"hits[0].group.key",
		"hits[0].group.title",
		"hits[0].group.targets[0].uri.uri",
		"hits[0].occurred_at",
		"warnings[0].message",
		"warnings[0].code",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("SearchResponse error %q does not contain %q", message, want)
		}
	}
}

func TestSearchResponseRejectsNonFiniteScore(t *testing.T) {
	response := &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{{
		Id:       "hit",
		Selector: "note:content",
		Title:    "Hit",
		Score:    proto.Float64(math.NaN()),
	}}}

	err := firstError(SearchResponse("example", response))
	if err == nil || !strings.Contains(err.Error(), "score") {
		t.Fatalf("SearchResponse score error = %v", err)
	}
}

func TestFilterSelectorsKeepsOnlyRequestedSelectorsAfterProviderSearch(t *testing.T) {
	noteHit := &searchv1.SearchHit{Id: "note-1", Selector: "note:content", Title: "Note"}
	eventHit := &searchv1.SearchHit{Id: "event-1", Selector: "event:content", Title: "Event"}
	warning := &searchv1.Warning{Message: "provider warning"}
	response := ProviderResponse{
		ProviderID: "example",
		Hits: []Hit{
			{ProviderID: "example", ProviderRank: 1, Hit: noteHit},
			{ProviderID: "example", ProviderRank: 2, Hit: eventHit},
		},
		Warnings: []Warning{{ProviderID: "example", Warning: warning}},
		Raw:      &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{noteHit, eventHit}, Warnings: []*searchv1.Warning{warning}},
	}

	filtered := FilterSelectors(response, []string{"event"})

	if len(filtered.Hits) != 1 || filtered.Hits[0].Hit.GetId() != "event-1" {
		t.Fatalf("filtered hits = %#v, want only event hit", filtered.Hits)
	}
	if len(filtered.Warnings) != 1 || filtered.Warnings[0].Warning.GetMessage() != "provider warning" {
		t.Fatalf("filtered warnings = %#v, want warnings preserved", filtered.Warnings)
	}
	if len(filtered.Raw.GetHits()) != 1 || filtered.Raw.GetHits()[0].GetId() != "event-1" {
		t.Fatalf("filtered raw response = %#v, want only event hit", filtered.Raw)
	}
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

func firstError(_ ProviderResponse, err error) error {
	return err
}
