package telegram

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/localization"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type AlertStore interface {
	UpsertUser(context.Context, int64, int64, string, string) (domain.User, error)
	ListUserChatIDs(context.Context) ([]int64, error)
	SetUserLanguage(context.Context, int64, string) (bool, error)
	ListChildAreas(context.Context, string) ([]domain.AreaChoice, error)
	ListRootAreas(context.Context) ([]domain.AreaChoice, error)
	CreateFilter(context.Context, domain.SearchFilter) (int64, error)
	ListFilters(context.Context, int64) ([]domain.SavedFilter, error)
	SetFilterEnabled(context.Context, int64, int64, bool) (bool, error)
	DeleteFilter(context.Context, int64, int64) (bool, error)
}

type Bot struct {
	poller      *tgbot.Bot
	api         *Client
	store       AlertStore
	catalog     *localization.Catalog
	logger      *slog.Logger
	adminUserID int64
	mu          sync.Mutex
	states      map[int64]*wizardState
}

func NewBot(token string, adminUserID int64, api *Client, store AlertStore, catalog *localization.Catalog, logger *slog.Logger) (*Bot, error) {
	adapter := &Bot{api: api, store: store, catalog: catalog, logger: logger, adminUserID: adminUserID, states: map[int64]*wizardState{}}
	poller, err := tgbot.New(token, tgbot.WithDefaultHandler(adapter.handle))
	if err != nil {
		return nil, err
	}
	adapter.poller = poller
	return adapter, nil
}

func (b *Bot) Run(ctx context.Context) error {
	b.poller.Start(ctx)
	return ctx.Err()
}

func (b *Bot) handle(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
	if update.Message != nil {
		b.handleMessage(ctx, update.Message)
		return
	}
	if update.CallbackQuery != nil {
		b.handleCallback(ctx, update.CallbackQuery)
	}
}

func (b *Bot) handleMessage(ctx context.Context, message *models.Message) {
	if message.From == nil || message.Text == "" {
		return
	}
	userID, chatID := message.From.ID, message.Chat.ID
	name := strings.TrimSpace(strings.TrimSpace(message.From.FirstName + " " + message.From.LastName))
	user, err := b.store.UpsertUser(ctx, userID, chatID, name, localization.Normalize(message.From.LanguageCode))
	if err != nil {
		b.logError(err)
		return
	}
	localizer := b.catalog.For(user.LanguageTag)
	text := strings.TrimSpace(message.Text)
	if strings.HasPrefix(text, "/") {
		command := strings.ToLower(strings.Split(strings.TrimPrefix(strings.Fields(text)[0], "/"), "@")[0])
		b.logger.Info("user command", "event", "user.command", "telegram_user_id", userID, "user_name", name, "username", message.From.Username, "chat_id", chatID, "command", command)

		switch command {
		case "start":
			b.clearState(userID)
			_, err := b.api.SendMessage(ctx, chatID, localizer.Text(localization.Welcome, nil), mainMenuKeyboard(localizer))
			b.logError(err)
		case "help":
			_, err := b.api.SendMessage(ctx, chatID, localizer.Text(localization.Help, nil), nil)
			b.logError(err)
		case "new":
			b.startWizard(ctx, userID, user.ID, chatID, user.LanguageTag, 0)
		case "alerts":
			b.sendAlerts(ctx, userID, chatID, user.LanguageTag, 0)
		case "pause", "resume":
			b.changeFilter(ctx, userID, chatID, user.LanguageTag, text, command == "resume")
		case "delete":
			b.deleteFilter(ctx, userID, chatID, user.LanguageTag, text)
		case "language":
			_, err := b.api.SendMessage(ctx, chatID, localizer.Text(localization.LanguagePrompt, nil), languageKeyboard(localizer))
			b.logError(err)
		case "broadcast":
			if userID == b.adminUserID {
				b.broadcast(ctx, chatID, text)
			}
		case "cancel":
			b.clearState(userID)
			_, err := b.api.SendMessage(ctx, chatID, localizer.Text(localization.SetupCancelled, nil), mainMenuKeyboard(localizer))
			b.logError(err)
		}
		return
	}
	state := b.getState(userID)
	if state == nil {
		return
	}
	switch state.Stage {
	case "custom_price":
		minimum, maximum, err := ParsePriceRange(text)
		if err != nil || minimum == nil && maximum == nil {
			_, sendErr := b.api.SendMessage(ctx, chatID, localizer.Text(localization.InvalidPriceRange, nil), nil)
			b.logError(sendErr)
			return
		}
		state.PriceMin, state.PriceMax = minimum, maximum
		state.Stage = "rooms"
		b.saveState(userID, state)
		b.logError(b.api.EditMessage(ctx, chatID, state.MessageID, renderRoomsStep(localizer), roomsKeyboard(localizer)))
	case "custom_size":
		minimum, maximum, err := ParseFloatRange(text)
		if err != nil || minimum == nil && maximum == nil {
			_, sendErr := b.api.SendMessage(ctx, chatID, localizer.Text(localization.InvalidSizeRange, nil), nil)
			b.logError(sendErr)
			return
		}
		state.AreaMin, state.AreaMax = minimum, maximum
		state.Stage = "confirm"
		b.saveState(userID, state)
		b.logError(b.api.EditMessage(ctx, chatID, state.MessageID, renderConfirmation(localizer, state), confirmationKeyboard(localizer)))
	}
}

