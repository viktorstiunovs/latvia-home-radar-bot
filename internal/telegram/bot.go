package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type AlertStore interface {
	UpsertUser(context.Context, int64, int64, string) (int64, error)
	ListChildAreas(context.Context, string) ([]domain.AreaChoice, error)
	ListRootAreas(context.Context) ([]domain.AreaChoice, error)
	CreateFilter(context.Context, domain.SearchFilter) (int64, error)
	ListFilters(context.Context, int64) ([]domain.SavedFilter, error)
	SetFilterEnabled(context.Context, int64, int64, bool) (bool, error)
	DeleteFilter(context.Context, int64, int64) (bool, error)
}

type Bot struct {
	poller *tgbot.Bot
	api    *Client
	store  AlertStore
	logger *slog.Logger
	mu     sync.Mutex
	states map[int64]*wizardState
}

func NewBot(token string, api *Client, store AlertStore, logger *slog.Logger) (*Bot, error) {
	adapter := &Bot{api: api, store: store, logger: logger, states: map[int64]*wizardState{}}
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
	text := strings.TrimSpace(message.Text)
	if strings.HasPrefix(text, "/") {
		command := strings.ToLower(strings.Split(strings.TrimPrefix(strings.Fields(text)[0], "/"), "@")[0])
		b.logger.Info("user command", "event", "user.command", "telegram_user_id", userID, "user_name", name, "username", message.From.Username, "chat_id", chatID, "command", command)

		switch command {
		case "start":
			_, err := b.store.UpsertUser(ctx, userID, chatID, name)
			if err == nil {
				b.clearState(userID)
				_, err = b.api.SendMessage(ctx, chatID, "🏠 <b>Property alerts for Latvia</b>\n\nCreate an alert once and I’ll send matching properties as soon as they appear.", mainMenuKeyboard())
			}
			b.logError(err)
		case "help":
			_, err := b.api.SendMessage(ctx, chatID, "<b>Commands</b>\n\n/new — create an alert\n/alerts — view your alerts\n/pause ID — pause an alert\n/resume ID — resume an alert\n/delete ID — delete an alert\n/cancel — stop the current setup", nil)
			b.logError(err)
		case "new":
			b.startWizard(ctx, userID, chatID, name, 0)
		case "alerts":
			b.sendAlerts(ctx, userID, chatID, 0)
		case "pause", "resume":
			b.changeFilter(ctx, userID, chatID, text, command == "resume")
		case "delete":
			b.deleteFilter(ctx, userID, chatID, text)
		case "cancel":
			b.clearState(userID)
			_, err := b.api.SendMessage(ctx, chatID, "Setup cancelled. You can start again whenever you’re ready.", mainMenuKeyboard())
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
			_, sendErr := b.api.SendMessage(ctx, chatID, "⚠️ Use min-max, min-, or -max. You may use k, for example 100k-200k.", nil)
			b.logError(sendErr)
			return
		}
		state.PriceMin, state.PriceMax = minimum, maximum
		state.Stage = "rooms"
		b.saveState(userID, state)
		b.logError(b.api.EditMessage(ctx, chatID, state.MessageID, renderRoomsStep(), roomsKeyboard()))
	case "custom_size":
		minimum, maximum, err := ParseFloatRange(text)
		if err != nil || minimum == nil && maximum == nil {
			_, sendErr := b.api.SendMessage(ctx, chatID, "⚠️ Use min-max, one number, min-, or -max.", nil)
			b.logError(sendErr)
			return
		}
		state.AreaMin, state.AreaMax = minimum, maximum
		state.Stage = "confirm"
		b.saveState(userID, state)
		b.logError(b.api.EditMessage(ctx, chatID, state.MessageID, renderConfirmation(state), confirmationKeyboard()))
	}
}

