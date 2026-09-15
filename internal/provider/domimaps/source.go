package domimaps

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
)

const (
	dataURL           = "https://d1.domimaps.lv"
	latviaBounds      = "37462032,20182576,38818936,21005473"
	itemsPerPage      = 25
	defaultMaxPages   = 4
	availabilityLimit = 15 * time.Second
)

type Client struct {
	transport *client
	details   *detailClient
}

type Source struct {
	property domain.PropertyType
	deal     domain.DealType
	client   *Client
	maxPages int
}

type metadataResponse struct {
	Timestamp int64       `json:"t"`
	Sources   [][][]int64 `json:"s"`
}

type listingsResponse struct {
	Total    int                `json:"t"`
	Adverts  map[string]summary `json:"a"`
	Ordering []int64            `json:"o"`
}

type favoritesResponse struct {
	Adverts map[string]summary `json:"a"`
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{transport: newClient(httpClient, dataURL)}
}

func NewClientWithDetails(httpClient *http.Client, detailServiceURL string) (*Client, error) {
	result := NewClient(httpClient)
	if strings.TrimSpace(detailServiceURL) == "" {
		return result, nil
	}
	details, err := newDetailClient(httpClient, detailServiceURL)
	if err != nil {
		return nil, err
	}
	result.details = details

	return result, nil
}

func New(property domain.PropertyType, deal domain.DealType, client *Client) *Source {
	return &Source{property: property, deal: deal, client: client, maxPages: defaultMaxPages}
}

func NewWithPages(property domain.PropertyType, deal domain.DealType, client *Client, maxPages int) (*Source, error) {
	if maxPages < 1 {
		return nil, fmt.Errorf("max pages must be positive")
	}

	return &Source{property: property, deal: deal, client: client, maxPages: maxPages}, nil
}

func (s *Source) Key() string {
	return fmt.Sprintf("domimaps.lv:latvia:%ss:%s", s.property, s.deal)
}

