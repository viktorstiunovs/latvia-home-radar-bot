package sslv

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestClassifyAvailability(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   domain.AvailabilityStatus
	}{
		{"live", http.StatusOK, `<div id="msg_div_msg">Advert</div><table><tr id="tr_cont"><td>Phone</td></tr></table>`, domain.AvailabilityActive},
		{"accessible expired", http.StatusOK, `<div id="msg_div_msg">Archived advert still visible</div><table><tr><td class="ads_opt_name">Price</td></tr></table><div id="tr_foto"></div>`, domain.AvailabilityInactive},
		{"missing", http.StatusNotFound, `not found`, domain.AvailabilityInactive},
		{"inconclusive", http.StatusOK, `<html><body>challenge</body></html>`, domain.AvailabilityUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation, err := ClassifyAvailability(test.status, "text/html; charset=utf-8", []byte(test.body))
			if err != nil || observation.Status != test.want || observation.Evidence == "" {
				t.Fatalf("observation=%+v err=%v", observation, err)
			}
		})
	}
}

func TestClassifyAvailabilityRetriesServerFailures(t *testing.T) {
	if _, err := ClassifyAvailability(http.StatusServiceUnavailable, "text/html", nil); err == nil {
		t.Fatal("server failure was not returned")
	}
}

func TestParseOriginalFixtures(t *testing.T) {
	rent, err := ParseFeed(fixture(t, "ss_rent.xml"), domain.DealRent, domain.PropertyApartment)
	if err != nil {
		t.Fatal(err)
	}
	l := rent[0]
	if l.ExternalID != "befikm" || *l.PriceEUR != 750 || *l.Rooms != 2 || *l.AreaM2 != 43.5 || *l.Floor != 4 || *l.TotalFloors != 6 || l.BuildingSeries != "Jaun." || l.AreaKey != "lv/riga/centrs" || l.SourceAreaKey != "riga/centre" || l.Address != "Stabu 46/48" {
		t.Fatalf("unexpected rental listing: %+v", l)
	}
	sale, err := ParseFeed(fixture(t, "ss_sale.xml"), domain.DealSale, domain.PropertyApartment)
	if err != nil {
		t.Fatal(err)
	}
	if *sale[0].PriceEUR != 71000 || sale[0].AreaKey != "lv/riga/imanta" {
		t.Fatalf("unexpected sale listing: %+v", sale[0])
	}
}

func TestEnrichmentAndPhotos(t *testing.T) {
	listing := domain.Listing{PropertyType: domain.PropertyApartment}
	page := `<table><tr><td class="ads_opt_name">Pilsēta:</td><td class="ads_opt"><b>Rīga</b></td></tr><tr><td class="ads_opt_name">Iela:</td><td class="ads_opt"><b>Brantkalna 11</b> [ Karte ]</td></tr><tr><td class="ads_opt_name">Stāvs:</td><td class="ads_opt">4/9/lifts</td></tr></table><div id="msg_div_msg">Pilns <b>dzīvokļa</b> apraksts.<br>Otra rinda.</div><a href="https://i.ss.lv/gallery/8/1/one.800.jpg"></a><a href="https://i.ss.lv/gallery/8/1/one.800.jpg"></a><a href="https://i.ss.lv/gallery/8/1/two.800.jpg"></a>`
	result := EnrichFromPage(listing, page)
	if !result.DetailsEnriched || result.Description != "Pilns dzīvokļa apraksts. Otra rinda." || result.City != "Rīga" || result.Address != "Brantkalna 11" || *result.Floor != 4 || *result.TotalFloors != 9 {
		t.Fatalf("unexpected enrichment: %+v", result)
	}
	photos := ParsePhotoURLs(page, 10)
	if len(photos) != 2 {
		t.Fatalf("expected two unique photos, got %v", photos)
	}
}

func TestDescriptionDoesNotReplaceTitle(t *testing.T) {
	listing := domain.Listing{Title: "Concise title", PropertyType: domain.PropertyApartment}
	result := EnrichFromPage(listing, `<div itemprop="description">Complete <i>useful</i> description</div>`)
	if result.Title != "Concise title" || result.Description != "Complete useful description" {
		t.Fatalf("unexpected listing text: %+v", result)
	}
}
