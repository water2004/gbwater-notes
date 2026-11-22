package app

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxQuoteRunes   = 500
	maxContextRunes = 80
	maxCommentRunes = 4000
	maxMemoRunes    = 20000
	maxHeadingRunes = 200
	maxLabelRunes   = 120
	maxStickyRunes  = 4000
	annotationLimit = 120
)

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

func tailRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[len(runes)-limit:])
}

func validHighlightColor(value string) bool {
	switch value {
	case HighlightYellow, HighlightGreen, HighlightBlue, HighlightPink:
		return true
	}
	return false
}

func (s *Server) listAnnotations(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	noteID := strings.TrimSpace(r.URL.Query().Get("noteId"))
	if !validNoteID(noteID) {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
		return
	}
	highlights, err := s.annotations.Highlights(r.Context(), user.ID, noteID)
	if err != nil {
		fail(w, err)
		return
	}
	bookmarks, err := s.annotations.Bookmarks(r.Context(), user.ID, noteID)
	if err != nil {
		fail(w, err)
		return
	}
	memo, err := s.annotations.Memo(r.Context(), user.ID, noteID)
	if err != nil {
		fail(w, err)
		return
	}
	stickyNotes, err := s.annotations.StickyNotes(r.Context(), user.ID, noteID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"highlights": highlights, "bookmarks": bookmarks, "memo": memo, "stickyNotes": stickyNotes,
	})
}

