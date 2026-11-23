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