func (b *Bot) handleCallback(ctx context.Context, callback *models.CallbackQuery) {
	userID := callback.From.ID
	data := callback.Data
	if callback.Message.Message == nil {
		return
	}
	chatID := callback.Message.Message.Chat.ID
	messageID := callback.Message.Message.ID
	answer := func(text string, alert bool) { b.logError(b.api.AnswerCallback(ctx, callback.ID, text, alert)) }
	switch {
	case data == "menu:new":
		answer("", false)
		b.startWizard(ctx, userID, chatID, strings.TrimSpace(callback.From.FirstName+" "+callback.From.LastName), messageID)
		return
	case data == "menu:filters" || data == "alert:back":
		answer("", false)
		b.sendAlerts(ctx, userID, chatID, messageID)
		return
	case strings.HasPrefix(data, "alert:stop:") || strings.HasPrefix(data, "alert:restart:"):
		parts := strings.Split(data, ":")
		id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		if err != nil {
			answer("Invalid alert.", true)
			return
		}
		enabled := parts[1] == "restart"
		changed, err := b.store.SetFilterEnabled(ctx, userID, id, enabled)
		if err != nil || !changed {
			answer("Alert not found.", true)
			b.logError(err)
			return
		}
		answer(map[bool]string{true: "Alert restarted.", false: "Alert stopped."}[enabled], false)
		b.sendAlerts(ctx, userID, chatID, messageID)
		return
	case strings.HasPrefix(data, "alert:delete:"):
		id := strings.TrimPrefix(data, "alert:delete:")
		answer("", false)
		b.logError(b.api.EditMessage(ctx, chatID, messageID, "🗑 <b>Delete alert #"+id+"?</b>\n\nThis permanently removes the alert and cannot be undone.", deleteAlertKeyboard(id)))
		return
	case strings.HasPrefix(data, "alert:confirm-delete:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "alert:confirm-delete:"), 10, 64)
		deleted, err := b.store.DeleteFilter(ctx, userID, id)
		if err != nil || !deleted {
			answer("Alert not found.", true)
			b.logError(err)
			return
		}
		answer("Alert deleted.", false)
		b.sendAlerts(ctx, userID, chatID, messageID)
		return
	}
	state := b.getState(userID)
	if state == nil || state.OwnerID != userID {
		answer("This setup has expired. Use /new to start again.", true)
		return
	}
	answer("", false)
	state.MessageID = messageID
	switch {
	case data == "w1:x":
		b.clearState(userID)
		b.logError(b.api.EditMessage(ctx, chatID, messageID, "Setup cancelled. You can start again whenever you’re ready.", mainMenuKeyboard()))
		return
	case strings.HasPrefix(data, "w1:t:"):
		value := strings.TrimPrefix(data, "w1:t:")
		state.PropertyTypes = map[string][]domain.PropertyType{"apartment": {domain.PropertyApartment}, "house": {domain.PropertyHouse}, "both": {domain.PropertyApartment, domain.PropertyHouse}}[value]
		if len(state.PropertyTypes) == 0 {
			return
		}
		state.Stage = "deal"
		b.editState(ctx, userID, state, renderDealStep(), dealKeyboard())
	case strings.HasPrefix(data, "w1:d:"):
		state.DealType = domain.DealType(strings.TrimPrefix(data, "w1:d:"))
		if !state.DealType.Valid() {
			return
		}
		state.Stage = "areas"
		b.editState(ctx, userID, state, renderAreaStep(), areaScopeKeyboard())
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
			b.logError(b.api.AnswerCallback(ctx, callback.ID, "Choose at least one area.", true))
			return
		}
		b.toPrice(ctx, userID, state)
	case strings.HasPrefix(data, "w1:p:"):
		action := strings.TrimPrefix(data, "w1:p:")
		if action == "custom" {
			state.Stage = "custom_price"
			b.editState(ctx, userID, state, "<b>Custom price</b>\n\n"+customPriceGuide+"\n\nSend the price range as your next message.", navigationKeyboard("w1:b:price"))
			return
		}
		option, ok := priceOption(state.DealType, action)
		if !ok {
			return
		}
		state.PriceMin, state.PriceMax = option.Min, option.Max
		state.Stage = "rooms"
		b.editState(ctx, userID, state, renderRoomsStep(), roomsKeyboard())
	case strings.HasPrefix(data, "w1:r:"):
		option, ok := roomOption(strings.TrimPrefix(data, "w1:r:"))
		if !ok {
			return
		}
		state.RoomsMin, state.RoomsMax = option.Min, option.Max
		state.Stage = "size"
		b.editState(ctx, userID, state, renderSizeStep(), sizeKeyboard())
	case strings.HasPrefix(data, "w1:s:"):
		action := strings.TrimPrefix(data, "w1:s:")
		if action == "custom" {
			state.Stage = "custom_size"
			b.editState(ctx, userID, state, "<b>Custom size</b>\n\nExamples: <code>40-80</code>, <code>50-</code>, <code>-70</code>\n\nSend the size range as your next message.", navigationKeyboard("w1:b:size"))
			return
		}
		option, ok := sizeOption(action)
		if !ok {
			return
		}
		state.AreaMin, state.AreaMax = option.Min, option.Max
		state.Stage = "confirm"
		b.editState(ctx, userID, state, renderConfirmation(state), confirmationKeyboard())
	case data == "w1:ok":
		userDBID, err := b.store.UpsertUser(ctx, userID, chatID, strings.TrimSpace(callback.From.FirstName+" "+callback.From.LastName))
		if err != nil {
			b.logError(err)
			return
		}
		filter := domain.SearchFilter{UserID: userDBID, DealType: state.DealType, PropertyTypes: state.PropertyTypes, AreaKeys: state.AreaKeys, PriceMin: state.PriceMin, PriceMax: state.PriceMax, RoomsMin: state.RoomsMin, RoomsMax: state.RoomsMax, AreaMin: state.AreaMin, AreaMax: state.AreaMax, Enabled: true}
		id, err := b.store.CreateFilter(ctx, filter)
		if err != nil {
			b.logError(err)
			return
		}
		b.clearState(userID)
		b.logger.Info("alert created", "event", "alert.created", "telegram_user_id", userID, "chat_id", chatID, "alert_id", id)
		b.logError(b.api.EditMessage(ctx, chatID, messageID, fmt.Sprintf("✅ <b>Alert #%d is active</b>\n\nI’ll notify you when a new matching property appears. Existing listings won’t be replayed.", id), mainMenuKeyboard()))
	}
}

