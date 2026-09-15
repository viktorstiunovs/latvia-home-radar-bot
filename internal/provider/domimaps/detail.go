package domimaps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	detailRequestLimit       = 25 * time.Second
	maxDetailResponseBytes   = 128 << 10
	maxDescriptionBytes      = 64 << 10
	detailServiceRequestPath = "/v1/domimaps/details"
)

type detailClient struct {
	http     *http.Client
	endpoint string
}

type detailRequest struct {
	ExternalID string `json:"external_id"`
}

type detailResponse struct {
	Description string `json:"description"`
	Error       string `json:"error"`
}

func newDetailClient(httpClient *http.Client, rawBaseURL string) (*detailClient, error) {
	baseURL, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse DOMImaps detail service URL: %w", err)
	}
	if (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("DOMImaps detail service URL must be an absolute HTTP or HTTPS URL")
	}
	if baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("DOMImaps detail service URL must not contain credentials, a query, or a fragment")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + detailServiceRequestPath
	baseURL.RawPath = ""
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &detailClient{http: httpClient, endpoint: baseURL.String()}, nil
}

func (c *detailClient) description(ctx context.Context, externalID string) (string, error) {
	if !numericAdvertID(externalID) {
		return "", fmt.Errorf("DOMImaps advert id must contain only digits")
	}
	payload, err := json.Marshal(detailRequest{ExternalID: externalID})
	if err != nil {
		return "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, detailRequestLimit)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create DOMImaps detail request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("request DOMImaps detail service: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDetailResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read DOMImaps detail response: %w", err)
	}
	if len(body) > maxDetailResponseBytes {
		return "", fmt.Errorf("DOMImaps detail response exceeds %d bytes", maxDetailResponseBytes)
	}
	var decoded detailResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", fmt.Errorf("decode DOMImaps detail response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		reason := strings.TrimSpace(decoded.Error)
		if reason == "" {
			reason = http.StatusText(response.StatusCode)
		}

		return "", fmt.Errorf("DOMImaps detail service HTTP %d: %s", response.StatusCode, reason)
	}
	description := strings.TrimSpace(decoded.Description)
	if description == "" {
		return "", fmt.Errorf("DOMImaps detail service returned an empty description")
	}
	if len(description) > maxDescriptionBytes {
		return "", fmt.Errorf("DOMImaps description exceeds %d bytes", maxDescriptionBytes)
	}

	return description, nil
}

func numericAdvertID(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}
