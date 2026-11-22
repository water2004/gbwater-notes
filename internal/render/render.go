package render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"gbwater/internal/app"
)

type Heading struct {
	Level int    `json:"level"`
	Title string `json:"title"`
}

type CatalogNote struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Category       string    `json:"category"`
	CategoryTitle  string    `json:"categoryTitle"`
	Course         string    `json:"course"`
	Slug           string    `json:"slug"`
	CourseSlug     string    `json:"courseSlug"`
	Order          int       `json:"order"`
	Draft          bool      `json:"draft"`
	Excerpt        string    `json:"excerpt"`
	Characters     int       `json:"characters"`
	ReadingMinutes int       `json:"readingMinutes"`
	Headings       []Heading `json:"headings"`
}

// MathEntry 的字段顺序与旧版 JSON 一致：成功时 width/height/baseline/tex/display，
// 失败时 error/tex/display。
type MathEntry struct {
	Width    *float64 `json:"width,omitempty"`
	Height   *float64 `json:"height,omitempty"`
	Baseline *float64 `json:"baseline,omitempty"`
	Error    string   `json:"error,omitempty"`
	Tex      string   `json:"tex"`
	Display  bool     `json:"display"`
}

type MermaidEntry struct {
	Width  *float64 `json:"width,omitempty"`
	Height *float64 `json:"height,omitempty"`
	Error  string   `json:"error,omitempty"`
	Source string   `json:"source"`
}

type NativeDocument struct {
	Math    map[string]MathEntry    `json:"math"`
	Mermaid map[string]MermaidEntry `json:"mermaid"`
}

type Formula struct {
	Tex     string
	Display bool
}

// mathRenderer 与 mermaidRenderer 由平台文件实现；非 Windows 平台返回明确错误。
type mathRenderer interface {
	Render(tex string, display bool) (svg string, width, height, baseline float64, err error)
	Close()
}

type mermaidRenderer interface {
	Render(source string, dark bool) (svg string, width, height float64, err error)
	Close()
}

type document struct {
	note      app.Note
	body      string
	formulas  []Formula
	diagrams  []string
	previous  NativeDocument
	native    NativeDocument
	docErrors []string
}

// Run generates every public artifact for a build.
func Run(ctx context.Context, root, base string, log io.Writer) error {
	if root == "" || base == "" {
		return errors.New("构建任务缺少站点根目录或挂载路径")
	}
	contentDir := filepath.Join(root, "content")
	generatedDir := filepath.Join(root, "web", "src", "generated")
	publicDir := filepath.Join(root, "web", "public")
	store := app.NewContentStore(contentDir)
	notes, err := store.List()
	if err != nil {
		return err
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].ID < notes[j].ID })
	var bridge *htmlBridge
	if len(notes) > 0 {
		bridge, err = loadHTMLBridge(root, base)
		if err != nil {
			return err
		}
	}

	documents := make([]document, 0, len(notes))
	needMath, needMermaid := false, false
	for _, note := range notes {
		loaded, err := store.Read(note.ID)
		if err != nil {
			return fmt.Errorf("%s: %w", note.ID, err)
		}
		body := markdownBody(loaded.Content)
		formulas := ExtractMath(body)
		diagrams := ExtractMermaid(body)
		if len(formulas) > 0 {
			needMath = true
		}
		if len(diagrams) > 0 {
			needMermaid = true
		}
		documents = append(documents, document{
			note:     loaded.Note,
			body:     body,
			formulas: formulas,
			diagrams: diagrams,
			previous: readNativeFile(generatedDir, note.ID),
			native:   NativeDocument{Math: map[string]MathEntry{}, Mermaid: map[string]MermaidEntry{}},
		})
	}

	var mathR mathRenderer
	if needMath {
		mathR, err = openMath(nativeLibrary(root, "Math", "FOLIA_MATH_DLL"))
		if err != nil {
			return err
		}
		defer mathR.Close()
	}
	var mermaidR mermaidRenderer
	if needMermaid {
		mermaidR, err = openMermaid(nativeLibrary(root, "Mermaid", "FOLIA_MERMAID_DLL"))
		if err != nil {
			return err
		}
		defer mermaidR.Close()
	}

	var failures []error
	published := make([]CatalogNote, 0, len(documents))
	mathCount, mermaidCount := 0, 0
	assetsDir := filepath.Join(publicDir, "generated")
	for index := range documents {
		if err := ctx.Err(); err != nil {
			return err
		}
		doc := &documents[index]
		renderDocument(doc, mathR, mermaidR, assetsDir, generatedDir)
		if err := bridge.render(ctx, doc, contentDir, generatedDir); err != nil {
			doc.docErrors = append(doc.docErrors, err.Error())
		}
		for _, message := range doc.docErrors {
			failures = append(failures, fmt.Errorf("%s: %s", doc.note.ID, message))
		}
		fmt.Fprintf(log, "[native] %s (%d errors)\n", doc.note.ID, len(doc.docErrors))
		if !doc.note.Draft {
			published = append(published, buildCatalog(doc.note, doc.body))
		}
		mathCount += len(doc.native.Math)
		mermaidCount += len(doc.native.Mermaid)
	}
	fmt.Fprintf(log, "[native] %d notes, %d public, %d formulas, %d diagrams\n", len(documents), len(published), mathCount, mermaidCount)
	if err := errors.Join(failures...); err != nil {
		return err
	}
	sort.Slice(published, func(i, j int) bool {
		a, b := published[i], published[j]
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		if a.Course != b.Course {
			return a.Course < b.Course
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Title < b.Title
	})

	if err := publishReaderArticles(publicDir, generatedDir, documents); err != nil {
		return err
	}
	return writeJSON(filepath.Join(publicDir, "catalog.json"), published)
}

