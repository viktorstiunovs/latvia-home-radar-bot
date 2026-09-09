package identity

import (
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

func TestEvaluateDuplicateAcceptsExactPhotoWithCompatibleContext(t *testing.T) {
	current := duplicateEvidence(1, "ss.lv", "a", "0123456789abcdef")
	candidate := duplicateEvidence(2, "ss.lv", "b", "0123456789abcdef")

	decision := EvaluateDuplicate(current, candidate)

	if decision.Status != domain.DuplicateAccepted || decision.Evidence.PhotoEvidence != "exact" {
		t.Fatalf("decision = %+v", decision)
	}
	if decision.RuleVersion != DuplicateRuleVersion || decision.Confidence < AutomaticMatchThreshold {
		t.Fatalf("unexpected rule or score: %+v", decision)
	}
}

func TestEvaluateDuplicateAcceptsCrossProviderPerceptualPhoto(t *testing.T) {
	current := duplicateEvidence(1, "ss.lv", "a", "0000000000000000")
	candidate := duplicateEvidence(2, "city24.lv", "b", "0000000000000003")
	candidate.Photos[0].ExactHash = "different"
	candidate.Listing.NormalizedDescription = "sunny renovated apartment balcony quiet courtyard"

	decision := EvaluateDuplicate(current, candidate)

	if decision.Status != domain.DuplicateAccepted || decision.Evidence.PhotoEvidence != "perceptual" {
		t.Fatalf("decision = %+v", decision)
	}
	if decision.Evidence.MinimumPhotoDistance == nil || *decision.Evidence.MinimumPhotoDistance != 2 {
		t.Fatalf("minimum distance = %v", decision.Evidence.MinimumPhotoDistance)
	}
}

func TestEvaluateDuplicateDoesNotAcceptAddressOnly(t *testing.T) {
	current := duplicateEvidence(1, "ss.lv", "a", "0000000000000000")
	candidate := duplicateEvidence(2, "city24.lv", "b", "ffffffffffffffff")
	candidate.Photos[0].ExactHash = "different"
	candidate.Listing.NormalizedDescription = "completely unrelated copy"
	candidate.Listing.AreaKey = ""
	candidate.Listing.Rooms = nil
	candidate.Listing.AreaM2 = nil
	candidate.Listing.Floor = nil
	candidate.Listing.TotalFloors = nil

	decision := EvaluateDuplicate(current, candidate)

	if decision.Status == domain.DuplicateAccepted {
		t.Fatalf("address-only match was accepted: %+v", decision)
	}
}

func TestEvaluateDuplicateLeavesSameBuildingWithoutPhotoEvidenceAmbiguous(t *testing.T) {
	current := duplicateEvidence(1, "ss.lv", "a", "0000000000000000")
	candidate := duplicateEvidence(2, "city24.lv", "b", "ffffffffffffffff")
	candidate.Photos[0].ExactHash = "different"

	decision := EvaluateDuplicate(current, candidate)

	if decision.Status != domain.DuplicateAmbiguous {
		t.Fatalf("same-building decision = %+v", decision)
	}
}

func TestEvaluateDuplicateRejectsAutomaticMergeOnConflictingFacts(t *testing.T) {
	current := duplicateEvidence(1, "ss.lv", "a", "0123456789abcdef")
	candidate := duplicateEvidence(2, "city24.lv", "b", "0123456789abcdef")
	candidate.Listing.Rooms = intTestPointer(5)

	decision := EvaluateDuplicate(current, candidate)

	if decision.Status == domain.DuplicateAccepted {
		t.Fatalf("conflicting facts were accepted: %+v", decision)
	}
	if len(decision.Evidence.ConflictingFacts) != 1 || decision.Evidence.ConflictingFacts[0] != "rooms" {
		t.Fatalf("conflicts = %v", decision.Evidence.ConflictingFacts)
	}
}

func TestHammingDistanceRejectsInvalidHashes(t *testing.T) {
	if _, ok := HammingDistance("not-a-hash", "0000000000000000"); ok {
		t.Fatal("invalid hash was accepted")
	}
}

func duplicateEvidence(id int64, source, externalID, perceptualHash string) domain.ListingEvidence {
	return domain.ListingEvidence{
		SnapshotID: id * 10,
		Listing: domain.Listing{
			ID:                    id,
			Source:                source,
			ExternalID:            externalID,
			PropertyType:          domain.PropertyApartment,
			DealType:              domain.DealSale,
			AreaKey:               "lv/riga/centrs",
			NormalizedAddress:     "brivibas iela 48",
			NormalizedDescription: "renovated sunny apartment with balcony quiet courtyard",
			Rooms:                 intTestPointer(2),
			AreaM2:                floatTestPointer(54.2),
			Floor:                 intTestPointer(3),
			TotalFloors:           intTestPointer(5),
		},
		Photos: []domain.PhotoFingerprint{{
			ExactHash:           "same-exact-hash",
			ExactAlgorithm:      "sha256-v1",
			PerceptualHash:      perceptualHash,
			PerceptualAlgorithm: "dhash-64-v1",
		}},
	}
}

func intTestPointer(value int) *int {
	return &value
}

func floatTestPointer(value float64) *float64 {
	return &value
}
