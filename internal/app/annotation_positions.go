package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gbwater/internal/anchor"
)

type publishedAnnotationNote struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

var (
	errInvalidArticleRevision = errors.New("正文版本格式无效")
	errUnknownArticleRevision = errors.New("正文版本尚未发布，请刷新页面")
)

func readAnnotationCatalog(directory string) ([]publishedAnnotationNote, error) {
	data, err := os.ReadFile(filepath.Join(directory, "catalog.json"))
	if err != nil {
		return nil, err
	}
	var notes []publishedAnnotationNote
	err = json.Unmarshal(data, &notes)
	return notes, err
}

func readAnnotationArticle(directory, slug string) (anchor.Article, error) {
	var article anchor.Article
	if !validSegment(slug) {
		return article, fmt.Errorf("公开笔记网址标识无效: %q", slug)
	}
	data, err := os.ReadFile(filepath.Join(directory, "note-content", slug+".json"))
	if err == nil {
		err = json.Unmarshal(data, &article)
	}
	return article, err
}

type cachedPosition struct {
	Key      string        `json:"anchor_key"`
	Location *anchor.Range `json:"location"`
}

type annotationDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func savePositions(ctx context.Context, db annotationDB, noteID, revision string, positions []cachedPosition) error {
	if len(positions) == 0 {
		return nil
	}
	data, err := json.Marshal(positions)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO annotation_positions (note_id, revision, anchor_key, location)
		SELECT $1, $2, anchor_key,
			CASE WHEN location IS NOT NULL THEN int4range((location->>'start')::integer, (location->>'end')::integer) END
		FROM jsonb_to_recordset($3::jsonb) AS entry(anchor_key text, location jsonb)
		ON CONFLICT (note_id, revision, anchor_key) DO NOTHING`, noteID, revision, string(data))
	return err
}

func matchPositions(ctx context.Context, text string, queries map[string]anchor.Query) ([]cachedPosition, error) {
	characters := []rune(text)
	positions := make([]cachedPosition, 0, len(queries))
	for key, query := range queries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := cachedPosition{Key: key}
		if position, found := anchor.Match(characters, query); found {
			item.Location = &position
		}
		positions = append(positions, item)
	}
	return positions, ctx.Err()
}

func hasAnnotationArticle(ctx context.Context, db annotationDB, noteID, revision string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM annotation_articles WHERE note_id = $1 AND revision = $2)`,
		noteID, revision).Scan(&exists)
	return exists, err
}

