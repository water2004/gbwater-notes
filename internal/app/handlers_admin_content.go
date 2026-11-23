package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

func (s *Server) getSiteSettings(w http.ResponseWriter, r *http.Request) {
	payload, err := s.history.PublicSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings_unavailable", "无法读取站点设置")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) updateSiteSettings(w http.ResponseWriter, r *http.Request) {
	var payload json.RawMessage
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.history.SavePublicSettings(r.Context(), payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	notes, err := s.store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "content_unavailable", err.Error())
		return
	}
	courses := map[string]bool{}
	drafts := 0
	for _, note := range notes {
		courses[note.Category+"/"+note.Course] = true
		if note.Draft {
			drafts++
		}
	}
	reading, readingErr := s.history.ReadingOverview()
	users, usersErr := s.users.Count(r.Context())
	if readingErr != nil || usersErr != nil {
		writeError(w, http.StatusInternalServerError, "overview_unavailable", "无法读取账号或阅读统计")
		return
	}
	slices.SortStableFunc(notes, func(a, b Note) int { return b.Modified.Compare(a.Modified) })
	writeJSON(w, http.StatusOK, map[string]any{"notes": len(notes), "courses": len(courses), "published": len(notes) - drafts, "drafts": drafts, "users": users, "recentNotes": notes[:min(len(notes), 6)], "reading": reading})
}

func (s *Server) notes(w http.ResponseWriter, _ *http.Request) {
	notes, err := s.store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "content_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
}

func (s *Server) readNote(w http.ResponseWriter, r *http.Request) {
	document, err := s.store.Read(r.URL.Query().Get("id"))
	if err != nil {
		failNote(w, err)
		return
	}
	writeJSON(w, http.StatusOK, document)
}

func failNote(w http.ResponseWriter, err error) {
	if errors.Is(err, errRevisionWrite) {
		log.Printf("commit Markdown: %v", err)
		writeError(w, http.StatusInternalServerError, "revision_write_failed", "保存文件或历史版本失败，请重新加载后核对")
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "note_not_found", "笔记不存在")
		return
	}
	if errors.Is(err, ErrRevisionConflict) {
		writeError(w, http.StatusConflict, "revision_conflict", "文件已被其他会话修改，请重新加载后再保存")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_note", err.Error())
}

func (s *Server) downloadNote(w http.ResponseWriter, r *http.Request) {
	document, err := s.store.Read(r.URL.Query().Get("id"))
	if err != nil {
		failNote(w, err)
		return
	}
	name := document.Note.Title + ".md"
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, document.Note.Modified, strings.NewReader(document.Content))
}

// readNoteNative 返回某篇笔记构建期生成的公式/图表元数据，供后台预览使用。
// 这些数据不进入公开静态资源，避免整批 TeX 源码被匿名下载。
func (s *Server) readNoteNative(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validNoteID(id) {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
		return
	}
	root := filepath.Join(s.config.Root, "web", "src", "generated", "notes")
	path := filepath.Join(root, filepath.FromSlash(id)+".json")
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(root)+string(os.PathSeparator)) {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
		return
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "native_not_found", "该笔记还没有预渲染数据")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) revisions(w http.ResponseWriter, r *http.Request) {
	noteID := strings.TrimSpace(r.URL.Query().Get("noteId"))
	if !validNoteID(noteID) {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
		return
	}
	records, err := s.history.RecentRevisions(r.Context(), noteID, 50)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": records})
}

func (s *Server) readRevision(w http.ResponseWriter, r *http.Request) {
	noteID := strings.TrimSpace(r.URL.Query().Get("noteId"))
	if !validNoteID(noteID) {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid_revision", "修订 id 无效")
		return
	}
	content, err := s.history.RevisionContent(r.Context(), noteID, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "revision_not_found", "笔记修订不存在")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (s *Server) refreshNoteSearch(document Document) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if document.Note.Draft {
		if err := s.search.DeleteDocuments(ctx, []string{document.Note.Slug}); err != nil {
			log.Printf("remove draft from Meilisearch: %v", err)
		}
	} else if err := s.search.IndexDocuments(ctx, []SearchDocument{searchDocument(document)}); err != nil {
		log.Printf("update Meilisearch document: %v", err)
	}
}

