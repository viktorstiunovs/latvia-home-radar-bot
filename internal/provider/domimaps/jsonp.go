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
	"sync/atomic"
)

const maxResponseBytes int64 = 8 << 20

var callbackSequence atomic.Uint64

type client struct {
	http    *http.Client
	baseURL string
}

func newClient(httpClient *http.Client, baseURL string) *client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &client{http: httpClient, baseURL: strings.TrimRight(baseURL, "/")}
}

func nextCallback() string {
	return fmt.Sprintf("LHR%05X", callbackSequence.Add(1)&0xfffff)
}

func (c *client) get(ctx context.Context, endpoint string, query url.Values) (int, string, []byte, error) {
	requestURL := c.baseURL + endpoint
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return 0, "", nil, fmt.Errorf("parse DOMImaps URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return 0, "", nil, fmt.Errorf("create DOMImaps request: %w", err)
	}
	req.Header.Set("Accept", "application/javascript, text/javascript;q=0.9")

	boundedClient := *c.http
	previousRedirect := c.http.CheckRedirect
	boundedClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != parsed.Scheme || !strings.EqualFold(request.URL.Hostname(), parsed.Hostname()) {
			return fmt.Errorf("DOMImaps redirect is outside the data host")
		}
		if len(via) >= 5 {
			return fmt.Errorf("too many DOMImaps redirects")
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}

		return nil
	}
	response, err := boundedClient.Do(req)
	if err != nil {
		return 0, "", nil, fmt.Errorf("request DOMImaps %s: %w", endpoint, err)
	}
	defer response.Body.Close()

	contentType := response.Header.Get("Content-Type")
	if response.ContentLength > maxResponseBytes {
		return response.StatusCode, contentType, nil, fmt.Errorf("DOMImaps response exceeds %d bytes", maxResponseBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return response.StatusCode, contentType, nil, fmt.Errorf("read DOMImaps response: %w", err)
	}
	if int64(len(body)) > maxResponseBytes {
		return response.StatusCode, contentType, nil, fmt.Errorf("DOMImaps response exceeds %d bytes", maxResponseBytes)
	}

	return response.StatusCode, contentType, body, nil
}

func decodeJSONP(body []byte, callback string, target any) error {
	trimmed := bytes.TrimSpace(body)
	prefix := []byte("UMap." + callback + "&&UMap." + callback + "(")
	suffix := []byte(");")
	if !bytes.HasPrefix(trimmed, prefix) || !bytes.HasSuffix(trimmed, suffix) {
		return fmt.Errorf("DOMImaps response does not match callback %q", callback)
	}
	payload := trimmed[len(prefix) : len(trimmed)-len(suffix)]
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode DOMImaps JSONP payload: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode DOMImaps JSONP payload: trailing JSON value")
		}

		return fmt.Errorf("decode DOMImaps JSONP payload: %w", err)
	}

	return nil
}

func isJavaScriptContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.Contains(contentType, "javascript") || strings.Contains(contentType, "json")
}
