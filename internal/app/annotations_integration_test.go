//go:build integration

package app

import (
	"context"
	"net/http"
	"testing"
)

func registerTestUser(t *testing.T, server *Server, mailer *capturingMailer, username, email, password string) (User, *http.Cookie) {
	t.Helper()
	code := requestJSON(t, server, http.MethodPost, "/api/auth/email-code",
		map[string]string{"email": email, "purpose": EmailPurposeRegister}, nil)
	if code.Code != http.StatusOK {
		t.Fatalf("email-code %s: %d %s", email, code.Code, code.Body.String())
	}
	response := requestJSON(t, server, http.MethodPost, "/api/auth/register",
		map[string]string{"username": username, "email": email, "password": password, "code": mailer.lastCode(t, email)}, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("register %s: %d %s", username, response.Code, response.Body.String())
	}
	var payload struct {
		User User `json:"user"`
	}
	decodeInto(t, response, &payload)
	return payload.User, firstCookie(t, response)
}

func TestAnnotationsLifecycleAndIsolation(t *testing.T) {
	mailer := &capturingMailer{}
	config := integrationConfig(t, freshDatabase(t))
	config.Mailer = mailer
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, ownerCookie := registerTestUser(t, server, mailer, "owner", "owner@example.com", "OwnerPass123")
	_, otherCookie := registerTestUser(t, server, mailer, "other", "other@example.com", "OtherPass123")

	const noteID = "math/线性代数/矩阵"
	created := requestJSON(t, server, http.MethodPost, "/api/me/highlights", map[string]any{
		"noteId": noteID, "quote": "线性方程组", "prefix": "对于", "suffix": "可以",
		"heading": "矩阵", "color": HighlightGreen, "comment": "重点", "revision": "rev-1",
	}, ownerCookie)
	if created.Code != http.StatusCreated {
		t.Fatalf("create highlight: %d %s", created.Code, created.Body.String())
	}
	var highlightPayload struct {
		Highlight Highlight `json:"highlight"`
	}
	decodeInto(t, created, &highlightPayload)
	highlightID := highlightPayload.Highlight.ID
	if highlightPayload.Highlight.Color != HighlightGreen || highlightPayload.Highlight.Revision != "rev-1" {
		t.Fatalf("created highlight: %+v", highlightPayload.Highlight)
	}

	// 其他用户不能读取或删除这些标注。
	foreignList := requestJSON(t, server, http.MethodGet, "/api/me/annotations?noteId="+noteID, nil, otherCookie)
	var otherState struct {
		Highlights []Highlight `json:"highlights"`
	}
	decodeInto(t, foreignList, &otherState)
	if len(otherState.Highlights) != 0 {
		t.Fatalf("other user should not see owner's highlights: %+v", otherState.Highlights)
	}
	foreignDelete := requestJSON(t, server, http.MethodDelete, "/api/me/highlights/"+highlightID, nil, otherCookie)
	if foreignDelete.Code != http.StatusNotFound {
		t.Fatalf("foreign delete: %d", foreignDelete.Code)
	}

	bookmark := requestJSON(t, server, http.MethodPost, "/api/me/bookmarks", map[string]any{
		"noteId": noteID, "label": "看到这里", "quote": "行图像", "position": 0.35, "revision": "rev-2",
	}, ownerCookie)
	if bookmark.Code != http.StatusCreated {
		t.Fatalf("create bookmark: %d %s", bookmark.Code, bookmark.Body.String())
	}
	var bookmarkPayload struct {
		Bookmark Bookmark `json:"bookmark"`
	}
	decodeInto(t, bookmark, &bookmarkPayload)
	moved := requestJSON(t, server, http.MethodPatch, "/api/me/bookmarks/"+bookmarkPayload.Bookmark.ID, map[string]any{
		"quote": "列图像", "position": 0.5, "revision": "rev-3",
	}, ownerCookie)
	if moved.Code != http.StatusOK {
		t.Fatalf("move bookmark: %d %s", moved.Code, moved.Body.String())
	}

	memo := requestJSON(t, server, http.MethodPut, "/api/me/memo", map[string]string{
		"noteId": noteID, "body": "复习时先看秩的定义",
	}, ownerCookie)
	if memo.Code != http.StatusOK {
		t.Fatalf("save memo: %d %s", memo.Code, memo.Body.String())
	}

	state := requestJSON(t, server, http.MethodGet, "/api/me/annotations?noteId="+noteID, nil, ownerCookie)
	var payload struct {
		Highlights []Highlight `json:"highlights"`
		Bookmarks  []Bookmark  `json:"bookmarks"`
		Memo       *Memo       `json:"memo"`
	}
	decodeInto(t, state, &payload)
	if len(payload.Highlights) != 1 || payload.Highlights[0].Quote != "线性方程组" || payload.Highlights[0].Revision != "rev-1" {
		t.Fatalf("highlights: %+v", payload.Highlights)
	}
	if len(payload.Bookmarks) != 1 || payload.Bookmarks[0].Position != 0.5 || payload.Bookmarks[0].Revision != "rev-3" {
		t.Fatalf("bookmarks: %+v", payload.Bookmarks)
	}
	if payload.Memo == nil || payload.Memo.Body == "" {
		t.Fatalf("memo: %+v", payload.Memo)
	}

	summary := requestJSON(t, server, http.MethodGet, "/api/me/annotations/summary", nil, ownerCookie)
	var summaryPayload AnnotationSummary
	decodeInto(t, summary, &summaryPayload)
	if summaryPayload.HighlightCount != 1 || summaryPayload.BookmarkCount != 1 || summaryPayload.MemoCount != 1 {
		t.Fatalf("summary: %+v", summaryPayload)
	}
	if len(summaryPayload.RecentHighlights) != 1 || summaryPayload.RecentHighlights[0].NoteTitle == "" {
		t.Fatalf("recent highlights: %+v", summaryPayload.RecentHighlights)
	}

	deleted := requestJSON(t, server, http.MethodDelete, "/api/me/highlights/"+highlightID, nil, ownerCookie)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete highlight: %d", deleted.Code)
	}
}

