package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	documentPackageFormat  = "gbwater-notes"
	documentPackageVersion = 1
	maxPackageBytes        = 64 << 20
	maxExpandedPackage     = 128 << 20
	maxPackageManifest     = 2 << 20
	maxPackageNote         = 8 << 20
	maxPackageEntries      = 4096
)

type packageEntry struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

type packageNoteEntry struct {
	packageEntry
	Metadata packageNoteMetadata `json:"metadata"`
}

type packageNoteMetadata struct {
	Title         string `json:"title"`
	Category      string `json:"category"`
	CategoryTitle string `json:"categoryTitle"`
	Course        string `json:"course"`
	CourseSlug    string `json:"courseSlug"`
	Slug          string `json:"slug"`
	Order         int    `json:"order"`
	Draft         bool   `json:"draft"`
}

func packageMetadata(note Note) packageNoteMetadata {
	return packageNoteMetadata{Title: note.Title, Category: note.Category, CategoryTitle: note.CategoryTitle,
		Course: note.Course, CourseSlug: note.CourseSlug, Slug: note.Slug, Order: note.Order, Draft: note.Draft}
}

type packageManifest struct {
	Format     string                      `json:"format"`
	Version    int                         `json:"version"`
	Categories map[string]categorySettings `json:"categories"`
	Notes      []packageNoteEntry          `json:"notes"`
	Assets     []packageEntry              `json:"assets"`
}

type documentPackage struct {
	categories map[string]categorySettings
	notes      []Document
	assets     map[string][]byte
}

func packageHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func invalidPackage(message string) error {
	return apiErr(http.StatusBadRequest, "invalid_document_package", message)
}

func oversizedPackage() error {
	return apiErr(http.StatusRequestEntityTooLarge, "document_package_too_large", "文档包最大 64 MiB，解压后最大 128 MiB")
}

func packagePathKey(id string) string { return norm.NFC.String(strings.ToLower(id)) }

func readPackageEntry(file *zip.File, limit int) ([]byte, error) {
	if !file.Mode().IsRegular() || file.UncompressedSize64 > uint64(limit) {
		return nil, invalidPackage("压缩包条目过大或不是普通文件")
	}
	reader, err := file.Open()
	if err != nil {
		return nil, invalidPackage("无法读取压缩包条目")
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil || len(data) > limit || uint64(len(data)) != file.UncompressedSize64 {
		return nil, invalidPackage("压缩包条目损坏或大小不符")
	}
	return data, nil
}

func decodeDocumentPackage(raw []byte) (documentPackage, error) {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(archive.File) > maxPackageEntries {
		return documentPackage{}, invalidPackage("请选择有效的 GBwater 文档包")
	}
	files := map[string]*zip.File{}
	paths := map[string]bool{}
	var total uint64
	for _, file := range archive.File {
		name := strings.TrimSuffix(file.Name, "/")
		key := packagePathKey(name)
		if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") || !utf8.ValidString(name) || paths[key] {
			return documentPackage{}, invalidPackage("压缩包含有无效或重复的路径")
		}
		paths[key] = true
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > maxExpandedPackage-total {
			return documentPackage{}, oversizedPackage()
		}
		total += file.UncompressedSize64
		files[name] = file
	}
	file := files["manifest.json"]
	if file == nil {
		return documentPackage{}, invalidPackage("压缩包缺少 manifest.json")
	}
	data, err := readPackageEntry(file, maxPackageManifest)
	if err != nil {
		return documentPackage{}, err
	}
	var manifest packageManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Format != documentPackageFormat || manifest.Version != documentPackageVersion || len(manifest.Notes) == 0 {
		return documentPackage{}, invalidPackage("文档包格式、版本或笔记清单无效")
	}
	delete(files, "manifest.json")
	load := func(name string, entry packageEntry, limit int) ([]byte, error) {
		file := files[name]
		if file == nil {
			return nil, invalidPackage("文档包缺少文件 " + name)
		}
		delete(files, name)
		data, err := readPackageEntry(file, limit)
		if err != nil {
			return nil, err
		}
		if packageHash(data) != entry.SHA256 {
			return nil, invalidPackage("文件校验失败 " + name)
		}
		return data, nil
	}
	pack := documentPackage{categories: manifest.Categories, assets: map[string][]byte{}}
	seenSlugs := map[string]bool{}
	seenCategories := map[string]bool{}
	for _, entry := range manifest.Notes {
		if !validNoteID(entry.ID) || !validAssetPath(entry.ID) || strings.Count(entry.ID, "/") != 2 {
			return documentPackage{}, invalidPackage("文档包中的笔记路径无效")
		}
		data, err := load("notes/"+entry.ID+".md", entry.packageEntry, maxPackageNote)
		if err != nil {
			return documentPackage{}, err
		}
		content, _ := packageAssetLinks(string(data), "/")
		note, err := parseMetadata(entry.ID, content, nil)
		if err != nil || !utf8.Valid(data) || bytes.ContainsRune(data, 0) || !validCategoryID(note.Category) || seenSlugs[note.Slug] {
			return documentPackage{}, invalidPackage("笔记信息或网址标识无效 " + entry.ID)
		}
		if entry.Metadata != packageMetadata(note) {
			return documentPackage{}, invalidPackage("清单元数据与 Markdown 不一致 " + entry.ID)
		}
		seenSlugs[note.Slug] = true
		seenCategories[note.Category] = true
		pack.notes = append(pack.notes, Document{Note: note, Content: content})
	}
	for id, category := range pack.categories {
		if !seenCategories[id] || strings.TrimSpace(category.Name) == "" || len(category.Name) > 256 || len(category.Description) > 4096 {
			return documentPackage{}, invalidPackage("文档包中的学科信息无效")
		}
	}
	if len(seenCategories) != len(pack.categories) {
		return documentPackage{}, invalidPackage("文档包缺少笔记所用的学科信息")
	}
	for _, entry := range manifest.Assets {
		if !validAssetPath(entry.ID) {
			return documentPackage{}, invalidPackage("文档包中的图片路径无效")
		}
		data, err := load("assets/"+entry.ID, entry, maxAssetBytes)
		if err != nil {
			return documentPackage{}, err
		}
		if !validPackageImage(entry.ID, data) {
			return documentPackage{}, invalidPackage("文档包含不受支持的图片 " + entry.ID)
		}
		pack.assets[entry.ID] = data
	}
	if len(files) != 0 {
		return documentPackage{}, invalidPackage("压缩包含有未在清单中声明的文件")
	}
	for _, doc := range pack.notes {
		_, links := packageAssetLinks(doc.Content, "/")
		for _, id := range links {
			if _, ok := pack.assets[id]; !ok {
				return documentPackage{}, invalidPackage("文档包缺少配图 " + id)
			}
		}
	}
	return pack, nil
}