func (b *Bot) startWizard(ctx context.Context, userID, chatID int64, name string, messageID int) {
	if _, err := b.store.UpsertUser(ctx, userID, chatID, name); err != nil {
		b.logError(err)
		return
	}
	state := &wizardState{OwnerID: userID, ChatID: chatID, MessageID: messageID, Stage: "property"}
	if messageID == 0 {
		id, err := b.api.SendMessage(ctx, chatID, renderPropertyStep(), propertyKeyboard())
		if err != nil {
			b.logError(err)
			return
		}
		state.MessageID = id
	} else {
		if err := b.api.EditMessage(ctx, chatID, messageID, renderPropertyStep(), propertyKeyboard()); err != nil {
			b.logError(err)
			return
		}
	}
	b.saveState(userID, state)
}

func (b *Bot) sendAlerts(ctx context.Context, userID, chatID int64, messageID int) {
	filters, err := b.store.ListFilters(ctx, userID)
	if err != nil {
		b.logError(err)
		return
	}
	text := filtersText(filters)
	if messageID > 0 {
		err = b.api.EditMessage(ctx, chatID, messageID, text, alertsKeyboard(filters))
	} else {
		_, err = b.api.SendMessage(ctx, chatID, text, alertsKeyboard(filters))
	}
	b.logError(err)
}

func (b *Bot) changeFilter(ctx context.Context, userID, chatID int64, text string, enabled bool) {
	id, ok := commandID(text)
	if !ok {
		command := "pause"
		if enabled {
			command = "resume"
		}
		_, err := b.api.SendMessage(ctx, chatID, "Usage: <code>/"+command+" ID</code>", nil)
		b.logError(err)
		return
	}
	changed, err := b.store.SetFilterEnabled(ctx, userID, id, enabled)
	if err != nil || !changed {
		_, sendErr := b.api.SendMessage(ctx, chatID, "Alert not found.", nil)
		b.logError(err)
		b.logError(sendErr)
		return
	}
	message := "Alert paused."
	if enabled {
		message = "Alert resumed. Paused listings won’t be replayed."
	}
	_, err = b.api.SendMessage(ctx, chatID, message, nil)
	b.logError(err)
}

func (b *Bot) deleteFilter(ctx context.Context, userID, chatID int64, text string) {
	id, ok := commandID(text)
	if !ok {
		_, err := b.api.SendMessage(ctx, chatID, "Usage: <code>/delete ID</code>", nil)
		b.logError(err)
		return
	}
	deleted, err := b.store.DeleteFilter(ctx, userID, id)
	if err != nil {
		b.logError(err)
		return
	}
	message := "Alert not found."
	if deleted {
		message = "Alert deleted."
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
