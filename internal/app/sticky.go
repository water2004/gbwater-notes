package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gbwater/internal/anchor"
)

// StickyNote 是笔记页上的便条卡片：一篇笔记可以有多张，可单独改颜色与内容。
type StickyNote struct {
	ID        string        `json:"id"`
	NoteID    string        `json:"noteId"`
	Body      string        `json:"body"`
	Quote     string        `json:"quote"`
	Prefix    string        `json:"prefix"`
	Suffix    string        `json:"suffix"`
	Heading   string        `json:"heading"`
	Revision  string        `json:"revision"`
	Location  *anchor.Range `json:"location,omitempty"`
	Color     string        `json:"color"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type StickyPatch struct {
	Body  *string
	Color *string
}

const stickyColumns = `id, note_id, body, color, quote, prefix, suffix, heading, revision, created_at, updated_at`

func (a *Annotations) CreateSticky(ctx context.Context, userID string, item StickyNote) (StickyNote, error) {
	location, err := resolveAnnotationPosition(ctx, a.db, item.NoteID, item.Revision, item.Quote, item.Prefix, item.Suffix)
	if err != nil {
		return StickyNote{}, err
	}
	item.Location = location
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	_, err = a.db.ExecContext(ctx, `INSERT INTO user_sticky_notes(id, user_id, note_id, body, color, quote, prefix, suffix, heading, revision, created_at, updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		item.ID, userID, item.NoteID, item.Body, item.Color, item.Quote, item.Prefix, item.Suffix, item.Heading, item.Revision, item.CreatedAt, item.UpdatedAt)
	return item, err
}

func (a *Annotations) UpdateSticky(ctx context.Context, userID, id string, patch StickyPatch) (*StickyNote, error) {
	var item StickyNote
	err := a.db.QueryRowContext(ctx, `UPDATE user_sticky_notes SET
		body=COALESCE($3, body), color=COALESCE($4, color), updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND user_id=$2 RETURNING `+stickyColumns, id, userID, patch.Body, patch.Color).
		Scan(&item.ID, &item.NoteID, &item.Body, &item.Color, &item.Quote, &item.Prefix, &item.Suffix,
			&item.Heading, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *Annotations) DeleteSticky(ctx context.Context, userID, id string) (bool, error) {
	result, err := a.db.ExecContext(ctx, `DELETE FROM user_sticky_notes WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}
