package domimaps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
)

func TestFetchRecentUsesMetadataAndDeduplicatesOrderedPages(t *testing.T) {
	var listCalls atomic.Int32
	client := testClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/javascript")
		switch request.URL.Path {
		case "/mm":
			if request.URL.Query().Get("f") != "1011" {
				t.Errorf("metadata filter = %q", request.URL.Query().Get("f"))
			}
			writeJSONP(t, response, request.URL.Query().Get("cb"), map[string]any{
				"t": 95,
				"s": []any{[]any{[]int64{26, 123}}},
			})
		case "/ld":
			listCalls.Add(1)
			query := request.URL.Query()
			if query.Get("fv") != "12395" || query.Get("s") != "2" || query.Get("cr") != "1" || query.Get("l") != "lv" || query.Get("bb") != latviaBounds {
				t.Errorf("listing query = %v", query)
			}
			page, _ := strconv.Atoi(query.Get("p"))
			ids := make([]int64, 0, itemsPerPage)
			if page == 1 {
				for id := int64(1); id <= itemsPerPage; id++ {
					ids = append(ids, id)
				}
			} else {
				ids = []int64{25, 26}
			}
			adverts := make(map[string]summary, len(ids))
			for _, id := range ids {
				adverts[strconv.FormatInt(id, 10)] = summary{Heading: "1 istabas dzīvoklis", Address: "Rīga, Centrs, Brīvības iela " + strconv.FormatInt(id, 10)}
			}
			adverts["1"] = summary{Price: "48.000 EUR", Heading: "2 istabu dzīvoklis", Address: "Rīga, Ķengarags, Latgales iela 268", Facts: []string{"42,6 m^2^", "2/5st."}, PhotoCount: 2, Published: "11.09.2026"}
			writeJSONP(t, response, query.Get("callback"), listingsResponse{Total: 26, Adverts: adverts, Ordering: ids})
		default:
			http.NotFound(response, request)
		}
	}))

	source, err := NewWithPages(domain.PropertyApartment, domain.DealSale, client, 4)
	if err != nil {
		t.Fatal(err)
	}
	listings, err := source.FetchRecent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if listCalls.Load() != 2 || len(listings) != 26 {
		t.Fatalf("calls=%d listings=%d", listCalls.Load(), len(listings))
	}
	if listings[0].ExternalID != "1" || listings[25].ExternalID != "26" || listings[0].URL != "https://ad.domimaps.lv/1" {
		t.Fatalf("listing order = first=%+v last=%+v", listings[0], listings[25])
	}
}

func TestFetchRecentHonorsPageLimit(t *testing.T) {
	var listCalls atomic.Int32
	client := testClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/javascript")
		callback := request.URL.Query().Get("callback")
		if request.URL.Path == "/mm" {
			callback = request.URL.Query().Get("cb")
			writeJSONP(t, response, callback, map[string]any{"t": 1, "s": []any{[]any{[]int64{1000, 5}}}})
			return
		}
		listCalls.Add(1)
		page, _ := strconv.Atoi(request.URL.Query().Get("p"))
		ids := make([]int64, 0, itemsPerPage)
		adverts := make(map[string]summary, itemsPerPage)
		for index := 1; index <= itemsPerPage; index++ {
			id := int64((page-1)*itemsPerPage + index)
			ids = append(ids, id)
			adverts[strconv.FormatInt(id, 10)] = summary{Heading: "Dzīvoklis"}
		}
		writeJSONP(t, response, callback, listingsResponse{Total: 1000, Adverts: adverts, Ordering: ids})
	}))

	source, err := NewWithPages(domain.PropertyApartment, domain.DealSale, client, 2)
	if err != nil {
		t.Fatal(err)
	}
	listings, err := source.FetchRecent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if listCalls.Load() != 2 || len(listings) != 50 {
		t.Fatalf("calls=%d listings=%d", listCalls.Load(), len(listings))
	}
}

