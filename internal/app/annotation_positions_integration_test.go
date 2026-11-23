//go:build integration

package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gbwater/internal/anchor"
)

func registerRenderedArticle(t *testing.T, server *Server, noteID, html string) string {
	t.Helper()
	text, err := anchor.RenderedText(html)
	if err != nil {
		t.Fatal(err)
	}
	revision := anchor.Revision(text)
	if err := server.annotations.prepareArticle(context.Background(), noteID, anchor.Article{HTML: html, Revision: revision}); err != nil {
		t.Fatal(err)
	}
	return revision
}

func TestAnnotationPositionsFollowRenderedRevisions(t *testing.T) {
	config := integrationConfig(t, freshDatabase(t))
	config.Root = t.TempDir()
	mailer := &capturingMailer{}
	config.Mailer = mailer
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	_, cookie := registerTestUser(t, server, mailer, "reader", "reader@example.com", "ReaderPass123")

	const noteID = "math/课/正文"
	const quote = "甲乙丙丁戊己庚辛壬癸子丑寅卯辰巳"
	const edited = "甲乙丙丁戊己新庚换壬癸子丑卯辰巳"
	writeArticle := func(subdir, html string) string {
		t.Helper()
		text, err := anchor.RenderedText(html)
		if err != nil {
			t.Fatal(err)
		}
		revision := anchor.Revision(text)
		dir := filepath.Join(config.Root, "web", subdir)
		if err := os.MkdirAll(filepath.Join(dir, "note-content"), 0o755); err != nil {
			t.Fatal(err)
		}
		catalog, _ := json.Marshal([]publishedAnnotationNote{{ID: noteID, Slug: "rendered"}})
		if err := os.WriteFile(filepath.Join(dir, "catalog.json"), catalog, 0o644); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(anchor.Article{HTML: html, Revision: revision})
		if err := os.WriteFile(filepath.Join(dir, "note-content", "rendered.json"), body, 0o644); err != nil {
			t.Fatal(err)
		}
		return revision
	}
	unregistered := writeArticle("dist", "<p>旧静态目录中的正文</p>")
	oldRevision := writeArticle("public", "<p>前文"+quote+"后文</p>")
	if err := server.prepareAnnotationPublication(context.Background(), map[string]string{"rendered": oldRevision}); err != nil {
		t.Fatal(err)
	}
	for revision, status := range map[string]int{"rev-1": http.StatusBadRequest, unregistered: http.StatusConflict} {
		query := url.Values{"noteId": {noteID}, "revision": {revision}}
		response := requestJSON(t, server, http.MethodGet, "/api/me/annotations?"+query.Encode(), nil, cookie)
		if response.Code != status {
			t.Fatalf("invalid reading revision %s: %d %s", revision, response.Code, response.Body.String())
		}
		response = requestJSON(t, server, http.MethodPost, "/api/me/highlights", map[string]string{
			"noteId": noteID, "quote": quote, "revision": revision,
		}, cookie)
		if response.Code != status {
			t.Fatalf("invalid writing revision %s: %d %s", revision, response.Code, response.Body.String())
		}
	}
	for _, route := range []string{"highlights", "sticky-notes"} {
		input := map[string]string{"noteId": noteID, "quote": quote, "prefix": "前文", "suffix": "后文", "revision": oldRevision}
		if route == "sticky-notes" {
			input["body"] = "复习"
		}
		created := requestJSON(t, server, http.MethodPost, "/api/me/"+route, input, cookie)
		if created.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", route, created.Code, created.Body.String())
		}
	}
	const discardedQuote = "已经删除的标注引文"
	if _, err := resolveAnnotationPosition(context.Background(), server.db, noteID, oldRevision, discardedQuote, "", ""); err != nil {
		t.Fatal(err)
	}
	newRevision := writeArticle("public", `<p>🌿新增 前文`+edited+`<img alt="不参与定位">后文</p>`)
	if err := server.prepareAnnotationPublication(context.Background(), map[string]string{"rendered": newRevision}); err != nil {
		t.Fatal(err)
	}
	readLocations := func(revision string) (*anchor.Range, *anchor.Range) {
		t.Helper()
		query := url.Values{"noteId": {noteID}, "revision": {revision}}
		response := requestJSON(t, server, http.MethodGet, "/api/me/annotations?"+query.Encode(), nil, cookie)
		if response.Code != http.StatusOK {
			t.Fatalf("read annotations: %d %s", response.Code, response.Body.String())
		}
		var values AnnotationPage
		decodeInto(t, response, &values)
		if values.Revision != revision || len(values.Highlights) != 1 || len(values.StickyNotes) != 1 ||
			values.Highlights[0].Quote != quote || values.StickyNotes[0].Body != "复习" ||
			values.Highlights[0].Revision != oldRevision || values.StickyNotes[0].Revision != oldRevision {
			t.Fatalf("original annotations: %+v", values)
		}
		return values.Highlights[0].Location, values.StickyNotes[0].Location
	}
	var discardedExists bool
	if err := server.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM annotation_positions WHERE note_id=$1 AND revision=$2 AND anchor_key=$3)`,
		noteID, oldRevision, anchor.NewQuery(discardedQuote, "", "").Key()).Scan(&discardedExists); err != nil || discardedExists {
		t.Fatalf("discarded location survived publication: %t, %v", discardedExists, err)
	}
	for revision, want := range map[string]anchor.Range{
		oldRevision: {Start: 2, End: 18},
		newRevision: {Start: 5, End: 21},
	} {
		highlight, sticky := readLocations(revision)
		if highlight == nil || sticky == nil || *highlight != want || *sticky != want {
			t.Fatalf("positions for %s: %+v %+v", revision, highlight, sticky)
		}
	}
	missingRevision := writeArticle("public", "<p>引用段落已撤下。</p>")
	if err := server.prepareAnnotationPublication(context.Background(), map[string]string{"rendered": missingRevision}); err != nil {
		t.Fatal(err)
	}
	if highlight, sticky := readLocations(missingRevision); highlight != nil || sticky != nil {
		t.Fatalf("removed quote located: %+v %+v", highlight, sticky)
	}
	var unmatched bool
	if err := server.db.QueryRow(`SELECT location IS NULL FROM annotation_positions WHERE note_id=$1 AND revision=$2 AND anchor_key=$3`,
		noteID, missingRevision, anchor.NewQuery(quote, "前文", "后文").Key()).Scan(&unmatched); err != nil || !unmatched {
		t.Fatalf("unmatched result was not cached: %t, %v", unmatched, err)
	}
}

func TestConcurrentStickyPatchesPreserveEachField(t *testing.T) {
	config := integrationConfig(t, freshDatabase(t))
	mailer := &capturingMailer{}
	config.Mailer = mailer
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	owner, _ := registerTestUser(t, server, mailer, "reader", "reader@example.com", "ReaderPass123")
	const noteID = "math/课/正文"
	revision := registerRenderedArticle(t, server, noteID, "<p>正文</p>")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sticky, err := server.annotations.CreateSticky(ctx, owner.ID, StickyNote{
		ID: "concurrent-sticky", NoteID: noteID, Body: "旧内容", Color: HighlightYellow, Revision: revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := server.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	var lockedID string
	if err := lock.QueryRowContext(ctx, `SELECT id FROM user_sticky_notes WHERE id=$1 FOR UPDATE`, sticky.ID).Scan(&lockedID); err != nil {
		t.Fatal(err)
	}
	body, color := "更新正文", HighlightBlue
	results := make(chan error, 2)
	for _, patch := range []StickyPatch{{Body: &body}, {Color: &color}} {
		go func() {
			item, err := server.annotations.UpdateSticky(ctx, owner.ID, sticky.ID, patch)
			if err == nil && item == nil {
				err = errors.New("sticky note disappeared")
			}
			results <- err
		}()
	}
	for {
		var blocked int
		if err := server.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname=current_database() AND query LIKE 'UPDATE user_sticky_notes SET%' AND wait_event_type='Lock'`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both updates did not reach the row lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := lock.Commit(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	page, err := server.annotations.Page(ctx, owner.ID, noteID, revision)
	if err != nil || len(page.StickyNotes) != 1 || page.StickyNotes[0].Body != body || page.StickyNotes[0].Color != color {
		t.Fatalf("partial update overwritten: %+v, %v", page.StickyNotes, err)
	}
}
