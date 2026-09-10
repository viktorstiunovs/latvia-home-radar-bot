package city24

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
)

type cityRoundTripFunc func(*http.Request) (*http.Response, error)

func (f cityRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestEnrichClassifiesRemovedListing(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &http.Client{Transport: cityRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			})}
			source := New(domain.PropertyApartment, domain.DealSale, client)

			_, err := source.Enrich(context.Background(), domain.Listing{ExternalID: "removed"})

			var unavailable *provider.ListingUnavailableError
			if !errors.As(err, &unavailable) || unavailable.Evidence != fmt.Sprintf("City24 HTTP %d", status) {
				t.Fatalf("error=%v unavailable=%+v", err, unavailable)
			}
		})
	}
}

func TestEnrichKeepsServerFailureRetryable(t *testing.T) {
	client := &http.Client{Transport: cityRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		}, nil
	})}
	source := New(domain.PropertyApartment, domain.DealSale, client)

	_, err := source.Enrich(context.Background(), domain.Listing{ExternalID: "temporarily-unavailable"})

	var unavailable *provider.ListingUnavailableError
	if err == nil || errors.As(err, &unavailable) {
		t.Fatalf("error=%v unavailable=%+v", err, unavailable)
	}
}
