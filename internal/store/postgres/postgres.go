package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, databaseURL string, attempts int) (*Store, error) {
	if attempts < 1 {
		return nil, fmt.Errorf("connect attempts must be positive")
	}
	var pool *pgxpool.Pool
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if err = migrate(ctx, databaseURL); err == nil {
			pool, err = pgxpool.New(ctx, databaseURL)
		}
		if err == nil {
			err = pool.Ping(ctx)
		}
		if err == nil {
			break
		}
		if pool != nil {
			pool.Close()
			pool = nil
		}
		if attempt+1 < attempts {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	if err != nil {
		return nil, err
	}
	store := &Store{pool: pool}
	if err := store.seedAreas(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) seedAreas(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, definition := range domain.AreaDefinitions() {
		if _, err := upsertArea(ctx, tx, definition); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
