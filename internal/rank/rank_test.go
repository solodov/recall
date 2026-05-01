package rank

import (
	"testing"

	"github.com/solodov/recall/internal/normalize"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

func TestBlendUsesProviderLocalOrderAndWeight(t *testing.T) {
	responses := []normalize.ProviderResponse{
		{
			ProviderID: "low-weight",
			Results: []normalize.Result{
				result("low-weight", 1, "lw-1", 1000),
				result("low-weight", 2, "lw-2", 2000),
			},
		},
		{
			ProviderID: "high-weight",
			Results: []normalize.Result{
				result("high-weight", 1, "hw-1", 0.1),
			},
		},
	}

	blended := Blend(responses, map[string]float64{
		"low-weight":  1,
		"high-weight": 2,
	})

	if len(blended) != 3 {
		t.Fatalf("blended result count = %d, want 3", len(blended))
	}
	if blended[0].Normalized.Result.GetId() != "hw-1" {
		t.Fatalf("first blended result = %q, want high-weight rank-1 result", blended[0].Normalized.Result.GetId())
	}
	if blended[1].Normalized.Result.GetId() != "lw-1" || blended[2].Normalized.Result.GetId() != "lw-2" {
		t.Fatalf("low-weight local order was not preserved for equal provider: %#v", []string{blended[1].Normalized.Result.GetId(), blended[2].Normalized.Result.GetId()})
	}
	if blended[0].BlendedScore <= blended[1].BlendedScore {
		t.Fatalf("weighted rank score did not dominate native score: first=%f second=%f", blended[0].BlendedScore, blended[1].BlendedScore)
	}
}

func TestBlendIgnoresProviderNativeScores(t *testing.T) {
	responses := []normalize.ProviderResponse{{
		ProviderID: "example",
		Results: []normalize.Result{
			result("example", 1, "rank-1-low-native-score", 0.01),
			result("example", 2, "rank-2-high-native-score", 9999),
		},
	}}

	blended := Blend(responses, map[string]float64{"example": 1})

	if blended[0].Normalized.Result.GetId() != "rank-1-low-native-score" {
		t.Fatalf("provider-native score affected ranking: first = %q", blended[0].Normalized.Result.GetId())
	}
	if blended[0].BlendedScore <= blended[1].BlendedScore {
		t.Fatalf("rank-1 blended score = %f, rank-2 blended score = %f", blended[0].BlendedScore, blended[1].BlendedScore)
	}
}

func result(providerID string, rank int, id string, nativeScore float64) normalize.Result {
	return normalize.Result{
		ProviderID:   providerID,
		ProviderRank: rank,
		Result: &searchv1.SearchResponse_Result{
			Id:       id,
			Selector: "note:content",
			Fields: []*searchv1.SearchResponse_Result_Field{{
				Key:   "title",
				Value: &searchv1.SearchResponse_Result_Field_Text{Text: id},
			}},
			Score: proto.Float64(nativeScore),
		},
	}
}
