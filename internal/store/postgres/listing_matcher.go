package postgres

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/events"
	"github.com/clive00lewis/latvia-home-radar/internal/store/postgres/sqlcgen"
)

const listingMatcherConsumer = "alert-matcher.v1"

func (s *Store) MatchListingEvent(ctx context.Context, eventID string, listingID int64, occurredAt time.Time) (int, error) {
	return s.matchEvent(ctx, eventID, listingID, occurredAt, domain.NotificationListingDiscovered, nil, nil, true)
}

func (s *Store) MatchPriceChangedEvent(ctx context.Context, eventID string, event events.ListingPriceChanged, occurredAt time.Time) (int, error) {
	knownChange := event.PreviousPriceEUR != nil && event.CurrentPriceEUR != nil && *event.PreviousPriceEUR > 0 && *event.CurrentPriceEUR >= 0 && *event.CurrentPriceEUR != *event.PreviousPriceEUR
	return s.matchEvent(ctx, eventID, event.ListingID, occurredAt, domain.NotificationPriceChanged, event.PreviousPriceEUR, event.CurrentPriceEUR, knownChange)
}

func (s *Store) matchEvent(ctx context.Context, eventID string, listingID int64, occurredAt time.Time, notificationType domain.NotificationType, previousPriceEUR, currentPriceEUR *int, eligible bool) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)

	tag, err := tx.Exec(ctx, `INSERT INTO consumed_events(consumer_name,event_id,consumed_at) VALUES($1,$2::uuid,$3) ON CONFLICT DO NOTHING`, listingMatcherConsumer, eventID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, nil
	}
	if !eligible {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, nil
	}
	listing, err := loadListing(ctx, tx, listingID)
	if err != nil {
		return 0, err
	}
	if notificationType == domain.NotificationPriceChanged {
		listing.PriceEUR = currentPriceEUR
	}
	listingJSON, err := json.Marshal(listing)
	if err != nil {
		return 0, err
	}
	filters, err := loadFilters(ctx, tx)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, filter := range matchingFiltersByUser(filters, listing, occurredAt) {
		inserted, err := queries.InsertMatchedNotification(ctx, sqlcgen.InsertMatchedNotificationParams{
			FilterID:         filter.ID,
			ListingID:        listingID,
			EventID:          eventID,
			NotificationType: string(notificationType),
			PreviousPriceEUR: previousPriceEUR,
			CurrentPriceEUR:  currentPriceEUR,
			ListingSnapshot:  listingJSON,
		})
		if err != nil {
			return 0, err
		}
		created += int(inserted)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return created, nil
}

func matchingFiltersByUser(filters []domain.SearchFilter, listing domain.Listing, occurredAt time.Time) []domain.SearchFilter {
	selected := make(map[int64]domain.SearchFilter)
	for _, filter := range filters {
		if filter.ActivatedAt != nil && !filter.ActivatedAt.Before(occurredAt) {
			continue
		}
		if !domain.Matches(listing, filter) {
			continue
		}
		current, exists := selected[filter.UserID]
		if !exists || filter.ID < current.ID {
			selected[filter.UserID] = filter
		}
	}

	result := make([]domain.SearchFilter, 0, len(selected))
	for _, filter := range selected {
		result = append(result, filter)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].UserID == result[right].UserID {
			return result[left].ID < result[right].ID
		}
		return result[left].UserID < result[right].UserID
	})
	return result
}

func loadListing(ctx context.Context, db sqlcgen.DBTX, listingID int64) (domain.Listing, error) {
	row, err := sqlcgen.New(db).LoadListing(ctx, listingID)
	if err != nil {
		return domain.Listing{}, err
	}

	listing := domain.Listing{
		ID:                    row.ID,
		Source:                row.Source,
		ExternalID:            row.ExternalID,
		URL:                   row.URL,
		PropertyType:          domain.PropertyType(row.PropertyType),
		DealType:              domain.DealType(row.DealType),
		Title:                 row.Title,
		Description:           row.Description,
		NormalizedAddress:     row.NormalizedAddress,
		NormalizedDescription: row.NormalizedDescription,
		PriceEUR:              row.PriceEUR,
		Rooms:                 row.Rooms,
		AreaM2:                row.AreaM2,
		City:                  row.City,
		District:              row.District,
		Address:               row.Address,
		Floor:                 row.Floor,
		TotalFloors:           row.TotalFloors,
		BuildingSeries:        row.BuildingSeries,
		BuildingType:          row.BuildingType,
		LandAreaM2:            row.LandAreaM2,
		DetailsEnriched:       row.DetailsEnriched,
		PublishedAt:           optionalTime(row.PublishedAt),
		ImageURL:              row.ImageURL,
		AreaKey:               row.AreaKey,
		AreaName:              row.AreaName,
		AreaType:              row.AreaType,
		AreaParentKey:         row.AreaParentKey,
		AreaParentName:        row.AreaParentName,
		SourceAreaKey:         row.SourceAreaKey,
		SourceAreaName:        row.SourceAreaName,
		AvailabilityStatus:    domain.AvailabilityStatus(row.AvailabilityStatus),
		FirstSeenAt:           row.FirstSeenAt.Time,
		LastSeenAt:            row.LastSeenAt.Time,
		AvailabilityChangedAt: row.AvailabilityChangedAt.Time,
	}
	if err := json.Unmarshal(row.PhotoURLs, &listing.PhotoURLs); err != nil {
		return domain.Listing{}, err
	}

	return listing, nil
}
