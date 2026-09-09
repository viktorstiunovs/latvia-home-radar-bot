package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	ListingDiscoveredV1   = "listing.discovered.v1"
	ListingPriceChangedV1 = "listing.price_changed.v1"
)

type Envelope struct {
	ID         string          `json:"event_id"`
	Type       string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Data       json.RawMessage `json:"data"`
}

type ListingDiscovered struct {
	ListingID  int64  `json:"listing_id"`
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
}

type ListingPriceChanged struct {
	ListingID        int64  `json:"listing_id"`
	PriceHistoryID   int64  `json:"price_history_id"`
	Source           string `json:"source"`
	ExternalID       string `json:"external_id"`
	PreviousPriceEUR *int   `json:"previous_price_eur"`
	CurrentPriceEUR  *int   `json:"current_price_eur"`
}

type permanentError struct{ cause error }

type dependencyPendingError struct{ cause error }

func (e permanentError) Error() string {
	return e.cause.Error()
}

func (e permanentError) Unwrap() error {
	return e.cause
}

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{cause: err}
}

func IsPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}

func (e dependencyPendingError) Error() string {
	return e.cause.Error()
}

func (e dependencyPendingError) Unwrap() error {
	return e.cause
}

func DependencyPending(err error) error {
	if err == nil {
		return nil
	}
	return dependencyPendingError{cause: err}
}

func IsDependencyPending(err error) bool {
	var target dependencyPendingError
	return errors.As(err, &target)
}

func DecodeListingDiscovered(event Envelope) (ListingDiscovered, error) {
	if event.Type != ListingDiscoveredV1 {
		return ListingDiscovered{}, fmt.Errorf("unexpected event type %q", event.Type)
	}
	var data ListingDiscovered
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return ListingDiscovered{}, fmt.Errorf("decode listing event: %w", err)
	}
	if data.ListingID <= 0 || data.Source == "" || data.ExternalID == "" {
		return ListingDiscovered{}, fmt.Errorf("invalid listing event payload")
	}
	return data, nil
}

func MarshalListingDiscovered(listingID int64, source, externalID string) ([]byte, error) {
	return json.Marshal(ListingDiscovered{
		ListingID:  listingID,
		Source:     source,
		ExternalID: externalID,
	})
}

func DecodeListingPriceChanged(event Envelope) (ListingPriceChanged, error) {
	if event.Type != ListingPriceChangedV1 {
		return ListingPriceChanged{}, fmt.Errorf("unexpected event type %q", event.Type)
	}
	var data ListingPriceChanged
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return ListingPriceChanged{}, fmt.Errorf("decode listing price event: %w", err)
	}
	if data.ListingID <= 0 || data.PriceHistoryID <= 0 || data.Source == "" || data.ExternalID == "" || samePrice(data.PreviousPriceEUR, data.CurrentPriceEUR) {
		return ListingPriceChanged{}, fmt.Errorf("invalid listing price event payload")
	}
	return data, nil
}

func MarshalListingPriceChanged(listingID, priceHistoryID int64, source, externalID string, previousPriceEUR, currentPriceEUR *int) ([]byte, error) {
	return json.Marshal(ListingPriceChanged{
		ListingID:        listingID,
		PriceHistoryID:   priceHistoryID,
		Source:           source,
		ExternalID:       externalID,
		PreviousPriceEUR: previousPriceEUR,
		CurrentPriceEUR:  currentPriceEUR,
	})
}

func samePrice(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
