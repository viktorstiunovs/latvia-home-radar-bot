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

func TestFormatPriceChangeIncludesPercentageAndListingDetails(t *testing.T) {
	l := domain.Listing{Source: "ss.lv", URL: "https://www.ss.lv/msg/example.html", PropertyType: domain.PropertyApartment, DealType: domain.DealRent, Title: "Bright flat", PriceEUR: intp(600), Rooms: intp(2), AreaM2: floatp(50), City: "Rīga", District: "Centrs", Address: "Brīvības 1", Floor: intp(2), TotalFloors: intp(5), BuildingSeries: "Renovated", BuildingType: "Brick"}
	text := FormatPriceChange(testLocalizer(t, localization.English), l, intp(800), intp(600))
	for _, want := range []string{"Apartment for rent · €600 / month", "🟢 <b>Price decreased · −25%</b>", "<s>€800 / month</s>  →  <b>€600 / month</b>", "2 rooms", "50 m²", "Brīvības 1", "Floor:</b> 2/5", "Renovated", "Brick", "Bright flat", "View on SS.lv"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "Reduction:") || strings.Contains(text, "€200") {
		t.Fatalf("price change contains an absolute reduction: %s", text)
	}
}

func TestFormatPriceIncreaseUsesSignedPercentage(t *testing.T) {
	l := domain.Listing{Source: "city24.lv", URL: "https://example.test/listing", PropertyType: domain.PropertyHouse, DealType: domain.DealSale, Title: "House"}
	text := FormatPriceChange(testLocalizer(t, localization.English), l, intp(700), intp(800))
	for _, want := range []string{"House for sale · €800", "🔴 <b>Price increased · +14.3%</b>", "<s>€700</s>  →  <b>€800</b>"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}

func TestFormatUnknownPriceTransitionOmitsReduction(t *testing.T) {
	l := domain.Listing{Source: "city24.lv", URL: "https://example.test/listing", PropertyType: domain.PropertyHouse, DealType: domain.DealSale, Title: "House"}
	text := FormatPriceChange(testLocalizer(t, localization.English), l, nil, intp(200000))
	for _, want := range []string{"Price change", "<s>Price on request</s>  →  <b>€200 000</b>"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "Change:</") || strings.Contains(text, "%") {
		t.Fatalf("unknown transition contains misleading reduction: %s", text)
	}
}

func TestFormatCheaperOfferIncludesActiveAlternatives(t *testing.T) {
	listing := domain.Listing{Source: "city24.lv", URL: "https://city24.test/current?a=1&b=2", PropertyType: domain.PropertyApartment, DealType: domain.DealSale, Title: "Apartment"}
	alternatives := []domain.ListingAlternative{{Source: "ss.lv", URL: "https://ss.test/other?a=1&b=2", PriceEUR: intp(100000)}}
	text := FormatCheaperOffer(testLocalizer(t, localization.English), listing, intp(100000), intp(90000), alternatives)
	for _, want := range []string{"🟢 <b>Cheaper offer · −10%</b>", "<s>€100 000</s>  →  <b>€90 000</b>", "Also currently available", "SS.lv", "€100 000", "a=1&amp;b=2"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}

func TestAlternativeHeadingsAreLocalized(t *testing.T) {
	listing := domain.Listing{Source: "ss.lv", URL: "https://ss.test/current", PropertyType: domain.PropertyHouse, DealType: domain.DealRent, Title: "House"}
	alternatives := []domain.ListingAlternative{{Source: "city24.lv", URL: "https://city24.test/other", PriceEUR: intp(700)}}
	tests := []struct {
		language string
		want     string
	}{
		{localization.English, "Also currently available"},
		{localization.Latvian, "Pašlaik pieejams arī"},
		{localization.Russian, "Также доступно сейчас"},
	}
	for _, test := range tests {
		text := FormatListingWithAlternatives(testLocalizer(t, test.language), listing, alternatives)
		if !strings.Contains(text, test.want) {
			t.Errorf("%s missing %q in %s", test.language, test.want, text)
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