func (s *Source) FetchRecent(ctx context.Context) ([]domain.Listing, error) {
	filter, err := filterCode(s.property, s.deal)
	if err != nil {
		return nil, err
	}
	version, err := s.client.catalogueVersion(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("fetch DOMImaps metadata for filter %s: %w", filter, err)
	}

	result := make([]domain.Listing, 0, s.maxPages*itemsPerPage)
	seen := make(map[string]bool)
	for page := 1; page <= s.maxPages; page++ {
		payload, err := s.client.listings(ctx, filter, version, page)
		if err != nil {
			return nil, fmt.Errorf("fetch DOMImaps filter %s page %d: %w", filter, page, err)
		}
		for _, numericID := range payload.Ordering {
			id := strconv.FormatInt(numericID, 10)
			if seen[id] {
				continue
			}
			value, ok := payload.Adverts[id]
			if !ok {
				return nil, fmt.Errorf("DOMImaps filter %s page %d is missing ordered advert %s", filter, page, id)
			}
			listing, err := listingFromSummary(id, value, s.deal, s.property)
			if err != nil {
				return nil, fmt.Errorf("map DOMImaps advert %s: %w", id, err)
			}
			result = append(result, listing)
			seen[id] = true
		}
		if len(payload.Ordering) < itemsPerPage || page*itemsPerPage >= payload.Total {
			break
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("DOMImaps filter %s response contained no listings", filter)
	}

	return result, nil
}

func (s *Source) Enrich(ctx context.Context, listing domain.Listing) (domain.Listing, error) {
	value, found, err := s.client.favorite(ctx, listing.ExternalID)
	if err != nil {
		return listing, fmt.Errorf("enrich DOMImaps advert %s: %w", listing.ExternalID, err)
	}
	if !found {
		return listing, &provider.ListingUnavailableError{Evidence: "DOMImaps favorites response omitted advert " + listing.ExternalID}
	}
	enriched, err := listingFromSummary(listing.ExternalID, value, s.deal, s.property)
	if err != nil {
		return listing, err
	}
	enriched.ID = listing.ID
	enriched.FirstSeenAt = listing.FirstSeenAt
	enriched.LastSeenAt = listing.LastSeenAt
	enriched.AvailabilityStatus = listing.AvailabilityStatus
	enriched.AvailabilityChangedAt = listing.AvailabilityChangedAt
	enriched.Description = listing.Description
	if s.client.details != nil {
		description, err := s.client.details.description(ctx, listing.ExternalID)
		if err != nil {
			enriched.DetailsEnriched = false

			return enriched, &provider.PartialEnrichmentError{Err: fmt.Errorf("fetch DOMImaps advert description: %w", err)}
		}
		enriched.Description = description
	}
	enriched.DetailsEnriched = true

	return enriched, nil
}

func (s *Source) CheckAvailability(ctx context.Context, listing domain.Listing) (domain.AvailabilityObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, availabilityLimit)
	defer cancel()
	callback := nextCallback()
	status, contentType, body, err := s.client.transport.get(ctx, "/fv", url.Values{
		"l":        {"lv"},
		"a":        {listing.ExternalID},
		"callback": {callback},
	})
	if err != nil {
		return domain.AvailabilityObservation{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return domain.AvailabilityObservation{}, fmt.Errorf("DOMImaps availability HTTP %d", status)
	}
	if !isJavaScriptContentType(contentType) {
		return domain.AvailabilityObservation{Status: domain.AvailabilityUnknown, Evidence: "DOMImaps returned non-JavaScript content"}, nil
	}
	var payload favoritesResponse
	if err := decodeJSONP(body, callback, &payload); err != nil {
		return domain.AvailabilityObservation{Status: domain.AvailabilityUnknown, Evidence: "DOMImaps availability response could not be decoded"}, nil
	}
	if payload.Adverts == nil {
		return domain.AvailabilityObservation{Status: domain.AvailabilityUnknown, Evidence: "DOMImaps availability response has no advert dictionary"}, nil
	}
	if _, found := payload.Adverts[listing.ExternalID]; found {
		return domain.AvailabilityObservation{Status: domain.AvailabilityActive, Evidence: "DOMImaps favorites response contains advert"}, nil
	}

	return domain.AvailabilityObservation{Status: domain.AvailabilityInactive, Evidence: "DOMImaps favorites response omitted advert"}, nil
}

func (c *Client) catalogueVersion(ctx context.Context, filter string) (int64, error) {
	callback := nextCallback()
	status, _, body, err := c.transport.get(ctx, "/mm", url.Values{"f": {filter}, "cb": {callback}})
	if err != nil {
		return 0, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("DOMImaps metadata HTTP %d", status)
	}
	var payload metadataResponse
	if err := decodeJSONP(body, callback, &payload); err != nil {
		return 0, err
	}
	objectIndex := int(filter[2] - '1')
	subtypeIndex := int(filter[3] - '1')
	if objectIndex < 0 || objectIndex >= len(payload.Sources) || subtypeIndex < 0 || subtypeIndex >= len(payload.Sources[objectIndex]) {
		return 0, fmt.Errorf("DOMImaps metadata has no source cell for filter %s", filter)
	}
	cell := payload.Sources[objectIndex][subtypeIndex]
	if len(cell) < 2 || cell[1] <= 0 {
		return 0, fmt.Errorf("DOMImaps metadata has invalid version cell for filter %s", filter)
	}

	return cell[1]*100 + payload.Timestamp%100, nil
}

func (c *Client) listings(ctx context.Context, filter string, version int64, page int) (listingsResponse, error) {
	callback := nextCallback()
	status, _, body, err := c.transport.get(ctx, "/ld", url.Values{
		"f":        {filter},
		"fv":       {strconv.FormatInt(version, 10)},
		"bb":       {latviaBounds},
		"p":        {strconv.Itoa(page)},
		"s":        {"2"},
		"cr":       {"1"},
		"l":        {"lv"},
		"callback": {callback},
	})
	if err != nil {
		return listingsResponse{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return listingsResponse{}, fmt.Errorf("DOMImaps listings HTTP %d", status)
	}
	var payload listingsResponse
	if err := decodeJSONP(body, callback, &payload); err != nil {
		return listingsResponse{}, err
	}
	if payload.Adverts == nil || payload.Ordering == nil {
		return listingsResponse{}, fmt.Errorf("DOMImaps listings response has no advert dictionary or ordering")
	}

	return payload, nil
}

func (c *Client) favorite(ctx context.Context, id string) (summary, bool, error) {
	if strings.TrimSpace(id) == "" {
		return summary{}, false, fmt.Errorf("DOMImaps advert id is empty")
	}
	callback := nextCallback()
	status, _, body, err := c.transport.get(ctx, "/fv", url.Values{
		"l":        {"lv"},
		"a":        {id},
		"callback": {callback},
	})
	if err != nil {
		return summary{}, false, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return summary{}, false, fmt.Errorf("DOMImaps favorites HTTP %d", status)
	}
	var payload favoritesResponse
	if err := decodeJSONP(body, callback, &payload); err != nil {
		return summary{}, false, err
	}
	if payload.Adverts == nil {
		return summary{}, false, fmt.Errorf("DOMImaps favorites response has no advert dictionary")
	}
	value, found := payload.Adverts[id]

	return value, found, nil
}

func filterCode(property domain.PropertyType, deal domain.DealType) (string, error) {
	switch {
	case property == domain.PropertyApartment && deal == domain.DealSale:
		return "1011", nil
	case property == domain.PropertyHouse && deal == domain.DealSale:
		return "1021", nil
	case property == domain.PropertyApartment && deal == domain.DealRent:
		return "5011", nil
	case property == domain.PropertyHouse && deal == domain.DealRent:
		return "5021", nil
	default:
		return "", fmt.Errorf("unsupported DOMImaps property/deal combination %q/%q", property, deal)
	}
}
