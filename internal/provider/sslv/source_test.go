package sslv

import (
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
	page := `<table><tr><td class="ads_opt_name">Pilsēta:</td><td class="ads_opt"><b>Rīga</b></td></tr><tr><td class="ads_opt_name">Iela:</td><td class="ads_opt"><b>Brantkalna 11</b> [ Karte ]</td></tr><tr><td class="ads_opt_name">Stāvs:</td><td class="ads_opt">4/9/lifts</td></tr></table><a href="https://i.ss.lv/gallery/8/1/one.800.jpg"></a><a href="https://i.ss.lv/gallery/8/1/one.800.jpg"></a><a href="https://i.ss.lv/gallery/8/1/two.800.jpg"></a>`
	result := EnrichFromPage(listing, page)
	if !result.DetailsEnriched || result.City != "Rīga" || result.Address != "Brantkalna 11" || *result.Floor != 4 || *result.TotalFloors != 9 {
		t.Fatalf("unexpected enrichment: %+v", result)
	}
	photos := ParsePhotoURLs(page, 10)
	if len(photos) != 2 {
		t.Fatalf("expected two unique photos, got %v", photos)
	}
}
