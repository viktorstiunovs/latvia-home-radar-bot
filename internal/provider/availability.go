package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const MaxAvailabilityResponseBytes int64 = 2 << 20

func FetchAvailability(ctx context.Context, client *http.Client, endpoint string, allowedHost func(string) bool, accept string) (int, string, []byte, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || !allowedHost(strings.ToLower(parsed.Hostname())) {
		return 0, "", nil, fmt.Errorf("availability URL is outside the provider host")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return 0, "", nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	boundedClient := *client
	previousRedirect := client.CheckRedirect
	boundedClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" || !allowedHost(strings.ToLower(request.URL.Hostname())) {
			return fmt.Errorf("availability redirect is outside the provider host")
		}
		if len(via) >= 5 {
			return fmt.Errorf("too many availability redirects")
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		return nil
	}
	response, err := boundedClient.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer response.Body.Close()
	if response.ContentLength > MaxAvailabilityResponseBytes {
		return response.StatusCode, response.Header.Get("Content-Type"), nil, fmt.Errorf("availability response exceeds %d bytes", MaxAvailabilityResponseBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxAvailabilityResponseBytes+1))
	if err != nil {
		return response.StatusCode, response.Header.Get("Content-Type"), nil, err
	}
	if int64(len(body)) > MaxAvailabilityResponseBytes {
		return response.StatusCode, response.Header.Get("Content-Type"), nil, fmt.Errorf("availability response exceeds %d bytes", MaxAvailabilityResponseBytes)
	}
	return response.StatusCode, response.Header.Get("Content-Type"), body, nil
}
