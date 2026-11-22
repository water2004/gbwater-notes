//go:build integration

package app

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func TestAdminNoteRevisionAPI(t *testing.T) {
	config := integrationConfig(t, freshDatabase(t))
	config.Root = t.TempDir()
	config.AdminUser = "admin"
	config.AdminEmail = "admin@example.com"
	config.AdminPassword = "AdminPassword123"
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	login := requestJSON(t, server, http.MethodPost, "/api/auth/login",
		map[string]string{"identifier": config.AdminUser, "password": config.AdminPassword}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	cookie := firstCookie(t, login)

	create := func(title, body string) Document {
		t.Helper()
		response := requestJSON(t, server, http.MethodPost, "/api/admin/notes", map[string]any{
			"note": Note{Title: title, Category: "math", Course: "课程 + 例题", Draft: true},
			"body": body,
		}, cookie)
		if response.Code != http.StatusCreated {
			t.Fatalf("create note: %d %s", response.Code, response.Body.String())
		}
		var document Document
		decodeInto(t, response, &document)
		return document
	}
	first := create("第一篇", "第一版正文")
	second := create("第二篇", "另一篇正文")
	content := first.Content + "\n第二版正文\n"
	update := requestJSON(t, server, http.MethodPut, "/api/admin/note", map[string]string{
		"id": first.Note.ID, "content": content, "expectedRevision": first.Note.Revision,
	}, cookie)
	if update.Code != http.StatusOK {
		t.Fatalf("update note: %d %s", update.Code, update.Body.String())
	}
	var updated Document
	decodeInto(t, update, &updated)
	if updated.Note.Revision == first.Note.Revision {
		t.Fatal("editing the note did not change its revision")
	}

	query := "?noteId=" + url.QueryEscape(first.Note.ID)
	list := requestJSON(t, server, http.MethodGet, "/api/admin/revisions"+query, nil, cookie)
	if list.Code != http.StatusOK {
		t.Fatalf("list revisions: %d %s", list.Code, list.Body.String())
	}
	var history struct {
		Revisions []RevisionRecord `json:"revisions"`
	}
	decodeInto(t, list, &history)
	records := history.Revisions
	if len(records) != 2 || records[0].NoteID != first.Note.ID || records[1].NoteID != first.Note.ID ||
		records[0].Revision != updated.Note.Revision || records[1].Revision != first.Note.Revision {
		t.Fatalf("unexpected revisions: %+v", records)
	}

	snapshotURL := "/api/admin/revisions/" + strconv.FormatInt(records[1].ID, 10)
	snapshot := requestJSON(t, server, http.MethodGet, snapshotURL+query, nil, cookie)
	if snapshot.Code != http.StatusOK {
		t.Fatalf("get snapshot: %d %s", snapshot.Code, snapshot.Body.String())
	}
	var old struct {
		Content string `json:"content"`
	}
	decodeInto(t, snapshot, &old)
	if old.Content != first.Content {
		t.Fatalf("snapshot content: %q, want %q", old.Content, first.Content)
	}

	wrongNote := requestJSON(t, server, http.MethodGet, snapshotURL+"?noteId="+url.QueryEscape(second.Note.ID), nil, cookie)
	if wrongNote.Code != http.StatusNotFound {
		t.Fatalf("snapshot from different note: %d %s", wrongNote.Code, wrongNote.Body.String())
	}
	unscoped := requestJSON(t, server, http.MethodGet, "/api/admin/revisions", nil, cookie)
	if unscoped.Code != http.StatusBadRequest {
		t.Fatalf("unscoped list: %d %s", unscoped.Code, unscoped.Body.String())
	}
}
