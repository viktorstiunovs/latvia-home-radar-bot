package identity

import (
	"math"
	"math/bits"
	"strconv"
	"strings"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

const (
	DuplicateRuleVersion    = "property-v1"
	PerceptualPhotoDistance = 10
	AutomaticMatchThreshold = 0.72
	AmbiguousMatchThreshold = 0.35
)

func EvaluateDuplicate(current, candidate domain.ListingEvidence) domain.DuplicateDecision {
	evidence := domain.DuplicateEvidence{
		PhotoEvidence:         "none",
		AddressMatch:          nonemptyEqual(current.Listing.NormalizedAddress, candidate.Listing.NormalizedAddress),
		AreaMatch:             nonemptyEqual(current.Listing.AreaKey, candidate.Listing.AreaKey),
		DescriptionSimilarity: tokenSimilarity(current.Listing.NormalizedDescription, candidate.Listing.NormalizedDescription),
	}
	evidence.ExactPhotoMatches, evidence.MinimumPhotoDistance = comparePhotos(current.Photos, candidate.Photos)
	if evidence.ExactPhotoMatches > 0 {
		evidence.PhotoEvidence = "exact"
	} else if evidence.MinimumPhotoDistance != nil && *evidence.MinimumPhotoDistance <= PerceptualPhotoDistance {
		evidence.PhotoEvidence = "perceptual"
	}
	evidence.CompatibleFacts, evidence.ConflictingFacts = compareFacts(current.Listing, candidate.Listing)

	score := 0.0
	switch evidence.PhotoEvidence {
	case "exact":
		score += 0.60
	case "perceptual":
		score += 0.48
	}
	if evidence.AddressMatch {
		score += 0.15
	}
	if evidence.AreaMatch {
		score += 0.08
	}
	for _, fact := range evidence.CompatibleFacts {
		switch fact {
		case "rooms":
			score += 0.05
		case "area_m2":
			score += 0.06
		case "floor":
			score += 0.03
		case "total_floors", "building_series", "building_type":
			score += 0.02
		case "land_area_m2":
			score += 0.05
		}
	}
	score += min(evidence.DescriptionSimilarity, 1) * 0.10
	if len(evidence.ConflictingFacts) > 0 {
		score -= min(float64(len(evidence.ConflictingFacts))*0.12, 0.36)
	}
	score = math.Round(max(0, min(score, 1))*10000) / 10000

	photoEvidence := evidence.PhotoEvidence != "none"
	compatibleContext := evidence.AddressMatch || evidence.AreaMatch || len(evidence.CompatibleFacts) >= 2
	status := domain.DuplicateRejected
	if photoEvidence && compatibleContext && len(evidence.ConflictingFacts) == 0 && score >= AutomaticMatchThreshold {
		status = domain.DuplicateAccepted
	} else if score >= AmbiguousMatchThreshold {
		status = domain.DuplicateAmbiguous
	}
	return domain.DuplicateDecision{Candidate: candidate, RuleVersion: DuplicateRuleVersion, Confidence: score, Status: status, Evidence: evidence}
}

func HammingDistance(left, right string) (int, bool) {
	a, err := strconv.ParseUint(left, 16, 64)
	if err != nil {
		return 0, false
	}
	b, err := strconv.ParseUint(right, 16, 64)
	if err != nil {
		return 0, false
	}
	return bits.OnesCount64(a ^ b), true
}

func comparePhotos(left, right []domain.PhotoFingerprint) (int, *int) {
	exact := 0
	minimum := 65
	for _, a := range left {
		for _, b := range right {
			if a.ExactAlgorithm == b.ExactAlgorithm && a.ExactHash != "" && a.ExactHash == b.ExactHash {
				exact++
			}
			if a.PerceptualAlgorithm != b.PerceptualAlgorithm {
				continue
			}
			distance, ok := HammingDistance(a.PerceptualHash, b.PerceptualHash)
			if ok && distance < minimum {
				minimum = distance
			}
		}
	}
	if minimum == 65 {
		return exact, nil
	}
	return exact, &minimum
}

func compareFacts(left, right domain.Listing) ([]string, []string) {
	var compatible, conflicting []string
	compareIntFact("rooms", left.Rooms, right.Rooms, 0, &compatible, &conflicting)
	compareFloatFact("area_m2", left.AreaM2, right.AreaM2, 5, 0.08, &compatible, &conflicting)
	compareIntFact("floor", left.Floor, right.Floor, 0, &compatible, &conflicting)
	compareIntFact("total_floors", left.TotalFloors, right.TotalFloors, 0, &compatible, &conflicting)
	compareTextFact("building_series", left.BuildingSeries, right.BuildingSeries, &compatible, &conflicting)
	compareTextFact("building_type", left.BuildingType, right.BuildingType, &compatible, &conflicting)
	compareFloatFact("land_area_m2", left.LandAreaM2, right.LandAreaM2, 50, 0.10, &compatible, &conflicting)
	return compatible, conflicting
}

func compareIntFact(name string, left, right *int, tolerance int, compatible, conflicting *[]string) {
	if left == nil || right == nil {
		return
	}
	if abs(*left-*right) <= tolerance {
		*compatible = append(*compatible, name)
	} else {
		*conflicting = append(*conflicting, name)
	}
}

func compareFloatFact(name string, left, right *float64, absoluteTolerance, relativeTolerance float64, compatible, conflicting *[]string) {
	if left == nil || right == nil {
		return
	}
	tolerance := max(absoluteTolerance, max(*left, *right)*relativeTolerance)
	if math.Abs(*left-*right) <= tolerance {
		*compatible = append(*compatible, name)
	} else {
		*conflicting = append(*conflicting, name)
	}
}

func compareTextFact(name, left, right string, compatible, conflicting *[]string) {
	left, right = NormalizeDescription(left), NormalizeDescription(right)
	if left == "" || right == "" {
		return
	}
	if left == right {
		*compatible = append(*compatible, name)
	} else {
		*conflicting = append(*conflicting, name)
	}
}

func tokenSimilarity(left, right string) float64 {
	leftTokens, rightTokens := tokenSet(left), tokenSet(right)
	if len(leftTokens) == 0 || len(rightTokens) == 0 {
		return 0
	}
	intersection := 0
	union := make(map[string]bool, len(leftTokens)+len(rightTokens))
	for token := range leftTokens {
		union[token] = true
	}
	for token := range rightTokens {
		if leftTokens[token] {
			intersection++
		}
		union[token] = true
	}
	return float64(intersection) / float64(len(union))
}

func tokenSet(value string) map[string]bool {
	result := map[string]bool{}
	for _, token := range strings.Fields(value) {
		if len([]rune(token)) >= 3 {
			result[token] = true
		}
	}
	return result
}

func nonemptyEqual(left, right string) bool {
	return left != "" && right != "" && left == right
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
