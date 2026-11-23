package app

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrRevisionConflict = errors.New("revision conflict")
var errRevisionWrite = errors.New("recording Markdown revision failed")

type Note struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Category      string    `json:"category"`
	CategoryTitle string    `json:"categoryTitle"`
	Course        string    `json:"course"`
	Slug          string    `json:"slug"`
	CourseSlug    string    `json:"courseSlug"`
	Order         int       `json:"order"`
	Draft         bool      `json:"draft"`
	Revision      string    `json:"revision"`
	Modified      time.Time `json:"modified"`
	Bytes         int64     `json:"bytes"`
}

type Document struct {
	Note    Note   `json:"note"`
	Content string `json:"content"`
}

type ContentStore struct {
	root   string
	mu     sync.RWMutex
	record func(Document, string, *Document) error
}

func NewContentStore(root string) *ContentStore { return &ContentStore{root: root} }

func validSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\:*?\"<>|") && strings.TrimSpace(value) == value
}

func validSlug(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func (s *ContentStore) path(id string) (string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 3 {
		return "", errors.New("笔记 id 必须包含分类、课程和标题")
	}
	if !validCategoryID(parts[0]) {
		return "", errors.New("学科网址标识无效")
	}
	for _, part := range parts {
		if !validSegment(part) {
			return "", errors.New("笔记路径包含无效字符")
		}
	}
	path := filepath.Join(s.root, filepath.FromSlash(id)+".md")
	if !strings.HasPrefix(path, filepath.Clean(s.root)+string(os.PathSeparator)) {
		return "", errors.New("笔记路径越界")
	}
	return path, nil
}

func revision(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:16])
}

func parseMetadata(id, content string, info fs.FileInfo) (Note, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	note := Note{ID: id, Order: 9999, Revision: revision(content)}
	if info != nil {
		note.Modified = info.ModTime()
		note.Bytes = info.Size()
	}
	if !strings.HasPrefix(normalized, "---\n") {
		return note, errors.New("Markdown 缺少 frontmatter")
	}
	end := strings.Index(normalized[4:], "\n---")
	if end < 0 {
		return note, errors.New("frontmatter 未闭合")
	}
	scanner := bufio.NewScanner(strings.NewReader(normalized[4 : 4+end]))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		switch strings.TrimSpace(key) {
		case "title":
			note.Title = value
		case "category":
			note.Category = value
		case "categoryTitle":
			note.CategoryTitle = value
		case "course":
			note.Course = value
		case "slug":
			note.Slug = value
		case "courseSlug":
			note.CourseSlug = value
		case "order":
			note.Order, _ = strconv.Atoi(value)
		case "draft":
			note.Draft = value == "true"
		}
	}
	if note.Title == "" || note.Category == "" || note.Course == "" {
		return note, errors.New("frontmatter 必须包含 title、category 和 course")
	}
	if note.Slug == "" {
		note.Slug = "note-" + revision(id)[:10]
	}
	if note.CourseSlug == "" {
		note.CourseSlug = "course-" + revision(note.Category + "/" + note.Course)[:10]
	}
	if !validSlug(note.Slug) || !validSlug(note.CourseSlug) {
		return note, errors.New("slug 和 courseSlug 只能包含小写字母、数字和连字符")
	}
	if expected := note.Category + "/" + note.Course + "/" + note.Title; expected != id {
		return note, fmt.Errorf("frontmatter 与文件路径不一致，应为 %s", expected)
	}
	return note, nil
}

func (s *ContentStore) List() ([]Note, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	notes := []Note{}
	if _, err := os.Stat(s.root); os.IsNotExist(err) {
		return notes, nil
	}
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		id := strings.TrimSuffix(filepath.ToSlash(relative), ".md")
		if _, err := s.path(id); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		note, err := parseMetadata(id, string(content), info)
		if err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		notes = append(notes, note)
		return nil
	})
	sort.Slice(notes, func(i, j int) bool {
		if notes[i].Category != notes[j].Category {
			return notes[i].Category < notes[j].Category
		}
		if notes[i].Course != notes[j].Course {
			return notes[i].Course < notes[j].Course
		}
		if notes[i].Order != notes[j].Order {
			return notes[i].Order < notes[j].Order
		}
		return notes[i].Title < notes[j].Title
	})
	return notes, err
}

