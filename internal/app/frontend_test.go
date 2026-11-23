package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gbwater/internal/anchor"
)

func TestFrontendInstallAndIncrementalPublish(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GBWATER_FRONTEND_BUNDLE", "")
	t.Setenv("PATH", "")
	config := Config{Root: root, BasePath: "/notes-site/", APIBasePath: "/service/"}
	put := func(name, value string) {
		t.Helper()
		if err := atomicWrite(filepath.Join(root, filepath.FromSlash(name)), value); err != nil {
			t.Fatal(err)
		}
	}
	get := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("web/dist/"+name)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	put("web/compiled/index.html", `<html lang="zh-CN"><head><title>Notes</title>
<meta name="description" content="Notes"><link rel="icon" href="./favicon.svg">
<script src="./assets/app-123.js"></script></head><body><div id="app"></div></body></html>`)
	put("web/compiled/assets/app-123.js", "app")
	put("web/public/favicon.svg", "<svg>default</svg>")
	if err := InstallFrontend(config); err != nil {
		t.Fatal(err)
	}
	login := get("login/index.html")
	for _, expected := range []string{`data-site-base="/notes-site/"`, `data-api-base="/service/"`, "/notes-site/assets/app-123.js", "/notes-site/favicon.svg"} {
		if !strings.Contains(login, expected) {
			t.Errorf("startup shell missing %s", expected)
		}
	}
	const noteFile = "note-content/one.json"
	const notePage = "notes/one/index.html"
	const noteCatalog = `[{"title":"<笔记>","slug":"one","category":"math","categoryTitle":"数学","course":"代数","courseSlug":"algebra","excerpt":"摘要"}]`
	const revision = "1234567890abcdef1234567890abcdef"
	rendered := RenderedPublication{Files: map[string]string{}}
	putRendered := func(name, value string) {
		put("web/public/"+name, value)
		rendered.Files[name] = fingerprint([]byte(value))
	}
	putArticle := func(html string) {
		t.Helper()
		data, err := json.Marshal(anchor.Article{HTML: html, Revision: revision})
		if err != nil {
			t.Fatal(err)
		}
		putRendered(noteFile, string(data))
	}
	put("web/public/site-settings.json", `{"brand":{"name":"站点"}}`)
	putRendered("catalog.json", noteCatalog)
	putArticle(`<p>首次正文</p><img class="math-image" alt="" src="/notes-site/generated/math/test.svg">`)
	putRendered("generated/math/test.svg", "<svg></svg>")
	put("web/public/content-assets/image.png", "image")
	build := func(assets ...string) string {
		t.Helper()
		var log bytes.Buffer
		if err := publishFrontend(t.Context(), root, config.BasePath, config.APIBasePath, rendered, assets, &log); err != nil {
			t.Fatal(err)
		}
		return log.String()
	}
	build("image.png")
	article := get(notePage)
	if !strings.Contains(article, "<title>&lt;笔记&gt; · 站点</title>") ||
		!strings.Contains(article, `data-annotation-revision="`+revision+`"`) ||
		!strings.Contains(article, `<p>首次正文</p><img class="math-image"`) {
		t.Fatalf("invalid published article: %s", article)
	}
	notePath := filepath.Join(root, "web", "dist", filepath.FromSlash(notePage))
	mtime := time.Unix(1700000000, 0)
	if err := os.Chtimes(notePath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if got := build("image.png"); !strings.Contains(got, "updated 0, removed 0") {
		t.Fatal(got)
	}
	put("web/public/site-icon.svg", "<svg>custom</svg>")
	if got := build("image.png"); !strings.Contains(got, "updated 1, removed 0") {
		t.Fatal(got)
	}
	if err := InstallFrontend(config); err != nil {
		t.Fatal(err)
	}
	if get("favicon.svg") != "<svg>custom</svg>" {
		t.Fatal("restart overwrote the published icon")
	}
	if info, err := os.Stat(notePath); err != nil || !info.ModTime().Equal(mtime) {
		t.Fatalf("icon change rewrote the article: %v, %v", info, err)
	}
	putArticle("<p>新正文</p>")
	rendered.Files["generated/math/missing.svg"] = "changed"
	if err := publishFrontend(t.Context(), root, config.BasePath, config.APIBasePath, rendered, []string{"image.png"}, &bytes.Buffer{}); err == nil {
		t.Fatal("missing asset did not fail the staging pass")
	}
	if get(notePage) != article {
		t.Fatal("failed staging replaced an existing article")
	}
	delete(rendered.Files, "generated/math/missing.svg")
	if got := build("image.png"); !strings.Contains(got, "updated 2, removed 0") ||
		!strings.Contains(get(notePage), "<p>新正文</p>") {
		t.Fatal(got)
	}
	putRendered("catalog.json", "[]")
	delete(rendered.Files, noteFile)
	build()
	for _, name := range []string{notePage, noteFile, "content-assets/image.png"} {
		if _, err := os.Stat(filepath.Join(root, "web", "dist", filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Errorf("removed artifact still present: %s (%v)", name, err)
		}
	}
}

func TestSingleNotePublishKeepsThePublishedSnapshotOfOtherNotes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GBWATER_FRONTEND_BUNDLE", "")
	const base = "/notes-site/"
	const api = "/service/"
	const original = "旧站点"
	const revision = "1234567890abcdef1234567890abcdef"
	put := func(name, data string) {
		t.Helper()
		if err := atomicWrite(filepath.Join(root, filepath.FromSlash(name)), data); err != nil {
			t.Fatal(err)
		}
	}
	get := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("web/dist/"+name)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	encode := func(value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	note := CatalogNote{ID: "math/course/one", Title: "原题", Slug: "one", Category: "math", Course: "课程", CourseSlug: "course"}
	other := CatalogNote{ID: "math/course/two", Title: "另一篇", Slug: "two", Category: "math", Course: "课程", CourseSlug: "course"}
	article := func(html string) string { return encode(anchor.Article{HTML: html, Revision: revision}) }
	put("web/compiled/index.html", `<html lang="zh-CN"><head><title>Notes</title></head><body><div id="app"></div></body></html>`)
	put("web/compiled/assets/site.js", "app")
	put("web/public/favicon.svg", "<svg/>")
	put("web/public/site-settings.json", `{"brand":{"name":"`+original+`"}}`)
	initial := RenderedPublication{Files: map[string]string{}}
	for name, data := range map[string]string{
		"catalog.json":         encode([]CatalogNote{note, other}),
		articleFilename("one"): article("<p>初版</p>"),
		articleFilename("two"): article("<p>已发布的另一篇</p>"),
	} {
		put("web/public/"+name, data)
		initial.Files[name] = fingerprint([]byte(data))
	}
	if err := publishFrontend(t.Context(), root, base, api, initial, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	secondPage := "notes/two/index.html"
	otherBefore := get(secondPage)
	otherPath := filepath.Join(root, "web", "dist", filepath.FromSlash(secondPage))
	mtime := time.Unix(1700000000, 0)
	if err := os.Chtimes(otherPath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	put("web/public/site-settings.json", `{"brand":{"name":"未发布站点设置"}}`)
	put("web/public/"+articleFilename("two"), article("<p>另一篇未发布的修改</p>"))
	put("web/public/content-assets/used.png", "used")
	put("web/public/content-assets/unrelated.png", "unrelated")
	note.Title, note.Slug = "新题", "renamed"
	target := Note{ID: note.ID, Slug: note.Slug}
	put("scoped/catalog.json", encode([]CatalogNote{note}))
	data := article(`<p>新版</p><img src="/notes-site/content-assets/used.png">`)
	put("scoped/"+articleFilename(note.Slug), data)
	delta := RenderedPublication{Files: map[string]string{articleFilename(note.Slug): fingerprint([]byte(data))}}
	if err := publishSingleNote(t.Context(), root, base, api, filepath.Join(root, "scoped"), target, delta,
		[]string{"used.png", "unrelated.png"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(get("notes/renamed/index.html"), "<p>新版</p>") ||
		!strings.Contains(get("courses/course/index.html"), "新题") ||
		!strings.Contains(get("site-settings.json"), original) || get(secondPage) != otherBefore ||
		get("content-assets/used.png") != "used" {
		t.Fatal("single-note publication changed the wrong artifacts")
	}
	if info, err := os.Stat(otherPath); err != nil || !info.ModTime().Equal(mtime) {
		t.Fatalf("unrelated article was rewritten: %v, %v", info, err)
	}
	for _, name := range []string{"notes/one/index.html", articleFilename("one"), "content-assets/unrelated.png"} {
		if _, err := os.Stat(filepath.Join(root, "web", "dist", filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Errorf("unexpected artifact %s: %v", name, err)
		}
	}
	if source, err := os.ReadFile(filepath.Join(root, "web", "public", "catalog.json")); err != nil || !strings.Contains(string(source), "renamed") {
		t.Fatalf("Vite reader catalogue did not update: %s, %v", source, err)
	}
}