func encodeDocumentPackage(pack documentPackage) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	manifest := packageManifest{Format: documentPackageFormat, Version: documentPackageVersion,
		Categories: pack.categories, Notes: []packageNoteEntry{}, Assets: []packageEntry{}}
	total := 0
	add := func(name string, data []byte, limit int) error {
		total += len(data)
		if total > maxExpandedPackage || len(data) > limit {
			return oversizedPackage()
		}
		writer, err := archive.Create(name)
		if err != nil {
			return err
		}
		_, err = writer.Write(data)
		return err
	}
	if len(pack.notes)+len(pack.assets)+1 > maxPackageEntries {
		return nil, oversizedPackage()
	}
	for _, doc := range pack.notes {
		data := []byte(doc.Content)
		manifest.Notes = append(manifest.Notes, packageNoteEntry{
			packageEntry: packageEntry{ID: doc.Note.ID, SHA256: packageHash(data)}, Metadata: packageMetadata(doc.Note),
		})
		if err := add("notes/"+doc.Note.ID+".md", data, maxPackageNote); err != nil {
			return nil, err
		}
	}
	ids := make([]string, 0, len(pack.assets))
	for id := range pack.assets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		data := pack.assets[id]
		manifest.Assets = append(manifest.Assets, packageEntry{ID: id, SHA256: packageHash(data)})
		if err := add("assets/"+id, data, maxAssetBytes); err != nil {
			return nil, err
		}
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := add("manifest.json", raw, maxPackageManifest); err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if buffer.Len() > maxPackageBytes {
		return nil, oversizedPackage()
	}
	return buffer.Bytes(), nil
}

// Only local URL prefixes written by the editor or older deployments are portable.
func packageAssetLinks(markdown, base string) (string, []string) {
	return rewritePackageAssetLinks(markdown, base, nil)
}

