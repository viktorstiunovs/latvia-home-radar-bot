package domain

import "testing"

func ptr[T any](v T) *T {
	return &v
}

func TestMatchesInclusiveAndHierarchical(t *testing.T) {
	l := Listing{DealType: DealRent, PropertyType: PropertyApartment, AreaKey: "lv/riga/agenskalns", PriceEUR: ptr(500), Rooms: ptr(2), AreaM2: ptr(45.0)}
	f := SearchFilter{DealType: DealRent, PropertyTypes: []PropertyType{PropertyApartment}, AreaKeys: []string{"lv/riga"}, PriceMin: ptr(500), PriceMax: ptr(500), RoomsMin: ptr(2), AreaMin: ptr(45.0)}
	if !Matches(l, f) {
		t.Fatal("expected listing to match inclusive parent-area filter")
	}
	f.AreaKeys = []string{"lv/jurmala"}
	if Matches(l, f) {
		t.Fatal("unrelated area matched")
	}
	f.AreaKeys = nil
	l.AreaM2 = nil
	if Matches(l, f) {
		t.Fatal("missing constrained value matched")
	}
}