func (b *Bot) broadcast(ctx context.Context, adminChatID int64, commandText string) {
	message := commandPayload(commandText)
	if message == "" {
		_, err := b.api.SendPlainMessage(ctx, adminChatID, "Usage: /broadcast <message>")
		b.logError(err)
		return
	}

	chatIDs, err := b.store.ListUserChatIDs(ctx)
	if err != nil {
		b.logger.Error("broadcast recipient loading failed", "event", "broadcast.recipients_failed", "error", err)
		_, sendErr := b.api.SendPlainMessage(ctx, adminChatID, "Broadcast failed: could not load recipients.")
		b.logError(sendErr)
		return
	}

	succeeded := 0
	failed := 0
	for index, chatID := range chatIDs {
		if _, err := b.api.SendPlainMessage(ctx, chatID, message); err != nil {
			failed++
			b.logger.Warn("broadcast delivery failed", "event", "broadcast.delivery_failed", "chat_id", chatID, "error", err)
		} else {
			succeeded++
		}

		if index < len(chatIDs)-1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}

	summary := "Broadcast complete: " + strconv.Itoa(succeeded) + " successful, " + strconv.Itoa(failed) + " failed."
	_, err = b.api.SendPlainMessage(ctx, adminChatID, summary)
	b.logError(err)
}

func commandPayload(text string) string {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return ""
	}

	return strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
}

