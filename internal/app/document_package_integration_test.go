//go:build integration

package app

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestAdminDocumentPackageImportReplaceAndExport(t *testing.T) {
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
	cookie := firstCookie(t, requestJSON(t, server, http.MethodPost, "/api/auth/login",
		map[string]string{"identifier": config.AdminUser, "password": config.AdminPassword}, nil))
	call := func(query string, raw []byte, want int) packageImportPlan {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/admin/document-packages?"+query, bytes.NewReader(raw))
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("import status %d, want %d: %s", response.Code, want, response.Body.Bytes())
		}
		var plan packageImportPlan
		if want == http.StatusOK {
			decodeInto(t, response, &plan)
		}
		return plan
	}
	encode := func(notes []Document, images map[string][]byte) []byte {
		t.Helper()
		raw, err := encodeDocumentPackage(documentPackage{notes: notes, assets: images,
			categories: map[string]categorySettings{"lab-notes": {Name: "实验记录", Description: "随包迁移"}}})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	note := func(title, slug, extra, body string) Document {
		t.Helper()
		id := "lab-notes/课程/" + title
		source := "---\ntitle: " + title + "\ncategory: lab-notes\ncategoryTitle: 实验记录\ncourse: 课程\nslug: " +
			slug + "\ncourseSlug: lab\n" + extra + "---\n\n" + body
		meta, err := parseMetadata(id, source, nil)
		if err != nil {
			t.Fatal(err)
		}
		return Document{Note: meta, Content: source}
	}
	picture := func(red uint8) []byte {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.SetRGBA(0, 0, color.RGBA{R: red, A: 255})
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}
	imageLink := func(id string) string { return "![图](/content-assets/" + id + ")\n" }
	shared, changed, removed, added := "lab-notes/课程/甲篇/shared.png", "lab-notes/课程/甲篇/changed.png",
		"lab-notes/课程/甲篇/removed.png", "lab-notes/课程/甲篇/added.png"
	first := note("甲篇", "first", "order: 10\n", "原正文\n\n"+imageLink(shared)+imageLink(changed)+imageLink(removed))
	second := note("乙篇", "second", "draft: true\n", "仍使用旧图。\n\n"+imageLink(shared)+imageLink(changed))
	oldBytes, newBytes := picture(30), picture(130)
	initial := encode([]Document{first, second}, map[string][]byte{shared: oldBytes, changed: oldBytes, removed: oldBytes})
	initialPlan := call("preview=true", initial, http.StatusOK)
	if initialPlan.NewNotes != 2 || initialPlan.NewAssets != 3 || !slices.Contains(initialPlan.Categories, "lab-notes") {
		t.Fatalf("initial preview: %+v", initialPlan)
	}
	if notes, assets, err := server.publicationInputs(t.Context()); err != nil || len(notes)+len(assets) != 0 {
		t.Fatalf("preview mutated publication inputs: %v %v %v", notes, assets, err)
	}
	call("digest="+initialPlan.Digest, initial, http.StatusOK)
	if plan := call("preview=true", initial, http.StatusOK); plan.NewNotes != 0 || plan.ReplaceNotes != 0 || plan.ReusedAssets != 3 {
		t.Fatalf("reimport preview: %+v", plan)
	}

	edited := note("甲篇", "first", "order: 24\ndraft: true\n", "新正文\n\n"+imageLink(changed)+imageLink(added))
	replacement := encode([]Document{edited}, map[string][]byte{changed: newBytes, added: newBytes})
	plan := call("preview=true", replacement, http.StatusOK)
	if plan.ReplaceNotes != 1 || plan.NewAssets != 2 || len(plan.RemappedAssets) != 1 ||
		!slices.Equal(plan.RecycledAssets, []string{removed}) || len(plan.Conflicts) != 0 {
		t.Fatalf("replacement preview: %+v", plan)
	}
	call("digest="+plan.Digest, replacement, http.StatusConflict)
	saved, err := server.store.Read(first.Note.ID)
	if err != nil || saved.Content != first.Content {
		t.Fatalf("replacement without confirmation changed the note: %v", err)
	}
	if _, err := server.store.Update(first.Note.ID, saved.Content+"\n另一位管理员的改动。\n", saved.Note.Revision); err != nil {
		t.Fatal(err)
	}
	call("digest="+plan.Digest+"&replace=true", replacement, http.StatusConflict)
	plan = call("preview=true", replacement, http.StatusOK)
	if _, err := server.db.Exec(`ALTER TABLE revisions ADD CONSTRAINT fixture_failure CHECK (content NOT LIKE '%新正文%')`); err != nil {
		t.Fatal(err)
	}
	call("digest="+plan.Digest+"&replace=true", replacement, http.StatusInternalServerError)
	saved, err = server.store.Read(first.Note.ID)
	if err != nil || !strings.Contains(saved.Content, "另一位管理员的改动") {
		t.Fatalf("failed import did not restore prior Markdown: %v", err)
	}
	if _, err := server.packageImageData(added, false); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("failed import left a new image behind: %v", err)
	}
	if _, err := server.db.Exec(`ALTER TABLE revisions DROP CONSTRAINT fixture_failure`); err != nil {
		t.Fatal(err)
	}
	call("digest="+plan.Digest+"&replace=true", replacement, http.StatusOK)
	saved, err = server.store.Read(first.Note.ID)
	if err != nil || saved.Note.Order != 24 || !saved.Note.Draft || !strings.Contains(saved.Content, "新正文") {
		t.Fatalf("replacement metadata or body: %+v, %v", saved.Note, err)
	}
	if hasAssetReference(saved.Content, changed) || !hasAssetReference(saved.Content, plan.RemappedAssets[0]) {
		t.Fatalf("imported note did not use the new image: %s", saved.Content)
	}
	other, err := server.store.Read(second.Note.ID)
	if err != nil || other.Content != second.Content {
		t.Fatalf("unselected note changed: %v", err)
	}
	originalImage, err := server.packageImageData(changed, false)
	if err != nil || !bytes.Equal(originalImage, oldBytes) {
		t.Fatalf("existing shared image changed: %v", err)
	}
	if _, err := server.packageImageData(removed, true); err != nil {
		t.Fatalf("removed image not in trash: %v", err)
	}
	if plan := call("preview=true", replacement, http.StatusOK); plan.NewNotes+plan.ReplaceNotes+plan.NewAssets != 0 {
		t.Fatalf("replacement is not idempotent: %+v", plan)
	}
	revisions, err := server.history.RecentRevisions(t.Context(), first.Note.ID, 10)
	if err != nil || len(revisions) != 3 || revisions[0].Action != "update" || revisions[2].Revision != first.Note.Revision {
		t.Fatalf("replacement history: %+v, %v", revisions, err)
	}
	exported := requestJSON(t, server, http.MethodPost, "/api/admin/document-packages/export",
		map[string][]string{"ids": {first.Note.ID, second.Note.ID}}, cookie)
	if exported.Code != http.StatusOK || exported.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export status: %d %s", exported.Code, exported.Body.Bytes())
	}
	pack, err := decodeDocumentPackage(exported.Body.Bytes())
	if err != nil || len(pack.notes) != 2 || len(pack.assets) != 4 || pack.categories["lab-notes"].Name != "实验记录" {
		t.Fatalf("export package metadata and scope: %v", err)
	}
	registered, assets, err := server.publicationInputs(t.Context())
	if err != nil || len(registered) != 2 || slices.Contains(assets, removed) || !slices.Contains(assets, plan.RemappedAssets[0]) {
		t.Fatalf("queued publication does not see the imported files: %v, %v", assets, err)
	}
	conflicting := encode([]Document{note("丙篇", "first", "", "重复网址。")}, nil)
	if conflict := call("preview=true", conflicting, http.StatusOK); len(conflict.Conflicts) != 1 {
		t.Fatalf("slug collision was not reported: %+v", conflict)
	}
	unauthorized := requestJSON(t, server, http.MethodPost, "/api/admin/document-packages/export",
		map[string][]string{"ids": {first.Note.ID}}, nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous export: %d", unauthorized.Code)
	}
}
