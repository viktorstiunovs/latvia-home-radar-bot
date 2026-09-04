package city24

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

func cityFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(content, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestParseOriginalFixtures(t *testing.T) {
	l, err := ParseOffer(cityFixture(t, "city24_apartment.json"), domain.DealRent, domain.PropertyApartment)
	if err != nil {
		t.Fatal(err)
	}
	if l.ExternalID != "2469393" || *l.PriceEUR != 800 || *l.Rooms != 3 || *l.AreaM2 != 77.6 || l.Address != "Stendes iela 5" || l.AreaKey != "lv/riga/sampeteris-pleskodale" || l.BuildingSeries != "New project" || len(l.PhotoURLs) != 2 {
		t.Fatalf("unexpected apartment: %+v", l)
	}
	house, err := ParseOffer(cityFixture(t, "city24_house.json"), domain.DealSale, domain.PropertyHouse)
	if err != nil {
		t.Fatal(err)
	}
	if *house.LandAreaM2 != 36000 || house.AreaKey != "lv/riga/bergi" || *house.TotalFloors != 2 {
		t.Fatalf("unexpected house: %+v", house)
	}
}
