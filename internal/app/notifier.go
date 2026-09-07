package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/localization"
	"github.com/clive00lewis/latvia-home-radar/internal/telegram"
)

type Notifier struct {
	store   NotificationStore
	api     *telegram.Client
	http    *http.Client
	catalog *localization.Catalog
	logger  *slog.Logger
}

func NewNotifier(store NotificationStore, api *telegram.Client, httpClient *http.Client, catalog *localization.Catalog, logger *slog.Logger) *Notifier {
	return &Notifier{store: store, api: api, http: httpClient, catalog: catalog, logger: logger}
}

func (n *Notifier) Run(ctx context.Context) error {
	for {
		pending, err := n.store.Pending(ctx, 20)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}
		for _, item := range pending {
			if err := n.deliver(ctx, item); err != nil {
				n.logger.Warn("notification delivery failed", "notification", item.ID, "error", err)
			}
		}
	}
}

func (n *Notifier) deliver(ctx context.Context, item domain.PendingNotification) error {
	cached, err := n.store.PhotoFileIDs(ctx, item.ListingID)
	if err != nil {
		return err
	}
	photos := []telegram.Photo(nil)
	if len(cached) == 0 {
		photos = telegram.DownloadPhotos(ctx, n.http, item.Listing, n.logger)
	}
	caption := n.caption(item)
	ids, err := n.api.SendPhotos(ctx, item.ChatID, caption, photos, cached)
	var apiErr *telegram.APIError
	if err != nil && len(cached) > 0 && errors.As(err, &apiErr) && apiErr.BadRequest() {
		if clearErr := n.store.ClearPhotoFileIDs(ctx, item.ListingID); clearErr != nil {
			return clearErr
		}
		photos = telegram.DownloadPhotos(ctx, n.http, item.Listing, n.logger)
		ids, err = n.api.SendPhotos(ctx, item.ChatID, caption, photos, nil)
	}
	if err != nil {
		if errors.As(err, &apiErr) {
			if apiErr.Forbidden() {
				if failErr := n.store.Fail(ctx, item.ID, err); failErr != nil {
					return failErr
				}
				_, disableErr := n.store.DisableFiltersForChat(ctx, item.ChatID)
				return disableErr
			}
			delay := time.Duration(apiErr.RetryAfter) * time.Second
			if retryErr := n.store.Retry(ctx, item.ID, item.Attempts, err, delay); retryErr != nil {
				return retryErr
			}
			return err
		}
		if retryErr := n.store.Retry(ctx, item.ID, item.Attempts, err, 0); retryErr != nil {
			return retryErr
		}
		return err
	}
	if len(ids) > 0 && !sameStrings(ids, cached) {
		if err := n.store.CachePhotoFileIDs(ctx, item.ListingID, ids); err != nil {
			return err
		}
	}
	if err := n.store.MarkSent(ctx, item.ID); err != nil {
		return err
	}
	n.logger.Info("notification sent", "event", "notification.sent", "telegram_user_id", item.TelegramUserID, "chat_id", item.ChatID, "notification_id", item.ID, "alert_id", item.FilterID, "listing_id", item.ListingID, "listing_source", item.Listing.Source, "listing_external_id", item.Listing.ExternalID, "listing_url", item.Listing.URL)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}
	return nil
}

func (n *Notifier) caption(item domain.PendingNotification) string {
	if item.Type == domain.NotificationPriceChanged {
		return telegram.FormatPriceChange(n.catalog.For(item.LanguageTag), item.Listing, item.PreviousPriceEUR, item.CurrentPriceEUR)
	}
	return telegram.FormatListing(n.catalog.For(item.LanguageTag), item.Listing)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
