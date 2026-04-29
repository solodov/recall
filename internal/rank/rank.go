// Package rank blends provider-local result order into one cross-provider order.
package rank

import (
	"math"
	"sort"

	"github.com/solodov/recall/internal/normalize"
)

const (
	// ReciprocalRankK is the damping constant in recall's initial reciprocal-rank
	// fusion formula. It keeps provider-local order comparable without treating
	// provider-native scores as cross-provider signals.
	ReciprocalRankK = 60.0
)

// Hit is one normalized hit annotated with recall's cross-provider blended
// score. Provider-native SearchHit.score remains provider-owned diagnostic data.
type Hit struct {
	Normalized     normalize.Hit
	ProviderWeight float64
	BlendedScore   float64
}

// Blend orders hits by provider-local result position and configured provider
// weight. Provider-native hit scores are intentionally ignored because they are
// not comparable across different search backends.
func Blend(responses []normalize.ProviderResponse, providerWeights map[string]float64) []Hit {
	blended := make([]Hit, 0, totalHitCount(responses))
	for _, response := range responses {
		weight := usableWeight(providerWeights[response.ProviderID])
		for index, normalizedHit := range response.Hits {
			providerRank := normalizedHit.ProviderRank
			if providerRank <= 0 {
				providerRank = index + 1
			}
			blended = append(blended, Hit{
				Normalized:     normalizedHit,
				ProviderWeight: weight,
				BlendedScore:   weight / (ReciprocalRankK + float64(providerRank)),
			})
		}
	}
	sort.SliceStable(blended, func(left, right int) bool {
		return blended[left].BlendedScore > blended[right].BlendedScore
	})
	return blended
}

func totalHitCount(responses []normalize.ProviderResponse) int {
	total := 0
	for _, response := range responses {
		total += len(response.Hits)
	}
	return total
}

func usableWeight(weight float64) float64 {
	if weight <= 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
		return 1
	}
	return weight
}
