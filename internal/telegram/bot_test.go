package telegram

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/localization"
	"github.com/go-telegram/bot/models"
)

type fakeAlertStore struct {
	user             domain.User
	detectedLanguage string
}

func (s *fakeAlertStore) UpsertUser(_ context.Context, _, _ int64, _, languageTag string) (domain.User, error) {
	s.detectedLanguage = languageTag
	if s.user.ID == 0 {
		s.user = domain.User{ID: 1, LanguageTag: languageTag}
	}
	return s.user, nil
}

func (s *fakeAlertStore) SetUserLanguage(_ context.Context, _ int64, languageTag string) (bool, error) {
	s.user.LanguageTag = languageTag
	return true, nil
}

func (s *fakeAlertStore) ListChildAreas(context.Context, string) ([]domain.AreaChoice, error) {
	return nil, nil
}

func (s *fakeAlertStore) ListRootAreas(context.Context) ([]domain.AreaChoice, error) {
	return nil, nil
}

func (s *fakeAlertStore) CreateFilter(context.Context, domain.SearchFilter) (int64, error) {
	return 1, nil
}

func (s *fakeAlertStore) ListFilters(context.Context, int64) ([]domain.SavedFilter, error) {
	return nil, nil
}

func (s *fakeAlertStore) SetFilterEnabled(context.Context, int64, int64, bool) (bool, error) {
	return true, nil
}

func (s *fakeAlertStore) DeleteFilter(context.Context, int64, int64) (bool, error) {
	return true, nil
}

type recordingTransport struct {
	mu     sync.Mutex
	bodies []string
}

func (t *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.bodies = append(t.bodies, string(body))
	t.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":99}}`)),
		Request:    request,
	}, nil
}

func (t *recordingTransport) joinedBodies() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.bodies, "\n")
}

func (t *recordingTransport) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bodies = nil
}

func TestStartInfersSupportedTelegramLanguage(t *testing.T) {
	bot, store, transport := newTestBot(t)
	bot.handleMessage(context.Background(), &models.Message{
		From: &models.User{ID: 42, FirstName: "Tester", LanguageCode: "ru-RU"},
		Chat: models.Chat{ID: 42},
		Text: "/start",
	})
	if store.detectedLanguage != localization.Russian {
		t.Fatalf("detected language = %q", store.detectedLanguage)
	}
	if body := transport.joinedBodies(); !strings.Contains(body, "Уведомления о недвижимости") {
		t.Fatalf("start response is not Russian: %s", body)
	}
}

func TestLanguageCallbackPersistsChoiceAndUsesItImmediately(t *testing.T) {
	bot, store, transport := newTestBot(t)
	store.user = domain.User{ID: 1, LanguageTag: localization.English}
	bot.handleCallback(context.Background(), &models.CallbackQuery{
		ID:   "callback",
		From: models.User{ID: 42, FirstName: "Tester", LanguageCode: "en-US"},
		Message: models.MaybeInaccessibleMessage{
			Type:    models.MaybeInaccessibleMessageTypeMessage,
			Message: &models.Message{ID: 10, Chat: models.Chat{ID: 42}},
		},
		Data: "lang:lv",
	})
	if store.user.LanguageTag != localization.Latvian {
		t.Fatalf("persisted language = %q", store.user.LanguageTag)
	}
	body := transport.joinedBodies()
	for _, want := range []string{"Valoda nomainīta", "Īpašumu paziņojumi Latvijā"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in Telegram requests: %s", want, body)
		}
	}
	transport.reset()
	restarted := &Bot{api: bot.api, store: store, catalog: bot.catalog, logger: bot.logger, states: map[int64]*wizardState{}}
	restarted.handleMessage(context.Background(), &models.Message{
		From: &models.User{ID: 42, FirstName: "Tester", LanguageCode: "ru-RU"},
		Chat: models.Chat{ID: 42},
		Text: "/start",
	})
	if body := transport.joinedBodies(); !strings.Contains(body, "Īpašumu paziņojumi Latvijā") {
		t.Fatalf("persisted language was not used after restart: %s", body)
	}
}

func newTestBot(t *testing.T) (*Bot, *fakeAlertStore, *recordingTransport) {
	t.Helper()
	catalog, err := localization.New()
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeAlertStore{}
	transport := &recordingTransport{}
	api := NewClient("test", &http.Client{Transport: transport})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Bot{api: api, store: store, catalog: catalog, logger: logger, states: map[int64]*wizardState{}}, store, transport
}
