package app

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgreSQLHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	history := &History{db: db}
	defer history.Close()
	now := time.Now()
	status := BuildStatus{BuildTask: BuildTask{ID: 1}, State: "success", StartedAt: now.Add(-time.Second), FinishedAt: now, Log: "ok"}
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE builds SET state=$2, started_at=$3, finished_at=$4, log=$5, error=$6 WHERE id=$1`)).
		WithArgs(status.ID, status.State, status.StartedAt, status.FinishedAt, status.Log, status.Error).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := history.updateQueuedBuild(t.Context(), status); err != nil {
		t.Fatal(err)
	}
	document := Document{Note: Note{ID: "math/课程/笔记", Title: "笔记", Slug: "note", CourseSlug: "course", Revision: "abc123", Modified: now}, Content: "content"}
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO notes\(`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO revisions(note_id, revision, title, action, content, created_at) VALUES($1,$2,$3,$4,$5,$6)`)).
		WithArgs(document.Note.ID, document.Note.Revision, document.Note.Title, "create", document.Content, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	if err := history.RecordRevision(document, "create", nil); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`SELECT id, mode, note_id, state, started_at, finished_at, error FROM builds WHERE state IN \('success','failed'\) ORDER BY id DESC LIMIT \$1`).
		WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"id", "mode", "note_id", "state", "started_at", "finished_at", "error"}).AddRow(1, BuildNote, document.Note.ID, "success", now, now, ""))
	builds, err := history.RecentBuilds(t.Context(), 10)
	if err != nil || len(builds) != 1 || builds[0].State != "success" || builds[0].NoteID != document.Note.ID {
		t.Fatalf("builds: %+v, %v", builds, err)
	}
	mock.ExpectQuery(`SELECT id, note_id, revision, title, action, created_at FROM revisions WHERE note_id = \$1 ORDER BY id DESC LIMIT \$2`).
		WithArgs(document.Note.ID, 10).WillReturnRows(sqlmock.NewRows([]string{"id", "note_id", "revision", "title", "action", "created_at"}).AddRow(1, document.Note.ID, "abc123", "笔记", "create", now))
	revisions, err := history.RecentRevisions(t.Context(), document.Note.ID, 10)
	if err != nil || len(revisions) != 1 || revisions[0].Revision != "abc123" {
		t.Fatalf("revisions: %+v, %v", revisions, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