func nativeLibrary(root, suffix, windowsOverride string) string {
	directory := os.Getenv("GBWATER_FOLIA_LIB_DIR")
	if directory == "" {
		directory = filepath.Join(root, "data", "folia-bridge-build", "lib")
	}
	if runtime.GOOS == "windows" {
		if path := os.Getenv(windowsOverride); path != "" {
			return path
		}
		return filepath.Join(directory, "FoliaNative"+suffix+".dll")
	}
	return filepath.Join(directory, "libFoliaNative"+suffix+".so")
}

func loadHTMLBridge(root, base string) (*htmlBridge, error) {
	name := os.Getenv("GBWATER_FOLIA_BRIDGE")
	if name == "" {
		executable := "folia_md"
		if runtime.GOOS == "windows" {
			executable += ".exe"
		}
		name = filepath.Join(root, "data", "folia-bridge-build", executable)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("找不到 Folia bridge %q，请先编译，或设置 GBWATER_FOLIA_BRIDGE: %w", name, err)
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	for _, file := range []string{executable, path} {
		input, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(h, input)
		_ = input.Close()
		if err != nil {
			return nil, err
		}
	}
	return &htmlBridge{path: path, base: base, signature: hex.EncodeToString(h.Sum(nil))}, nil
}

func renderDocument(doc *document, mathR mathRenderer, mermaidR mermaidRenderer, publicDir, generatedDir string) {
	for _, formula := range doc.formulas {
		key := mathKey(formula.Tex, formula.Display)
		destination := filepath.Join(publicDir, "math", key+".svg")
		entry := MathEntry{}
		if fileExists(destination) {
			if previous, ok := doc.previous.Math[key]; ok && previous.Error == "" && previous.Width != nil {
				entry = previous
			}
		}
		if entry.Width == nil {
			if mathR == nil {
				doc.failMath(key, formula, "原生数学渲染器未初始化")
				continue
			}
			svg, width, height, baseline, err := mathR.Render(formula.Tex, formula.Display)
			if err != nil {
				doc.failMath(key, formula, err.Error())
				continue
			}
			if err := writeIfChanged(destination, []byte(svg)); err != nil {
				doc.failMath(key, formula, err.Error())
				continue
			}
			roundedWidth, roundedHeight, roundedBaseline := round4(width), round4(height), round4(baseline)
			entry.Width, entry.Height, entry.Baseline = &roundedWidth, &roundedHeight, &roundedBaseline
		}
		if data, err := os.ReadFile(destination); err == nil {
			if unknown := unknownMathCommands(string(data)); len(unknown) > 0 {
				doc.failMath(key, formula, "缓存公式包含渲染错误: "+strings.Join(unknown, ", "))
				continue
			}
		}
		if data, err := os.ReadFile(destination); err == nil {
			if invisible := invisibleMathLines(string(data)); len(invisible) > 0 {
				doc.failMath(key, formula, "缓存公式包含不可见的数组线")
				continue
			}
		}
		entry.Tex = formula.Tex
		entry.Display = formula.Display
		doc.native.Math[key] = entry
	}

	for _, source := range doc.diagrams {
		key := mermaidKey(source)
		lightPath := filepath.Join(publicDir, "mermaid", key+"-light.svg")
		darkPath := filepath.Join(publicDir, "mermaid", key+"-dark.svg")
		entry := MermaidEntry{}
		if fileExists(lightPath) && fileExists(darkPath) {
			if previous, ok := doc.previous.Mermaid[key]; ok && previous.Error == "" && previous.Width != nil {
				entry = previous
			}
		}
		if entry.Width == nil {
			if mermaidR == nil {
				doc.failMermaid(key, source, "原生 Mermaid 渲染器未初始化")
				continue
			}
			light, width, height, err := mermaidR.Render(source, false)
			if err != nil {
				doc.failMermaid(key, source, err.Error())
				continue
			}
			dark, _, _, err := mermaidR.Render(source, true)
			if err != nil {
				doc.failMermaid(key, source, err.Error())
				continue
			}
			if err := writeIfChanged(lightPath, []byte(light)); err != nil {
				doc.failMermaid(key, source, err.Error())
				continue
			}
			if err := writeIfChanged(darkPath, []byte(dark)); err != nil {
				doc.failMermaid(key, source, err.Error())
				continue
			}
			roundedWidth, roundedHeight := round3(width), round3(height)
			entry.Width, entry.Height = &roundedWidth, &roundedHeight
		}
		entry.Source = source
		doc.native.Mermaid[key] = entry
	}

	if err := writeJSON(filepath.Join(generatedDir, "notes", doc.note.ID+".json"), doc.native); err != nil {
		doc.docErrors = append(doc.docErrors, err.Error())
	}
}

func (doc *document) failMath(key string, formula Formula, message string) {
	doc.native.Math[key] = MathEntry{Error: message, Tex: formula.Tex, Display: formula.Display}
	doc.docErrors = append(doc.docErrors, fmt.Sprintf("公式 %s: %s", key, message))
}

func (doc *document) failMermaid(key, source, message string) {
	doc.native.Mermaid[key] = MermaidEntry{Error: message, Source: source}
	doc.docErrors = append(doc.docErrors, fmt.Sprintf("Mermaid %s: %s", key, message))
}

func buildCatalog(note app.Note, body string) CatalogNote {
	text := plainText(body)
	runes := []rune(text)
	excerpt := runes
	if len(excerpt) > 180 {
		excerpt = excerpt[:180]
	}
	headings := []Heading{}
	for _, match := range headingPattern.FindAllStringSubmatch(body, -1) {
		headings = append(headings, Heading{Level: len(match[1]), Title: strings.TrimSpace(match[2])})
	}
	return CatalogNote{
		ID: note.ID, Title: note.Title, Category: note.Category, CategoryTitle: note.CategoryTitle,
		Course: note.Course, Slug: note.Slug, CourseSlug: note.CourseSlug, Order: note.Order, Draft: note.Draft,
		Excerpt: string(excerpt), Characters: utf8.RuneCountInString(text),
		ReadingMinutes: max(1, (utf8.RuneCountInString(text)+399)/400),
		Headings:       headings,
	}
}

func readNativeFile(generatedDir, id string) NativeDocument {
	empty := NativeDocument{Math: map[string]MathEntry{}, Mermaid: map[string]MermaidEntry{}}
	data, err := os.ReadFile(filepath.Join(generatedDir, "notes", id+".json"))
	if err != nil {
		return empty
	}
	var parsed NativeDocument
	if err := json.Unmarshal(data, &parsed); err != nil {
		return empty
	}
	if parsed.Math == nil {
		parsed.Math = map[string]MathEntry{}
	}
	if parsed.Mermaid == nil {
		parsed.Mermaid = map[string]MermaidEntry{}
	}
	return parsed
}

func writeJSON(path string, value any) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	return writeIfChanged(path, bytes.TrimSuffix(buffer.Bytes(), []byte("\n")))
}

