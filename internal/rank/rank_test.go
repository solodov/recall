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
			Hits: []normalize.Hit{
				hit("low-weight", 1, "lw-1", 1000),
				hit("low-weight", 2, "lw-2", 2000),
			},
		},
		{
			ProviderID: "high-weight",
			Hits: []normalize.Hit{
				hit("high-weight", 1, "hw-1", 0.1),
			},
		},
	}

	blended := Blend(responses, map[string]float64{
		"low-weight":  1,
		"high-weight": 2,
	})

	if len(blended) != 3 {
		t.Fatalf("blended hit count = %d, want 3", len(blended))
	}
	if blended[0].Normalized.Hit.GetId() != "hw-1" {
		t.Fatalf("first blended hit = %q, want high-weight rank-1 hit", blended[0].Normalized.Hit.GetId())
	}
	if blended[1].Normalized.Hit.GetId() != "lw-1" || blended[2].Normalized.Hit.GetId() != "lw-2" {
		t.Fatalf("low-weight local order was not preserved for equal provider: %#v", []string{blended[1].Normalized.Hit.GetId(), blended[2].Normalized.Hit.GetId()})
	}
	if blended[0].BlendedScore <= blended[1].BlendedScore {
		t.Fatalf("weighted rank score did not dominate native score: first=%f second=%f", blended[0].BlendedScore, blended[1].BlendedScore)
	}
}

func TestBlendIgnoresProviderNativeScores(t *testing.T) {
	responses := []normalize.ProviderResponse{{
		ProviderID: "example",
		Hits: []normalize.Hit{
			hit("example", 1, "rank-1-low-native-score", 0.01),
			hit("example", 2, "rank-2-high-native-score", 9999),
		},
	}}

	blended := Blend(responses, map[string]float64{"example": 1})

	if blended[0].Normalized.Hit.GetId() != "rank-1-low-native-score" {
		t.Fatalf("provider-native score affected ranking: first = %q", blended[0].Normalized.Hit.GetId())
	}
	if blended[0].BlendedScore <= blended[1].BlendedScore {
		t.Fatalf("rank-1 blended score = %f, rank-2 blended score = %f", blended[0].BlendedScore, blended[1].BlendedScore)
	}
}

func hit(providerID string, rank int, id string, nativeScore float64) normalize.Hit {
	return normalize.Hit{
		ProviderID:   providerID,
		ProviderRank: rank,
		Hit: &searchv1.SearchHit{
			Id:    id,
			Kind:  "note",
			Title: id,
			Score: proto.Float64(nativeScore),
		},
	}
}
