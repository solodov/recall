package provider

import (
	"bytes"
	"context"
	"testing"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestServeSearchWithOptionsDecodesTextprotoAndMirrorsResponse(t *testing.T) {
	request := []byte("query: \"sample\"\n")
	var stdout bytes.Buffer
	var sawUnlimited bool

	err := ServeSearchWithOptions(context.Background(), SearchFunc(func(_ context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
		if request.GetQuery() != "sample" {
			t.Fatalf("query = %q, want sample", request.GetQuery())
		}
		_, sawUnlimited = RequestedLimit(request)
		return &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{{Id: "hit:1", Kind: "note", Title: "Sample"}}}, nil
	}), ServeOptions{
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
	if response.GetHits()[0].GetId() != "hit:1" {
		t.Fatalf("response = %#v", response)
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
