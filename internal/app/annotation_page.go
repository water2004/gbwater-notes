package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"gbwater/internal/anchor"
)

type AnnotationPage struct {
	Revision    string       `json:"revision"`
	Highlights  []Highlight  `json:"highlights"`
	Bookmarks   []Bookmark   `json:"bookmarks"`
	StickyNotes []StickyNote `json:"stickyNotes"`
}

type AnnotationSummary struct {
	HighlightCount   int               `json:"highlightCount"`
	BookmarkCount    int               `json:"bookmarkCount"`
	StickyCount      int               `json:"stickyCount"`
	RecentHighlights []RecentHighlight `json:"recentHighlights"`
	RecentBookmarks  []RecentBookmark  `json:"recentBookmarks"`
}

type RecentHighlight struct {
	ID        string `json:"id"`
	NoteID    string `json:"noteId"`
	NoteTitle string `json:"noteTitle"`
	Quote     string `json:"quote"`
	Color     string `json:"color"`
}

type RecentBookmark struct {
	ID        string `json:"id"`
	NoteID    string `json:"noteId"`
	NoteTitle string `json:"noteTitle"`
	Label     string `json:"label"`
	Heading   string `json:"heading"`
}

// A single statement reads all three kinds of annotations from one MVCC snapshot.
const annotationPageSQL = `SELECT json_build_object(
	'revision', a.revision,
	'highlights', COALESCE((SELECT json_agg(h ORDER BY h."createdAt", h.id) FROM (
		SELECT id, note_id AS "noteId", quote, prefix, suffix, heading, color, comment, revision,
			created_at AS "createdAt", updated_at AS "updatedAt"
		FROM user_highlights WHERE user_id = $1 AND note_id = a.note_id
	) h), '[]'::json),
	'bookmarks', COALESCE((SELECT json_agg(b ORDER BY b."createdAt" DESC, b.id) FROM (
		SELECT id, note_id AS "noteId", label, quote, prefix, suffix, heading, position, revision,
			created_at AS "createdAt", updated_at AS "updatedAt"
		FROM user_bookmarks WHERE user_id = $1 AND note_id = a.note_id
	) b), '[]'::json),
	'stickyNotes', COALESCE((SELECT json_agg(s ORDER BY s."createdAt", s.id) FROM (
		SELECT id, note_id AS "noteId", body, color, quote, prefix, suffix, heading, revision,
			created_at AS "createdAt", updated_at AS "updatedAt"
		FROM user_sticky_notes WHERE user_id = $1 AND note_id = a.note_id
	) s), '[]'::json))
FROM annotation_articles a WHERE a.note_id = $2 AND a.revision = $3`

func (a *Annotations) Page(ctx context.Context, userID, noteID, revision string) (AnnotationPage, error) {
	var page AnnotationPage
	if !validArticleRevision(revision) {
		return page, errInvalidArticleRevision
	}
	var payload []byte
	err := a.db.QueryRowContext(ctx, annotationPageSQL, userID, noteID, revision).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return page, errUnknownArticleRevision
	}
	if err != nil {
		return page, err
	}
	if err := json.Unmarshal(payload, &page); err != nil {
		return page, err
	}

	queries := map[string]anchor.Query{}
	targets := map[string][]**anchor.Range{}
	add := func(quote, prefix, suffix string, destination **anchor.Range) {
		query := anchor.NewQuery(quote, prefix, suffix)
		if query.Empty() {
			return
		}
		key := query.Key()
		queries[key] = query
		targets[key] = append(targets[key], destination)
	}
	for i := range page.Highlights {
		item := &page.Highlights[i]
		add(item.Quote, item.Prefix, item.Suffix, &item.Location)
	}
	for i := range page.Bookmarks {
		item := &page.Bookmarks[i]
		add(item.Quote, item.Prefix, item.Suffix, &item.Location)
	}
	for i := range page.StickyNotes {
		item := &page.StickyNotes[i]
		add(item.Quote, item.Prefix, item.Suffix, &item.Location)
	}
	if len(queries) == 0 {
		return page, nil
	}
	positions, err := resolvePositions(ctx, a.db, noteID, revision, queries)
	if err != nil {
		return page, err
	}
	for key, location := range positions {
		for _, target := range targets[key] {
			*target = location
		}
	}
	return page, nil
}

const annotationSummarySQL = `SELECT json_build_object(
	'highlightCount', (SELECT COUNT(*) FROM user_highlights WHERE user_id = $1),
	'bookmarkCount', (SELECT COUNT(*) FROM user_bookmarks WHERE user_id = $1),
	'stickyCount', (SELECT COUNT(*) FROM user_sticky_notes WHERE user_id = $1),
	'recentHighlights', COALESCE((SELECT json_agg(h.item ORDER BY h.updated_at DESC, h.id) FROM (
		SELECT h.id, h.updated_at, json_build_object('id', h.id, 'noteId', h.note_id,
			'noteTitle', COALESCE(n.title, h.note_id), 'quote', h.quote, 'color', h.color) AS item
		FROM user_highlights h LEFT JOIN notes n ON n.id = h.note_id
		WHERE h.user_id = $1 ORDER BY h.updated_at DESC, h.id LIMIT 10
	) h), '[]'::json),
	'recentBookmarks', COALESCE((SELECT json_agg(b.item ORDER BY b.updated_at DESC, b.id) FROM (
		SELECT b.id, b.updated_at, json_build_object('id', b.id, 'noteId', b.note_id,
			'noteTitle', COALESCE(n.title, b.note_id), 'label', b.label, 'heading', b.heading) AS item
		FROM user_bookmarks b LEFT JOIN notes n ON n.id = b.note_id
		WHERE b.user_id = $1 ORDER BY b.updated_at DESC, b.id LIMIT 10
	) b), '[]'::json))`

func (a *Annotations) Summary(ctx context.Context, userID string) (AnnotationSummary, error) {
	var summary AnnotationSummary
	var payload []byte
	if err := a.db.QueryRowContext(ctx, annotationSummarySQL, userID).Scan(&payload); err != nil {
		return summary, err
	}
	err := json.Unmarshal(payload, &summary)
	return summary, err
}