func writeIfChanged(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".render-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0o644); err == nil {
		_, err = file.Write(data)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		err = os.Rename(file.Name(), path)
		// Windows 上文件监视器可能短暂打开正在替换的文件。
		if runtime.GOOS != "windows" || attempt == 3 ||
			(!os.IsPermission(err) && !errors.Is(err, syscall.Errno(32))) { // ERROR_SHARING_VIOLATION
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 30 * time.Millisecond)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

var headingPattern = regexp.MustCompile(`(?m)^(#{1,3})\s+(.+)$`)

var (
	fencePattern       = regexp.MustCompile("(?ms)^```.*?^```")
	displayMathPattern = regexp.MustCompile(`(?s)\$\$(.+?)\$\$`)
	inlineMathPattern  = regexp.MustCompile(`\$([^$\n]+)\$`)
	imagePattern       = regexp.MustCompile(`!\[([^]]*)\]\([^)]+\)`)
	linkPattern        = regexp.MustCompile(`\[([^]]+)\]\([^)]+\)`)
	htmlTagPattern     = regexp.MustCompile(`<[^>]+>`)
	markupPattern      = regexp.MustCompile("[\\\\#*_>`|:{}~-]+")
	// Python 的 \s 是 Unicode 语义（含全角空格 U+3000），Go 的 RE2 \s 只覆盖 ASCII。
	spacePattern = regexp.MustCompile("[\\t\\n\\v\\f\\r \\x{0085}\\x{001C}-\\x{001F}\\x{2028}\\x{2029}\\p{Zs}]+")
)

// plainText 与旧版脚本一致：去代码块、公式定界符、图片与链接语法后压平。
func plainText(markdown string) string {
	text := fencePattern.ReplaceAllString(markdown, " ")
	text = displayMathPattern.ReplaceAllString(text, " $1 ")
	text = inlineMathPattern.ReplaceAllString(text, " $1 ")
	text = imagePattern.ReplaceAllString(text, " $1 ")
	text = linkPattern.ReplaceAllString(text, " $1 ")
	text = htmlTagPattern.ReplaceAllString(text, " ")
	text = markupPattern.ReplaceAllString(text, " ")
	return strings.TrimSpace(spacePattern.ReplaceAllString(text, " "))
}

// markdownBody 去掉 frontmatter，保留正文。
func markdownBody(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return normalized
	}
	end := strings.Index(normalized[4:], "\n---")
	if end < 0 {
		return normalized
	}
	return strings.TrimLeft(normalized[4+end+4:], "\n")
}

