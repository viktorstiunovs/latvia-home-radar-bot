package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/localization"
	"github.com/clive00lewis/latvia-home-radar/internal/telegram"
)

type notifierStoreStub struct {
	cached   []string
	markedID int64
}

func (s *notifierStoreStub) Pending(context.Context, int) ([]domain.PendingNotification, error) {
	return nil, nil
}

func (s *notifierStoreStub) MarkSent(_ context.Context, id int64) error {
	s.markedID = id
	return nil
}

func (s *notifierStoreStub) Retry(context.Context, int64, int, error, time.Duration) error {
	return nil
}

func (s *notifierStoreStub) Fail(context.Context, int64, error) error {
	return nil
}

func (s *notifierStoreStub) DisableFiltersForChat(context.Context, int64) (int64, error) {
	return 0, nil
}

func (s *notifierStoreStub) PhotoFileIDs(context.Context, int64) ([]string, error) {
	return s.cached, nil
}

func (s *notifierStoreStub) CachePhotoFileIDs(context.Context, int64, []string) error {
	return nil
}

func (s *notifierStoreStub) ClearPhotoFileIDs(context.Context, int64) error {
	return nil
}

type notifierRoundTripFunc func(*http.Request) (*http.Response, error)

func (f notifierRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestNotificationCaptionUsesPersistedLanguage(t *testing.T) {
	catalog, err := localization.New()
	if err != nil {
		t.Fatal(err)
	}
	notifier := &Notifier{catalog: catalog}
	item := domain.PendingNotification{
		LanguageTag: localization.Russian,
		Listing: domain.Listing{
			Source:       "ss.lv",
			URL:          "https://example.test/listing",
			PropertyType: domain.PropertyApartment,
			DealType:     domain.DealRent,
			Title:        "Квартира",
		},
	}
	caption := notifier.caption(item)
	for _, want := range []string{"Квартира в аренду", "Цена по запросу", "Открыть на SS.lv"} {
		if !strings.Contains(caption, want) {
			t.Errorf("missing %q in %s", want, caption)
		}
	}
}

func TestPriceChangeCaptionUsesTriggeringPriceSnapshot(t *testing.T) {
	catalog, err := localization.New()
	if err != nil {
		t.Fatal(err)
	}
	notifier := &Notifier{catalog: catalog}
	item := domain.PendingNotification{
		LanguageTag:      localization.English,
		Type:             domain.NotificationPriceChanged,
		PreviousPriceEUR: intPointer(900),
		CurrentPriceEUR:  intPointer(700),
		Listing: domain.Listing{
			Source:       "ss.lv",
			URL:          "https://example.test/listing",
			PropertyType: domain.PropertyApartment,
			DealType:     domain.DealRent,
			Title:        "Apartment",
			PriceEUR:     intPointer(650),
		},
	}
	caption := notifier.caption(item)
	for _, want := range []string{"Apartment for rent · €700 / month", "🟢 <b>Price decreased · −22.2%</b>", "<s>€900 / month</s>  →  <b>€700 / month</b>"} {
		if !strings.Contains(caption, want) {
			t.Errorf("missing %q in %s", want, caption)
		}
	}
	if strings.Contains(caption, "€650") {
		t.Fatalf("caption used later listing price: %s", caption)
	}
}

func TestPriceChangeDeliveryReusesCachedTelegramPhoto(t *testing.T) {
	catalog, err := localization.New()
	if err != nil {
		t.Fatal(err)
	}
	store := &notifierStoreStub{cached: []string{"cached-photo-id"}}
	requests := 0
	httpClient := &http.Client{Transport: notifierRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if !strings.HasSuffix(request.URL.Path, "/sendPhoto") {
			t.Fatalf("unexpected Telegram method: %s", request.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["photo"] != "cached-photo-id" {
			t.Fatalf("cached photo was not reused: %+v", payload)
		}
		caption, _ := payload["caption"].(string)
		if !strings.Contains(caption, "Price decreased") || !strings.Contains(caption, "<s>€900 / month</s>") || !strings.Contains(caption, "<b>€700 / month</b>") {
			t.Fatalf("missing price-change caption: %s", caption)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
	})}
	notifier := NewNotifier(store, telegram.NewClient("test", httpClient), httpClient, catalog, slog.New(slog.NewTextHandler(io.Discard, nil)))
	item := domain.PendingNotification{
		ID:               12,
		ChatID:           42,
		ListingID:        7,
		LanguageTag:      localization.English,
		Type:             domain.NotificationPriceChanged,
		PreviousPriceEUR: intPointer(900),
		CurrentPriceEUR:  intPointer(700),
		Listing: domain.Listing{
			Source:       "ss.lv",
			URL:          "https://example.test/listing",
			PropertyType: domain.PropertyApartment,
			DealType:     domain.DealRent,
			Title:        "Apartment",
			PhotoURLs:    []string{"https://example.test/should-not-download.jpg"},
		},
	}
	if err := notifier.deliver(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || store.markedID != item.ID {
		t.Fatalf("delivery requests=%d marked=%d", requests, store.markedID)
	}
}

func intPointer(value int) *int {
	return &value
}
