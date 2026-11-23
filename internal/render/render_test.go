package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gbwater/internal/anchor"
	"gbwater/internal/app"
)

func TestReaderArticlesOnlyPublishCatalogBodies(t *testing.T) {
	root := t.TempDir()
	publicDir := filepath.Join(root, "public")
	generatedDir := filepath.Join(root, "generated")
	published := document{note: app.Note{ID: "math/course/公开", Slug: "public", Revision: "published-revision"}}
	draft := document{note: app.Note{ID: "math/course/草稿", Slug: "draft", Draft: true}}
	for _, item := range []document{published, draft} {
		if err := writeIfChanged(cachedArticlePath(generatedDir, item.note.ID), []byte("<p>"+item.note.ID+"</p>")); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(publicDir, "note-content")
	if err := writeIfChanged(filepath.Join(dir, "withdrawn.json"), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := publishReaderArticles(publicDir, generatedDir, []document{published, draft}); err != nil {
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

func TestExtractMathSkipsCodeAndDeduplicates(t *testing.T) {
	source := "行内 $a$ 与 $$b$$。\n\n```\n$ignore$\n```\n\n行内代码 `$alsoIgnore$`\n\n$$\nc\nd\n$$\n\n再写一次 $a$。"
	got := ExtractMath(source)
	want := []Formula{
		{Tex: "a", Display: false},
		{Tex: "b", Display: true},
		{Tex: "c\nd", Display: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d formulas: %+v", len(got), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("formula %d: got %+v want %+v", index, got[index], want[index])
		}
	}
}

func TestExtractMathIgnoresEscapedDollar(t *testing.T) {
	got := ExtractMath(`价格 \$5，公式 $x$ 结束`)
	if len(got) != 1 || got[0].Tex != "x" {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractMermaid(t *testing.T) {
	source := "```mermaid\nflowchart TD\nA-->B\n```\n\n```mermaid\nflowchart TD\nA-->B\n```\n"
	got := ExtractMermaid(source)
	if len(got) != 1 || got[0] != "flowchart TD\nA-->B\n" {
		t.Fatalf("got %+v", got)
	}
}

func TestMarkdownBodyStripsFrontmatter(t *testing.T) {
	for _, source := range []string{
		"---\ntitle: \"测试\"\n---\n\n正文",
		"---\r\ntitle: \"测试\"\r\n---\r\n\r\n正文",
	} {
		if got := markdownBody(source); got != "正文" {
			t.Fatalf("got %q", got)
		}
	}
}

func TestPlainText(t *testing.T) {
	source := "## 标题\n\n![图注](/a.png)\n\n[链接](https://x.test)\n\n公式 $x^2$ 与\n\n$$\ny=1\n$$\n\n`code`"
	got := plainText(source)
	if got == "" {
		t.Fatal("plain text should not be empty")
	}
	for _, unwanted := range []string{"$", "![", "```", "/a.png", "https://x.test"} {
		if contains(got, unwanted) {
			t.Fatalf("plain text still contains %q: %q", unwanted, got)
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
