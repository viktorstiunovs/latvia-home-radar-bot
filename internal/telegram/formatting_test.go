package telegram

import (
	"strings"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/localization"
)

func intp(v int) *int {
	return &v
}

func floatp(v float64) *float64 {
	return &v
}

func TestFormatListingParity(t *testing.T) {
	l := domain.Listing{Source: "ss.lv", URL: "https://www.ss.lv/msg/example.html", PropertyType: domain.PropertyApartment, DealType: domain.DealRent, Title: "Bright <flat>", PriceEUR: intp(540), Rooms: intp(2), AreaM2: floatp(25), City: "Rīga", District: "Centrs", Address: "A&B", Floor: intp(4), TotalFloors: intp(6), BuildingSeries: "Renovated", BuildingType: "Brick"}
	text := FormatListing(testLocalizer(t, localization.English), l)
	for _, want := range []string{"🏢 <b>Apartment for rent · €540 / month</b>", "📍 <b>Centrs, Rīga</b>", "🚪 2 rooms  ·  📐 25 m²", "A&amp;B", "Floor:</b> 4/6", "&lt;flat&gt;", "View on SS.lv →"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}

func TestFormatListingLanguagesAndEscaping(t *testing.T) {
	l := domain.Listing{Source: "city24.lv", URL: "https://example.test/?a=1&b=2", PropertyType: domain.PropertyHouse, DealType: domain.DealSale, Title: "Дом <мечты>", Rooms: intp(5), Address: "A&B", TotalFloors: intp(2)}
	tests := []struct {
		languageTag string
		want        []string
	}{
		{localization.English, []string{"House for sale", "5 rooms", "Street:", "View on City24.lv"}},
		{localization.Latvian, []string{"Māja pārdošanai", "5 istabas", "Iela:", "Skatīt vietnē City24.lv"}},
		{localization.Russian, []string{"Дом на продажу", "5 комнат", "Улица:", "Открыть на City24.lv"}},
	}
	for _, test := range tests {
		text := FormatListing(testLocalizer(t, test.languageTag), l)
		for _, want := range test.want {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing %q in %s", test.languageTag, want, text)
			}
		}
		for _, escaped := range []string{"Дом &lt;мечты&gt;", "A&amp;B", "a=1&amp;b=2"} {
			if !strings.Contains(text, escaped) {
				t.Errorf("%s missing escaped value %q in %s", test.languageTag, escaped, text)
			}
		}
	}
}

func testLocalizer(t *testing.T, languageTag string) localization.Localizer {
	t.Helper()
	catalog, err := localization.New()
	if err != nil {
		t.Fatal(err)
	}
	return catalog.For(languageTag)
}