func (s *Server) updateNote(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID               string `json:"id"`
		Content          string `json:"content"`
		ExpectedRevision string `json:"expectedRevision"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	document, err := s.store.Update(input.ID, input.Content, input.ExpectedRevision)
	if err != nil {
		failNote(w, err)
		return
	}
	if document.Note.Revision != input.ExpectedRevision {
		s.refreshNoteSearch(document)
	}
	writeJSON(w, http.StatusOK, document)
}

var draftField = regexp.MustCompile(`(?m)^[ \t]*draft[ \t]*:[^\r\n]*`)

func (s *Server) setNoteDraft(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID               string `json:"id"`
		Draft            *bool  `json:"draft"`
		ExpectedRevision string `json:"expectedRevision"`
	}
	if err := decodeJSON(w, r, &input); err != nil || input.Draft == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "需要笔记 ID、当前版本及目标草稿状态")
		return
	}
	document, err := s.store.Read(input.ID)
	if err != nil {
		failNote(w, err)
		return
	}
	if input.ExpectedRevision != document.Note.Revision {
		failNote(w, ErrRevisionConflict)
		return
	}
	if *input.Draft != document.Note.Draft {
		// 仅编辑 frontmatter 中的草稿字段，保留 Markdown 正文和其他字段。
		content := document.Content
		end := 4 + strings.Index(content[4:], "\n---")
		if content[end-1] == '\r' {
			end--
		}
		header := content[:end]
		draft := "draft: " + strconv.FormatBool(*input.Draft)
		if draftField.MatchString(header) {
			header = draftField.ReplaceAllString(header, draft)
		} else {
			breakLine := "\n"
			if strings.HasPrefix(content, "---\r\n") {
				breakLine = "\r\n"
			}
			header += breakLine + draft
		}
		document, err = s.store.Update(input.ID, header+content[end:], input.ExpectedRevision)
		if err != nil {
			failNote(w, err)
			return
		}
		s.refreshNoteSearch(document)
	}
	writeJSON(w, http.StatusOK, map[string]Note{"note": document.Note})
}

func (s *Server) createNote(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Note Note   `json:"note"`
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if input.Body == "" {
		input.Body = "从这里开始撰写笔记。\n"
	}
	settings, err := s.history.PublicSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings_unavailable", "无法读取学科设置")
		return
	}
	categories, err := configuredCategories(settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid_settings", err.Error())
		return
	}
	category, ok := categories[input.Note.Category]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown_category", "请先在站点设置中添加这个学科")
		return
	}
	input.Note.CategoryTitle = category.Name
	document, err := s.store.Create(input.Note, input.Body)
	if err != nil {
		failNote(w, err)
		return
	}
	s.refreshNoteSearch(document)
	writeJSON(w, http.StatusCreated, document)
}

type buildResponse struct {
	BuildStatus
	Enabled bool `json:"enabled"`
}

func (s *Server) buildStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, buildResponse{BuildStatus: s.builds.Status(), Enabled: s.config.BuildEnabled})
}

func (s *Server) buildHistory(w http.ResponseWriter, r *http.Request) {
	builds, err := s.history.RecentBuilds(r.Context(), 20)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"builds": builds})
}

func (s *Server) startBuild(w http.ResponseWriter, r *http.Request) {
	if !s.config.BuildEnabled {
		writeError(w, http.StatusConflict, "build_disabled", "当前部署未启用站内构建，请在构建环境中生成静态产物")
		return
	}
	request := BuildRequest{}
	if r.Body != http.NoBody && decodeJSON(w, r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid_build", "构建请求格式不正确")
		return
	}
	if request.Mode == "" {
		request.Mode = BuildIncremental
	}
	switch request.Mode {
	case BuildNote:
		if !validNoteID(request.NoteID) {
			writeError(w, http.StatusBadRequest, "invalid_build", "请选择要重建的笔记")
			return
		}
		var exists bool
		if err := s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM notes WHERE id=$1)`, request.NoteID).Scan(&exists); err != nil {
			fail(w, err)
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "note_not_found", "该笔记已移除")
			return
		}
	case BuildIncremental, BuildAll:
		if request.NoteID != "" {
			writeError(w, http.StatusBadRequest, "invalid_build", "全站构建无需指定笔记")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid_build", "未知的构建方式")
		return
	}
	status, err := s.builds.Start(r.Context(), request)
	if err != nil {
		log.Printf("enqueue build: %v", err)
		writeError(w, http.StatusInternalServerError, "build_enqueue_failed", "无法将任务加入发布队列")
		return
	}
	writeJSON(w, http.StatusAccepted, buildResponse{BuildStatus: status, Enabled: s.config.BuildEnabled})
}
