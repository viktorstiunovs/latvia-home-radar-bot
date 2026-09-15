package domimaps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
)

func TestDetailClientRequestsDescriptionByExternalID(t *testing.T) {
	httpClient := testHTTPClient(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/internal"+detailServiceRequestPath {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if contentType := request.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type = %q", contentType)
		}
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprint(response, `{"external_id":"908232815","description":"  Plašs dzīvoklis.  "}`)
	}))
	details, err := newDetailClient(httpClient, "https://detail.test/internal/")
	if err != nil {
		t.Fatal(err)
	}

	description, err := details.description(context.Background(), "908232815")
	if err != nil {
		t.Fatal(err)
	}
	if description != "Plašs dzīvoklis." {
		t.Fatalf("description = %q", description)
	}
}

func TestDetailClientRejectsUnsafeConfigurationAndAdvertIDs(t *testing.T) {
	for _, rawURL := range []string{"", "ftp://detail.example", "http://user:pass@detail.example", "http://detail.example?target=elsewhere"} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := newDetailClient(http.DefaultClient, rawURL); err == nil {
				t.Fatalf("URL %q was accepted", rawURL)
			}
		})
	}
	details, err := newDetailClient(http.DefaultClient, "http://detail.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, externalID := range []string{"", "ad-123", "123/../../target", "https://example.com", strings.Repeat("1", 21)} {
		if _, err := details.description(context.Background(), externalID); err == nil {
			t.Fatalf("external ID %q was accepted", externalID)
		}
	}
}

func TestDetailClientRejectsRetryableServiceFailures(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"unavailable": func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(response, `{"error":"challenge did not clear"}`)
		},
		"malformed": func(response http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(response, `{`)
		},
		"empty": func(response http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(response, `{"description":" "}`)
		},
		"oversized": func(response http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(response, `{"description":"%s"}`, strings.Repeat("x", maxDetailResponseBytes))
		},
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			details, err := newDetailClient(testHTTPClient(handler), "https://detail.test")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := details.description(context.Background(), "908232815"); err == nil {
				t.Fatal("invalid response was accepted")
			}
		})
	}
}

func TestEnrichMergesDescriptionAndKeepsBrowserFailurePartial(t *testing.T) {
	const externalID = "908232815"
	client := testClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/javascript")
		writeJSONP(t, response, request.URL.Query().Get("callback"), favoritesResponse{Adverts: map[string]summary{
			externalID: {Price: "700 EUR", Heading: "2 istabu dzīvoklis", PhotoCount: 1},
		}})
	}))
	detailAvailable := true
	detailHTTPClient := testHTTPClient(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if !detailAvailable {
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(response, `{"description":"Sludinājuma apraksts"}`)
	}))
	details, err := newDetailClient(detailHTTPClient, "https://detail.test")
	if err != nil {
		t.Fatal(err)
	}
	client.details = details
	source := New(domain.PropertyApartment, domain.DealRent, client)
	listing := domain.Listing{ID: 7, Source: "domimaps.lv", ExternalID: externalID, DealType: domain.DealRent, PropertyType: domain.PropertyApartment}

	enriched, err := source.Enrich(context.Background(), listing)
	if err != nil {
		t.Fatal(err)
	}
	if enriched.Description != "Sludinājuma apraksts" || !enriched.DetailsEnriched {
		t.Fatalf("enriched = %+v", enriched)
	}

	detailAvailable = false
	partial, err := source.Enrich(context.Background(), listing)
	if err == nil {
		t.Fatal("detail outage did not produce an error")
	}
	var partialError *provider.PartialEnrichmentError
	if !errors.As(err, &partialError) {
		t.Fatalf("error = %T %v", err, err)
	}
	if partial.DetailsEnriched || partial.PriceEUR == nil || *partial.PriceEUR != 700 {
		t.Fatalf("partial listing = %+v", partial)
	}
}

func testHTTPClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		return response.Result(), nil
	})}
}
