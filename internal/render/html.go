package render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gbwater/internal/anchor"
	"gbwater/internal/app"
)

var (
	bridgeInlineMathPattern = regexp.MustCompile(`(?s)<span class="math-inline">(.*?)</span>`)
	bridgeBlockMathPattern  = regexp.MustCompile(`(?s)<div class="math-block">(.*?)</div>`)
	bridgeMermaidPattern    = regexp.MustCompile(`(?s)<pre class="mermaid-source"><code>(.*?)</code></pre>`)
)

type htmlBridge struct {
	path      string
	base      string
	signature string
}

type articlePublication struct {
	Article anchor.Article
	Catalog app.CatalogNote
}

type cachedArticle struct {
	Key         string
	Publication articlePublication
}

func (bridge *htmlBridge) cacheKey(doc *document, assets NativeDocument) (string, error) {
	metadata, err := json.Marshal([2]any{assets, doc.publication})
	if err != nil {
		return "", err
	}
	prefix := bridge.signature + "\x00" + bridge.base + "\x00" + doc.note.Revision + "\x00" + string(metadata)
	digest := sha256.Sum256([]byte(prefix))
	return hex.EncodeToString(digest[:]), nil
}

// prepare uses Folia's parsed math and diagram nodes as the source for asset rendering.
func (bridge *htmlBridge) prepare(ctx context.Context, doc *document, contentDir, generatedDir string) error {
	target := cachedArticlePath(generatedDir, doc.note.ID)
	var cached cachedArticle
	if !doc.force {
		content, err := os.ReadFile(target)
		if err == nil && json.Unmarshal(content, &cached) == nil {
			doc.publication = cached.Publication
			key, err := bridge.cacheKey(doc, doc.previous)
			if err != nil {
				return err
			}
			if key == cached.Key {
				doc.cached = true
				for _, formula := range doc.previous.Math {
					doc.formulas = append(doc.formulas, Formula{Tex: formula.Tex, Display: formula.Display})
				}
				for _, diagram := range doc.previous.Mermaid {
					doc.diagrams = append(doc.diagrams, diagram.Source)
				}
				return nil
			}
		}
	}
	if err := bridge.parse(ctx, doc, contentDir); err != nil {
		return err
	}
	doc.collectAssets()
	return nil
}

