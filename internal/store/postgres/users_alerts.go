package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/store/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
)

func (s *Store) UpsertUser(ctx context.Context, telegramUserID, chatID int64, name, languageTag string) (domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (telegram_user_id, chat_id, name, language_tag, created_at)
		VALUES ($1, $2, nullif($3, ''), coalesce(nullif($4, ''), 'en'), $5)
		ON CONFLICT (telegram_user_id) DO UPDATE
		SET chat_id = excluded.chat_id,
		    name = coalesce(excluded.name, users.name)
		RETURNING id, language_tag
	`, telegramUserID, chatID, name, languageTag, time.Now().UTC()).Scan(&user.ID, &user.LanguageTag)
	return user, err
}

func (s *Store) ListUserChatIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT chat_id FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list user chat IDs: %w", err)
	}
	defer rows.Close()

	var result []int64
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return nil, fmt.Errorf("scan user chat ID: %w", err)
		}
		result = append(result, chatID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user chat IDs: %w", err)
	}

	return result, nil
}

func (s *Store) SetUserLanguage(ctx context.Context, telegramUserID int64, languageTag string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET language_tag=$1 WHERE telegram_user_id=$2`, languageTag, telegramUserID)
	return tag.RowsAffected() == 1, err
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
	err = tx.QueryRow(ctx, `
		INSERT INTO filters (
			user_id,
			enabled,
			deal_type,
			property_types,
			price_min,
			price_max,
			rooms_min,
			rooms_max,
			area_min,
			area_max,
			activated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, filter.UserID, true, filter.DealType, properties, filter.PriceMin, filter.PriceMax, filter.RoomsMin, filter.RoomsMax, filter.AreaMin, filter.AreaMax, activated).Scan(&id)
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
	rows, err := s.queries.ListSavedFilters(ctx, telegramUserID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.SavedFilter, 0, len(rows))
	for _, row := range rows {
		item := domain.SavedFilter{
			ID:         row.ID,
			DealType:   domain.DealType(row.DealType),
			PriceMin:   row.PriceMin,
			PriceMax:   row.PriceMax,
			RoomsMin:   row.RoomsMin,
			RoomsMax:   row.RoomsMax,
			AreaMin:    row.AreaMin,
			AreaMax:    row.AreaMax,
			Enabled:    row.Enabled,
			AreaLabels: row.AreaLabels,
		}
		for _, p := range row.PropertyTypes {
			item.PropertyTypes = append(item.PropertyTypes, domain.PropertyType(p))
		}
		result = append(result, item)
	}
	return result, nil
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
	rows, err := sqlcgen.New(tx).ListEnabledFilters(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]domain.SearchFilter, 0, len(rows))
	for _, row := range rows {
		activated := row.ActivatedAt.Time
		filter := domain.SearchFilter{
			ID:          row.ID,
			UserID:      row.UserID,
			DealType:    domain.DealType(row.DealType),
			PriceMin:    row.PriceMin,
			PriceMax:    row.PriceMax,
			RoomsMin:    row.RoomsMin,
			RoomsMax:    row.RoomsMax,
			AreaMin:     row.AreaMin,
			AreaMax:     row.AreaMax,
			Enabled:     true,
			ActivatedAt: &activated,
			AreaKeys:    row.AreaKeys,
		}
		for _, p := range row.PropertyTypes {
			filter.PropertyTypes = append(filter.PropertyTypes, domain.PropertyType(p))
		}
		result = append(result, filter)
	}
	return result, nil
}
