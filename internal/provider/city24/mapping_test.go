package city24

import (
	"encoding/json"
	"net/http"
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

func TestClassifyAvailability(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   domain.AvailabilityStatus
	}{
		{"published", http.StatusOK, `{"id":"42","status_id":2}`, domain.AvailabilityActive},
		{"removed", http.StatusGone, `{}`, domain.AvailabilityInactive},
		{"unrecognized status", http.StatusOK, `{"id":"42","status_id":7}`, domain.AvailabilityUnknown},
		{"malformed", http.StatusOK, `{`, domain.AvailabilityUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation, err := ClassifyAvailability(test.status, "application/json", []byte(test.body))
			if err != nil || observation.Status != test.want || observation.Evidence == "" {
				t.Fatalf("observation=%+v err=%v", observation, err)
			}
		})
	}
}

func TestParseOriginalFixtures(t *testing.T) {
	l, err := ParseOffer(cityFixture(t, "city24_apartment.json"), domain.DealRent, domain.PropertyApartment)
	if err != nil {
		t.Fatal(err)
	}
	if l.ExternalID != "2469393" || *l.PriceEUR != 800 || *l.Rooms != 3 || *l.AreaM2 != 77.6 || l.Address != "Stendes iela 5" || l.AreaKey != "lv/riga/sampeteris-pleskodale" || l.BuildingSeries != "New project" || len(l.PhotoURLs) != 2 || l.Description != "Mājīgs 3 istabu dzīvoklis Šampēterī. Pieejams tagad." {
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

func TestDescriptionCombinesCompleteLocalizedFields(t *testing.T) {
	payload := cityFixture(t, "city24_apartment.json")
	payload["descriptions"] = map[string]any{"lv_LV": map[string]any{
		"introduction": "Īss <b>ievads</b>.",
		"description":  "Pilns apraksts.<br>Otra rinda.",
	}}
	listing, err := ParseOffer(payload, domain.DealRent, domain.PropertyApartment)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Description != "Īss ievads. Pilns apraksts. Otra rinda." || listing.Title != "Īss ievads." {
		t.Fatalf("unexpected listing text: %+v", listing)
	}
}
