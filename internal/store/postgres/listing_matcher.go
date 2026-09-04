package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/jackc/pgx/v5"
)

const listingMatcherConsumer = "alert-matcher.v1"

func (s *Store) MatchListingEvent(ctx context.Context, eventID string, listingID int64, occurredAt time.Time) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
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
	listing, err := loadListing(ctx, tx, listingID)
	if err != nil {
		return 0, err
	}
	filters, err := loadFilters(ctx, tx)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, filter := range filters {
		if filter.ActivatedAt != nil && !filter.ActivatedAt.Before(occurredAt) {
			continue
		}
		if !domain.Matches(listing, filter) {
			continue
		}
		tag, err := tx.Exec(ctx, `INSERT INTO notifications(filter_id,listing_id,status,next_attempt_at) VALUES($1,$2,'pending',now()) ON CONFLICT(filter_id,listing_id) DO NOTHING`, filter.ID, listingID)
		if err != nil {
			return 0, err
		}
		created += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return created, nil
}

func loadListing(ctx context.Context, tx pgx.Tx, listingID int64) (domain.Listing, error) {
	var listing domain.Listing
	var property, deal string
	var photoJSON []byte
	err := tx.QueryRow(ctx, `SELECT l.id,l.source,l.external_id,l.url,l.property_type,l.deal_type,l.title,l.price_eur,l.rooms,l.area_m2,COALESCE(l.city,''),COALESCE(l.district,''),COALESCE(l.address,''),l.floor,l.total_floors,COALESCE(l.building_series,''),COALESCE(l.building_type,''),l.land_area_m2,l.details_enriched,l.published_at,COALESCE(l.image_url,''),l.photo_urls,COALESCE(a.key,''),COALESCE(a.name,''),COALESCE(a.type,''),COALESCE(parent.key,''),COALESCE(parent.name,''),COALESCE(sa.external_key,''),COALESCE(sa.raw_name,'') FROM listings l LEFT JOIN areas a ON a.id=l.area_id LEFT JOIN areas parent ON parent.id=a.parent_id LEFT JOIN source_areas sa ON sa.id=l.source_area_id WHERE l.id=$1`, listingID).Scan(
		&listing.ID, &listing.Source, &listing.ExternalID, &listing.URL, &property, &deal, &listing.Title,
		&listing.PriceEUR, &listing.Rooms, &listing.AreaM2, &listing.City, &listing.District, &listing.Address,
		&listing.Floor, &listing.TotalFloors, &listing.BuildingSeries, &listing.BuildingType, &listing.LandAreaM2,
		&listing.DetailsEnriched, &listing.PublishedAt, &listing.ImageURL, &photoJSON, &listing.AreaKey,
		&listing.AreaName, &listing.AreaType, &listing.AreaParentKey, &listing.AreaParentName,
		&listing.SourceAreaKey, &listing.SourceAreaName,
	)
	if err != nil {
		return domain.Listing{}, err
	}
	listing.PropertyType = domain.PropertyType(property)
	listing.DealType = domain.DealType(deal)
	if err := json.Unmarshal(photoJSON, &listing.PhotoURLs); err != nil {
		return domain.Listing{}, err
	}
	return listing, nil
}
