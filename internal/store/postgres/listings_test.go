package postgres

import (
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

func TestEnrichmentRequiredRetriesIncompleteDetails(t *testing.T) {
	listing := domain.Listing{
		Source:          "domimaps.lv",
		ExternalID:      "908232815",
		DetailsEnriched: false,
	}

	if !enrichmentRequired(listing, listing) {
		t.Fatal("incomplete stored details were not retried")
	}
	listing.DetailsEnriched = true
	if enrichmentRequired(listing, listing) {
		t.Fatal("unchanged complete details were retried")
	}
}
