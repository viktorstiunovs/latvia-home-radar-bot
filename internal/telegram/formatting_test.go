package telegram

import (
	"strings"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

func intp(v int) *int {
	return &v
}

func floatp(v float64) *float64 {
	return &v
}

func TestFormatListingParity(t *testing.T) {
	l := domain.Listing{Source: "ss.lv", URL: "https://www.ss.lv/msg/example.html", PropertyType: domain.PropertyApartment, DealType: domain.DealRent, Title: "Bright <flat>", PriceEUR: intp(540), Rooms: intp(2), AreaM2: floatp(25), City: "Rīga", District: "Centrs", Address: "A&B", Floor: intp(4), TotalFloors: intp(6), BuildingSeries: "Renovated", BuildingType: "Brick"}
	text := FormatListing(l)
	for _, want := range []string{"🏢 <b>Apartment for rent · €540 / month</b>", "📍 <b>Centrs, Rīga</b>", "🚪 2 rooms  ·  📐 25 m²", "A&amp;B", "Floor:</b> 4/6", "&lt;flat&gt;", "View on SS.lv →"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}