func (b *Bot) handleCallback(ctx context.Context, callback *models.CallbackQuery) {
	userID := callback.From.ID
	data := callback.Data
	if callback.Message.Message == nil {
		return
	}
	chatID := callback.Message.Message.Chat.ID
	messageID := callback.Message.Message.ID
	name := strings.TrimSpace(callback.From.FirstName + " " + callback.From.LastName)
	user, err := b.store.UpsertUser(ctx, userID, chatID, name, localization.Normalize(callback.From.LanguageCode))
	if err != nil {
		b.logError(err)
		return
	}
	localizer := b.catalog.For(user.LanguageTag)
	answer := func(text string, alert bool) { b.logError(b.api.AnswerCallback(ctx, callback.ID, text, alert)) }
	switch {
	case data == "menu:new":
		answer("", false)
		b.startWizard(ctx, userID, user.ID, chatID, user.LanguageTag, messageID)
		return
	case data == "menu:filters" || data == "alert:back":
		answer("", false)
		b.sendAlerts(ctx, userID, chatID, user.LanguageTag, messageID)
		return
	case data == "menu:language":
		answer("", false)
		b.logError(b.api.EditMessage(ctx, chatID, messageID, localizer.Text(localization.LanguagePrompt, nil), languageKeyboard(localizer)))
		return
	case strings.HasPrefix(data, "lang:"):
		languageTag := strings.TrimPrefix(data, "lang:")
		if !supportedLanguage(languageTag) {
			answer(localizer.Text(localization.InvalidAlert, nil), true)
			return
		}
		changed, err := b.store.SetUserLanguage(ctx, userID, languageTag)
		if err != nil || !changed {
			b.logError(err)
			return
		}
		if state := b.getState(userID); state != nil {
			state.LanguageTag = languageTag
			b.saveState(userID, state)
		}
		localizer = b.catalog.For(languageTag)
		answer(localizer.Text(localization.LanguageChanged, nil), false)
		b.logError(b.api.EditMessage(ctx, chatID, messageID, localizer.Text(localization.Welcome, nil), mainMenuKeyboard(localizer)))
		return
	case strings.HasPrefix(data, "alert:stop:") || strings.HasPrefix(data, "alert:restart:"):
		parts := strings.Split(data, ":")
		id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		if err != nil {
			answer(localizer.Text(localization.InvalidAlert, nil), true)
			return
		}
		enabled := parts[1] == "restart"
		changed, err := b.store.SetFilterEnabled(ctx, userID, id, enabled)
		if err != nil || !changed {
			answer(localizer.Text(localization.AlertNotFound, nil), true)
			b.logError(err)
			return
		}
		if enabled {
			answer(localizer.Text(localization.AlertRestarted, nil), false)
		} else {
			answer(localizer.Text(localization.AlertStopped, nil), false)
		}
		b.sendAlerts(ctx, userID, chatID, user.LanguageTag, messageID)
		return
	case strings.HasPrefix(data, "alert:delete:"):
		id := strings.TrimPrefix(data, "alert:delete:")
		answer("", false)
		text := localizer.Text(localization.DeleteAlertConfirm, map[string]any{"ID": id})
		b.logError(b.api.EditMessage(ctx, chatID, messageID, text, deleteAlertKeyboard(localizer, id)))
		return
	case strings.HasPrefix(data, "alert:confirm-delete:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "alert:confirm-delete:"), 10, 64)
		deleted, err := b.store.DeleteFilter(ctx, userID, id)
		if err != nil || !deleted {
			answer(localizer.Text(localization.AlertNotFound, nil), true)
			b.logError(err)
			return
		}
		answer(localizer.Text(localization.AlertDeleted, nil), false)
		b.sendAlerts(ctx, userID, chatID, user.LanguageTag, messageID)
		return
	}
	state := b.getState(userID)
	if state == nil || state.OwnerID != userID {
		answer(localizer.Text(localization.WizardExpired, nil), true)
		return
	}
	answer("", false)
	state.MessageID = messageID
	switch {
	case data == "w1:x":
		b.clearState(userID)
		b.logError(b.api.EditMessage(ctx, chatID, messageID, localizer.Text(localization.SetupCancelled, nil), mainMenuKeyboard(localizer)))
		return
	case strings.HasPrefix(data, "w1:t:"):
		value := strings.TrimPrefix(data, "w1:t:")
		state.PropertyTypes = map[string][]domain.PropertyType{"apartment": {domain.PropertyApartment}, "house": {domain.PropertyHouse}, "both": {domain.PropertyApartment, domain.PropertyHouse}}[value]
		if len(state.PropertyTypes) == 0 {
			return
		}
		state.Stage = "deal"
		b.editState(ctx, userID, state, renderDealStep(localizer), dealKeyboard(localizer))
	case strings.HasPrefix(data, "w1:d:"):
		state.DealType = domain.DealType(strings.TrimPrefix(data, "w1:d:"))
		if !state.DealType.Valid() {
			return
		}
		state.Stage = "areas"
		b.editState(ctx, userID, state, renderAreaStep(localizer), areaScopeKeyboard(localizer))
	case data == "w1:a:all":
		state.AreaKeys = nil
		state.AreaLabels = nil
		b.toPrice(ctx, userID, state)
	case strings.HasPrefix(data, "w1:ag:"):
		b.openAreaGroup(ctx, userID, state, strings.TrimPrefix(data, "w1:ag:"))
	case data == "w1:aa":
		if state.AreaParentKey != "" {
			state.AreaKeys = []string{state.AreaParentKey}
			state.AreaLabels = []string{state.AreaParentLabel}
			b.toPrice(ctx, userID, state)
		}
	case strings.HasPrefix(data, "w1:at:"):
		key := strings.TrimPrefix(data, "w1:at:")
		b.toggleArea(ctx, userID, state, key)
	case data == "w1:ad":
		if len(state.AreaKeys) == 0 {
			b.logError(b.api.AnswerCallback(ctx, callback.ID, localizer.Text(localization.ChooseAtLeastOneArea, nil), true))
			return
		}
		b.toPrice(ctx, userID, state)
	case strings.HasPrefix(data, "w1:p:"):
		action := strings.TrimPrefix(data, "w1:p:")
		if action == "custom" {
			state.Stage = "custom_price"
			b.editState(ctx, userID, state, localizer.Text(localization.CustomPricePrompt, nil), navigationKeyboard(localizer, "w1:b:price"))
			return
		}
		option, ok := priceOption(state.DealType, action)
		if !ok {
			return
		}
		state.PriceMin, state.PriceMax = option.Min, option.Max
		state.Stage = "rooms"
		b.editState(ctx, userID, state, renderRoomsStep(localizer), roomsKeyboard(localizer))
	case strings.HasPrefix(data, "w1:r:"):
		option, ok := roomOption(strings.TrimPrefix(data, "w1:r:"))
		if !ok {
			return
		}
		state.RoomsMin, state.RoomsMax = option.Min, option.Max
		state.Stage = "size"
		b.editState(ctx, userID, state, renderSizeStep(localizer), sizeKeyboard(localizer))
	case strings.HasPrefix(data, "w1:s:"):
		action := strings.TrimPrefix(data, "w1:s:")
		if action == "custom" {
			state.Stage = "custom_size"
			b.editState(ctx, userID, state, localizer.Text(localization.CustomSizePrompt, nil), navigationKeyboard(localizer, "w1:b:size"))
			return
		}
		option, ok := sizeOption(action)
		if !ok {
			return
		}
		state.AreaMin, state.AreaMax = option.Min, option.Max
		state.Stage = "confirm"
		b.editState(ctx, userID, state, renderConfirmation(localizer, state), confirmationKeyboard(localizer))
	case data == "w1:ok":
		filter := domain.SearchFilter{UserID: state.UserID, DealType: state.DealType, PropertyTypes: state.PropertyTypes, AreaKeys: state.AreaKeys, PriceMin: state.PriceMin, PriceMax: state.PriceMax, RoomsMin: state.RoomsMin, RoomsMax: state.RoomsMax, AreaMin: state.AreaMin, AreaMax: state.AreaMax, Enabled: true}
		id, err := b.store.CreateFilter(ctx, filter)
		if err != nil {
			b.logError(err)
			return
		}
		b.clearState(userID)
		b.logger.Info("alert created", "event", "alert.created", "telegram_user_id", userID, "chat_id", chatID, "alert_id", id)
		text := localizer.Text(localization.AlertCreated, map[string]any{"ID": id})
		b.logError(b.api.EditMessage(ctx, chatID, messageID, text, mainMenuKeyboard(localizer)))
	}
}

func (b *Bot) startWizard(ctx context.Context, userID, databaseUserID, chatID int64, languageTag string, messageID int) {
	localizer := b.catalog.For(languageTag)
	state := &wizardState{OwnerID: userID, UserID: databaseUserID, ChatID: chatID, MessageID: messageID, Stage: "property", LanguageTag: languageTag}
	if messageID == 0 {
		id, err := b.api.SendMessage(ctx, chatID, renderPropertyStep(localizer), propertyKeyboard(localizer))
		if err != nil {
			b.logError(err)
			return
		}
		state.MessageID = id
	} else {
		if err := b.api.EditMessage(ctx, chatID, messageID, renderPropertyStep(localizer), propertyKeyboard(localizer)); err != nil {
			b.logError(err)
			return
		}
	}
	b.saveState(userID, state)
}

func (b *Bot) sendAlerts(ctx context.Context, userID, chatID int64, languageTag string, messageID int) {
	filters, err := b.store.ListFilters(ctx, userID)
	if err != nil {
		b.logError(err)
		return
	}
	localizer := b.catalog.For(languageTag)
	text := filtersText(localizer, filters)
	if messageID > 0 {
		err = b.api.EditMessage(ctx, chatID, messageID, text, alertsKeyboard(localizer, filters))
	} else {
		_, err = b.api.SendMessage(ctx, chatID, text, alertsKeyboard(localizer, filters))
	}
	b.logError(err)
}

func (b *Bot) changeFilter(ctx context.Context, userID, chatID int64, languageTag, text string, enabled bool) {
	localizer := b.catalog.For(languageTag)
	id, ok := commandID(text)
	if !ok {
		command := "pause"
		if enabled {
			command = "resume"
		}
		_, err := b.api.SendMessage(ctx, chatID, localizer.Text(localization.Usage, map[string]any{"Command": command}), nil)
		b.logError(err)
		return
	}
	changed, err := b.store.SetFilterEnabled(ctx, userID, id, enabled)
	if err != nil || !changed {
		_, sendErr := b.api.SendMessage(ctx, chatID, localizer.Text(localization.AlertNotFound, nil), nil)
		b.logError(err)
		b.logError(sendErr)
		return
	}
	message := localizer.Text(localization.AlertPaused, nil)
	if enabled {
		message = localizer.Text(localization.AlertResumed, nil)
	}
	_, err = b.api.SendMessage(ctx, chatID, message, nil)
	b.logError(err)
}

func (b *Bot) deleteFilter(ctx context.Context, userID, chatID int64, languageTag, text string) {
	localizer := b.catalog.For(languageTag)
	id, ok := commandID(text)
	if !ok {
		_, err := b.api.SendMessage(ctx, chatID, localizer.Text(localization.Usage, map[string]any{"Command": "delete"}), nil)
		b.logError(err)
		return
	}
	deleted, err := b.store.DeleteFilter(ctx, userID, id)
	if err != nil {
		b.logError(err)
		return
	}
	message := localizer.Text(localization.AlertNotFound, nil)
	if deleted {
		message = localizer.Text(localization.AlertDeleted, nil)
	}
	_, err = b.api.SendMessage(ctx, chatID, message, nil)
	b.logError(err)
}

func commandID(text string) (int64, bool) {
	parts := strings.Fields(text)
	if len(parts) != 2 {
		return 0, false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	return id, err == nil
}

func supportedLanguage(languageTag string) bool {
	for _, supported := range localization.SupportedLanguages() {
		if languageTag == supported {
			return true
		}
	}

	return false
}

func (b *Bot) getState(id int64) *wizardState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if state := b.states[id]; state != nil {
		copy := *state
		copy.AreaKeys = append([]string(nil), state.AreaKeys...)
		copy.AreaLabels = append([]string(nil), state.AreaLabels...)
		copy.PropertyTypes = append([]domain.PropertyType(nil), state.PropertyTypes...)
		copy.AreaChoices = map[string]string{}
		for key, value := range state.AreaChoices {
			copy.AreaChoices[key] = value
		}
		return &copy
	}
	return nil
}

func (b *Bot) saveState(id int64, state *wizardState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.states[id] = state
}

func (b *Bot) clearState(id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.states, id)
}

func (b *Bot) editState(ctx context.Context, id int64, state *wizardState, text string, markup *keyboard) {
	b.saveState(id, state)
	b.logError(b.api.EditMessage(ctx, state.ChatID, state.MessageID, text, markup))
}

func (b *Bot) logError(err error) {
	if err != nil {
		b.logger.Error("telegram operation failed", "error", err)
	}
}
