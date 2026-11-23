package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gbwater/internal/anchor"
	"gbwater/internal/app"
)

func TestArticleHTMLAndCatalogKeepOnlyRenderedContent(t *testing.T) {
	fragment := `<h2 id="chapter-raw_1">章<strong>节</strong><span class="math-inline">\frac{raw_1}{2}</span></h2>
<p>说明 <em>正文</em> <a href="/reference">链接</a>。</p>
<div class="math-block">\sum_{raw_2}</div>
<pre class="mermaid-source"><code>flowchart LR
A--&gt;B</code></pre>`
	assets := NativeDocument{Math: map[string]MathEntry{
		"inline":  {Tex: `\frac{raw_1}{2}`, Frames: []MathFrame{{Width: 1.2, Height: .8}}},
		"display": {Tex: `\sum_{raw_2}`, Display: true, Frames: []MathFrame{{Width: 1.2, Height: .8}}},
	}, Mermaid: map[string]MermaidEntry{
		"diagram": {Source: "flowchart LR\nA-->B\n"},
	}}
	page, err := injectNativeAssets(fragment, assets, "/notes/")
	if err != nil {
		t.Fatal(err)
	}
	page, err = normalizeArticleHeadings(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`\frac`, `\sum`, "raw_1", "raw_2", "flowchart", "A--&gt;B", "mermaid-source"} {
		if strings.Contains(page, raw) {
			t.Errorf("article exposed %q: %s", raw, page)
		}
	}
	for _, rendered := range []string{`id="章节"`, `role="math" aria-label="公式"`, `alt=""`, "inline.svg", "display.svg", "diagram-light.svg", "diagram-dark.svg"} {
		if !strings.Contains(page, rendered) {
			t.Errorf("article is missing %q: %s", rendered, page)
		}
	}
	catalog, err := buildCatalog(app.Note{Title: "测试"}, page)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Excerpt != "章节 说明 正文 链接。" || len(catalog.Headings) != 1 || catalog.Headings[0] != (app.Heading{Level: 2, Title: "章节", Slug: "章节"}) {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
}

func TestFoliaMathBreaksUseOneAssetAndRelativeSize(t *testing.T) {
	output := mathSVG{source: `<mjx-container><svg xmlns="http://www.w3.org/2000/svg" width="1ex" viewBox="0 -750 500 1000"><g data-latex="original"><path d="M0 0h5"/></g></svg><mjx-break size="3"> </mjx-break><svg xmlns="http://www.w3.org/2000/svg" width="2ex" viewBox="0 -750 1000 1000"><g><path d="M0 0h10"/></g></svg></mjx-container>`}
	parts, err := parseMathParts(output)
	if err != nil {
		t.Fatal(err)
	}
	svg, frames := packMath(parts)
	page := mathMarkup("/notes/generated/math/asset.svg", frames, false)
	for _, want := range []string{"<wbr>", "width:0.222em", "width:0.5em", "vertical-align:-0.25em", "asset.svg#math-1", "asset.svg#math-2"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %s: %s", want, page)
		}
	}
	if strings.Contains(svg, "original") || !strings.Contains(svg, `<view id="math-2" viewBox="0 1000 1000 1000"`) {
		t.Fatal(svg)
	}
	text, err := anchor.RenderedText("<p>公式前" + page + "公式后</p>")
	if err != nil || text != "公式前公式后" {
		t.Fatalf("formula changed the text anchor: %q, %v", text, err)
	}
}

func TestReaderArticlesOnlyPublishCatalogBodies(t *testing.T) {
	root := t.TempDir()
	publicDir := filepath.Join(root, "public")
	published := document{note: app.Note{ID: "math/course/公开", Slug: "public", Revision: "published-revision"}}
	draft := document{note: app.Note{ID: "math/course/草稿", Slug: "draft", Draft: true}}
	for _, item := range []*document{&published, &draft} {
		item.publication.Article = anchor.Article{
			HTML: "<p>" + item.note.ID + "</p>", Revision: anchor.Revision(item.note.ID),
		}
	}
	dir := filepath.Join(publicDir, "note-content")
	if err := writeIfChanged(filepath.Join(dir, "withdrawn.json"), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := publishReaderArticles(publicDir, []document{published, draft}, func(name string, value any) error {
		return writeJSON(filepath.Join(publicDir, name), value)
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "public.json"))
	if err != nil {
		t.Fatal(err)
	}
	var article anchor.Article
	if err := json.Unmarshal(data, &article); err != nil {
		t.Fatal(err)
	}
	if article.Revision != anchor.Revision(published.note.ID) || article.HTML != "<p>"+published.note.ID+"</p>" {
		t.Fatalf("unexpected article: %+v", article)
	}
	for _, name := range []string{"draft.json", "withdrawn.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s was exposed or inaccessible: %v", name, err)
		}
	}
}

func TestUnknownMathCommands(t *testing.T) {
	red := `<svg><g data-mml-node="mtext" fill="red" data-latex="\bm">bm</g></svg>`
	if got := unknownMathCommands(red); len(got) != 1 || got[0] != `\bm` {
		t.Fatalf("red mtext: %+v", got)
	}
	merror := `<svg><g data-mml-node="merror" data-mjx-error="Undefined control sequence"></g></svg>`
	if got := unknownMathCommands(merror); len(got) != 1 || got[0] != "Undefined control sequence" {
		t.Fatalf("merror: %+v", got)
	}
	clean := `<svg><g data-mml-node="mi"><use href="#x"/></g></svg>`
	if got := unknownMathCommands(clean); len(got) != 0 {
		t.Fatalf("clean: %+v", got)
	}
}

func TestInvisibleMathLines(t *testing.T) {
	cases := []struct {
		svg  string
		want int
	}{
		{`<svg><line data-line="v"/></svg>`, 1},
		{`<svg><line data-line="v" stroke-width="0"/></svg>`, 1},
		{`<svg><line data-line="h" stroke-width="70"/></svg>`, 0},
		{`<svg><line stroke-width="0"/></svg>`, 0},
	}
	for _, testCase := range cases {
		if got := len(invisibleMathLines(testCase.svg)); got != testCase.want {
			t.Fatalf("%q: got %d want %d", testCase.svg, got, testCase.want)
		}
	}
}

func TestKeysAreStableAndDistinct(t *testing.T) {
	first := mathKey("x^2", false)
	if len(first) != 24 {
		t.Fatalf("key length: %d", len(first))
	}
	if first != mathKey("x^2", false) {
		t.Fatal("key must be stable")
	}
	if first == mathKey("x^2", true) || first == mathKey("x^3", false) {
		t.Fatal("key must differ by display flag and content")
	}
	if len(mermaidKey("a-->b")) != 24 {
		t.Fatal("mermaid key length")
	}
}

func contains(value, substring string) bool {
	return len(substring) == 0 || len(value) >= len(substring) && indexOf(value, substring) >= 0
}

func indexOf(value, substring string) int {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return index
		}
	}
	return -1
}
