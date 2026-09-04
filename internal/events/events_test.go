package events

import (
	"encoding/json"
	"testing"
)

func TestListingDiscoveredRoundTrip(t *testing.T) {
	payload, err := MarshalListingDiscovered(42, "ss.lv", "abc")
	if err != nil {
		t.Fatal(err)
	}
	event := Envelope{Type: ListingDiscoveredV1, Data: json.RawMessage(payload)}
	listing, err := DecodeListingDiscovered(event)
	if err != nil {
		t.Fatal(err)
	}
	if listing.ListingID != 42 || listing.Source != "ss.lv" || listing.ExternalID != "abc" {
		t.Fatalf("unexpected event data: %+v", listing)
	}
}

func TestListingDiscoveredRejectsInvalidEvent(t *testing.T) {
	event := Envelope{Type: "listing.discovered.v2", Data: json.RawMessage(`{}`)}
	if _, err := DecodeListingDiscovered(event); err == nil {
		t.Fatal("expected invalid event to fail")
	}
}
