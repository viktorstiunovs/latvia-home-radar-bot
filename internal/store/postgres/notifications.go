package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/store/postgres/sqlcgen"
)

func (s *Store) Pending(ctx context.Context, limit int) ([]domain.PendingNotification, error) {
	rows, err := s.queries.ListPendingNotifications(ctx, int32(limit))
	if err != nil {
		return nil, err
	}
	result := make([]domain.PendingNotification, 0, len(rows))
	for _, row := range rows {
		n := pendingNotificationFromRow(row)
		if n.Type == domain.NotificationPriceChanged {
			n.Listing.PriceEUR = n.CurrentPriceEUR
		}
		_ = json.Unmarshal(row.PhotoURLs, &n.Listing.PhotoURLs)
		var snapshot domain.Listing
		if err := json.Unmarshal(row.ListingSnapshot, &snapshot); err != nil {
			return nil, err
		}
		if snapshot.ID > 0 {
			n.Listing = snapshot
			n.ListingID = snapshot.ID
		}
		if err := json.Unmarshal(row.ActiveAlternatives, &n.Alternatives); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, nil
}

func pendingNotificationFromRow(row sqlcgen.ListPendingNotificationsRow) domain.PendingNotification {
	return domain.PendingNotification{
		ID:               row.NotificationID,
		FilterID:         row.FilterID,
		Attempts:         row.Attempts,
		Type:             domain.NotificationType(row.NotificationType),
		PreviousPriceEUR: row.PreviousPriceEUR,
		CurrentPriceEUR:  row.CurrentPriceEUR,
		PropertyID:       row.PropertyID,
		ComparisonID:     row.ComparisonListingID,
		TelegramUserID:   row.TelegramUserID,
		UserName:         row.UserName,
		ChatID:           row.ChatID,
		LanguageTag:      row.LanguageTag,
		ListingID:        row.ListingID,
		Listing: domain.Listing{
			ID:              row.ListingID,
			Source:          row.Source,
			ExternalID:      row.ExternalID,
			URL:             row.URL,
			PropertyType:    domain.PropertyType(row.PropertyType),
			DealType:        domain.DealType(row.DealType),
			Title:           row.Title,
			PriceEUR:        row.PriceEUR,
			Rooms:           row.Rooms,
			AreaM2:          row.AreaM2,
			City:            row.City,
			District:        row.District,
			Address:         row.Address,
			Floor:           row.Floor,
			TotalFloors:     row.TotalFloors,
			BuildingSeries:  row.BuildingSeries,
			BuildingType:    row.BuildingType,
			LandAreaM2:      row.LandAreaM2,
			DetailsEnriched: row.DetailsEnriched,
			PublishedAt:     optionalTime(row.PublishedAt),
			ImageURL:        row.ImageURL,
			AreaKey:         row.AreaKey,
			AreaName:        row.AreaName,
			AreaType:        row.AreaType,
			AreaParentKey:   row.AreaParentKey,
			AreaParentName:  row.AreaParentName,
			SourceAreaKey:   row.SourceAreaKey,
			SourceAreaName:  row.SourceAreaName,
		},
	}
}

func (s *Store) MarkSent(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE notifications SET status='sent',sent_at=now(),attempts=attempts+1 WHERE id=$1`, id)
	return err
}

func (s *Store) Retry(ctx context.Context, id int64, attempts int, cause error, delay time.Duration) error {
	if delay <= 0 {
		seconds := 5 * (1 << min(attempts, 6))
		if seconds > 300 {
			seconds = 300
		}
		delay = time.Duration(seconds) * time.Second
	}
	message := cause.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := s.pool.Exec(ctx, `UPDATE notifications SET attempts=attempts+1,next_attempt_at=$1,last_error=$2 WHERE id=$3`, time.Now().UTC().Add(delay), message, id)
	return err
}

func (s *Store) Fail(ctx context.Context, id int64, cause error) error {
	message := cause.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := s.pool.Exec(ctx, `UPDATE notifications SET status='failed',attempts=attempts+1,last_error=$1 WHERE id=$2`, message, id)
	return err
}

func (s *Store) DisableFiltersForChat(ctx context.Context, chatID int64) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE filters SET enabled=FALSE WHERE enabled=TRUE AND user_id=(SELECT id FROM users WHERE chat_id=$1)`, chatID)
	return tag.RowsAffected(), err
}

func (s *Store) PhotoFileIDs(ctx context.Context, listingID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT telegram_file_id FROM listing_photos WHERE listing_id=$1 ORDER BY position`, listingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) CachePhotoFileIDs(ctx context.Context, listingID int64, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM listing_photos WHERE listing_id=$1`, listingID); err != nil {
		return err
	}
	for position, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO listing_photos(listing_id,position,telegram_file_id) VALUES($1,$2,$3)`, listingID, position, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ClearPhotoFileIDs(ctx context.Context, listingID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM listing_photos WHERE listing_id=$1`, listingID)
	return err
}