func (s *Server) createHighlight(w http.ResponseWriter, r *http.Request) {
	if !s.throttleUser(w, r, "annotations", annotationLimit, time.Minute) {
		return
	}
	var input struct {
		NoteID   string `json:"noteId"`
		Quote    string `json:"quote"`
		Prefix   string `json:"prefix"`
		Suffix   string `json:"suffix"`
		Heading  string `json:"heading"`
		Color    string `json:"color"`
		Comment  string `json:"comment"`
		Revision string `json:"revision"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	if !validNoteID(input.NoteID) {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
		return
	}
	quote := truncateRunes(input.Quote, maxQuoteRunes)
	if quote == "" {
		writeError(w, http.StatusBadRequest, "invalid_quote", "请先选中要划线的文字")
		return
	}
	color := input.Color
	if color == "" {
		color = HighlightYellow
	}
	if !validHighlightColor(color) {
		writeError(w, http.StatusBadRequest, "invalid_color", "不支持的高亮颜色")
		return
	}
	id, err := newID()
	if err != nil {
		fail(w, err)
		return
	}
	item, err := s.annotations.CreateHighlight(r.Context(), s.currentUser(r).ID, Highlight{
		ID: id, NoteID: input.NoteID, Quote: quote,
		Prefix: tailRunes(input.Prefix, maxContextRunes), Suffix: truncateRunes(input.Suffix, maxContextRunes),
		Heading: truncateRunes(input.Heading, maxHeadingRunes), Color: color,
		Comment: truncateRunes(input.Comment, maxCommentRunes), Revision: truncateRunes(input.Revision, 64),
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"highlight": item})
}

func (s *Server) deleteHighlight(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.annotations.DeleteHighlight(r.Context(), s.currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "highlight_not_found", "划线不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) createBookmark(w http.ResponseWriter, r *http.Request) {
	if !s.throttleUser(w, r, "annotations", annotationLimit, time.Minute) {
		return
	}
	var input struct {
		NoteID   string  `json:"noteId"`
		Label    string  `json:"label"`
		Quote    string  `json:"quote"`
		Prefix   string  `json:"prefix"`
		Suffix   string  `json:"suffix"`
		Heading  string  `json:"heading"`
		Position float64 `json:"position"`
		Revision string  `json:"revision"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	if !validNoteID(input.NoteID) {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
		return
	}
	position := input.Position
	if position < 0 {
		position = 0
	}
	if position > 1 {
		position = 1
	}
	id, err := newID()
	if err != nil {
		fail(w, err)
		return
	}
	item, err := s.annotations.CreateBookmark(r.Context(), s.currentUser(r).ID, Bookmark{
		ID: id, NoteID: input.NoteID, Label: truncateRunes(input.Label, maxLabelRunes),
		Quote:  truncateRunes(input.Quote, maxQuoteRunes),
		Prefix: tailRunes(input.Prefix, maxContextRunes), Suffix: truncateRunes(input.Suffix, maxContextRunes),
		Heading: truncateRunes(input.Heading, maxHeadingRunes), Position: position,
		Revision: truncateRunes(input.Revision, 64),
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"bookmark": item})
}

func (s *Server) updateBookmark(w http.ResponseWriter, r *http.Request) {
	if !s.throttleUser(w, r, "annotations", annotationLimit, time.Minute) {
		return
	}
	var input struct {
		Label    *string  `json:"label"`
		Revision *string  `json:"revision"`
		Quote    *string  `json:"quote"`
		Prefix   *string  `json:"prefix"`
		Suffix   *string  `json:"suffix"`
		Heading  *string  `json:"heading"`
		Position *float64 `json:"position"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	patch := BookmarkPatch{}
	if input.Label != nil {
		value := truncateRunes(*input.Label, maxLabelRunes)
		patch.Label = &value
	}
	if input.Revision != nil {
		value := truncateRunes(*input.Revision, 64)
		patch.Revision = &value
	}
	if input.Quote != nil {
		value := truncateRunes(*input.Quote, maxQuoteRunes)
		patch.Quote = &value
	}
	if input.Prefix != nil {
		value := tailRunes(*input.Prefix, maxContextRunes)
		patch.Prefix = &value
	}
	if input.Suffix != nil {
		value := truncateRunes(*input.Suffix, maxContextRunes)
		patch.Suffix = &value
	}
	if input.Heading != nil {
		value := truncateRunes(*input.Heading, maxHeadingRunes)
		patch.Heading = &value
	}
	if input.Position != nil {
		value := *input.Position
		if value < 0 {
			value = 0
		}
		if value > 1 {
			value = 1
		}
		patch.Position = &value
	}
	item, err := s.annotations.UpdateBookmark(r.Context(), s.currentUser(r).ID, r.PathValue("id"), patch)
	if err != nil {
		fail(w, err)
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "bookmark_not_found", "书签不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bookmark": item})
}

func (s *Server) deleteBookmark(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.annotations.DeleteBookmark(r.Context(), s.currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "bookmark_not_found", "书签不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) saveMemo(w http.ResponseWriter, r *http.Request) {
	if !s.throttleUser(w, r, "annotations", annotationLimit, time.Minute) {
		return
	}
	var input struct {
		NoteID string `json:"noteId"`
		Body   string `json:"body"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	if !validNoteID(input.NoteID) {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
		return
	}
	if utf8.RuneCountInString(input.Body) > maxMemoRunes {
		writeError(w, http.StatusBadRequest, "memo_too_long", "便条内容过长")
		return
	}
	item, err := s.annotations.SaveMemo(r.Context(), s.currentUser(r).ID, input.NoteID, input.Body)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memo": item})
}

func (s *Server) annotationSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.annotations.Summary(r.Context(), s.currentUser(r).ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
func (s *Server) createStickyNote(w http.ResponseWriter, r *http.Request) {
	if !s.throttleUser(w, r, "annotations", annotationLimit, time.Minute) {
		return
	}
	var input struct {
		NoteID   string `json:"noteId"`
		Body     string `json:"body"`
		Color    string `json:"color"`
		Quote    string `json:"quote"`
		Prefix   string `json:"prefix"`
		Suffix   string `json:"suffix"`
		Heading  string `json:"heading"`
		Revision string `json:"revision"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	if !validNoteID(input.NoteID) {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
		return
	}
	body := strings.TrimSpace(input.Body)
	if body == "" || utf8.RuneCountInString(body) > maxStickyRunes {
		writeError(w, http.StatusBadRequest, "invalid_body", "便条内容不能为空，且不超过 4000 字")
		return
	}
	color := input.Color
	if color == "" {
		color = HighlightYellow
	}
	if !validHighlightColor(color) {
		writeError(w, http.StatusBadRequest, "invalid_color", "不支持的便条颜色")
		return
	}
	id, err := newID()
	if err != nil {
		fail(w, err)
		return
	}
	item, err := s.annotations.CreateSticky(r.Context(), s.currentUser(r).ID, StickyNote{
		ID: id, NoteID: input.NoteID, Body: body, Color: color,
		Quote:    truncateRunes(input.Quote, maxQuoteRunes),
		Prefix:   tailRunes(input.Prefix, maxContextRunes),
		Suffix:   truncateRunes(input.Suffix, maxContextRunes),
		Heading:  truncateRunes(input.Heading, maxHeadingRunes),
		Revision: truncateRunes(input.Revision, 64),
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"stickyNote": item})
}

func (s *Server) updateStickyNote(w http.ResponseWriter, r *http.Request) {
	if !s.throttleUser(w, r, "annotations", annotationLimit, time.Minute) {
		return
	}
	var input struct {
		Body  *string `json:"body"`
		Color *string `json:"color"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	patch := StickyPatch{}
	if input.Body != nil {
		body := strings.TrimSpace(*input.Body)
		if body == "" || utf8.RuneCountInString(body) > maxStickyRunes {
			writeError(w, http.StatusBadRequest, "invalid_body", "便条内容不能为空，且不超过 4000 字")
			return
		}
		patch.Body = &body
	}
	if input.Color != nil {
		if !validHighlightColor(*input.Color) {
			writeError(w, http.StatusBadRequest, "invalid_color", "不支持的便条颜色")
			return
		}
		patch.Color = input.Color
	}
	item, err := s.annotations.UpdateSticky(r.Context(), s.currentUser(r).ID, r.PathValue("id"), patch)
	if err != nil {
		fail(w, err)
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "sticky_not_found", "便条不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stickyNote": item})
}

func (s *Server) deleteStickyNote(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.annotations.DeleteSticky(r.Context(), s.currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "sticky_not_found", "便条不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
