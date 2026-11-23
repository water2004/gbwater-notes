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
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"gbwater/internal/app"
)

type Heading struct {
	Level int    `json:"level"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
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
	Digest   string   `json:"digest,omitempty"`
	Width    *float64 `json:"width,omitempty"`
	Height   *float64 `json:"height,omitempty"`
	Baseline *float64 `json:"baseline,omitempty"`
	Error    string   `json:"error,omitempty"`
	Tex      string   `json:"tex"`
	Display  bool     `json:"display"`
}

type MermaidEntry struct {
	LightDigest string   `json:"lightDigest,omitempty"`
	DarkDigest  string   `json:"darkDigest,omitempty"`
	Width       *float64 `json:"width,omitempty"`
	Height      *float64 `json:"height,omitempty"`
	Error       string   `json:"error,omitempty"`
	Source      string   `json:"source"`
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
	note        app.Note
	fragment    string
	cached      bool
	publication articlePublication
	formulas    []Formula
	diagrams    []string
	previous    NativeDocument
	native      NativeDocument
	docErrors   []string
}

type buildAssets struct {
	math        map[string]MathEntry
	mermaid     map[string]MermaidEntry
	openMath    func() (mathRenderer, error)
	openMermaid func() (mermaidRenderer, error)
}

// Run generates every public artifact for a build.
func Run(ctx context.Context, root, base string, notes []app.Note, log io.Writer) error {
	if root == "" || base == "" {
		return errors.New("构建任务缺少站点根目录或挂载路径")
	}
	contentDir := filepath.Join(root, "content")
	generatedDir := filepath.Join(root, "web", "src", "generated")
	publicDir := filepath.Join(root, "web", "public")
	var bridge *htmlBridge
	if len(notes) > 0 {
		var err error
		bridge, err = loadHTMLBridge(root, base)
		if err != nil {
			return err
		}
	}

	documents := make([]document, 0, len(notes))
	for _, note := range notes {
		doc := document{
			note:     note,
			previous: readNativeFile(generatedDir, note.ID),
			native:   NativeDocument{Math: map[string]MathEntry{}, Mermaid: map[string]MermaidEntry{}},
		}
		if err := bridge.prepare(ctx, &doc, contentDir, generatedDir); err != nil {
			return fmt.Errorf("%s: %w", note.ID, err)
		}
		documents = append(documents, doc)
	}

	var mathR mathRenderer
	var mermaidR mermaidRenderer
	assets := buildAssets{
		math:    map[string]MathEntry{},
		mermaid: map[string]MermaidEntry{},
		openMath: sync.OnceValues(func() (mathRenderer, error) {
			renderer, err := openMath(nativeLibrary(root, "Math", "FOLIA_MATH_DLL"))
			mathR = renderer
			return renderer, err
		}),
		openMermaid: sync.OnceValues(func() (mermaidRenderer, error) {
			renderer, err := openMermaid(nativeLibrary(root, "Mermaid", "FOLIA_MERMAID_DLL"))
			mermaidR = renderer
			return renderer, err
		}),
	}
	defer func() {
		if mathR != nil {
			mathR.Close()
		}
		if mermaidR != nil {
			mermaidR.Close()
		}
	}()

	var failures []error
	published := make([]CatalogNote, 0, len(documents))
	mathCount, mermaidCount := 0, 0
	reused := 0
	assetsDir := filepath.Join(publicDir, "generated")
	for index := range documents {
		if err := ctx.Err(); err != nil {
			return err
		}
		doc := &documents[index]
		renderDocument(doc, &assets, assetsDir, generatedDir)
		if len(doc.docErrors) == 0 {
			if err := bridge.render(ctx, doc, contentDir, generatedDir); err != nil {
				doc.docErrors = append(doc.docErrors, err.Error())
			}
		}
		for _, message := range doc.docErrors {
			failures = append(failures, fmt.Errorf("%s: %s", doc.note.ID, message))
		}
		if !doc.cached || len(doc.docErrors) > 0 {
			fmt.Fprintf(log, "[native] %s (%d errors)\n", doc.note.ID, len(doc.docErrors))
		}
		if !doc.note.Draft && len(doc.docErrors) == 0 {
			published = append(published, doc.publication.Catalog)
		}
		if doc.cached {
			reused++
		}
		mathCount += len(doc.native.Math)
		mermaidCount += len(doc.native.Mermaid)
	}
	fmt.Fprintf(log, "[native] %d notes, %d public, %d formulas, %d diagrams\n", len(documents), len(published), mathCount, mermaidCount)
	if err := errors.Join(failures...); err != nil {
		return err
	}
	fmt.Fprintf(log, "[articles] rendered %d, reused %d\n", len(documents)-reused, reused)
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

	state := filepath.Join(generatedDir, app.RenderManifest)
	var previous app.RenderedPublication
	if raw, err := os.ReadFile(state); err == nil {
		_ = json.Unmarshal(raw, &previous)
	}
	manifest := map[string]string{}
	invalidated := false
	writePublished := func(name string, value any) error {
		data, err := encodeJSON(value)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		version := hex.EncodeToString(digest[:])
		manifest[name] = version
		target := filepath.Join(publicDir, name)
		if previous.Files[name] == version && fileExists(target) {
			return nil
		}
		if !invalidated {
			if err := os.Remove(state); err != nil && !os.IsNotExist(err) {
				return err
			}
			invalidated = true
		}
		return replaceFile(target, data)
	}
	if err := publishReaderArticles(publicDir, documents, writePublished); err != nil {
		return err
	}
	if err := writePublished("catalog.json", published); err != nil {
		return err
	}
	for key, entry := range assets.math {
		manifest["generated/math/"+key+".svg"] = entry.Digest
	}
	for key, entry := range assets.mermaid {
		manifest["generated/mermaid/"+key+"-light.svg"] = entry.LightDigest
		manifest["generated/mermaid/"+key+"-dark.svg"] = entry.DarkDigest
	}
	versions := make(map[string]string, len(published))
	for _, doc := range documents {
		if !doc.note.Draft {
			versions[doc.note.Slug] = doc.publication.Article.Revision
		}
	}
	return writeJSON(state, app.RenderedPublication{Files: manifest, Versions: versions})
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

func renderDocument(doc *document, assets *buildAssets, publicDir, generatedDir string) {
	for _, formula := range doc.formulas {
		key := mathKey(formula.Tex, formula.Display)
		if entry, ok := assets.math[key]; ok {
			doc.native.Math[key] = entry
			continue
		}
		destination := filepath.Join(publicDir, "math", key+".svg")
		entry := MathEntry{}
		var rendered string
		if fileExists(destination) {
			if previous, ok := doc.previous.Math[key]; ok && previous.Error == "" && previous.Width != nil {
				entry = previous
			}
		}
		if entry.Width == nil {
			mathR, err := assets.openMath()
			if err != nil {
				doc.failMath(key, formula, err.Error())
				continue
			}
			svg, width, height, baseline, err := mathR.Render(formula.Tex, formula.Display)
			if err != nil {
				doc.failMath(key, formula, err.Error())
				continue
			}
			rendered = svg
			roundedWidth, roundedHeight, roundedBaseline := round4(width), round4(height), round4(baseline)
			entry.Width, entry.Height, entry.Baseline = &roundedWidth, &roundedHeight, &roundedBaseline
		}
		if entry.Digest == "" {
			data := rendered
			if data == "" {
				contents, err := os.ReadFile(destination)
				if err != nil {
					doc.failMath(key, formula, err.Error())
					continue
				}
				data = string(contents)
			}
			if unknown := unknownMathCommands(data); len(unknown) > 0 {
				doc.failMath(key, formula, "缓存公式包含渲染错误: "+strings.Join(unknown, ", "))
				continue
			}
			if invisible := invisibleMathLines(data); len(invisible) > 0 {
				doc.failMath(key, formula, "缓存公式包含不可见的数组线")
				continue
			}
			entry.Digest = assetDigest([]byte(data))
		}
		if rendered != "" {
			if err := replaceFile(destination, []byte(rendered)); err != nil {
				doc.failMath(key, formula, err.Error())
				continue
			}
		}
		entry.Tex = formula.Tex
		entry.Display = formula.Display
		doc.native.Math[key] = entry
		assets.math[key] = entry
	}

	for _, source := range doc.diagrams {
		key := mermaidKey(source)
		if entry, ok := assets.mermaid[key]; ok {
			doc.native.Mermaid[key] = entry
			continue
		}
		lightPath := filepath.Join(publicDir, "mermaid", key+"-light.svg")
		darkPath := filepath.Join(publicDir, "mermaid", key+"-dark.svg")
		entry := MermaidEntry{}
		if fileExists(lightPath) && fileExists(darkPath) {
			if previous, ok := doc.previous.Mermaid[key]; ok && previous.Error == "" && previous.Width != nil {
				entry = previous
			}
		}
		if entry.Width == nil {
			mermaidR, err := assets.openMermaid()
			if err != nil {
				doc.failMermaid(key, source, err.Error())
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
			entry.LightDigest, entry.DarkDigest = assetDigest([]byte(light)), assetDigest([]byte(dark))
		}
		if entry.LightDigest == "" || entry.DarkDigest == "" {
			light, errLight := os.ReadFile(lightPath)
			dark, errDark := os.ReadFile(darkPath)
			if err := errors.Join(errLight, errDark); err != nil {
				doc.failMermaid(key, source, err.Error())
				continue
			}
			entry.LightDigest, entry.DarkDigest = assetDigest(light), assetDigest(dark)
		}
		entry.Source = source
		doc.native.Mermaid[key] = entry
		assets.mermaid[key] = entry
	}

	nativePath := filepath.Join(generatedDir, "notes", doc.note.ID+".json")
	if reflect.DeepEqual(doc.previous, doc.native) && fileExists(nativePath) {
		return
	}
	if err := writeJSON(nativePath, doc.native); err != nil {
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

func encodeJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func writeJSON(path string, value any) error {
	data, err := encodeJSON(value)
	if err != nil {
		return err
	}
	return writeIfChanged(path, data)
}

func writeIfChanged(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	return replaceFile(path, data)
}

func replaceFile(path string, data []byte) error {
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

func assetDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
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
