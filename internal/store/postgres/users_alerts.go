package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) UpsertUser(ctx context.Context, telegramUserID, chatID int64, name string) (int64, error) {
	var id int64
	var err = s.pool.QueryRow(ctx, `INSERT INTO users(telegram_user_id,chat_id,name,created_at) VALUES($1,$2,NULLIF($3,''),$4) ON CONFLICT(telegram_user_id) DO UPDATE SET chat_id=excluded.chat_id,name=COALESCE(excluded.name,users.name) RETURNING id`, telegramUserID, chatID, name, time.Now().UTC()).Scan(&id)
	return id, err
}

func (s *Store) ListChildAreas(ctx context.Context, parentKey string) ([]domain.AreaChoice, error) {
	rows, err := s.pool.Query(ctx, `SELECT child.key,child.name FROM areas child JOIN areas parent ON parent.id=child.parent_id WHERE parent.key=$1 ORDER BY child.name`, parentKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.AreaChoice
	for rows.Next() {
		var item domain.AreaChoice
		if err := rows.Scan(&item.Key, &item.Label); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListRootAreas(ctx context.Context) ([]domain.AreaChoice, error) {
	rows, err := s.pool.Query(ctx, `SELECT key,name FROM areas WHERE parent_id IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.AreaChoice
	for rows.Next() {
		var item domain.AreaChoice
		if err := rows.Scan(&item.Key, &item.Label); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CreateFilter(ctx context.Context, filter domain.SearchFilter) (int64, error) {
	activated := time.Now().UTC()
	if filter.ActivatedAt != nil {
		activated = *filter.ActivatedAt
	}
	properties := make([]string, len(filter.PropertyTypes))
	for i, p := range filter.PropertyTypes {
		properties[i] = string(p)
	}
	if len(properties) == 0 {
		properties = []string{"apartment"}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO filters(user_id,enabled,deal_type,property_types,price_min,price_max,rooms_min,rooms_max,area_min,area_max,activated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`, filter.UserID, true, filter.DealType, properties, filter.PriceMin, filter.PriceMax, filter.RoomsMin, filter.RoomsMax, filter.AreaMin, filter.AreaMax, activated).Scan(&id)
	if err != nil {
		return 0, err
	}
	for _, key := range filter.AreaKeys {
		tag, err := tx.Exec(ctx, `INSERT INTO filter_areas(filter_id,area_id) SELECT $1,id FROM areas WHERE key=$2`, id, key)
		if err != nil {
			return 0, err
		}
		if tag.RowsAffected() != 1 {
			return 0, fmt.Errorf("unknown canonical area: %s", key)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) ListFilters(ctx context.Context, telegramUserID int64) ([]domain.SavedFilter, error) {
	rows, err := s.pool.Query(ctx, `SELECT f.id,f.deal_type,f.property_types,f.price_min,f.price_max,f.rooms_min,f.rooms_max,f.area_min,f.area_max,f.enabled,COALESCE(array_agg(CASE WHEN parent.name IS NULL THEN a.name ELSE a.name||' ('||parent.name||')' END ORDER BY a.key) FILTER (WHERE a.id IS NOT NULL),'{}') FROM filters f JOIN users u ON u.id=f.user_id LEFT JOIN filter_areas fa ON fa.filter_id=f.id LEFT JOIN areas a ON a.id=fa.area_id LEFT JOIN areas parent ON parent.id=a.parent_id WHERE u.telegram_user_id=$1 GROUP BY f.id ORDER BY f.id`, telegramUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.SavedFilter
	for rows.Next() {
		var item domain.SavedFilter
		var deal string
		var properties []string
		if err := rows.Scan(&item.ID, &deal, &properties, &item.PriceMin, &item.PriceMax, &item.RoomsMin, &item.RoomsMax, &item.AreaMin, &item.AreaMax, &item.Enabled, &item.AreaLabels); err != nil {
			return nil, err
		}
		item.DealType = domain.DealType(deal)
		for _, p := range properties {
			item.PropertyTypes = append(item.PropertyTypes, domain.PropertyType(p))
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) SetFilterEnabled(ctx context.Context, telegramUserID, filterID int64, enabled bool) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE filters SET enabled=$1,activated_at=CASE WHEN $1 THEN $2 ELSE activated_at END WHERE id=$3 AND user_id=(SELECT id FROM users WHERE telegram_user_id=$4)`, enabled, time.Now().UTC(), filterID, telegramUserID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 && !enabled {
		if _, err := tx.Exec(ctx, `DELETE FROM notifications WHERE filter_id=$1 AND status='pending'`, filterID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) DeleteFilter(ctx context.Context, telegramUserID, filterID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM filters WHERE id=$1 AND user_id=(SELECT id FROM users WHERE telegram_user_id=$2)`, filterID, telegramUserID)
	return tag.RowsAffected() == 1, err
}

func loadFilters(ctx context.Context, tx pgx.Tx) ([]domain.SearchFilter, error) {
	rows, err := tx.Query(ctx, `SELECT f.id,f.user_id,f.deal_type,f.property_types,f.price_min,f.price_max,f.rooms_min,f.rooms_max,f.area_min,f.area_max,f.activated_at,COALESCE(array_agg(a.key ORDER BY a.key) FILTER(WHERE a.id IS NOT NULL),'{}') FROM filters f LEFT JOIN filter_areas fa ON fa.filter_id=f.id LEFT JOIN areas a ON a.id=fa.area_id WHERE f.enabled=TRUE GROUP BY f.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.SearchFilter
	for rows.Next() {
		var f domain.SearchFilter
		var deal string
		var props []string
		var activated time.Time
		if err := rows.Scan(&f.ID, &f.UserID, &deal, &props, &f.PriceMin, &f.PriceMax, &f.RoomsMin, &f.RoomsMax, &f.AreaMin, &f.AreaMax, &activated, &f.AreaKeys); err != nil {
			return nil, err
		}
		f.DealType = domain.DealType(deal)
		for _, p := range props {
			f.PropertyTypes = append(f.PropertyTypes, domain.PropertyType(p))
		}
		f.Enabled = true
		f.ActivatedAt = &activated
		result = append(result, f)
	}
	return result, rows.Err()
}
