package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gbwater/internal/anchor"
	"golang.org/x/net/html"
)

func publishSingleNote(ctx context.Context, root, base, api, output string, note Note, rendered RenderedPublication, assets []string, log io.Writer) error {
	dist := filepath.Join(root, "web", "dist")
	public := filepath.Join(root, "web", "public")
	generated := filepath.Join(root, "web", "src", "generated")
	state := filepath.Join(generated, frontendManifest)
	var previous map[string]string
	if err := readSiteJSON(state, &previous); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if !note.Draft {
			return errors.New("请先从发布页完成一次全站发布，再单篇重建")
		}
	}
	bundle := siteBundle{files: make(map[string]siteFile)}
	if !note.Draft {
		var err error
		bundle, err = readSiteBundle(root, base, api)
		if err != nil {
			return err
		}
		shell, err := os.ReadFile(filepath.Join(dist, "index.html"))
		if err != nil {
			return err
		}
		if !bytes.Contains(shell, []byte(`data-site-base="`+bundle.base+`"`)) {
			return errors.New("站点路径已更改，请先从发布页重新发布全站")
		}
	}
	for name, key := range previous {
		if !validAssetPath(name) || key == "" {
			return fmt.Errorf("无效的已发布产物路径: %s", name)
		}
		if note.Draft || !publicPageRoute(name) {
			if _, exists := bundle.files[name]; !exists {
				bundle.files[name] = siteFile{key: key, source: filepath.Join(dist, filepath.FromSlash(name))}
			}
		}
	}
	for name, key := range rendered.Files {
		if strings.HasPrefix(name, "generated/") && validAssetPath(name) && key != "" {
			bundle.files[name] = siteFile{key: key, source: filepath.Join(public, filepath.FromSlash(name))}
		}
	}
	if note.Draft {
		if previous == nil {
			for name, file := range bundle.files {
				if err := installSiteFile(filepath.Join(dist, filepath.FromSlash(name)), file, true); err != nil {
					return err
				}
			}
			return nil
		}
		return applyPublication(ctx, dist, state, bundle.files, log)
	}
	var catalog, result []CatalogNote
	if err := readSiteJSON(filepath.Join(dist, "catalog.json"), &catalog); err != nil {
		return err
	}
	if err := readSiteJSON(filepath.Join(output, "catalog.json"), &result); err != nil {
		return err
	}
	if len(result) != 1 || result[0].ID != note.ID || result[0].Slug != note.Slug {
		return errors.New("单篇构建缺少目录记录")
	}
	next := make([]CatalogNote, 0, len(catalog)+1)
	var retired string
	for _, published := range catalog {
		if !validSlug(published.Slug) {
			return errors.New("已发布目录含有无效的笔记链接")
		}
		switch {
		case published.ID == note.ID:
			retired = articleFilename(published.Slug)
			delete(bundle.files, retired)
		case published.Slug == note.Slug:
			return errors.New("目标链接与另一篇已发布笔记冲突，请先从发布页更新目录")
		default:
			next = append(next, published)
		}
	}
	next = append(next, result[0])
	slices.SortFunc(next, CompareCatalogNotes)
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(next); err != nil {
		return err
	}
	data := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
	bundle.files["catalog.json"] = siteFile{key: fingerprint(data), page: func() ([]byte, error) { return data, nil }}
	filename := articleFilename(note.Slug)
	articleSource := filepath.Join(output, filepath.FromSlash(filename))
	bundle.files[filename] = siteFile{key: rendered.Files[filename], source: articleSource}
	if err := addNoteAssets(bundle, public, articleSource, assets); err != nil {
		return err
	}
	if err := planPublicPages(bundle, filepath.Join(dist, "site-settings.json"), next); err != nil {
		return err
	}
	if err := applyPublication(ctx, dist, state, bundle.files, log); err != nil {
		return err
	}
	return syncNoteOutput(generated, public, filename, retired, bundle.files)
}

func publicPageRoute(name string) bool {
	if !strings.HasSuffix(name, "index.html") {
		return false
	}
	first, _, _ := strings.Cut(name, "/")
	switch first {
	case "index.html", "library", "courses", "notes", "about", "account", "search":
		return true
	}
	return false
}

func articleFilename(slug string) string { return "note-content/" + slug + ".json" }

// Keep the Vite reader's public inputs current after a partial publication.
func syncNoteOutput(generated, public, article, retired string, files map[string]siteFile) error {
	state := filepath.Join(generated, RenderManifest)
	var staged RenderedPublication
	if err := readSiteJSON(state, &staged); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if staged.Files == nil {
		staged.Files = map[string]string{}
	}
	changed := false
	for _, name := range []string{article, "catalog.json"} {
		file := files[name]
		target := filepath.Join(public, filepath.FromSlash(name))
		if staged.Files[name] == file.key && regularFile(target) {
			continue
		}
		if err := installSiteFile(target, file, false); err != nil {
			return err
		}
		staged.Files[name] = file.key
		changed = true
	}
	if retired != "" && retired != article {
		if err := os.Remove(filepath.Join(public, filepath.FromSlash(retired))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		delete(staged.Files, retired)
		changed = true
	}
	if !changed {
		return nil
	}
	encoded, err := json.Marshal(staged)
	if err != nil {
		return err
	}
	return atomicWrite(state, string(encoded))
}

func addNoteAssets(bundle siteBundle, public, articleSource string, assets []string) error {
	var article anchor.Article
	if err := readSiteJSON(articleSource, &article); err != nil {
		return err
	}
	registered := make(map[string]bool, len(assets))
	for _, id := range assets {
		registered[id] = true
	}
	prefix := bundle.base + "content-assets/"
	tokens := html.NewTokenizer(strings.NewReader(article.HTML))
	for kind := tokens.Next(); kind != html.ErrorToken; kind = tokens.Next() {
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		for _, attr := range tokens.Token().Attr {
			if attr.Key != "src" && attr.Key != "href" {
				continue
			}
			raw, ok := strings.CutPrefix(attr.Val, prefix)
			if !ok {
				continue
			}
			raw, _, _ = strings.Cut(raw, "#")
			raw, _, _ = strings.Cut(raw, "?")
			id, err := url.PathUnescape(raw)
			if err != nil || !validAssetPath(id) {
				return fmt.Errorf("无效的图片路径: %s", raw)
			}
			name := "content-assets/" + id
			if _, published := bundle.files[name]; published {
				continue
			}
			if !registered[id] {
				return fmt.Errorf("笔记引用的图片不存在: %s", id)
			}
			bundle.files[name] = siteFile{key: name, source: filepath.Join(public, filepath.FromSlash(name))}
		}
	}
	if err := tokens.Err(); !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
