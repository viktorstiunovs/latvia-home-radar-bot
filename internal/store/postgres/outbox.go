package postgres

import (
	"context"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/events"
)

func (s *Store) PendingOutbox(ctx context.Context, limit int) ([]events.Envelope, error) {
	rows, err := s.pool.Query(ctx, `
		WITH due AS (
			SELECT id
			FROM outbox_events
			WHERE published_at IS NULL AND next_attempt_at <= now()
			ORDER BY occurred_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE outbox_events event
		SET attempts = event.attempts + 1,
			next_attempt_at = now() + interval '30 seconds'
		FROM due
		WHERE event.id = due.id
		RETURNING event.id::text, event.event_type, event.occurred_at, event.payload`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []events.Envelope
	for rows.Next() {
		var event events.Envelope
		if err := rows.Scan(&event.ID, &event.Type, &event.OccurredAt, &event.Data); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (s *Store) MarkOutboxPublished(ctx context.Context, eventID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE outbox_events SET published_at=$1,last_error=NULL WHERE id=$2::uuid AND published_at IS NULL`, time.Now().UTC(), eventID)
	return err
}

func (s *Store) RetryOutbox(ctx context.Context, eventID string, cause error) error {
	message := cause.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := s.pool.Exec(ctx, `UPDATE outbox_events SET next_attempt_at=now()+(power(2,LEAST(attempts,6))*interval '5 seconds'),last_error=$1 WHERE id=$2::uuid AND published_at IS NULL`, message, eventID)
	return err
}