func (s *ContentStore) Read(id string) (Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.read(id)
}

func (s *ContentStore) read(id string) (Document, error) {
	path, err := s.path(id)
	if err != nil {
		return Document{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Document{}, err
	}
	note, err := parseMetadata(id, string(content), info)
	return Document{Note: note, Content: string(content)}, err
}

func (s *ContentStore) slugTaken(slug, exceptID string) (bool, error) {
	taken := false
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		id := strings.TrimSuffix(filepath.ToSlash(relative), ".md")
		if id == exceptID {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		note, err := parseMetadata(id, string(content), nil)
		if err == nil && note.Slug == slug {
			taken = true
			return fs.SkipAll
		}
		return nil
	})
	if os.IsNotExist(err) {
		return false, nil
	}
	return taken, err
}

func atomicWrite(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".note-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err = temporary.WriteString(content); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (s *ContentStore) Update(id, content, expectedRevision string) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path(id)
	if err != nil {
		return Document{}, err
	}
	current, err := s.read(id)
	if err != nil {
		return Document{}, err
	}
	if expectedRevision == "" || current.Note.Revision != expectedRevision {
		return Document{}, ErrRevisionConflict
	}
	if current.Content == content {
		return current, nil
	}
	updated, err := parseMetadata(id, content, nil)
	if err != nil {
		return Document{}, err
	}
	taken, err := s.slugTaken(updated.Slug, id)
	if err != nil {
		return Document{}, err
	}
	if taken {
		return Document{}, errors.New("这个英文 slug 已被其他笔记使用")
	}
	return s.write(path, id, content, "update", &current)
}

func (s *ContentStore) Create(note Note, body string) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := note.Category + "/" + note.Course + "/" + note.Title
	path, err := s.path(id)
	if err != nil {
		return Document{}, err
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return Document{}, errors.New("这篇笔记已经存在")
	}
	if note.CategoryTitle == "" {
		defaults, err := configuredCategories(json.RawMessage("{}"))
		if err != nil {
			return Document{}, err
		}
		note.CategoryTitle = defaults[note.Category].Name
		if note.CategoryTitle == "" {
			note.CategoryTitle = note.Category
		}
	}
	if note.Slug == "" {
		note.Slug = "note-" + revision(id)[:10]
	}
	if note.CourseSlug == "" {
		note.CourseSlug = "course-" + revision(note.Category + "/" + note.Course)[:10]
	}
	quote := func(value string) string { encoded, _ := json.Marshal(value); return string(encoded) }
	content := fmt.Sprintf("---\ntitle: %s\ncategory: %s\ncategoryTitle: %s\ncourse: %s\nslug: %s\ncourseSlug: %s\norder: %d\ndraft: %t\n---\n\n%s", quote(note.Title), quote(note.Category), quote(note.CategoryTitle), quote(note.Course), quote(note.Slug), quote(note.CourseSlug), note.Order, note.Draft, body)
	created, err := parseMetadata(id, content, nil)
	if err != nil {
		return Document{}, err
	}
	taken, err := s.slugTaken(created.Slug, id)
	if err != nil {
		return Document{}, err
	}
	if taken {
		return Document{}, errors.New("这个英文 slug 已被其他笔记使用")
	}
	return s.write(path, id, content, "create", nil)
}

func (s *ContentStore) write(path, id, content, action string, before *Document) (Document, error) {
	if err := atomicWrite(path, content); err != nil {
		return Document{}, err
	}
	document, err := s.read(id)
	if err == nil && s.record != nil {
		err = s.record(document, action, before)
	}
	if err == nil {
		return document, nil
	}
	var rollbackErr error
	if before == nil {
		rollbackErr = os.Remove(path)
	} else {
		rollbackErr = atomicWrite(path, before.Content)
	}
	return Document{}, errors.Join(errRevisionWrite, err, rollbackErr)
}
