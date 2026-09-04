package city24

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

const (
	apiURL       = "https://api.city24.lv"
	offersURL    = apiURL + "/realties/offers"
	publicURL    = "https://www.city24.lv/real-estate/"
	itemsPerPage = 100
)

type Source struct {
	property domain.PropertyType
	deal     domain.DealType
	client   *http.Client
	maxPages int
}

func New(property domain.PropertyType, deal domain.DealType, client *http.Client) *Source {
	return &Source{property: property, deal: deal, client: client, maxPages: 1}
}

func NewWithPages(property domain.PropertyType, deal domain.DealType, client *http.Client, maxPages int) (*Source, error) {
	if maxPages < 1 {
		return nil, fmt.Errorf("max pages must be positive")
	}
	return &Source{property: property, deal: deal, client: client, maxPages: maxPages}, nil
}

func (s *Source) Key() string {
	return fmt.Sprintf("city24.lv:latvia:%ss:%s", s.property, s.deal)
}

func (s *Source) FetchRecent(ctx context.Context) ([]domain.Listing, error) {
	var result []domain.Listing
	for page := 1; page <= s.maxPages; page++ {
		query := url.Values{"address[cc]": {"2"}, "tsType": {string(s.deal)}, "unitType": {unitType(s.property)}, "itemsPerPage": {strconv.Itoa(itemsPerPage)}, "page": {strconv.Itoa(page)}, "order[datePublished]": {"desc"}}
		var payload []map[string]any
		response, err := s.getJSON(ctx, offersURL+"?"+query.Encode(), &payload)
		if err != nil {
			return nil, err
		}
		for _, item := range payload {
			listing, err := ParseOffer(item, s.deal, s.property)
			if err != nil {
				return nil, err
			}
			result = append(result, listing)
		}
		if len(payload) < itemsPerPage || lastPage(response.Header.Get("Content-Range"), page) {
			break
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("City24 offers response contained no listings")
	}
	return result, nil
}

func (s *Source) Enrich(ctx context.Context, listing domain.Listing) (domain.Listing, error) {
	var payload map[string]any
	_, err := s.getJSON(ctx, apiURL+"/realties/"+url.PathEscape(listing.ExternalID), &payload)
	if err != nil {
		return listing, err
	}
	detailed, err := ParseOffer(payload, s.deal, s.property)
	if err != nil {
		return listing, err
	}
	detailed.DetailsEnriched = true
	return detailed, nil
}

func (s *Source) getJSON(ctx context.Context, endpoint string, target any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, fmt.Errorf("city24 HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return response, fmt.Errorf("decode City24 response: %w", err)
	}
	return response, nil
}

func unitType(property domain.PropertyType) string {
	if property == domain.PropertyHouse {
		return "House"
	}
	return "Apartment"
}

func transactionType(deal domain.DealType) string {
	if deal == domain.DealRent {
		return "/transaction_types/2"
	}
	return "/transaction_types/1"
}

func lastPage(header string, page int) bool {
	parts := strings.Split(header, "/")
	if len(parts) != 2 {
		return false
	}
	total, err := strconv.Atoi(parts[1])
	return err == nil && page*itemsPerPage >= total
}