// ExtractMath 提取 $...$ 与 $$...$$，跳过围栏代码块与行内代码。
func ExtractMath(markdown string) []Formula {
	result := []Formula{}
	index, length := 0, len(markdown)
	atLineStart := true
	for index < length {
		if strings.HasPrefix(markdown[index:], "```") && atLineStart {
			end := strings.Index(markdown[index+3:], "\n```")
			if end < 0 {
				break
			}
			end += index + 3
			newline := strings.Index(markdown[end+4:], "\n")
			if newline < 0 {
				index = length
			} else {
				index = end + 4 + newline + 1
			}
			atLineStart = true
			continue
		}
		if markdown[index] == '`' {
			run := 1
			for index+run < length && markdown[index+run] == '`' {
				run++
			}
			delimiter := markdown[index : index+run]
			end := strings.Index(markdown[index+run:], delimiter)
			if end < 0 {
				index = length
			} else {
				index += run + end + run
			}
			atLineStart = false
			continue
		}
		if markdown[index] == '$' && (index == 0 || markdown[index-1] != '\\') {
			count := 1
			if strings.HasPrefix(markdown[index:], "$$") {
				count = 2
			}
			delimiter := strings.Repeat("$", count)
			cursor := index + count
			matched := false
			for {
				end := strings.Index(markdown[cursor:], delimiter)
				if end < 0 {
					break
				}
				end += cursor
				slashes := 0
				for back := end - 1; back >= 0 && markdown[back] == '\\'; back-- {
					slashes++
				}
				content := markdown[index+count : end]
				if slashes%2 == 0 && (count == 2 || !strings.Contains(content, "\n")) {
					if tex := cleanMathTex(content); tex != "" {
						result = append(result, Formula{Tex: tex, Display: count == 2})
					}
					index = end + count
					matched = true
					atLineStart = false
					break
				}
				cursor = end + count
			}
			if matched {
				continue
			}
			index += count
			atLineStart = false
			continue
		}
		atLineStart = markdown[index] == '\n'
		index++
	}
	return dedupeFormulas(result)
}

// cleanMathTex 去掉公式内容里残留的引用块前缀（> 或 > >），
// 让 blockquote 内的公式与普通公式共用同一份 key。
func cleanMathTex(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for index, line := range lines {
		line = strings.TrimSpace(line)
		// 只去掉引用块前缀（"> " 或单独的 ">"），不能误伤 $>10$ 这种比较运算符。
		for line == ">" || strings.HasPrefix(line, "> ") || strings.HasPrefix(line, ">\t") {
			line = strings.TrimSpace(line[1:])
		}
		lines[index] = line
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
func dedupeFormulas(formulas []Formula) []Formula {
	seen := map[string]bool{}
	result := make([]Formula, 0, len(formulas))
	for _, formula := range formulas {
		key := formulaKey(formula)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, formula)
	}
	return result
}

func formulaKey(formula Formula) string {
	return fmt.Sprintf("%t\x00%s", formula.Display, formula.Tex)
}

var mermaidPattern = regexp.MustCompile("(?ms)^```mermaid\\s*\\n(.*?)^```\\s*$")

// ExtractMermaid 提取 ```mermaid 代码块并去重。
func ExtractMermaid(markdown string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, match := range mermaidPattern.FindAllStringSubmatch(markdown, -1) {
		if seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		result = append(result, match[1])
	}
	return result
}

func mathKey(tex string, display bool) string {
	prefix := "0"
	if display {
		prefix = "1"
	}
	digest := sha256.Sum256([]byte(prefix + "\x00" + tex))
	return hex.EncodeToString(digest[:])[:24]
}

func mermaidKey(source string) string {
	digest := sha256.Sum256([]byte(source))
	return hex.EncodeToString(digest[:])[:24]
}

func round4(value float64) float64 { return math.Round(value*10000) / 10000 }
func round3(value float64) float64 { return math.Round(value*1000) / 1000 }
