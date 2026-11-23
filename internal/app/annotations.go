package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gbwater/internal/anchor"
)

const (
	HighlightYellow = "yellow"
	HighlightGreen  = "green"
	HighlightBlue   = "blue"
	HighlightPink   = "pink"
)

// Highlight 保留原始锚点，并可带上请求版本的显示位置。
type Highlight struct {
	ID        string        `json:"id"`
	NoteID    string        `json:"noteId"`
	Quote     string        `json:"quote"`
	Prefix    string        `json:"prefix"`
	Suffix    string        `json:"suffix"`
	Heading   string        `json:"heading"`
	Color     string        `json:"color"`
	Comment   string        `json:"comment"`
	Revision  string        `json:"revision"`
	Location  *anchor.Range `json:"location,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type Bookmark struct {
	ID        string        `json:"id"`
	NoteID    string        `json:"noteId"`
	Label     string        `json:"label"`
	Quote     string        `json:"quote"`
	Prefix    string        `json:"prefix"`
	Suffix    string        `json:"suffix"`
	Heading   string        `json:"heading"`
	Position  float64       `json:"position"`
	Revision  string        `json:"revision"`
	Location  *anchor.Range `json:"location,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type BookmarkPatch struct {
	Label    *string
	Revision string
	Quote    *string
	Prefix   *string
	Suffix   *string
	Heading  *string
	Position *float64
}

type Annotations struct{ db *sql.DB }

func NewAnnotations(db *sql.DB) *Annotations { return &Annotations{db: db} }

const bookmarkColumns = `id, note_id, label, quote, prefix, suffix, heading, position, revision, created_at, updated_at`

func (a *Annotations) CreateHighlight(ctx context.Context, userID string, item Highlight) (Highlight, error) {
	location, err := resolveAnnotationPosition(ctx, a.db, item.NoteID, item.Revision, item.Quote, item.Prefix, item.Suffix)
	if err != nil {
		return Highlight{}, err
	}
	item.Location = location
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	_, err = a.db.ExecContext(ctx, `INSERT INTO user_highlights(id, user_id, note_id, quote, prefix, suffix, heading,
		color, comment, revision, created_at, updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		item.ID, userID, item.NoteID, item.Quote, item.Prefix, item.Suffix, item.Heading,
		item.Color, item.Comment, item.Revision, item.CreatedAt, item.UpdatedAt)
	return item, err
}

func (a *Annotations) DeleteHighlight(ctx context.Context, userID, id string) (bool, error) {
	result, err := a.db.ExecContext(ctx, `DELETE FROM user_highlights WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (a *Annotations) CreateBookmark(ctx context.Context, userID string, item Bookmark) (Bookmark, error) {
	location, err := resolveAnnotationPosition(ctx, a.db, item.NoteID, item.Revision, item.Quote, item.Prefix, item.Suffix)
	if err != nil {
		return Bookmark{}, err
	}
	item.Location = location
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	_, err = a.db.ExecContext(ctx, `INSERT INTO user_bookmarks(id, user_id, note_id, label, quote, prefix, suffix,
		heading, position, revision, created_at, updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		item.ID, userID, item.NoteID, item.Label, item.Quote, item.Prefix, item.Suffix, item.Heading,
		item.Position, item.Revision, item.CreatedAt, item.UpdatedAt)
	return item, err
}

func (a *Annotations) UpdateBookmark(ctx context.Context, userID, id string, patch BookmarkPatch) (*Bookmark, error) {
	if !validArticleRevision(patch.Revision) {
		return nil, errInvalidArticleRevision
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var item Bookmark
	err = tx.QueryRowContext(ctx, `UPDATE user_bookmarks SET
		label=COALESCE($3, label), quote=COALESCE($4, quote), prefix=COALESCE($5, prefix),
		suffix=COALESCE($6, suffix), heading=COALESCE($7, heading), position=COALESCE($8, position),
		revision=$9, updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND user_id=$2
		RETURNING `+bookmarkColumns, id, userID, patch.Label, patch.Quote, patch.Prefix,
		patch.Suffix, patch.Heading, patch.Position, patch.Revision).
		Scan(&item.ID, &item.NoteID, &item.Label, &item.Quote, &item.Prefix, &item.Suffix,
			&item.Heading, &item.Position, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Location, err = resolveAnnotationPosition(ctx, tx, item.NoteID, item.Revision, item.Quote, item.Prefix, item.Suffix)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *Annotations) DeleteBookmark(ctx context.Context, userID, id string) (bool, error) {
	result, err := a.db.ExecContext(ctx, `DELETE FROM user_bookmarks WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}
