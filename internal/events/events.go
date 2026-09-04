package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const ListingDiscoveredV1 = "listing.discovered.v1"

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

type permanentError struct{ cause error }

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
