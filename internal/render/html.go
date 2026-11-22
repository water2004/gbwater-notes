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
	"regexp"
	"strconv"
	"strings"
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

// render 让 Folia bridge 生成文章 HTML，再用 native 渲染结果替换公式/图表占位。
func (bridge *htmlBridge) render(ctx context.Context, doc *document, contentDir, generatedDir string) error {
	target := cachedArticlePath(generatedDir, doc.note.ID)
	metadata, err := json.Marshal(doc.native)
	if err != nil {
		return err
	}
	prefix := bridge.signature + "\x00" + bridge.base + "\x00" + doc.note.Revision + "\x00" + string(metadata)
	cacheKey := func(html []byte) string {
		digest := sha256.Sum256(append([]byte(prefix), html...))
		return hex.EncodeToString(digest[:])
	}
	cached, keyErr := os.ReadFile(target + ".hash")
	if existing, htmlErr := os.ReadFile(target); keyErr == nil && htmlErr == nil && string(cached) == cacheKey(existing) {
		return nil
	}
	sourcePath := filepath.Join(contentDir, doc.note.ID+".md")
	command := exec.CommandContext(ctx, bridge.path, sourcePath)
	output, err := command.Output()
	if err != nil {
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
	page, err := injectNativeAssets(string(output), doc.native, bridge.base)
	if err != nil {
		return err
	}
	if err := writeIfChanged(target, []byte(page)); err != nil {
		return err
	}
	return writeIfChanged(target+".hash", []byte(cacheKey([]byte(page))))
}

func cachedArticlePath(generatedDir, id string) string {
	return filepath.Join(generatedDir, "notes-html", id+".html")
}

type readerArticle struct {
	HTML     string `json:"html"`
	Revision string `json:"revision"`
}

// publishReaderArticles exports the same Folia-produced body for Vite and the static site.
func publishReaderArticles(publicDir, generatedDir string, documents []document) error {
	dir := filepath.Join(publicDir, "note-content")
	published := make(map[string]readerArticle)
	for _, doc := range documents {
		if doc.note.Draft {
			continue
		}
		name := doc.note.Slug + ".json"
		if _, exists := published[name]; exists {
			return fmt.Errorf("重复的公开笔记 slug: %s", doc.note.Slug)
		}
		fragment, err := os.ReadFile(cachedArticlePath(generatedDir, doc.note.ID))
		if err != nil {
			return fmt.Errorf("读取正文 %s: %w", doc.note.ID, err)
		}
		published[name] = readerArticle{HTML: string(fragment), Revision: doc.note.Revision}
	}
	for name, payload := range published {
		if err := writeJSON(filepath.Join(dir, name), payload); err != nil {
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
		escaped := html.EscapeString(tex)
		found, ok := math[mathLookupKey(tex, display)]
		if !ok || found.entry.Error != "" || found.entry.Width == nil || found.entry.Height == nil {
			message := "公式未预渲染"
			if ok && found.entry.Error != "" {
				message = found.entry.Error
			}
			injectionErr = fmt.Errorf("%s: %q", message, tex)
			return ""
		}
		entry := found.entry
		width, height := formatFloat(*entry.Width), formatFloat(*entry.Height)
		baseline := *entry.Height
		if entry.Baseline != nil {
			baseline = *entry.Baseline
		}
		image := `<img class="math-image" src="` + base + `generated/math/` + found.key + `.svg" alt="` + escaped +
			`" title="点击放大公式" width="` + width + `" height="` + height +
			`" style="vertical-align:` + formatFloat(baseline-*entry.Height) + `px" loading="lazy" decoding="async">`
		if display {
			return `<div class="math-display" role="math" aria-label="` + escaped + `">` + image + `</div>`
		}
		return `<span class="math-inline" role="math" aria-label="` + escaped + `">` + image + `</span>`
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
	lines := strings.Split(strings.ReplaceAll(tex, "\r\n", "\n"), "\n")
	for index := range lines {
		lines[index] = strings.TrimSpace(lines[index])
	}
	prefix := "0"
	if display {
		prefix = "1"
	}
	return prefix + "\x00" + strings.Join(lines, "\n")
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
