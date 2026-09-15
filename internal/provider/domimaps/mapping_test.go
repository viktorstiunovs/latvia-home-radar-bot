package domimaps

import (
	"testing"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

func TestListingFromSummaryMapsApartmentFields(t *testing.T) {
	listing, err := listingFromSummary("29980096", summary{
		Price:      "48.000 EUR",
		Heading:    "2 istabu dzīvoklis",
		Address:    "Rīga, Ķengarags, Latgales Iela 268 K-6",
		Facts:      []string{"42,6 m^2^", "2/5st.", "1970 g."},
		PhotoCount: 12,
		Published:  "11.09.2026",
	}, domain.DealSale, domain.PropertyApartment)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Source != "domimaps.lv" || listing.ExternalID != "29980096" || listing.URL != "https://ad.domimaps.lv/29980096" {
		t.Fatalf("identity = %+v", listing)
	}
	if listing.PriceEUR == nil || *listing.PriceEUR != 48000 || listing.Rooms == nil || *listing.Rooms != 2 {
		t.Fatalf("price=%v rooms=%v", listing.PriceEUR, listing.Rooms)
	}
	if listing.AreaM2 == nil || *listing.AreaM2 != 42.6 || listing.Floor == nil || *listing.Floor != 2 || listing.TotalFloors == nil || *listing.TotalFloors != 5 {
		t.Fatalf("area=%v floor=%v total=%v", listing.AreaM2, listing.Floor, listing.TotalFloors)
	}
	if listing.City != "Rīga" || listing.District != "Ķengarags" || listing.Address != "Latgales Iela 268 K-6" {
		t.Fatalf("location = %q / %q / %q", listing.City, listing.District, listing.Address)
	}
	if listing.PublishedAt == nil || listing.PublishedAt.Format(time.DateOnly) != "2026-09-11" {
		t.Fatalf("published = %v", listing.PublishedAt)
	}
	if len(listing.PhotoURLs) != 10 || listing.ImageURL != "https://adr.domimaps.lv/get_image/m-umap/29980096_0.jpg" || listing.PhotoURLs[9] != "https://adr.domimaps.lv/get_image/m-umap/29980096_9.jpg" {
		t.Fatalf("photos = %#v", listing.PhotoURLs)
	}
}

func TestListingFromSummaryMapsHouseAndToleratesMissingFields(t *testing.T) {
	listing, err := listingFromSummary("908232815", summary{
		Price:      "1.750.000 EUR",
		Heading:    "Dzīvojamā māja",
		Address:    "Jelgavas nov., Kalnciems, Draudzības iela",
		Facts:      []string{"2.285 m^2^", "zeme 3.700 m^2^", "2 st."},
		PhotoCount: 3,
	}, domain.DealSale, domain.PropertyHouse)
	if err != nil {
		t.Fatal(err)
	}
	if listing.PriceEUR == nil || *listing.PriceEUR != 1750000 || listing.AreaM2 == nil || *listing.AreaM2 != 2285 || listing.LandAreaM2 == nil || *listing.LandAreaM2 != 3700 {
		t.Fatalf("price=%v area=%v land=%v", listing.PriceEUR, listing.AreaM2, listing.LandAreaM2)
	}
	if listing.Floor != nil || listing.TotalFloors == nil || *listing.TotalFloors != 2 || listing.Rooms != nil || listing.PublishedAt != nil {
		t.Fatalf("optional fields floor=%v total=%v rooms=%v published=%v", listing.Floor, listing.TotalFloors, listing.Rooms, listing.PublishedAt)
	}
	if listing.City != "Jelgavas nov." || listing.District != "Kalnciems" || listing.Address != "Draudzības iela" || len(listing.PhotoURLs) != 3 {
		t.Fatalf("location/photos = %q / %q / %q / %d", listing.City, listing.District, listing.Address, len(listing.PhotoURLs))
	}

	missing, err := listingFromSummary("1", summary{Price: "Pēc vienošanās", Address: "Rīga, Brīvības iela"}, domain.DealRent, domain.PropertyApartment)
	if err != nil {
		t.Fatal(err)
	}
	if missing.PriceEUR != nil || missing.AreaM2 != nil || missing.LandAreaM2 != nil || missing.City != "Rīga" || missing.Address != "Brīvības iela" || len(missing.PhotoURLs) != 0 {
		t.Fatalf("missing-field mapping = %+v", missing)
	}
}

func TestFilterCodes(t *testing.T) {
	tests := []struct {
		property domain.PropertyType
		deal     domain.DealType
		want     string
	}{
		{domain.PropertyApartment, domain.DealSale, "1011"},
		{domain.PropertyHouse, domain.DealSale, "1021"},
		{domain.PropertyApartment, domain.DealRent, "5011"},
		{domain.PropertyHouse, domain.DealRent, "5021"},
	}
	for _, test := range tests {
		got, err := filterCode(test.property, test.deal)
		if err != nil || got != test.want {
			t.Fatalf("filterCode(%q, %q) = %q, %v", test.property, test.deal, got, err)
		}
	}
}