func TestEnrichAndAvailabilityUseValidFavoriteOmissionOnly(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		id := request.URL.Query().Get("a")
		callback := request.URL.Query().Get("callback")
		switch id {
		case "active":
			response.Header().Set("Content-Type", "application/javascript")
			writeJSONP(t, response, callback, favoritesResponse{Adverts: map[string]summary{"active": {Price: "700 EUR", Heading: "2 istabu dzīvoklis", PhotoCount: 1}}})
		case "missing":
			response.Header().Set("Content-Type", "application/javascript")
			writeJSONP(t, response, callback, favoritesResponse{Adverts: map[string]summary{}})
		case "malformed":
			response.Header().Set("Content-Type", "application/javascript")
			fmt.Fprintf(response, "UMap.%s&&UMap.%s({", callback, callback)
		case "challenge":
			response.Header().Set("Content-Type", "text/html")
			fmt.Fprint(response, "<html>challenge</html>")
		case "schema":
			response.Header().Set("Content-Type", "application/javascript")
			writeJSONP(t, response, callback, map[string]any{})
		case "failure":
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(response, request)
		}
	}))

	source := New(domain.PropertyApartment, domain.DealRent, client)
	base := domain.Listing{ID: 7, Source: "domimaps.lv", ExternalID: "active", DealType: domain.DealRent, PropertyType: domain.PropertyApartment}
	enriched, err := source.Enrich(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if enriched.ID != 7 || !enriched.DetailsEnriched || enriched.PriceEUR == nil || *enriched.PriceEUR != 700 || enriched.URL != "https://ad.domimaps.lv/active" {
		t.Fatalf("enriched = %+v", enriched)
	}
	observation, err := source.CheckAvailability(context.Background(), base)
	if err != nil || observation.Status != domain.AvailabilityActive {
		t.Fatalf("active observation = %+v, %v", observation, err)
	}

	missing := base
	missing.ExternalID = "missing"
	if _, err := source.Enrich(context.Background(), missing); err == nil {
		t.Fatal("missing advert enriched without error")
	} else {
		var unavailable *provider.ListingUnavailableError
		if !errors.As(err, &unavailable) {
			t.Fatalf("missing error = %T %v", err, err)
		}
	}
	observation, err = source.CheckAvailability(context.Background(), missing)
	if err != nil || observation.Status != domain.AvailabilityInactive {
		t.Fatalf("missing observation = %+v, %v", observation, err)
	}

	for _, id := range []string{"malformed", "challenge", "schema"} {
		listing := base
		listing.ExternalID = id
		observation, err = source.CheckAvailability(context.Background(), listing)
		if err != nil || observation.Status != domain.AvailabilityUnknown {
			t.Fatalf("%s observation = %+v, %v", id, observation, err)
		}
	}
	failure := base
	failure.ExternalID = "failure"
	if _, err := source.CheckAvailability(context.Background(), failure); err == nil {
		t.Fatal("HTTP failure was treated as availability evidence")
	}
}

func TestFetchRecentRejectsWrongCallbackAndPropagatesCancellation(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(response, `UMap.WRONGCB1&&UMap.WRONGCB1({"t":1,"s":[]});`)
	}))
	source := New(domain.PropertyApartment, domain.DealSale, client)
	if _, err := source.FetchRecent(context.Background()); err == nil || !strings.Contains(err.Error(), "does not match callback") {
		t.Fatalf("wrong-callback error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.FetchRecent(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := request.Context().Err(); err != nil {
			return nil, err
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		return response.Result(), nil
	})}

	return &Client{transport: newClient(httpClient, "https://d1.domimaps.test")}
}

func writeJSONP(t *testing.T, response http.ResponseWriter, callback string, payload any) {
	t.Helper()
	if len(callback) != 8 {
		t.Errorf("callback = %q", callback)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(response, "UMap.%s&&UMap.%s(%s);", callback, callback, encoded)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := f(request)
	if response != nil && response.Body == nil {
		response.Body = io.NopCloser(strings.NewReader(""))
	}

	return response, err
}
