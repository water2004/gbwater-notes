package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	HighlightYellow = "yellow"
	HighlightGreen  = "green"
	HighlightBlue   = "blue"
	HighlightPink   = "pink"
)

// Highlight 用「原文引用 + 前后文 + 所在标题」保存锚点。
// 客户端从当前正文计算显示位置。
type Highlight struct {
	ID        string    `json:"id"`
	NoteID    string    `json:"noteId"`
	Quote     string    `json:"quote"`
	Prefix    string    `json:"prefix"`
	Suffix    string    `json:"suffix"`
	Heading   string    `json:"heading"`
	Color     string    `json:"color"`
	Comment   string    `json:"comment"`
	Revision  string    `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Bookmark struct {
	ID        string    `json:"id"`
	NoteID    string    `json:"noteId"`
	Label     string    `json:"label"`
	Quote     string    `json:"quote"`
	Prefix    string    `json:"prefix"`
	Suffix    string    `json:"suffix"`
	Heading   string    `json:"heading"`
	Position  float64   `json:"position"`
	Revision  string    `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Memo struct {
	NoteID    string    `json:"noteId"`
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type BookmarkPatch struct {
	Label    *string
	Revision *string
	Quote    *string
	Prefix   *string
	Suffix   *string
	Heading  *string
	Position *float64
}

type HighlightWithNote struct {
	Highlight
	NoteTitle string `json:"noteTitle"`
	Course    string `json:"course"`
}

type BookmarkWithNote struct {
	Bookmark
	NoteTitle string `json:"noteTitle"`
	Course    string `json:"course"`
}

type AnnotationSummary struct {
	HighlightCount   int                 `json:"highlightCount"`
	BookmarkCount    int                 `json:"bookmarkCount"`
	MemoCount        int                 `json:"memoCount"`
	StickyCount      int                 `json:"stickyCount"`
	RecentHighlights []HighlightWithNote `json:"recentHighlights"`
	RecentBookmarks  []BookmarkWithNote  `json:"recentBookmarks"`
}

type Annotations struct{ db *sql.DB }

func NewAnnotations(db *sql.DB) *Annotations { return &Annotations{db: db} }

const highlightColumns = `id, note_id, quote, prefix, suffix, heading, color, comment, revision, created_at, updated_at`
const bookmarkColumns = `id, note_id, label, quote, prefix, suffix, heading, position, revision, created_at, updated_at`

func (a *Annotations) Highlights(ctx context.Context, userID, noteID string) ([]Highlight, error) {
	if !validNoteID(noteID) {
		return nil, ErrInvalidNoteID
	}
	rows, err := a.db.QueryContext(ctx, `SELECT `+highlightColumns+` FROM user_highlights
		WHERE user_id = $1 AND note_id = $2 ORDER BY created_at`, userID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Highlight{}
	for rows.Next() {
		var item Highlight
		if err := rows.Scan(&item.ID, &item.NoteID, &item.Quote, &item.Prefix, &item.Suffix, &item.Heading,
			&item.Color, &item.Comment, &item.Revision, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *Annotations) CreateHighlight(ctx context.Context, userID string, item Highlight) (Highlight, error) {
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	_, err := a.db.ExecContext(ctx, `INSERT INTO user_highlights(id, user_id, note_id, quote, prefix, suffix, heading,
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

func (a *Annotations) Bookmarks(ctx context.Context, userID, noteID string) ([]Bookmark, error) {
	if !validNoteID(noteID) {
		return nil, ErrInvalidNoteID
	}
	rows, err := a.db.QueryContext(ctx, `SELECT `+bookmarkColumns+` FROM user_bookmarks
		WHERE user_id = $1 AND note_id = $2 ORDER BY created_at DESC`, userID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Bookmark{}
	for rows.Next() {
		var item Bookmark
		if err := rows.Scan(&item.ID, &item.NoteID, &item.Label, &item.Quote, &item.Prefix, &item.Suffix,
			&item.Heading, &item.Position, &item.Revision, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *Annotations) CreateBookmark(ctx context.Context, userID string, item Bookmark) (Bookmark, error) {
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	_, err := a.db.ExecContext(ctx, `INSERT INTO user_bookmarks(id, user_id, note_id, label, quote, prefix, suffix,
		heading, position, revision, created_at, updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		item.ID, userID, item.NoteID, item.Label, item.Quote, item.Prefix, item.Suffix, item.Heading,
		item.Position, item.Revision, item.CreatedAt, item.UpdatedAt)
	return item, err
}

func (a *Annotations) Bookmark(ctx context.Context, userID, id string) (*Bookmark, error) {
	var item Bookmark
	err := a.db.QueryRowContext(ctx, `SELECT `+bookmarkColumns+` FROM user_bookmarks WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&item.ID, &item.NoteID, &item.Label, &item.Quote, &item.Prefix, &item.Suffix,
			&item.Heading, &item.Position, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *Annotations) UpdateBookmark(ctx context.Context, userID, id string, patch BookmarkPatch) (*Bookmark, error) {
	item, err := a.Bookmark(ctx, userID, id)
	if err != nil || item == nil {
		return nil, err
	}
	if patch.Label != nil {
		item.Label = *patch.Label
	}
	if patch.Revision != nil {
		item.Revision = *patch.Revision
	}
	if patch.Quote != nil {
		item.Quote = *patch.Quote
	}
	if patch.Prefix != nil {
		item.Prefix = *patch.Prefix
	}
	if patch.Suffix != nil {
		item.Suffix = *patch.Suffix
	}
	if patch.Heading != nil {
		item.Heading = *patch.Heading
	}
	if patch.Position != nil {
		item.Position = *patch.Position
	}
	item.UpdatedAt = time.Now()
	_, err = a.db.ExecContext(ctx, `UPDATE user_bookmarks SET label=$2, quote=$3, prefix=$4, suffix=$5, heading=$6,
		position=$7, revision=$8, updated_at=$9 WHERE id = $1 AND user_id = $10`,
		item.ID, item.Label, item.Quote, item.Prefix, item.Suffix, item.Heading, item.Position,
		item.Revision, item.UpdatedAt, userID)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (a *Annotations) DeleteBookmark(ctx context.Context, userID, id string) (bool, error) {
	result, err := a.db.ExecContext(ctx, `DELETE FROM user_bookmarks WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (a *Annotations) Memo(ctx context.Context, userID, noteID string) (*Memo, error) {
	if !validNoteID(noteID) {
		return nil, ErrInvalidNoteID
	}
	var item Memo
	err := a.db.QueryRowContext(ctx, `SELECT note_id, body, updated_at FROM user_memos WHERE user_id = $1 AND note_id = $2`, userID, noteID).
		Scan(&item.NoteID, &item.Body, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *Annotations) SaveMemo(ctx context.Context, userID, noteID, body string) (Memo, error) {
	if !validNoteID(noteID) {
		return Memo{}, ErrInvalidNoteID
	}
	item := Memo{NoteID: noteID, Body: body, UpdatedAt: time.Now()}
	_, err := a.db.ExecContext(ctx, `INSERT INTO user_memos(user_id, note_id, body, updated_at)
		VALUES($1,$2,$3,$4) ON CONFLICT (user_id, note_id) DO UPDATE
		SET body = EXCLUDED.body, updated_at = EXCLUDED.updated_at`, userID, noteID, body, item.UpdatedAt)
	return item, err
}

func (a *Annotations) Summary(ctx context.Context, userID string) (AnnotationSummary, error) {
	summary := AnnotationSummary{RecentHighlights: []HighlightWithNote{}, RecentBookmarks: []BookmarkWithNote{}}
	if err := a.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM user_highlights WHERE user_id = $1),
		(SELECT COUNT(*) FROM user_bookmarks WHERE user_id = $1),
		(SELECT COUNT(*) FROM user_memos WHERE user_id = $1 AND body <> ''),
		(SELECT COUNT(*) FROM user_sticky_notes WHERE user_id = $1)`, userID).
		Scan(&summary.HighlightCount, &summary.BookmarkCount, &summary.MemoCount, &summary.StickyCount); err != nil {
		return summary, err
	}
	highlightRows, err := a.db.QueryContext(ctx, `SELECT h.`+strings.Join(strings.Split(highlightColumns, ", "), ", h.")+`,
		COALESCE(n.title, h.note_id), COALESCE(n.course, '')
		FROM user_highlights h LEFT JOIN notes n ON n.id = h.note_id
		WHERE h.user_id = $1 ORDER BY h.updated_at DESC LIMIT 10`, userID)
	if err != nil {
		return summary, err
	}
	defer highlightRows.Close()
	for highlightRows.Next() {
		var item HighlightWithNote
		if err := highlightRows.Scan(&item.ID, &item.NoteID, &item.Quote, &item.Prefix, &item.Suffix,
			&item.Heading, &item.Color, &item.Comment, &item.Revision,
			&item.CreatedAt, &item.UpdatedAt, &item.NoteTitle, &item.Course); err != nil {
			return summary, err
		}
		summary.RecentHighlights = append(summary.RecentHighlights, item)
	}
	bookmarkRows, err := a.db.QueryContext(ctx, `SELECT b.`+strings.Join(strings.Split(bookmarkColumns, ", "), ", b.")+`,
		COALESCE(n.title, b.note_id), COALESCE(n.course, '')
		FROM user_bookmarks b LEFT JOIN notes n ON n.id = b.note_id
		WHERE b.user_id = $1 ORDER BY b.updated_at DESC LIMIT 10`, userID)
	if err != nil {
		return summary, err
	}
	defer bookmarkRows.Close()
	for bookmarkRows.Next() {
		var item BookmarkWithNote
		if err := bookmarkRows.Scan(&item.ID, &item.NoteID, &item.Label, &item.Quote, &item.Prefix,
			&item.Suffix, &item.Heading, &item.Position, &item.Revision,
			&item.CreatedAt, &item.UpdatedAt, &item.NoteTitle, &item.Course); err != nil {
			return summary, err
		}
		summary.RecentBookmarks = append(summary.RecentBookmarks, item)
	}
	return summary, nil
}
