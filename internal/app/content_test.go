package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestContentStoreCreateReadAndConflict(t *testing.T) {
	store := NewContentStore(t.TempDir())
	created, err := store.Create(Note{Title: "测试笔记", Category: "math", Course: "数学分析", Order: 1, Draft: true}, "含公式 $x^2$。\n")
	if err != nil {
		t.Fatal(err)
	}
	if !created.Note.Draft || created.Note.Revision == "" {
		t.Fatalf("unexpected note: %+v", created.Note)
	}
	notes, err := store.List()
	if err != nil || len(notes) != 1 {
		t.Fatalf("list: %v, %+v", err, notes)
	}
	updated, err := store.Update(created.Note.ID, created.Content+"\n新增一行。\n", created.Note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(created.Note.ID, "invalid", created.Note.Revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	read, err := store.Read(created.Note.ID)
	if err != nil || read.Content != updated.Content {
		t.Fatalf("read: %v", err)
	}
	if _, err := store.Create(Note{Title: "另一篇", Category: "math", Course: "数学分析", Slug: created.Note.Slug, CourseSlug: created.Note.CourseSlug}, "正文"); err == nil {
		t.Fatal("expected duplicate slug to fail")
	}
}

func TestHistoryWriteFailureKeepsPreviousMarkdown(t *testing.T) {
	store := NewContentStore(t.TempDir())
	original, err := store.Create(Note{Title: "原稿", Category: "math", Course: "课程"}, "原文\n")
	if err != nil {
		t.Fatal(err)
	}
	store.record = func(Document, string, *Document) error { return errors.New("history unavailable") }
	if _, err := store.Update(original.Note.ID, original.Content+"修改内容\n", original.Note.Revision); !errors.Is(err, errRevisionWrite) {
		t.Fatalf("expected revision write error, got %v", err)
	}
	if current, err := store.Read(original.Note.ID); err != nil || current.Content != original.Content {
		t.Fatalf("changed original after failed save: %q, %v", current.Content, err)
	}
}

func TestContentStoreRejectsTraversalAndBadMetadata(t *testing.T) {
	store := NewContentStore(t.TempDir())
	for _, id := range []string{"../secret", "math/../../secret", "math/course/../secret", "CON/course/title", "math/course"} {
		if _, err := store.path(id); err == nil {
			t.Errorf("expected invalid id %q to fail", id)
		}
	}
	note := Note{Title: "坏内容", Category: "math", Course: "课程", Order: 1}
	created, err := store.Create(note, "正文")
	if err != nil {
		t.Fatal(err)
	}
	bad := "---\ntitle: \"其他标题\"\ncategory: \"math\"\ncourse: \"课程\"\n---\n"
	if _, err := store.Update(created.Note.ID, bad, created.Note.Revision); err == nil {
		t.Fatal("expected mismatched metadata to fail")
	}
}

func TestNewSubjectSettingsAndContent(t *testing.T) {
	categories, err := configuredCategories([]byte(`{"categories":{"astronomy":{"name":"天文学"},"math":{"description":"新说明"}}}`))
	if err != nil || categories["math"].Name != "数学" {
		t.Fatalf("categories: %v, %v", categories, err)
	}
	for _, payload := range []string{`{"categories":{"../":{"name":"天文学"}}}`, `{"categories":{"con":{"name":"天文学"}}}`, `{"categories":{"astronomy":{"name":""}}}`} {
		if _, err := configuredCategories([]byte(payload)); err == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
	store := NewContentStore(t.TempDir())
	created, err := store.Create(Note{Title: "恒星", Course: "天体物理", Category: "astronomy", CategoryTitle: categories["astronomy"].Name}, "正文")
	if err != nil {
		t.Fatal(err)
	}
	read, err := store.Read(created.Note.ID)
	if err != nil || read.Note.CategoryTitle != "天文学" {
		t.Fatalf("new subject note: %+v, %v", read.Note, err)
	}
}

func TestAtomicWriteKeepsContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "note.md")
	if err := atomicWrite(path, "你好，Folia。\n"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "你好，Folia。\n" {
		t.Fatalf("content %q, err %v", content, err)
	}
}
