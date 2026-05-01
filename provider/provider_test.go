package provider

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestServeSearchWithOptionsDecodesTextprotoAndMirrorsResponse(t *testing.T) {
	request := []byte("query: \"sample\"\n")
	var stdout bytes.Buffer
	var sawUnlimited bool

	testProvider := staticProvider{
		SearchFunc: func(_ context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
			if request.GetQuery() != "sample" {
				t.Fatalf("query = %q, want sample", request.GetQuery())
			}
			_, sawUnlimited = RequestedLimit(request)
			return &searchv1.SearchResponse{Results: []*searchv1.SearchResponse_Result{{
				Id:       "result:1",
				Selector: "note:content",
				Fields: []*searchv1.SearchResponse_Result_Field{{
					Key:   "title",
					Value: &searchv1.SearchResponse_Result_Field_Text{Text: "Sample"},
				}},
				Format: &searchv1.SearchResponse_Result_Format{TitleFields: []string{"title"}},
			}}}, nil
		},
	}

	err := ServeSearchWithOptions(context.Background(), testProvider, ServeOptions{
		Stdin:  bytes.NewReader(request),
		Stdout: &stdout,
		Args:   []string{searchv1.SearchProviderSearchPath},
	})
	if err != nil {
		t.Fatalf("ServeSearchWithOptions returned error: %v", err)
	}
	if sawUnlimited {
		t.Fatal("missing limit was reported as caller-specified")
	}

	response := &searchv1.SearchResponse{}
	if err := prototext.Unmarshal(stdout.Bytes(), response); err != nil {
		t.Fatalf("response was not textproto: %v", err)
	}
	if len(response.GetResults()) != 1 || response.GetResults()[0].GetId() != "result:1" || response.GetResults()[0].GetFields()[0].GetText() != "Sample" {
		t.Fatalf("response = %#v", response)
	}
	if !reflect.DeepEqual(response.GetResults()[0].GetFormat().GetTitleFields(), []string{"title"}) {
		t.Fatalf("format = %#v", response.GetResults()[0].GetFormat())
	}
}

func TestServeSearchWithOptionsListsCapabilities(t *testing.T) {
	request := []byte("")
	var stdout bytes.Buffer

	err := ServeSearchWithOptions(context.Background(), staticProvider{}, ServeOptions{
		Stdin:  bytes.NewReader(request),
		Stdout: &stdout,
		Args:   []string{searchv1.SearchProviderListCapabilitiesPath},
	})
	if err != nil {
		t.Fatalf("ServeSearchWithOptions returned error: %v", err)
	}

	response := &searchv1.ListCapabilitiesResponse{}
	if err := prototext.Unmarshal(stdout.Bytes(), response); err != nil {
		t.Fatalf("capability response was not textproto: %v", err)
	}
	if len(response.GetSurfaces()) != 1 || response.GetSurfaces()[0].GetSelector() != "note:content" {
		t.Fatalf("surfaces = %#v, want note:content", response.GetSurfaces())
	}
}

func TestRequestedSelectorsReturnsNonEmptyHints(t *testing.T) {
	selectors := RequestedSelectors(&searchv1.SearchRequest{SelectorHints: []string{"pr:content", "", " file:content ", "pr:content"}})
	if len(selectors) != 2 || !selectors["pr:content"] || !selectors["file:content"] {
		t.Fatalf("selectors = %#v, want pr:content and file:content", selectors)
	}
	if selectors := RequestedSelectors(nil); len(selectors) != 0 {
		t.Fatalf("nil request selectors = %#v, want none", selectors)
	}
}

func TestRequestedLimitRequiresPositivePresentLimit(t *testing.T) {
	for name, request := range map[string]*searchv1.SearchRequest{
		"nil":      nil,
		"missing":  {Query: "sample"},
		"zero":     {Query: "sample", Limit: proto.Uint32(0)},
		"positive": {Query: "sample", Limit: proto.Uint32(2)},
	} {
		limit, ok := RequestedLimit(request)
		if name == "positive" {
			if !ok || limit != 2 {
				t.Fatalf("%s: limit=%d ok=%v, want 2 true", name, limit, ok)
			}
			continue
		}
		if ok || limit != 0 {
			t.Fatalf("%s: limit=%d ok=%v, want 0 false", name, limit, ok)
		}
	}
}

type staticProvider struct {
	SearchFunc
}

func (provider staticProvider) ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	return &searchv1.ListCapabilitiesResponse{Surfaces: []*searchv1.SearchSurface{{
		Selector:    "note:content",
		Title:       "Notes",
		Description: "Search note text",
	}}}, nil
}
