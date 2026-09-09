package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type availabilityRoundTripFunc func(*http.Request) (*http.Response, error)

func (f availabilityRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestFetchAvailabilityEnforcesHostRedirectAndSize(t *testing.T) {
	if _, _, _, err := FetchAvailability(context.Background(), http.DefaultClient, "https://example.com/listing", func(string) bool { return false }, ""); err == nil {
		t.Fatal("disallowed initial host was accepted")
	}

	redirectClient := &http.Client{Transport: availabilityRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://evil.test/outside"}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}
	if _, _, _, err := FetchAvailability(context.Background(), redirectClient, "https://example.test/listing", func(candidate string) bool { return candidate == "example.test" }, ""); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("outside redirect error = %v", err)
	}

	largeClient := &http.Client{Transport: availabilityRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", int(MaxAvailabilityResponseBytes)+1))), Request: request}, nil
	})}
	if _, _, _, err := FetchAvailability(context.Background(), largeClient, "https://example.test/listing", func(candidate string) bool { return candidate == "example.test" }, ""); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestFetchAvailabilityPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &http.Client{Transport: availabilityRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("request cancelled: %w", request.Context().Err())
	})}

	_, _, _, err := FetchAvailability(ctx, client, "https://example.test/listing", func(candidate string) bool { return candidate == "example.test" }, "")
	if err == nil {
		t.Fatal("cancelled request succeeded")
	}
}
