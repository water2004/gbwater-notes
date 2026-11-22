package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	assetDir      = "web/public/content-assets"
	assetTrashDir = "data/asset-trash"
	maxAssetBytes = 10 << 20
)

var imageTypes = map[string]string{
	".png": "image/png", ".jpeg": "image/jpeg", ".jpg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml",
}

type Asset struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Bytes    int64     `json:"bytes"`
	Modified time.Time `json:"modified"`
}

type AssetStore struct {
	root string
	mu   sync.Mutex
}

func assetInfo(id string, info fs.FileInfo) Asset {
	return Asset{ID: id, Name: path.Base(id), Bytes: info.Size(), Modified: info.ModTime()}
}

func validAssetPath(id string) bool {
	if len(id) > 1024 || !fs.ValidPath(id) || id == "." {
		return false
	}
	for _, part := range strings.Split(id, "/") {
		if !validSegment(part) || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") ||
			strings.ContainsFunc(part, unicode.IsControl) || reservedCategoryID.MatchString(strings.ToLower(strings.SplitN(part, ".", 2)[0])) {
			return false
		}
	}
	return true
}

func assetFolder(trash bool) string {
	if trash {
		return assetTrashDir
	}
	return assetDir
}

func (s *AssetStore) list(trash bool, noteID string) ([]Asset, error) {
	items := []Asset{}
	root, err := os.OpenRoot(filepath.Join(s.root, assetFolder(trash)))
	if errors.Is(err, fs.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := root.FS()
	if noteID != "" {
		folder, err := root.OpenRoot(noteID)
		if errors.Is(err, fs.ErrNotExist) {
			return items, nil
		}
		if err != nil {
			return nil, err
		}
		defer folder.Close()
		files = folder.FS()
	}
	err = fs.WalkDir(files, ".", func(relative string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		id := path.Join(noteID, relative)
		if !entry.Type().IsRegular() || !validAssetPath(id) || imageTypes[strings.ToLower(path.Ext(id))] == "" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		items = append(items, assetInfo(id, info))
		return nil
	})
	slices.SortFunc(items, func(a, b Asset) int {
		if cmp := b.Modified.Compare(a.Modified); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ID, b.ID)
	})
	return items, err
}

func (s *AssetStore) open(id string, trash bool) (*os.File, fs.FileInfo, error) {
	root, err := os.OpenRoot(filepath.Join(s.root, assetFolder(trash)))
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	info, err := root.Lstat(id)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fs.ErrNotExist
	}
	file, err := root.Open(id)
	return file, info, err
}

func uploadFileName(name, contentType string) (string, error) {
	var ext string
	switch contentType {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	default:
		return "", apiErr(http.StatusUnsupportedMediaType, "unsupported_image", "请上传 PNG、JPEG、GIF 或 WebP 图片")
	}
	name = strings.TrimSpace(strings.TrimSuffix(name, path.Ext(name)))
	name = strings.Trim(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name), "-")
	runes := []rune(name)
	if len(runes) > 48 {
		name = string(runes[:48])
	}
	if name == "" || reservedCategoryID.MatchString(strings.ToLower(name)) {
		name = "image"
	}
	return name + ext, nil
}

func (s *AssetStore) add(folder, name string, data []byte) (Asset, error) {
	name, err := uploadFileName(name, http.DetectContentType(data))
	if err != nil {
		return Asset{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return Asset{}, err
	}
	defer root.Close()
	if err := root.MkdirAll(path.Join(assetDir, folder), 0o755); err != nil {
		return Asset{}, err
	}
	id := path.Join(folder, name)
	for copy := 2; ; copy++ {
		available := true
		for _, dir := range []string{assetDir, assetTrashDir} {
			if _, err := root.Lstat(path.Join(dir, id)); !errors.Is(err, fs.ErrNotExist) {
				if err != nil {
					return Asset{}, err
				}
				available = false
			}
		}
		if available {
			break
		}
		ext := path.Ext(name)
		id = path.Join(folder, fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, ext), copy, ext))
	}
	tempID, err := newID()
	if err != nil {
		return Asset{}, err
	}
	// 暂存于发布目录之外，构建只会读到写完的图片。
	if err := root.MkdirAll("data", 0o755); err != nil {
		return Asset{}, err
	}
	temp := path.Join("data", ".asset-"+tempID)
	defer root.Remove(temp)
	if err := root.WriteFile(temp, data, 0o644); err != nil {
		return Asset{}, err
	}
	target := path.Join(assetDir, id)
	if err := root.Rename(temp, target); err != nil {
		return Asset{}, err
	}
	info, err := root.Stat(target)
	if err != nil {
		return Asset{}, err
	}
	return assetInfo(id, info), nil
}

func (s *AssetStore) move(id string, restore bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return err
	}
	defer root.Close()
	source := path.Join(assetFolder(restore), id)
	target := path.Join(assetFolder(!restore), id)
	file, err := root.Lstat(source)
	if err != nil {
		return err
	}
	if !file.Mode().IsRegular() {
		return fs.ErrNotExist
	}
	if _, err := root.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			return err
		}
		return apiErr(http.StatusConflict, "asset_exists", "目标位置已有同名图片")
	}
	if err := root.MkdirAll(path.Dir(target), 0o755); err != nil {
		return err
	}
	return root.Rename(source, target)
}

func readAssetUpload(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAssetBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, apiErr(http.StatusRequestEntityTooLarge, "image_too_large", "单张图片最大 10 MB")
	}
	return data, err
}

type AssetReference struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func hasAssetReference(body, id string) bool {
	const prefix = "/content-assets/"
	const delimiters = " \t\r\n\"'<>)]}?#"
	for {
		_, rest, match := strings.Cut(body, prefix)
		if !match {
			return false
		}
		if strings.HasPrefix(rest, id) && (len(rest) == len(id) || strings.ContainsRune(delimiters, rune(rest[len(id)]))) {
			return true
		}
		end := strings.IndexAny(rest, delimiters)
		if end < 0 {
			end = len(rest)
		}
		if decoded, err := url.PathUnescape(rest[:end]); err == nil && decoded == id {
			return true
		}
		body = rest
	}
}

// assetReferences 读取当前 Markdown 中的站内图片路径，调用方持有内容锁。
func (s *ContentStore) assetReferences(id string, firstOnly bool) ([]AssetReference, error) {
	references := []AssetReference{}
	err := filepath.WalkDir(s.root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() || filepath.Ext(file) != ".md" {
			return nil
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if hasAssetReference(string(data), id) {
			relative, err := filepath.Rel(s.root, file)
			if err != nil {
				return err
			}
			references = append(references, AssetReference{
				ID:    strings.TrimSuffix(filepath.ToSlash(relative), ".md"),
				Title: strings.TrimSuffix(filepath.Base(file), ".md"),
			})
			if firstOnly {
				return fs.SkipAll
			}
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		// 没有内容目录时允许管理共享图床。
		if _, statErr := os.Stat(s.root); os.IsNotExist(statErr) {
			return references, nil
		}
	}
	return references, err
}

func validUploadName(name string) bool {
	return name != "" && len(name) <= 1024 && utf8.ValidString(name)
}
