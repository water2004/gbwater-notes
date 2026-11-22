package app

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// StickyNote 是笔记页上的便条卡片：一篇笔记可以有多张，可单独改颜色与内容。
type StickyNote struct {
	ID        string    `json:"id"`
	NoteID    string    `json:"noteId"`
	Body      string    `json:"body"`
	Quote     string    `json:"quote"`
	Prefix    string    `json:"prefix"`
	Suffix    string    `json:"suffix"`
	Heading   string    `json:"heading"`
	Revision  string    `json:"revision"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type StickyPatch struct {
	Body  *string
	Color *string
}

const stickyColumns = `id, note_id, body, color, quote, prefix, suffix, heading, revision, created_at, updated_at`

func (a *Annotations) StickyNotes(ctx context.Context, userID, noteID string) ([]StickyNote, error) {
	if !validNoteID(noteID) {
		return nil, ErrInvalidNoteID
	}
	rows, err := a.db.QueryContext(ctx, `SELECT `+stickyColumns+` FROM user_sticky_notes
		WHERE user_id = $1 AND note_id = $2 ORDER BY created_at`, userID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []StickyNote{}
	for rows.Next() {
		var item StickyNote
		if err := rows.Scan(&item.ID, &item.NoteID, &item.Body, &item.Color, &item.Quote, &item.Prefix, &item.Suffix, &item.Heading, &item.Revision, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *Annotations) CreateSticky(ctx context.Context, userID string, item StickyNote) (StickyNote, error) {
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	_, err := a.db.ExecContext(ctx, `INSERT INTO user_sticky_notes(id, user_id, note_id, body, color, quote, prefix, suffix, heading, revision, created_at, updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		item.ID, userID, item.NoteID, item.Body, item.Color, item.Quote, item.Prefix, item.Suffix, item.Heading, item.Revision, item.CreatedAt, item.UpdatedAt)
	return item, err
}

func (a *Annotations) Sticky(ctx context.Context, userID, id string) (*StickyNote, error) {
	var item StickyNote
	err := a.db.QueryRowContext(ctx, `SELECT `+stickyColumns+` FROM user_sticky_notes WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&item.ID, &item.NoteID, &item.Body, &item.Color, &item.Quote, &item.Prefix, &item.Suffix, &item.Heading, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *Annotations) UpdateSticky(ctx context.Context, userID, id string, patch StickyPatch) (*StickyNote, error) {
	item, err := a.Sticky(ctx, userID, id)
	if err != nil || item == nil {
		return nil, err
	}
	if patch.Body != nil {
		item.Body = *patch.Body
	}
	if patch.Color != nil {
		item.Color = *patch.Color
	}
	item.UpdatedAt = time.Now()
	_, err = a.db.ExecContext(ctx, `UPDATE user_sticky_notes SET body=$2, color=$3, updated_at=$4 WHERE id=$1 AND user_id=$5`,
		item.ID, item.Body, item.Color, item.UpdatedAt, userID)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (a *Annotations) DeleteSticky(ctx context.Context, userID, id string) (bool, error) {
	result, err := a.db.ExecContext(ctx, `DELETE FROM user_sticky_notes WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}
