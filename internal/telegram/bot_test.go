package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"reflect"
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
	recipientChatIDs []int64
	listUserCalls    int
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

func (s *fakeAlertStore) ListUserChatIDs(context.Context) ([]int64, error) {
	s.listUserCalls++
	return append([]int64(nil), s.recipientChatIDs...), nil
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
	mu             sync.Mutex
	bodies         []string
	failedChatIDs  map[int64]bool
	attemptedChats []int64
}

func (t *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.bodies = append(t.bodies, string(body))
	var payload struct {
		ChatID int64 `json:"chat_id"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.mu.Unlock()
		return nil, err
	}
	t.attemptedChats = append(t.attemptedChats, payload.ChatID)
	failed := t.failedChatIDs[payload.ChatID]
	t.mu.Unlock()
	responseBody := `{"ok":true,"result":{"message_id":99}}`
	if failed {
		responseBody = `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(responseBody)),
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
	t.attemptedChats = nil
}

func (t *recordingTransport) attemptedChatIDs() []int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]int64(nil), t.attemptedChats...)
}

func (t *recordingTransport) texts() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]string, 0, len(t.bodies))
	for _, body := range t.bodies {
		var payload struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(body), &payload) == nil {
			result = append(result, payload.Text)
		}
	}
	return result
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

func TestBroadcastIgnoresUnauthorizedUser(t *testing.T) {
	bot, store, transport := newTestBot(t)
	bot.adminUserID = 7
	store.recipientChatIDs = []int64{100, 200}

	bot.handleMessage(context.Background(), &models.Message{
		From: &models.User{ID: 42, FirstName: "Tester", LanguageCode: "en"},
		Chat: models.Chat{ID: 42},
		Text: "/broadcast hello",
	})

	if store.listUserCalls != 0 {
		t.Fatalf("recipient list calls = %d", store.listUserCalls)
	}
	if attempts := transport.attemptedChatIDs(); len(attempts) != 0 {
		t.Fatalf("unexpected Telegram attempts: %v", attempts)
	}
}

func TestBroadcastRequiresMessage(t *testing.T) {
	bot, store, transport := newTestBot(t)
	bot.adminUserID = 42

	bot.handleMessage(context.Background(), &models.Message{
		From: &models.User{ID: 42, FirstName: "Admin", LanguageCode: "en"},
		Chat: models.Chat{ID: 42},
		Text: "/broadcast",
	})

	if store.listUserCalls != 0 {
		t.Fatalf("recipient list calls = %d", store.listUserCalls)
	}
	if texts := transport.texts(); !reflect.DeepEqual(texts, []string{"Usage: /broadcast <message>"}) {
		t.Fatalf("Telegram texts = %v", texts)
	}
}

func TestBroadcastLoadsAllRecipientsAndContinuesAfterFailure(t *testing.T) {
	bot, store, transport := newTestBot(t)
	bot.adminUserID = 42
	store.recipientChatIDs = []int64{100, 200, 300}
	transport.failedChatIDs = map[int64]bool{200: true}

	bot.handleMessage(context.Background(), &models.Message{
		From: &models.User{ID: 42, FirstName: "Admin", LanguageCode: "en"},
		Chat: models.Chat{ID: 42},
		Text: "/broadcast Maintenance & <update>",
	})

	if store.listUserCalls != 1 {
		t.Fatalf("recipient list calls = %d", store.listUserCalls)
	}
	if attempts := transport.attemptedChatIDs(); !reflect.DeepEqual(attempts, []int64{100, 200, 300, 42}) {
		t.Fatalf("Telegram attempts = %v", attempts)
	}
	if bodies := transport.joinedBodies(); strings.Contains(bodies, `"parse_mode"`) {
		t.Fatalf("broadcast messages unexpectedly enable Telegram parsing: %s", bodies)
	}
	if texts := transport.texts(); !reflect.DeepEqual(texts, []string{
		"Maintenance & <update>",
		"Maintenance & <update>",
		"Maintenance & <update>",
		"Broadcast complete: 2 successful, 1 failed.",
	}) {
		t.Fatalf("Telegram texts = %v", texts)
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