func (bridge *htmlBridge) parse(ctx context.Context, doc *document, contentDir string) error {
	sourcePath := filepath.Join(contentDir, doc.note.ID+".md")
	command := exec.CommandContext(ctx, bridge.path, sourcePath)
	output, err := command.Output()
	if err != nil {
		if exited, ok := err.(*exec.ExitError); ok && len(exited.Stderr) > 0 {
			return fmt.Errorf("folia bridge: %s", strings.TrimSpace(string(exited.Stderr)))
		}
		return fmt.Errorf("folia bridge: %w", err)
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(source)
	if hex.EncodeToString(digest[:16]) != doc.note.Revision {
		return fmt.Errorf("渲染期间原文发生修改，请重新构建")
	}
	doc.fragment = string(output)
	return nil
}

func (doc *document) collectAssets() {
	formulas := map[Formula]bool{}
	for _, display := range []bool{false, true} {
		pattern := bridgeInlineMathPattern
		if display {
			pattern = bridgeBlockMathPattern
		}
		for _, match := range pattern.FindAllStringSubmatch(doc.fragment, -1) {
			formula := Formula{Tex: normalizeMathTex(html.UnescapeString(match[1])), Display: display}
			if !formulas[formula] {
				doc.formulas = append(doc.formulas, formula)
				formulas[formula] = true
			}
		}
	}
	diagrams := map[string]bool{}
	for _, match := range bridgeMermaidPattern.FindAllStringSubmatch(doc.fragment, -1) {
		source := strings.TrimSpace(html.UnescapeString(match[1])) + "\n"
		if !diagrams[source] {
			doc.diagrams = append(doc.diagrams, source)
			diagrams[source] = true
		}
	}
}

func (bridge *htmlBridge) render(ctx context.Context, doc *document, contentDir, generatedDir string) error {
	if doc.cached {
		if reflect.DeepEqual(doc.previous, doc.native) {
			return nil
		}
		// A recreated asset may have different geometry.
		if err := bridge.parse(ctx, doc, contentDir); err != nil {
			return err
		}
		doc.cached = false
	}
	page, err := injectNativeAssets(doc.fragment, doc.native, bridge.base)
	if err != nil {
		return err
	}
	page, err = normalizeArticleHeadings(page)
	if err != nil {
		return err
	}
	text, err := anchor.RenderedText(page)
	if err != nil {
		return err
	}
	catalog, err := buildCatalog(doc.note, page)
	if err != nil {
		return err
	}
	doc.publication = articlePublication{
		Article: anchor.Article{HTML: page, Revision: anchor.Revision(text)},
		Catalog: catalog,
	}
	key, err := bridge.cacheKey(doc, doc.native)
	if err != nil {
		return err
	}
	return writeJSON(cachedArticlePath(generatedDir, doc.note.ID), cachedArticle{Key: key, Publication: doc.publication})
}

func cachedArticlePath(generatedDir, id string) string {
	return filepath.Join(generatedDir, "notes-html", id+".json")
}

// publishReaderArticles exports the same Folia-produced body for Vite and the static site.
func publishReaderArticles(publicDir string, documents []document, write func(string, any) error) error {
	dir := filepath.Join(publicDir, "note-content")
	published := make(map[string]anchor.Article)
	for _, doc := range documents {
		if doc.note.Draft {
			continue
		}
		name := doc.note.Slug + ".json"
		if _, exists := published[name]; exists {
			return fmt.Errorf("重复的公开笔记 slug: %s", doc.note.Slug)
		}
		published[name] = doc.publication.Article
	}
	for name, payload := range published {
		if err := write("note-content/"+name, payload); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		_, keep := published[entry.Name()]
		if !keep && entry.Type().IsRegular() && filepath.Ext(entry.Name()) == ".json" {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func injectNativeAssets(page string, native NativeDocument, base string) (string, error) {
	base = strings.TrimSpace(base)
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	math := make(map[string]struct {
		key   string
		entry MathEntry
	}, len(native.Math))
	for key, entry := range native.Math {
		math[mathLookupKey(entry.Tex, entry.Display)] = struct {
			key   string
			entry MathEntry
		}{key, entry}
	}
	mermaid := make(map[string]struct {
		key   string
		entry MermaidEntry
	}, len(native.Mermaid))
	for key, entry := range native.Mermaid {
		mermaid[strings.TrimSpace(entry.Source)] = struct {
			key   string
			entry MermaidEntry
		}{key, entry}
	}

	var injectionErr error
	renderMath := func(tex string, display bool) string {
		found, ok := math[mathLookupKey(tex, display)]
		if !ok || found.entry.Error != "" || len(found.entry.Frames) == 0 {
			message := "公式未预渲染"
			if ok && found.entry.Error != "" {
				message = found.entry.Error
			}
			injectionErr = fmt.Errorf("%s: %q", message, tex)
			return ""
		}
		return mathMarkup(base+"generated/math/"+found.key+".svg", found.entry.Frames, display)
	}

	page = bridgeInlineMathPattern.ReplaceAllStringFunc(page, func(match string) string {
		parts := bridgeInlineMathPattern.FindStringSubmatch(match)
		return renderMath(html.UnescapeString(parts[1]), false)
	})
	page = bridgeBlockMathPattern.ReplaceAllStringFunc(page, func(match string) string {
		parts := bridgeBlockMathPattern.FindStringSubmatch(match)
		return renderMath(html.UnescapeString(parts[1]), true)
	})
	page = bridgeMermaidPattern.ReplaceAllStringFunc(page, func(match string) string {
		parts := bridgeMermaidPattern.FindStringSubmatch(match)
		source := html.UnescapeString(parts[1])
		found, ok := mermaid[strings.TrimSpace(source)]
		if !ok || found.entry.Error != "" {
			injectionErr = fmt.Errorf("Mermaid 未预渲染: %q", strings.TrimSpace(source))
			return ""
		}
		width, height := "800", "450"
		if found.entry.Width != nil {
			width = formatFloat(*found.entry.Width)
		}
		if found.entry.Height != nil {
			height = formatFloat(*found.entry.Height)
		}
		light := base + "generated/mermaid/" + found.key + "-light.svg"
		dark := base + "generated/mermaid/" + found.key + "-dark.svg"
		return `<figure class="mermaid-figure"><img class="mermaid-light" src="` + light + `" width="` + width +
			`" height="` + height + `" alt="Mermaid 图表" loading="lazy"><img class="mermaid-dark" src="` + dark +
			`" width="` + width + `" height="` + height + `" alt="Mermaid 图表" loading="lazy"></figure>`
	})
	page = strings.ReplaceAll(page, `src="/mypdf/`, `src="`+base)
	page = strings.ReplaceAll(page, `href="/mypdf/`, `href="`+base)
	page = strings.ReplaceAll(page, `](/mypdf/`, `](`+base)
	page = strings.ReplaceAll(page, `src="/content-assets/`, `src="`+base+`content-assets/`)
	page = strings.ReplaceAll(page, `href="/content-assets/`, `href="`+base+`content-assets/`)
	if injectionErr != nil {
		return "", injectionErr
	}
	return page, nil
}

func mathLookupKey(tex string, display bool) string {
	return mathKey(normalizeMathTex(tex), display)
}

func normalizeMathTex(tex string) string {
	lines := strings.Split(strings.ReplaceAll(tex, "\r\n", "\n"), "\n")
	for index := range lines {
		lines[index] = strings.TrimSpace(lines[index])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
