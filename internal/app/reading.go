package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type ProgressRecord struct {
	NoteID     string     `json:"noteId"`
	Completed  bool       `json:"completed"`
	LastReadAt *time.Time `json:"lastReadAt,omitempty"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type FavoriteRecord struct {
	NoteID    string    `json:"noteId"`
	CreatedAt time.Time `json:"createdAt"`
}

type Reading struct{ db *sql.DB }

func NewReading(db *sql.DB) *Reading { return &Reading{db: db} }

var ErrInvalidNoteID = errors.New("invalid note id")

func validNoteID(noteID string) bool {
	noteID = strings.TrimSpace(noteID)
	return noteID != "" && len(noteID) <= 512 && !strings.ContainsAny(noteID, "\r\n\x00")
}

func (s *Reading) ListProgress(ctx context.Context, userID string) ([]ProgressRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT note_id, completed, last_read_at, updated_at
		FROM user_note_progress WHERE user_id = $1 ORDER BY updated_at DESC LIMIT 500`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []ProgressRecord{}
	for rows.Next() {
		record := ProgressRecord{}
		var lastRead sql.NullTime
		if err := rows.Scan(&record.NoteID, &record.Completed, &lastRead, &record.UpdatedAt); err != nil {
			return nil, err
		}
		if lastRead.Valid {
			value := lastRead.Time
			record.LastReadAt = &value
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

const upsertProgressSQL = `INSERT INTO user_note_progress(user_id, note_id, completed, last_read_at, updated_at)
	VALUES($1, $2, COALESCE($3::boolean, FALSE), CASE WHEN $4::boolean THEN $5::timestamptz ELSE NULL END, $5)
	ON CONFLICT (user_id, note_id) DO UPDATE SET
		completed = COALESCE($3::boolean, user_note_progress.completed),
		last_read_at = CASE WHEN $4::boolean THEN $5::timestamptz ELSE user_note_progress.last_read_at END,
		updated_at = $5`

// UpsertProgress 中 completed 为 nil 表示"只记录阅读时间，不改已读状态"。
func (s *Reading) UpsertProgress(ctx context.Context, userID, noteID string, completed *bool, read bool) error {
	if !validNoteID(noteID) {
		return ErrInvalidNoteID
	}
	_, err := s.db.ExecContext(ctx, upsertProgressSQL, userID, noteID, completed, read, time.Now())
	return err
}

func (s *Reading) DeleteProgress(ctx context.Context, userID, noteID string) error {
	if !validNoteID(noteID) {
		return ErrInvalidNoteID
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_note_progress WHERE user_id = $1 AND note_id = $2`, userID, noteID)
	return err
}

// MergeProgress 用于登录时把浏览器本地记录合并到云端，同一笔记以服务端较新的记录为准。
func (s *Reading) MergeProgress(ctx context.Context, userID string, items []ProgressRecord) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now()
	for _, item := range items {
		if !validNoteID(item.NoteID) {
			continue
		}
		completed := item.Completed
		readAt := item.LastReadAt
		if readAt == nil && item.UpdatedAt.IsZero() {
			continue
		}
		if readAt == nil {
			value := now
			if !item.UpdatedAt.IsZero() {
				value = item.UpdatedAt
			}
			readAt = &value
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_note_progress(user_id, note_id, completed, last_read_at, updated_at)
			VALUES($1, $2, $3, $4, $5)
			ON CONFLICT (user_id, note_id) DO UPDATE SET
				completed = user_note_progress.completed OR EXCLUDED.completed,
				last_read_at = GREATEST(COALESCE(user_note_progress.last_read_at, EXCLUDED.last_read_at), EXCLUDED.last_read_at),
				updated_at = GREATEST(user_note_progress.updated_at, EXCLUDED.updated_at)`,
			userID, item.NoteID, completed, readAt, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Reading) ListFavorites(ctx context.Context, userID string) ([]FavoriteRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT note_id, created_at FROM user_favorites
		WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1000`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []FavoriteRecord{}
	for rows.Next() {
		record := FavoriteRecord{}
		if err := rows.Scan(&record.NoteID, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Reading) AddFavorite(ctx context.Context, userID, noteID string) error {
	if !validNoteID(noteID) {
		return ErrInvalidNoteID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_favorites(user_id, note_id, created_at)
		VALUES($1, $2, $3) ON CONFLICT (user_id, note_id) DO NOTHING`, userID, noteID, time.Now())
	return err
}

func (s *Reading) RemoveFavorite(ctx context.Context, userID, noteID string) error {
	if !validNoteID(noteID) {
		return ErrInvalidNoteID
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_favorites WHERE user_id = $1 AND note_id = $2`, userID, noteID)
	return err
}

func (s *Reading) MergeFavorites(ctx context.Context, userID string, noteIDs []string) error {
	if len(noteIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now()
	for _, noteID := range noteIDs {
		if !validNoteID(noteID) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_favorites(user_id, note_id, created_at)
			VALUES($1, $2, $3) ON CONFLICT (user_id, note_id) DO NOTHING`, userID, noteID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}
