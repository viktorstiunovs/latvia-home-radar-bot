package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/events"
	"github.com/jackc/pgx/v5"
)

func (s *Store) IsSourceInitialized(ctx context.Context, key string) (bool, error) {
	var value bool
	err := s.pool.QueryRow(ctx, `SELECT initialized FROM source_state WHERE source_key=$1`, key).Scan(&value)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return value, err
}

func (s *Store) UnseenKeys(ctx context.Context, listings []domain.Listing) (map[domain.ListingKey]struct{}, error) {
	result := map[domain.ListingKey]struct{}{}
	for _, listing := range listings {
		result[listing.Key()] = struct{}{}
	}
	if len(result) == 0 {
		return result, nil
	}
	for source, ids := range groupIDs(listings) {
		rows, err := s.pool.Query(ctx, `SELECT external_id FROM listings WHERE source=$1 AND external_id=ANY($2)`, source, ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			delete(result, domain.ListingKey{Source: source, ExternalID: id})
		}
		rows.Close()
	}
	return result, nil
}

func groupIDs(listings []domain.Listing) map[string][]string {
	result := map[string][]string{}
	for _, l := range listings {
		result[l.Source] = append(result[l.Source], l.ExternalID)
	}
	return result
}

func (s *Store) ProcessDiscovered(ctx context.Context, sourceKey string, listings []domain.Listing, notify bool) (domain.DiscoveryResult, error) {
	now := time.Now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.DiscoveryResult{}, err
	}
	defer tx.Rollback(ctx)
	result := domain.DiscoveryResult{}
	for _, listing := range listings {
		areaID, sourceAreaID, err := upsertListingArea(ctx, tx, listing)
		if err != nil {
			return result, err
		}
		photos, _ := json.Marshal(listing.PhotoURLs)
		raw, _ := json.Marshal(map[string]string{"url": listing.URL, "title": listing.Title})
		var listingID int64
		err = tx.QueryRow(ctx, `INSERT INTO listings(source,external_id,url,property_type,deal_type,price_eur,rooms,area_m2,city,district,address,floor,total_floors,building_series,building_type,land_area_m2,details_enriched,title,published_at,image_url,photo_urls,area_id,source_area_id,first_seen_at,raw_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),$12,$13,NULLIF($14,''),NULLIF($15,''),$16,$17,$18,$19,NULLIF($20,''),$21,$22,$23,$24,$25) ON CONFLICT(source,external_id) DO NOTHING RETURNING id`, listing.Source, listing.ExternalID, listing.URL, listing.PropertyType, listing.DealType, listing.PriceEUR, listing.Rooms, listing.AreaM2, listing.City, listing.District, listing.Address, listing.Floor, listing.TotalFloors, listing.BuildingSeries, listing.BuildingType, listing.LandAreaM2, listing.DetailsEnriched, listing.Title, listing.PublishedAt, listing.ImageURL, photos, areaID, sourceAreaID, now, raw).Scan(&listingID)
		if err == pgx.ErrNoRows {
			_, err = tx.Exec(ctx, `UPDATE listings SET area_id=COALESCE($1,area_id),source_area_id=COALESCE($2,source_area_id),price_eur=COALESCE($3,price_eur),rooms=COALESCE($4,rooms),area_m2=COALESCE($5,area_m2),city=COALESCE(NULLIF($6,''),city),district=COALESCE(NULLIF($7,''),district),address=COALESCE(NULLIF($8,''),address),floor=COALESCE($9,floor),total_floors=COALESCE($10,total_floors),building_series=COALESCE(NULLIF($11,''),building_series),building_type=COALESCE(NULLIF($12,''),building_type),land_area_m2=COALESCE($13,land_area_m2),photo_urls=CASE WHEN jsonb_array_length($14::jsonb)>0 THEN $14::jsonb ELSE photo_urls END,details_enriched=details_enriched OR $15 WHERE source=$16 AND external_id=$17`, areaID, sourceAreaID, listing.PriceEUR, listing.Rooms, listing.AreaM2, listing.City, listing.District, listing.Address, listing.Floor, listing.TotalFloors, listing.BuildingSeries, listing.BuildingType, listing.LandAreaM2, photos, listing.DetailsEnriched, listing.Source, listing.ExternalID)
			if err != nil {
				return result, err
			}
			continue
		}
		if err != nil {
			return result, err
		}
		result.Inserted++
		if !notify {
			continue
		}
		payload, err := events.MarshalListingDiscovered(listingID, listing.Source, listing.ExternalID)
		if err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(event_type,aggregate_id,payload,occurred_at,next_attempt_at) VALUES($1,$2,$3,$4,now())`, events.ListingDiscoveredV1, listingID, payload, now); err != nil {
			return result, err
		}
		result.Events++
	}
	if _, err := tx.Exec(ctx, `INSERT INTO source_state(source_key,initialized,last_success_at) VALUES($1,TRUE,$2) ON CONFLICT(source_key) DO UPDATE SET initialized=TRUE,last_success_at=excluded.last_success_at`, sourceKey, now); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func upsertArea(ctx context.Context, tx pgx.Tx, d domain.AreaDefinition) (*int64, error) {
	var parentID *int64
	if d.ParentKey != "" {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM areas WHERE key=$1`, d.ParentKey).Scan(&id); err != nil {
			return nil, err
		}
		parentID = &id
	}
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO areas(key,parent_id,type,name) VALUES($1,$2,$3,$4) ON CONFLICT(key) DO UPDATE SET parent_id=excluded.parent_id,type=excluded.type,name=excluded.name RETURNING id`, d.Key, parentID, d.Type, d.Name).Scan(&id)
	return &id, err
}

func upsertListingArea(ctx context.Context, tx pgx.Tx, l domain.Listing) (*int64, *int64, error) {
	if l.AreaParentKey != "" && l.AreaParentName != "" {
		typ := "region"
		if l.AreaParentKey == "lv/riga" || l.AreaParentKey == "lv/jurmala" {
			typ = "city"
		}
		if _, err := upsertArea(ctx, tx, domain.AreaDefinition{Key: l.AreaParentKey, Name: l.AreaParentName, Type: typ}); err != nil {
			return nil, nil, err
		}
	}
	var areaID *int64
	var err error
	if l.AreaKey != "" && l.AreaName != "" {
		typ := l.AreaType
		if typ == "" {
			typ = "area"
		}
		areaID, err = upsertArea(ctx, tx, domain.AreaDefinition{Key: l.AreaKey, Name: l.AreaName, Type: typ, ParentKey: l.AreaParentKey})
		if err != nil {
			return nil, nil, err
		}
	}
	var sourceID *int64
	if l.SourceAreaKey != "" {
		var id int64
		err = tx.QueryRow(ctx, `INSERT INTO source_areas(source,external_key,raw_name,area_id) VALUES($1,$2,NULLIF($3,''),$4) ON CONFLICT(source,external_key) DO UPDATE SET raw_name=COALESCE(excluded.raw_name,source_areas.raw_name),area_id=COALESCE(excluded.area_id,source_areas.area_id) RETURNING id`, l.Source, l.SourceAreaKey, l.SourceAreaName, areaID).Scan(&id)
		if err != nil {
			return nil, nil, err
		}
		sourceID = &id
	}
	return areaID, sourceID, nil
}
