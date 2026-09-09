package postgres

import (
	"testing"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

func TestMatchingFiltersByUserSelectsOneDeterministicMatch(t *testing.T) {
	occurredAt := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	beforeEvent := occurredAt.Add(-time.Minute)
	price := 90000
	broadMaximum := 200000
	matchingMaximum := 100000
	nonMatchingMaximum := 80000
	listing := domain.Listing{
		DealType:     domain.DealSale,
		PropertyType: domain.PropertyApartment,
		PriceEUR:     &price,
	}
	filters := []domain.SearchFilter{
		{ID: 7, UserID: 10, DealType: domain.DealSale, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, PriceMax: &broadMaximum, ActivatedAt: &beforeEvent},
		{ID: 3, UserID: 10, DealType: domain.DealSale, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, PriceMax: &matchingMaximum, ActivatedAt: &beforeEvent},
		{ID: 1, UserID: 10, DealType: domain.DealSale, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, PriceMax: &nonMatchingMaximum, ActivatedAt: &beforeEvent},
		{ID: 4, UserID: 20, DealType: domain.DealSale, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, PriceMax: &matchingMaximum},
		{ID: 2, UserID: 30, DealType: domain.DealSale, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, PriceMax: &matchingMaximum, ActivatedAt: &occurredAt},
	}

	matched := matchingFiltersByUser(filters, listing, occurredAt)

	if len(matched) != 2 {
		t.Fatalf("matching filters = %+v, want one for each of two users", matched)
	}
	if matched[0].UserID != 10 || matched[0].ID != 3 {
		t.Fatalf("first match = user %d filter %d, want user 10 filter 3", matched[0].UserID, matched[0].ID)
	}
	if matched[1].UserID != 20 || matched[1].ID != 4 {
		t.Fatalf("second match = user %d filter %d, want user 20 filter 4", matched[1].UserID, matched[1].ID)
	}
}
