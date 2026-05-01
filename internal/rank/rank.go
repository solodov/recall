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

// Result is one normalized result annotated with recall's cross-provider blended
// score. Provider-native Result.score remains provider-owned diagnostic data.
type Result struct {
	Normalized     normalize.Result
	ProviderWeight float64
	BlendedScore   float64
}

// Blend orders results by provider-local result position and configured provider
// weight. Provider-native result scores are intentionally ignored because they
// are not comparable across different search backends.
func Blend(responses []normalize.ProviderResponse, providerWeights map[string]float64) []Result {
	blended := make([]Result, 0, totalResultCount(responses))
	for _, response := range responses {
		weight := usableWeight(providerWeights[response.ProviderID])
		for index, normalizedResult := range response.Results {
			providerRank := normalizedResult.ProviderRank
			if providerRank <= 0 {
				providerRank = index + 1
			}
			blended = append(blended, Result{
				Normalized:     normalizedResult,
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

func totalResultCount(responses []normalize.ProviderResponse) int {
	total := 0
	for _, response := range responses {
		total += len(response.Results)
	}
	return total
}

func usableWeight(weight float64) float64 {
	if weight <= 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
		return 1
	}
	return weight
}
