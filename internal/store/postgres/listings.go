package postgres

import (
	"context"
	"encoding/json"
	"slices"
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

func (s *Store) EnrichmentKeys(ctx context.Context, listings []domain.Listing) (map[domain.ListingKey]struct{}, error) {
	result := map[domain.ListingKey]struct{}{}
	incoming := map[domain.ListingKey]domain.Listing{}
	for _, listing := range listings {
		result[listing.Key()] = struct{}{}
		incoming[listing.Key()] = listing
	}
	if len(result) == 0 {
		return result, nil
	}
	for source, ids := range groupIDs(listings) {
		rows, err := s.pool.Query(ctx, `SELECT l.external_id,l.price_eur,COALESCE(l.description,''),COALESCE(l.address,''),l.rooms,l.area_m2,l.floor,l.total_floors,COALESCE(l.building_series,''),COALESCE(l.building_type,''),l.land_area_m2,COALESCE(l.image_url,''),l.photo_urls,COALESCE(a.key,''),l.details_enriched FROM listings l LEFT JOIN areas a ON a.id=l.area_id WHERE l.source=$1 AND l.external_id=ANY($2)`, source, ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var stored domain.Listing
			var photoJSON []byte
			if err := rows.Scan(&stored.ExternalID, &stored.PriceEUR, &stored.Description, &stored.Address, &stored.Rooms, &stored.AreaM2, &stored.Floor, &stored.TotalFloors, &stored.BuildingSeries, &stored.BuildingType, &stored.LandAreaM2, &stored.ImageURL, &photoJSON, &stored.AreaKey, &stored.DetailsEnriched); err != nil {
				rows.Close()
				return nil, err
			}
			if err := json.Unmarshal(photoJSON, &stored.PhotoURLs); err != nil {
				rows.Close()
				return nil, err
			}
			key := domain.ListingKey{Source: source, ExternalID: stored.ExternalID}
			if !enrichmentRequired(stored, incoming[key]) {
				delete(result, key)
			}
		}
		rows.Close()
	}
	return result, nil
}

func enrichmentRequired(stored, incoming domain.Listing) bool {
	if !pricesEqual(stored.PriceEUR, incoming.PriceEUR) {
		return true
	}
	if incoming.DetailsEnriched && incoming.Description != "" && incoming.Description != stored.Description {
		return true
	}
	if incoming.Address != "" && incoming.Address != stored.Address ||
		incoming.AreaKey != "" && incoming.AreaKey != stored.AreaKey ||
		incoming.BuildingSeries != "" && incoming.BuildingSeries != stored.BuildingSeries ||
		incoming.BuildingType != "" && incoming.BuildingType != stored.BuildingType ||
		incoming.Rooms != nil && !intPointersEqual(stored.Rooms, incoming.Rooms) ||
		incoming.AreaM2 != nil && !floatPointersEqual(stored.AreaM2, incoming.AreaM2) ||
		incoming.Floor != nil && !intPointersEqual(stored.Floor, incoming.Floor) ||
		incoming.TotalFloors != nil && !intPointersEqual(stored.TotalFloors, incoming.TotalFloors) ||
		incoming.LandAreaM2 != nil && !floatPointersEqual(stored.LandAreaM2, incoming.LandAreaM2) {
		return true
	}
	if len(incoming.PhotoURLs) > 0 && !slices.Equal(incoming.PhotoURLs, stored.PhotoURLs) {
		return true
	}
	return incoming.ImageURL != "" && incoming.ImageURL != stored.ImageURL
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
		err = tx.QueryRow(ctx, `INSERT INTO listings(source,external_id,url,property_type,deal_type,price_eur,rooms,area_m2,city,district,address,floor,total_floors,building_series,building_type,land_area_m2,details_enriched,title,description,published_at,image_url,photo_urls,area_id,source_area_id,first_seen_at,availability_status,last_seen_at,availability_changed_at,raw_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),$12,$13,NULLIF($14,''),NULLIF($15,''),$16,$17,$18,$19,$20,NULLIF($21,''),$22,$23,$24,$25,'active',$25,$25,$26) ON CONFLICT(source,external_id) DO NOTHING RETURNING id`, listing.Source, listing.ExternalID, listing.URL, listing.PropertyType, listing.DealType, listing.PriceEUR, listing.Rooms, listing.AreaM2, listing.City, listing.District, listing.Address, listing.Floor, listing.TotalFloors, listing.BuildingSeries, listing.BuildingType, listing.LandAreaM2, listing.DetailsEnriched, listing.Title, listing.Description, listing.PublishedAt, listing.ImageURL, photos, areaID, sourceAreaID, now, raw).Scan(&listingID)
		if err == pgx.ErrNoRows {
			var previousPrice *int
			var previousAvailability string
			err = tx.QueryRow(ctx, `SELECT id,price_eur,availability_status FROM listings WHERE source=$1 AND external_id=$2 FOR UPDATE`, listing.Source, listing.ExternalID).Scan(&listingID, &previousPrice, &previousAvailability)
			if err != nil {
				return result, err
			}
			storedListing, err := loadListing(ctx, tx, listingID)
			if err != nil {
				return result, err
			}
			signalsChanged := matchingSignalsChanged(storedListing, listing)
			_, err = tx.Exec(ctx, `UPDATE listings SET url=$1,title=COALESCE(NULLIF($2,''),title),description=COALESCE(NULLIF($3,''),description),published_at=COALESCE($4,published_at),image_url=COALESCE(NULLIF($5,''),image_url),area_id=COALESCE($6,area_id),source_area_id=COALESCE($7,source_area_id),price_eur=$8,rooms=COALESCE($9,rooms),area_m2=COALESCE($10,area_m2),city=COALESCE(NULLIF($11,''),city),district=COALESCE(NULLIF($12,''),district),address=COALESCE(NULLIF($13,''),address),floor=COALESCE($14,floor),total_floors=COALESCE($15,total_floors),building_series=COALESCE(NULLIF($16,''),building_series),building_type=COALESCE(NULLIF($17,''),building_type),land_area_m2=COALESCE($18,land_area_m2),photo_urls=CASE WHEN jsonb_array_length($19::jsonb)>0 THEN $19::jsonb ELSE photo_urls END,details_enriched=details_enriched OR $20,raw_json=$21,availability_status='active',last_seen_at=$22,availability_changed_at=CASE WHEN availability_status<>'active' THEN $22 ELSE availability_changed_at END WHERE id=$23`, listing.URL, listing.Title, listing.Description, listing.PublishedAt, listing.ImageURL, areaID, sourceAreaID, listing.PriceEUR, listing.Rooms, listing.AreaM2, listing.City, listing.District, listing.Address, listing.Floor, listing.TotalFloors, listing.BuildingSeries, listing.BuildingType, listing.LandAreaM2, photos, listing.DetailsEnriched, raw, now, listingID)
			if err != nil {
				return result, err
			}
			if err := recordFeedAvailability(ctx, tx, listingID, domain.AvailabilityStatus(previousAvailability), now); err != nil {
				return result, err
			}
			if signalsChanged {
				if err := queueListingSignalJob(ctx, tx, listingID, now); err != nil {
					return result, err
				}
			}
			if pricesEqual(previousPrice, listing.PriceEUR) {
				continue
			}
			var historyID int64
			if err := tx.QueryRow(ctx, `INSERT INTO listing_price_history(listing_id,price_eur,observed_at) VALUES($1,$2,$3) RETURNING id`, listingID, listing.PriceEUR, now).Scan(&historyID); err != nil {
				return result, err
			}
			if !notify {
				continue
			}
			payload, err := events.MarshalListingPriceChanged(listingID, historyID, listing.Source, listing.ExternalID, previousPrice, listing.PriceEUR)
			if err != nil {
				return result, err
			}
			if err := insertOutbox(ctx, tx, events.ListingPriceChangedV1, listingID, payload, now); err != nil {
				return result, err
			}
			result.Events++
			continue
		}
		if err != nil {
			return result, err
		}
		result.Inserted++
		if err := recordFeedAvailability(ctx, tx, listingID, "", now); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO listing_availability_jobs(listing_id,next_attempt_at,created_at,updated_at) VALUES($1,now(),$2,$2) ON CONFLICT(listing_id) DO NOTHING`, listingID, now); err != nil {
			return result, err
		}
		if err := queueListingSignalJob(ctx, tx, listingID, now); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO listing_price_history(listing_id,price_eur,observed_at) VALUES($1,$2,$3)`, listingID, listing.PriceEUR, now); err != nil {
			return result, err
		}
		if !notify {
			continue
		}
		payload, err := events.MarshalListingDiscovered(listingID, listing.Source, listing.ExternalID)
		if err != nil {
			return result, err
		}
		if err := insertOutbox(ctx, tx, events.ListingDiscoveredV1, listingID, payload, now); err != nil {
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

func (s *Store) RecordFeedSightings(ctx context.Context, listings []domain.Listing) error {
	if len(listings) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	for source, ids := range groupIDs(listings) {
		rows, err := tx.Query(ctx, `SELECT id,availability_status FROM listings WHERE source=$1 AND external_id=ANY($2) FOR UPDATE`, source, ids)
		if err != nil {
			return err
		}
		var transitioned []int64
		previous := map[int64]string{}
		for rows.Next() {
			var listingID int64
			var status string
			if err := rows.Scan(&listingID, &status); err != nil {
				rows.Close()
				return err
			}
			if status != string(domain.AvailabilityActive) {
				transitioned = append(transitioned, listingID)
				previous[listingID] = status
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		if _, err := tx.Exec(ctx, `UPDATE listings SET availability_status='active',last_seen_at=$1,availability_changed_at=CASE WHEN availability_status<>'active' THEN $1 ELSE availability_changed_at END WHERE source=$2 AND external_id=ANY($3)`, now, source, ids); err != nil {
			return err
		}
		for _, listingID := range transitioned {
			if _, err := tx.Exec(ctx, `INSERT INTO listing_availability_observations(listing_id,status,previous_status,observed_at,observation_kind,evidence,is_transition) VALUES($1,'active',$2,$3,'feed','advert observed in provider results',TRUE)`, listingID, previous[listingID], now); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func matchingSignalsChanged(stored, incoming domain.Listing) bool {
	effective := stored
	mergeString(&effective.Description, incoming.Description)
	mergeString(&effective.Address, incoming.Address)
	mergeString(&effective.AreaKey, incoming.AreaKey)
	mergeString(&effective.BuildingSeries, incoming.BuildingSeries)
	mergeString(&effective.BuildingType, incoming.BuildingType)
	mergeString(&effective.ImageURL, incoming.ImageURL)
	if incoming.Rooms != nil {
		effective.Rooms = incoming.Rooms
	}
	if incoming.AreaM2 != nil {
		effective.AreaM2 = incoming.AreaM2
	}
	if incoming.Floor != nil {
		effective.Floor = incoming.Floor
	}
	if incoming.TotalFloors != nil {
		effective.TotalFloors = incoming.TotalFloors
	}
	if incoming.LandAreaM2 != nil {
		effective.LandAreaM2 = incoming.LandAreaM2
	}
	if len(incoming.PhotoURLs) > 0 {
		effective.PhotoURLs = incoming.PhotoURLs
	}

	return stored.Description != effective.Description ||
		stored.Address != effective.Address ||
		stored.AreaKey != effective.AreaKey ||
		stored.BuildingSeries != effective.BuildingSeries ||
		stored.BuildingType != effective.BuildingType ||
		!intPointersEqual(stored.Rooms, effective.Rooms) ||
		!floatPointersEqual(stored.AreaM2, effective.AreaM2) ||
		!intPointersEqual(stored.Floor, effective.Floor) ||
		!intPointersEqual(stored.TotalFloors, effective.TotalFloors) ||
		!floatPointersEqual(stored.LandAreaM2, effective.LandAreaM2) ||
		!slices.Equal(effectivePhotoURLs(stored), effectivePhotoURLs(effective))
}

func mergeString(target *string, value string) {
	if value != "" {
		*target = value
	}
}

func effectivePhotoURLs(listing domain.Listing) []string {
	if len(listing.PhotoURLs) > 0 {
		return listing.PhotoURLs
	}
	if listing.ImageURL != "" {
		return []string{listing.ImageURL}
	}
	return nil
}

func intPointersEqual(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func floatPointersEqual(left, right *float64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func recordFeedAvailability(ctx context.Context, tx pgx.Tx, listingID int64, previous domain.AvailabilityStatus, observedAt time.Time) error {
	var prior any
	transition := false
	if previous.Valid() {
		prior = previous
		transition = previous != domain.AvailabilityActive
	}
	_, err := tx.Exec(ctx, `INSERT INTO listing_availability_observations(listing_id,status,previous_status,observed_at,observation_kind,evidence,is_transition) VALUES($1,'active',$2,$3,'feed','advert observed in provider results',$4)`, listingID, prior, observedAt, transition)
	return err
}

func queueListingSignalJob(ctx context.Context, tx pgx.Tx, listingID int64, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO listing_signal_jobs(listing_id,next_attempt_at,created_at,updated_at) VALUES($1,now(),$2,$2) ON CONFLICT(listing_id) DO UPDATE SET generation=listing_signal_jobs.generation+1,status='pending',attempts=0,next_attempt_at=now(),claimed_at=NULL,last_error=NULL,updated_at=excluded.updated_at`, listingID, now)
	return err
}

func insertOutbox(ctx context.Context, tx pgx.Tx, eventType string, listingID int64, payload []byte, occurredAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO outbox_events(event_type,aggregate_id,payload,occurred_at,next_attempt_at) VALUES($1,$2,$3,$4,now())`, eventType, listingID, payload, occurredAt)
	return err
}

func pricesEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
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
