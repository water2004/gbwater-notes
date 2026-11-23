//go:build integration

package app

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminAssetLifecycleAndMarkdownHistory(t *testing.T) {
	config := integrationConfig(t, freshDatabase(t))
	config.Root = t.TempDir()
	config.AdminUser, config.AdminEmail, config.AdminPassword = "admin", "test@example.com", "FixturePassword123"
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	login := requestJSON(t, server, http.MethodPost, "/api/auth/login",
		map[string]string{"identifier": config.AdminUser, "password": config.AdminPassword}, nil)
	cookie := firstCookie(t, login)

	// 迁移来的文件尚无 PostgreSQL 修订记录。
	store := NewContentStore(filepath.Join(config.Root, "content"))
	original, err := store.Create(Note{Title: "阅读笔记", Category: "math", Course: "测试课程"}, "初始正文。\n")
	if err != nil {
		t.Fatal(err)
	}
	noteID := original.Note.ID

	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	uploads := "/api/admin/assets?" + url.Values{"noteId": {noteID}, "name": {"配图.png"}}.Encode()
	upload := func(data []byte, want int) Asset {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, uploads, bytes.NewReader(data))
		req.AddCookie(cookie)
		out := httptest.NewRecorder()
		server.ServeHTTP(out, req)
		if out.Code != want {
			t.Fatalf("upload: %d %s", out.Code, out.Body.String())
		}
		var uploaded struct{ Asset Asset }
		if want == http.StatusCreated {
			decodeInto(t, out, &uploaded)
		}
		return uploaded.Asset
	}
	upload([]byte("<svg></svg>"), http.StatusUnsupportedMediaType)
	asset := upload(pngBytes.Bytes(), http.StatusCreated)
	second := upload(pngBytes.Bytes(), http.StatusCreated)
	if asset.ID == "" || second.ID == asset.ID {
		t.Fatalf("uploads should keep both filenames: %+v %+v", asset, second)
	}
	list := func(id string, trash bool, want int) []Asset {
		t.Helper()
		query := url.Values{"noteId": {id}}
		if trash {
			query.Set("trash", "true")
		}
		response := requestJSON(t, server, http.MethodGet, "/api/admin/assets?"+query.Encode(), nil, cookie)
		if response.Code != want {
			t.Fatalf("list: %d %s", response.Code, response.Body.String())
		}
		var result struct{ Assets []Asset }
		if want == http.StatusOK {
			decodeInto(t, response, &result)
		}
		return result.Assets
	}
	if assets := list(noteID, false, http.StatusOK); len(assets) != 2 {
		t.Fatalf("expected both scoped images: %+v", assets)
	}
	if assets := list("math/测试课程/其他笔记", false, http.StatusOK); len(assets) != 0 {
		t.Fatalf("unexpected assets in sibling note: %+v", assets)
	}
	list("math/../测试课程", false, http.StatusBadRequest)
	assetURL := "/api/admin/asset/content?id=" + url.QueryEscape(asset.ID)
	if response := requestJSON(t, server, http.MethodGet, assetURL, nil, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous asset status: %d", response.Code)
	}
	if response := requestJSON(t, server, http.MethodGet, assetURL, nil, cookie); response.Code != http.StatusOK ||
		response.Header().Get("Content-Type") != "image/png" || !bytes.Equal(response.Body.Bytes(), pngBytes.Bytes()) {
		t.Fatalf("preview status or image bytes: %d", response.Code)
	}

	save := func(md string, expected string) Document {
		t.Helper()
		response := requestJSON(t, server, http.MethodPut, "/api/admin/note", map[string]string{"id": noteID, "content": md, "expectedRevision": expected}, cookie)
		if response.Code != http.StatusOK {
			t.Fatalf("save: %d %s", response.Code, response.Body.String())
		}
		var document Document
		decodeInto(t, response, &document)
		return document
	}
	segments := strings.Split(asset.ID, "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	current := save(original.Content+"\n![配图](/mypdf/content-assets/"+strings.Join(segments, "/")+")\n", original.Note.Revision)
	save(current.Content, current.Note.Revision)
	move := func(asset Asset, restore bool, want int) {
		t.Helper()
		response := requestJSON(t, server, http.MethodPatch, "/api/admin/assets", map[string]any{"id": asset.ID, "restore": restore}, cookie)
		if response.Code != want {
			t.Fatalf("move: %d %s", response.Code, response.Body.String())
		}
	}
	move(asset, false, http.StatusConflict)
	references := requestJSON(t, server, http.MethodGet, "/api/admin/asset/references?id="+url.QueryEscape(asset.ID), nil, cookie)
	if references.Code != http.StatusOK {
		t.Fatalf("asset references: %d %s", references.Code, references.Body.String())
	}
	var usage struct{ References []AssetReference }
	decodeInto(t, references, &usage)
	if len(usage.References) != 1 || usage.References[0].ID != noteID {
		t.Fatalf("unexpected asset references: %+v", usage.References)
	}
	move(second, false, http.StatusOK)
	registeredNotes, registeredAssets, err := server.publicationInputs(t.Context())
	if err != nil || len(registeredAssets) != 1 || registeredAssets[0] != asset.ID ||
		len(registeredNotes) != 1 || registeredNotes[0].Revision != current.Note.Revision {
		t.Fatalf("publication snapshot after API saves: %v, %v, %v", registeredNotes, registeredAssets, err)
	}
	if assets := list(noteID, true, http.StatusOK); len(assets) != 1 || assets[0].ID != second.ID {
		t.Fatalf("unexpected scoped trash: %+v", assets)
	}
	move(second, true, http.StatusOK)
	_, registeredAssets, err = server.publicationInputs(t.Context())
	if err != nil || len(registeredAssets) != 2 {
		t.Fatalf("publication snapshot after restore: %v, %v", registeredAssets, err)
	}

	revisions, err := server.history.RecentRevisions(t.Context(), noteID, 10)
	if err != nil || len(revisions) != 2 || revisions[1].Action != "baseline" || revisions[1].Revision != original.Note.Revision {
		t.Fatalf("Markdown history: %+v, %v", revisions, err)
	}
	restored := save(original.Content, current.Note.Revision)
	move(asset, false, http.StatusOK)
	move(asset, true, http.StatusOK)
	download := requestJSON(t, server, http.MethodGet, "/api/admin/note/download?id="+url.QueryEscape(noteID), nil, cookie)
	if download.Code != http.StatusOK || download.Body.String() != original.Content || download.Header().Get("Content-Disposition") == "" {
		t.Fatalf("download: %d %s", download.Code, download.Body.String())
	}
	response := requestJSON(t, server, http.MethodPatch, "/api/admin/note", map[string]any{"id": noteID, "draft": true, "expectedRevision": restored.Note.Revision}, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("archive: %d %s", response.Code, response.Body.String())
	}
	doc, err := server.store.Read(noteID)
	if err != nil || !doc.Note.Draft || doc.Note.ID != noteID {
		t.Fatalf("archive document: %+v, %v", doc.Note, err)
	}
}
