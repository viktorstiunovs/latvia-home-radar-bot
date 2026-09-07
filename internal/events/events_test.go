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

func TestListingPriceChangedRoundTripIncludesNullableTransitions(t *testing.T) {
	tests := []struct {
		name     string
		previous *int
		current  *int
	}{
		{name: "decrease", previous: eventInt(900), current: eventInt(700)},
		{name: "known to unknown", previous: eventInt(700)},
		{name: "unknown to known", current: eventInt(650)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := MarshalListingPriceChanged(42, 7, "ss.lv", "abc", test.previous, test.current)
			if err != nil {
				t.Fatal(err)
			}
			event := Envelope{Type: ListingPriceChangedV1, Data: json.RawMessage(payload)}
			change, err := DecodeListingPriceChanged(event)
			if err != nil {
				t.Fatal(err)
			}
			if change.ListingID != 42 || change.PriceHistoryID != 7 || !samePrice(change.PreviousPriceEUR, test.previous) || !samePrice(change.CurrentPriceEUR, test.current) {
				t.Fatalf("unexpected event data: %+v", change)
			}
		})
	}
}

func TestListingPriceChangedRejectsUnchangedPrice(t *testing.T) {
	payload, err := MarshalListingPriceChanged(42, 7, "ss.lv", "abc", eventInt(700), eventInt(700))
	if err != nil {
		t.Fatal(err)
	}
	event := Envelope{Type: ListingPriceChangedV1, Data: json.RawMessage(payload)}
	if _, err := DecodeListingPriceChanged(event); err == nil {
		t.Fatal("expected unchanged price event to fail")
	}
}

func eventInt(value int) *int {
	return &value
}