func rewritePackageAssetLinks(markdown, base string, assets map[string]string) (string, []string) {
	prefixes := []string{"/content-assets/", "/mypdf/content-assets/"}
	if base != "/" && base != "/mypdf/" && base != "" {
		prefixes = append(prefixes, strings.TrimRight(base, "/")+"/content-assets/")
	}
	for index := range prefixes {
		prefixes[index] = regexp.QuoteMeta(prefixes[index])
	}
	links := regexp.MustCompile(`(?:^|[\s("'=<])(` + strings.Join(prefixes, "|") + `)([^\s"'<>\)\]\}?#]+)`)
	var ids []string
	var result strings.Builder
	offset := 0
	for _, match := range links.FindAllStringSubmatchIndex(markdown, -1) {
		id, err := url.PathUnescape(markdown[match[4]:match[5]])
		if err != nil || !validAssetPath(id) {
			continue
		}
		ids = append(ids, id)
		result.WriteString(markdown[offset:match[2]])
		result.WriteString("/content-assets/")
		if target, ok := assets[id]; ok && target != id {
			parts := strings.Split(target, "/")
			for index, part := range parts {
				parts[index] = url.PathEscape(part)
			}
			result.WriteString(strings.Join(parts, "/"))
		} else {
			result.WriteString(markdown[match[4]:match[5]])
		}
		offset = match[1]
	}
	result.WriteString(markdown[offset:])
	return result.String(), ids
}

var localSVGFragment = regexp.MustCompile(`^#[a-zA-Z0-9_.:-]+$`)
var localSVGPaint = regexp.MustCompile(`^url\(#[a-zA-Z0-9_.:-]+\)$`)
var numericSVGColor = regexp.MustCompile(`^(?:rgb|rgba|hsl|hsla)\([-+0-9.,%\s]+\)$`)

func validPackageImage(id string, data []byte) bool {
	ext := strings.ToLower(path.Ext(id))
	if ext != ".svg" {
		if imageTypes[ext] == "" {
			return false
		}
		// Existing notes may have an old filename extension. Validate the
		// raster format from its bytes while keeping its original URL.
		switch http.DetectContentType(data) {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
			return true
		default:
			return false
		}
	}
	return validPackageSVG(data)
}

// Packages can carry vector drawings with local definitions. The allowlists
// exclude scripting, stylesheets, animation and externally fetched resources.
func validPackageSVG(data []byte) bool {
	const svgNS = "http://www.w3.org/2000/svg"
	const xlinkNS = "http://www.w3.org/1999/xlink"
	const elements = " svg g defs path rect circle ellipse line polyline polygon text tspan title desc clipPath mask marker symbol use linearGradient radialGradient stop "
	const attributes = " id class d fill fill-rule fill-opacity stroke stroke-width stroke-opacity stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset transform width height x y x1 x2 y1 y2 cx cy r rx ry fx fy points viewBox preserveAspectRatio opacity clip-path clip-rule mask marker-start marker-mid marker-end gradientUnits gradientTransform spreadMethod offset stop-color stop-opacity font-size font-family font-weight text-anchor dy dx refX refY orient markerWidth markerHeight markerUnits "
	reader := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	rootSeen := false
	for {
		token, err := reader.Token()
		if errors.Is(err, io.EOF) {
			return rootSeen && depth == 0
		}
		if err != nil {
			return false
		}
		switch item := token.(type) {
		case xml.StartElement:
			if item.Name.Space != svgNS || !strings.Contains(elements, " "+item.Name.Local+" ") || depth >= 256 {
				return false
			}
			if depth == 0 {
				if rootSeen || item.Name.Local != "svg" {
					return false
				}
				rootSeen = true
			}
			depth++
			for _, a := range item.Attr {
				switch {
				case a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns"):
					if a.Value != svgNS && a.Value != xlinkNS {
						return false
					}
				case a.Name.Local == "href" && (a.Name.Space == xlinkNS || a.Name.Space == ""):
					if !localSVGFragment.MatchString(a.Value) {
						return false
					}
				case a.Name.Space != "" || !strings.Contains(attributes, " "+a.Name.Local+" "):
					return false
				default:
					value := strings.TrimSpace(a.Value)
					if strings.ContainsAny(value, "\\()") && a.Name.Local != "transform" &&
						a.Name.Local != "gradientTransform" && !localSVGPaint.MatchString(value) && !numericSVGColor.MatchString(value) {
						return false
					}
				}
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(item)) != "" {
				return false
			}
		case xml.ProcInst:
			if rootSeen || item.Target != "xml" {
				return false
			}
		case xml.Directive:
			return false
		}
	}
}

func packageConflict(detail string) error {
	return apiErr(http.StatusConflict, "document_package_conflict", fmt.Sprintf("%s，未导入文档包", detail))
}