func TestSearchRequiresLoginAndRateLimit(t *testing.T) {
	config := integrationConfig(t, freshDatabase(t))
	config.MeiliURL = "http://127.0.0.1:1"
	config.AdminPassword = "AdminPass123"
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}

	anonymous := requestJSON(t, server, http.MethodGet, "/api/search?q=矩阵", nil, nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous search: %d", anonymous.Code)
	}

	login := requestJSON(t, server, http.MethodPost, "/api/auth/login",
		map[string]string{"identifier": "admin", "password": "AdminPass123"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	cookie := firstCookie(t, login)
	for attempt := 0; attempt < 30; attempt++ {
		response := requestJSON(t, server, http.MethodGet, "/api/search?q=矩阵", nil, cookie)
		if response.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d should still be allowed", attempt+1)
		}
	}
	limited := requestJSON(t, server, http.MethodGet, "/api/search?q=矩阵", nil, cookie)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("31st search should be rate limited, got %d", limited.Code)
	}
	if limited.Header().Get("Retry-After") == "" {
		t.Fatal("rate limited response must include Retry-After")
	}
}
func TestStickyNotesAndUserSettings(t *testing.T) {
	mailer := &capturingMailer{}
	config := integrationConfig(t, freshDatabase(t))
	config.Mailer = mailer
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, ownerCookie := registerTestUser(t, server, mailer, "owner", "owner@example.com", "OwnerPass123")
	_, otherCookie := registerTestUser(t, server, mailer, "other", "other@example.com", "OtherPass123")

	const noteID = "math/线性代数/矩阵"
	created := requestJSON(t, server, http.MethodPost, "/api/me/sticky-notes", map[string]any{
		"noteId": noteID, "body": "这里要复习", "color": HighlightYellow,
		"quote": "线性方程组", "prefix": "对于", "suffix": "可以",
		"heading": "矩阵", "revision": "rev-1",
	}, ownerCookie)
	if created.Code != http.StatusCreated {
		t.Fatalf("create sticky: %d %s", created.Code, created.Body.String())
	}
	var stickyPayload struct {
		StickyNote StickyNote `json:"stickyNote"`
	}
	decodeInto(t, created, &stickyPayload)
	stickyID := stickyPayload.StickyNote.ID
	if stickyPayload.StickyNote.Quote != "线性方程组" || stickyPayload.StickyNote.Color != HighlightYellow {
		t.Fatalf("sticky anchor missing: %+v", stickyPayload.StickyNote)
	}

	state := requestJSON(t, server, http.MethodGet, "/api/me/annotations?noteId="+noteID, nil, ownerCookie)
	var annotations struct {
		StickyNotes []StickyNote `json:"stickyNotes"`
	}
	decodeInto(t, state, &annotations)
	if len(annotations.StickyNotes) != 1 || annotations.StickyNotes[0].ID != stickyID {
		t.Fatalf("sticky list: %+v", annotations.StickyNotes)
	}

	foreign := requestJSON(t, server, http.MethodPatch, "/api/me/sticky-notes/"+stickyID,
		map[string]string{"color": HighlightBlue}, otherCookie)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign sticky patch: %d", foreign.Code)
	}
	recolored := requestJSON(t, server, http.MethodPatch, "/api/me/sticky-notes/"+stickyID,
		map[string]string{"color": HighlightBlue}, ownerCookie)
	if recolored.Code != http.StatusOK {
		t.Fatalf("recolor sticky: %d", recolored.Code)
	}

	summary := requestJSON(t, server, http.MethodGet, "/api/me/annotations/summary", nil, ownerCookie)
	var summaryPayload AnnotationSummary
	decodeInto(t, summary, &summaryPayload)
	if summaryPayload.StickyCount != 1 {
		t.Fatalf("sticky count: %+v", summaryPayload)
	}

	// 阅读设置按用户隔离并可往返保存。
	saved := requestJSON(t, server, http.MethodPut, "/api/me/settings", map[string]any{
		"reading": map[string]any{"fontScale": 1.1, "width": 900, "lineHeight": 2.3, "font": "sans"},
	}, ownerCookie)
	if saved.Code != http.StatusOK {
		t.Fatalf("save settings: %d %s", saved.Code, saved.Body.String())
	}
	read := requestJSON(t, server, http.MethodGet, "/api/me/settings", nil, ownerCookie)
	var readPayload struct {
		Reading struct {
			FontScale float64 `json:"fontScale"`
			Width     int     `json:"width"`
			Font      string  `json:"font"`
		} `json:"reading"`
	}
	decodeInto(t, read, &readPayload)
	if readPayload.Reading.Width != 900 || readPayload.Reading.Font != "sans" {
		t.Fatalf("settings roundtrip: %+v", readPayload)
	}
	other := requestJSON(t, server, http.MethodGet, "/api/me/settings", nil, otherCookie)
	var otherPayload struct {
		Reading map[string]any `json:"reading"`
	}
	decodeInto(t, other, &otherPayload)
	if len(otherPayload.Reading) != 0 {
		t.Fatalf("settings should be per user: %+v", otherPayload.Reading)
	}

	deleted := requestJSON(t, server, http.MethodDelete, "/api/me/sticky-notes/"+stickyID, nil, ownerCookie)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete sticky: %d", deleted.Code)
	}
}