func (a *Annotations) prepareArticle(ctx context.Context, noteID string, article anchor.Article) error {
	if !validArticleRevision(article.Revision) {
		return errInvalidArticleRevision
	}
	known, err := hasAnnotationArticle(ctx, a.db, noteID, article.Revision)
	if err != nil || known {
		return err
	}
	text, err := anchor.RenderedText(article.HTML)
	if err != nil {
		return err
	}
	if anchor.Revision(text) != article.Revision {
		return fmt.Errorf("正文版本与渲染文本不一致: %s", article.Revision)
	}
	rows, err := a.db.QueryContext(ctx, `SELECT quote, prefix, suffix FROM user_highlights WHERE note_id = $1
		UNION SELECT quote, prefix, suffix FROM user_bookmarks WHERE note_id = $1
		UNION SELECT quote, prefix, suffix FROM user_sticky_notes WHERE note_id = $1`, noteID)
	if err != nil {
		return err
	}
	defer rows.Close()
	queries := map[string]anchor.Query{}
	for rows.Next() {
		var quote, prefix, suffix string
		if err := rows.Scan(&quote, &prefix, &suffix); err != nil {
			return err
		}
		query := anchor.NewQuery(quote, prefix, suffix)
		if !query.Empty() {
			queries[query.Key()] = query
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	positions, err := matchPositions(ctx, text, queries)
	if err != nil {
		return err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO annotation_articles (note_id, revision, body_text)
		VALUES ($1, $2, $3) ON CONFLICT (note_id, revision) DO NOTHING`, noteID, article.Revision, text)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM annotation_positions WHERE note_id = $1 AND NOT (anchor_key = ANY($2::text[]))`,
		noteID, anchorKeys(queries)); err != nil {
		return err
	}
	if err := savePositions(ctx, tx, noteID, article.Revision, positions); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) prepareAnnotationPublication(ctx context.Context, directory string, revisions map[string]string) error {
	notes, err := readAnnotationCatalog(directory)
	if err != nil {
		return err
	}
	type version struct {
		NoteID   string `json:"note_id"`
		Revision string `json:"revision"`
	}
	versions := make([]version, 0, len(notes))
	notesByID := make(map[string]publishedAnnotationNote, len(notes))
	for _, note := range notes {
		if err := ctx.Err(); err != nil {
			return err
		}
		revision := revisions[note.Slug]
		if !validArticleRevision(revision) {
			return fmt.Errorf("定位标注 %s: %w", note.ID, errInvalidArticleRevision)
		}
		notesByID[note.ID] = note
		versions = append(versions, version{NoteID: note.ID, Revision: revision})
	}
	if len(versions) == 0 {
		return nil
	}
	payload, err := json.Marshal(versions)
	if err != nil {
		return err
	}
	rows, err := s.annotations.db.QueryContext(ctx, `SELECT source.note_id
		FROM jsonb_to_recordset($1::jsonb) AS source(note_id text, revision text)
		WHERE NOT EXISTS (SELECT 1 FROM annotation_articles saved
			WHERE saved.note_id = source.note_id AND saved.revision = source.revision)`, string(payload))
	if err != nil {
		return err
	}
	defer rows.Close()
	var pending []string
	for rows.Next() {
		var noteID string
		if err := rows.Scan(&noteID); err != nil {
			return err
		}
		pending = append(pending, noteID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, noteID := range pending {
		slug := notesByID[noteID].Slug
		article, err := readAnnotationArticle(directory, slug)
		if err == nil && article.Revision != revisions[slug] {
			err = errInvalidArticleRevision
		}
		if err == nil {
			err = s.annotations.prepareArticle(ctx, noteID, article)
		}
		if err != nil {
			return fmt.Errorf("定位标注 %s: %w", noteID, err)
		}
	}
	return nil
}

func validArticleRevision(value string) bool {
	return len(value) == 32 && strings.Trim(value, "0123456789abcdef") == ""
}

func anchorKeys(queries map[string]anchor.Query) []string {
	keys := make([]string, 0, len(queries))
	for key := range queries {
		keys = append(keys, key)
	}
	return keys
}

func resolvePositions(ctx context.Context, db annotationDB, noteID, revision string, queries map[string]anchor.Query) (map[string]*anchor.Range, error) {
	if !validArticleRevision(revision) {
		return nil, errInvalidArticleRevision
	}
	positions := make(map[string]*anchor.Range, len(queries))
	if len(queries) == 0 {
		known, err := hasAnnotationArticle(ctx, db, noteID, revision)
		if err != nil {
			return nil, err
		}
		if !known {
			return nil, errUnknownArticleRevision
		}
		return positions, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT anchor_key, lower(location), upper(location) FROM annotation_positions
		WHERE note_id = $1 AND revision = $2 AND anchor_key = ANY($3::text[])`, noteID, revision, anchorKeys(queries))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var start, end sql.NullInt32
		if err := rows.Scan(&key, &start, &end); err != nil {
			return nil, err
		}
		var location *anchor.Range
		if start.Valid && end.Valid {
			location = &anchor.Range{Start: int(start.Int32), End: int(end.Int32)}
		}
		positions[key] = location
		delete(queries, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(queries) == 0 {
		return positions, nil
	}
	var text string
	err = db.QueryRowContext(ctx, `SELECT body_text FROM annotation_articles WHERE note_id = $1 AND revision = $2`,
		noteID, revision).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errUnknownArticleRevision
	}
	if err != nil {
		return nil, err
	}
	matched, err := matchPositions(ctx, text, queries)
	if err != nil {
		return nil, err
	}
	if err := savePositions(ctx, db, noteID, revision, matched); err != nil {
		return nil, err
	}
	for _, item := range matched {
		positions[item.Key] = item.Location
	}
	return positions, nil
}

func resolveAnnotationPosition(ctx context.Context, db annotationDB, noteID, revision, quote, prefix, suffix string) (*anchor.Range, error) {
	query := anchor.NewQuery(quote, prefix, suffix)
	queries := map[string]anchor.Query{}
	key := query.Key()
	if !query.Empty() {
		queries[key] = query
	}
	positions, err := resolvePositions(ctx, db, noteID, revision, queries)
	if err != nil {
		return nil, err
	}
	return positions[key], nil
}
