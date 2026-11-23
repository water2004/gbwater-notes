package app

import (
	"context"
	"database/sql"
	_ "embed"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql
var schemaSQL string

type History struct{ db *sql.DB }

type BuildRecord struct {
	ID int64 `json:"id"`
	BuildRequest
	State      string    `json:"state"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	Error      string    `json:"error"`
}

type RevisionRecord struct {
	ID        int64     `json:"id"`
	NoteID    string    `json:"noteId"`
	Revision  string    `json:"revision"`
	Title     string    `json:"title"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"createdAt"`
}

func NewHistory(db *sql.DB) *History { return &History{db: db} }

func openSchemaDatabase(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(3)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// OpenHistory 为独立运行的工具与集成测试打开专属连接。
func OpenHistory(databaseURL string) (*History, error) {
	db, err := openSchemaDatabase(databaseURL)
	if err != nil {
		return nil, err
	}
	return NewHistory(db), nil
}

func (h *History) Close() error {
	if h == nil || h.db == nil {
		return nil
	}
	return h.db.Close()
}

type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func publicationNotes(ctx context.Context, tx *sql.Tx) ([]Note, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, slug, course_slug, title, category, category_title, course,
		position, draft, revision FROM notes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notes := []Note{}
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.Slug, &note.CourseSlug, &note.Title, &note.Category,
			&note.CategoryTitle, &note.Course, &note.Order, &note.Draft, &note.Revision); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, rows.Err()
}

func (h *History) searchDocuments(ctx context.Context) ([]SearchDocument, []string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, slug, title, course, category, draft, plain_text,
		left(plain_text, 260) FROM notes`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var documents []SearchDocument
	var drafts []string
	for rows.Next() {
		var document SearchDocument
		var draft bool
		if err := rows.Scan(&document.NoteID, &document.ID, &document.Title, &document.Course,
			&document.Category, &draft, &document.Text, &document.Excerpt); err != nil {
			return nil, nil, err
		}
		if draft {
			drafts = append(drafts, document.ID)
		} else {
			documents = append(documents, document)
		}
	}
	return documents, drafts, rows.Err()
}

func (h *History) indexDocument(db sqlExecer, document Document) error {
	plain := documentPlainText(document.Content)
	excerpt := []rune(plain)
	if len(excerpt) > 220 {
		excerpt = excerpt[:220]
	}
	_, err := db.Exec(`INSERT INTO notes(
		id, slug, course_slug, title, category, category_title, course, position, draft,
		content, plain_text, excerpt, revision, modified_at
	) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	ON CONFLICT(id) DO UPDATE SET
		slug=EXCLUDED.slug, course_slug=EXCLUDED.course_slug, title=EXCLUDED.title,
		category=EXCLUDED.category, category_title=EXCLUDED.category_title, course=EXCLUDED.course,
		position=EXCLUDED.position, draft=EXCLUDED.draft, content=EXCLUDED.content,
		plain_text=EXCLUDED.plain_text, excerpt=EXCLUDED.excerpt, revision=EXCLUDED.revision,
		modified_at=EXCLUDED.modified_at
	WHERE (notes.revision, notes.modified_at) IS DISTINCT FROM (EXCLUDED.revision, EXCLUDED.modified_at)`,
		document.Note.ID, document.Note.Slug, document.Note.CourseSlug, document.Note.Title,
		document.Note.Category, document.Note.CategoryTitle, document.Note.Course, document.Note.Order,
		document.Note.Draft, document.Content, plain, string(excerpt), document.Note.Revision, document.Note.Modified,
	)
	return err
}

func (h *History) enqueueBuild(ctx context.Context, request BuildRequest) (int64, error) {
	var id int64
	err := h.db.QueryRowContext(ctx, `INSERT INTO builds(state, started_at, finished_at, mode, note_id)
		VALUES('queued', now(), now(), $1, $2) RETURNING id`, request.Mode, request.NoteID).Scan(&id)
	return id, err
}

func (h *History) resumeBuildQueue(ctx context.Context) ([]BuildTask, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, mode, note_id FROM builds WHERE state IN ('queued','running') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []BuildTask
	for rows.Next() {
		var task BuildTask
		if err := rows.Scan(&task.ID, &task.Mode, &task.NoteID); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (h *History) updateQueuedBuild(ctx context.Context, status BuildStatus) error {
	_, err := h.db.ExecContext(ctx, `UPDATE builds SET state=$2, started_at=$3, finished_at=$4, log=$5, error=$6 WHERE id=$1`,
		status.ID, status.State, status.StartedAt, status.FinishedAt, status.Log, status.Error)
	return err
}

func (h *History) RecordRevision(document Document, action string, before *Document) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := h.recordRevision(tx, document, action, before); err != nil {
		return err
	}
	return tx.Commit()
}

func (h *History) recordRevision(tx *sql.Tx, document Document, action string, before *Document) error {
	if err := h.indexDocument(tx, document); err != nil {
		return err
	}
	if before != nil {
		_, err := tx.Exec(`INSERT INTO revisions(note_id, revision, title, action, content, created_at)
			SELECT $1, $2, $3, 'baseline', $4, $5 WHERE NOT EXISTS
			(SELECT 1 FROM revisions WHERE note_id = $1 AND revision = $2)`,
			before.Note.ID, before.Note.Revision, before.Note.Title, before.Content, before.Note.Modified)
		if err != nil {
			return err
		}
	}
	_, err := tx.Exec(`INSERT INTO revisions(note_id, revision, title, action, content, created_at) VALUES($1,$2,$3,$4,$5,$6)`, document.Note.ID, document.Note.Revision, document.Note.Title, action, document.Content, time.Now())
	return err
}

func (h *History) RecentBuilds(ctx context.Context, limit int) ([]BuildRecord, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, mode, note_id, state, started_at, finished_at, error FROM builds
		WHERE state IN ('success','failed') ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []BuildRecord{}
	for rows.Next() {
		var record BuildRecord
		if err := rows.Scan(&record.ID, &record.Mode, &record.NoteID, &record.State, &record.StartedAt, &record.FinishedAt, &record.Error); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func (h *History) RecentRevisions(ctx context.Context, noteID string, limit int) ([]RevisionRecord, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, note_id, revision, title, action, created_at FROM revisions
		WHERE note_id = $1 ORDER BY id DESC LIMIT $2`, noteID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RevisionRecord{}
	for rows.Next() {
		var record RevisionRecord
		if err := rows.Scan(&record.ID, &record.NoteID, &record.Revision, &record.Title, &record.Action, &record.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func (h *History) RevisionContent(ctx context.Context, noteID string, id int64) (string, error) {
	var content string
	err := h.db.QueryRowContext(ctx, `SELECT content FROM revisions WHERE id = $1 AND note_id = $2`, id, noteID).Scan(&content)
	return content, err
}
