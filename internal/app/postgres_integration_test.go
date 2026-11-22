//go:build integration

package app

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPostgreSQLHistoryRoundTrip(t *testing.T) {
	history, err := OpenHistory(freshDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	document := Document{Note: Note{
		ID: "physics/量子力学/狄拉克符号", Slug: "dirac-notation", CourseSlug: "quantum-mechanics",
		Title: "狄拉克符号", Category: "physics", CategoryTitle: "物理", Course: "量子力学",
		Revision: "rev1", Modified: time.Now(),
	}, Content: "---\ntitle: test\n---\n\n内积与算符。"}
	if err := history.RecordRevision(document, "create", nil); err != nil {
		t.Fatal(err)
	}
	if err := history.RecordBuild(BuildStatus{State: "success", StartedAt: time.Now(), FinishedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	revisions, err := history.RecentRevisions(t.Context(), document.Note.ID, 10)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("revisions: %+v, %v", revisions, err)
	}
	builds, err := history.RecentBuilds(t.Context(), 10)
	if err != nil || len(builds) != 1 || builds[0].State != "success" {
		t.Fatalf("builds: %+v, %v", builds, err)
	}
}

// TestPostgreSQLSyncsContent 在全新的数据库上跑一次内容同步，
// 验证 schema 与 notes 索引足以从 content/ 重建全部公开笔记。
func TestPostgreSQLSyncsContent(t *testing.T) {
	config := integrationConfig(t, freshDatabase(t))
	config.Root = t.TempDir()
	store := NewContentStore(filepath.Join(config.Root, "content"))
	for _, title := range []string{"首篇", "次篇"} {
		if _, err := store.Create(Note{Title: title, Category: "math", Course: "演示课程"}, "一段测试正文。"); err != nil {
			t.Fatal(err)
		}
	}
	config.MeiliURL = "http://127.0.0.1:1" // 让搜索同步快速失败，测试只关心 PostgreSQL
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	catalog, err := server.store.List()
	if err != nil || len(catalog) != 2 {
		t.Fatalf("list content: %d notes, %v", len(catalog), err)
	}
	server.SyncContent()

	var notes, slugs int
	if err := server.db.QueryRow(`SELECT count(*), count(DISTINCT slug) FROM notes`).Scan(&notes, &slugs); err != nil {
		t.Fatal(err)
	}
	if notes != len(catalog) || slugs != len(catalog) {
		t.Fatalf("expected %d notes with unique slugs, got %d and %d", len(catalog), notes, slugs)
	}
	var userTables int
	if err := server.db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name IN
		('users','user_sessions','email_codes','user_note_progress','user_favorites')`).Scan(&userTables); err != nil {
		t.Fatal(err)
	}
	if userTables != 5 {
		t.Fatalf("user tables missing: %d/5", userTables)
	}
}
