package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

func (s *Store) Pending(ctx context.Context, limit int) ([]domain.PendingNotification, error) {
	rows, err := s.pool.Query(ctx, `SELECT n.id,n.filter_id,n.attempts,n.notification_type,n.previous_price_eur,n.current_price_eur,n.property_id,n.comparison_listing_id,n.listing_snapshot,n.active_alternatives,u.telegram_user_id,COALESCE(u.name,''),u.chat_id,u.language_tag,l.id,l.source,l.external_id,l.url,l.property_type,l.deal_type,l.title,l.price_eur,l.rooms,l.area_m2,COALESCE(l.city,''),COALESCE(l.district,''),COALESCE(l.address,''),l.floor,l.total_floors,COALESCE(l.building_series,''),COALESCE(l.building_type,''),l.land_area_m2,l.details_enriched,l.published_at,COALESCE(l.image_url,''),l.photo_urls,COALESCE(a.key,''),COALESCE(a.name,''),COALESCE(a.type,''),COALESCE(parent.key,''),COALESCE(parent.name,''),COALESCE(sa.external_key,''),COALESCE(sa.raw_name,'') FROM notifications n JOIN filters f ON f.id=n.filter_id JOIN users u ON u.id=f.user_id JOIN listings l ON l.id=n.listing_id LEFT JOIN areas a ON a.id=l.area_id LEFT JOIN areas parent ON parent.id=a.parent_id LEFT JOIN source_areas sa ON sa.id=l.source_area_id WHERE n.status='pending' AND n.next_attempt_at<=now() AND f.enabled=TRUE ORDER BY n.next_attempt_at,n.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.PendingNotification
	for rows.Next() {
		var n domain.PendingNotification
		var property, deal, notificationType string
		var photoJSON, listingJSON, alternativesJSON []byte
		if err := rows.Scan(&n.ID, &n.FilterID, &n.Attempts, &notificationType, &n.PreviousPriceEUR, &n.CurrentPriceEUR, &n.PropertyID, &n.ComparisonID, &listingJSON, &alternativesJSON, &n.TelegramUserID, &n.UserName, &n.ChatID, &n.LanguageTag, &n.ListingID, &n.Listing.Source, &n.Listing.ExternalID, &n.Listing.URL, &property, &deal, &n.Listing.Title, &n.Listing.PriceEUR, &n.Listing.Rooms, &n.Listing.AreaM2, &n.Listing.City, &n.Listing.District, &n.Listing.Address, &n.Listing.Floor, &n.Listing.TotalFloors, &n.Listing.BuildingSeries, &n.Listing.BuildingType, &n.Listing.LandAreaM2, &n.Listing.DetailsEnriched, &n.Listing.PublishedAt, &n.Listing.ImageURL, &photoJSON, &n.Listing.AreaKey, &n.Listing.AreaName, &n.Listing.AreaType, &n.Listing.AreaParentKey, &n.Listing.AreaParentName, &n.Listing.SourceAreaKey, &n.Listing.SourceAreaName); err != nil {
			return nil, err
		}
		n.Type = domain.NotificationType(notificationType)
		n.Listing.ID = n.ListingID
		n.Listing.PropertyType = domain.PropertyType(property)
		n.Listing.DealType = domain.DealType(deal)
		if n.Type == domain.NotificationPriceChanged {
			n.Listing.PriceEUR = n.CurrentPriceEUR
		}
		_ = json.Unmarshal(photoJSON, &n.Listing.PhotoURLs)
		var snapshot domain.Listing
		if err := json.Unmarshal(listingJSON, &snapshot); err != nil {
			return nil, err
		}
		if snapshot.ID > 0 {
			n.Listing = snapshot
			n.ListingID = snapshot.ID
		}
		if err := json.Unmarshal(alternativesJSON, &n.Alternatives); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
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
