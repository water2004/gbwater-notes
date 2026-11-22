package app

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

type readingState struct {
	Progress  []ProgressRecord `json:"progress"`
	Favorites []FavoriteRecord `json:"favorites"`
}

func (s *Server) handleReadingState(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	progress, err := s.reading.ListProgress(r.Context(), user.ID)
	if err != nil {
		fail(w, err)
		return
	}
	favorites, err := s.reading.ListFavorites(r.Context(), user.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, readingState{Progress: progress, Favorites: favorites})
}

func (s *Server) handleSaveProgress(w http.ResponseWriter, r *http.Request) {
	var input struct {
		NoteID    string `json:"noteId"`
		Completed *bool  `json:"completed"`
		Read      bool   `json:"read"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	if input.Completed == nil && !input.Read {
		writeError(w, http.StatusBadRequest, "invalid_request", "至少需要更新已读状态或阅读时间")
		return
	}
	if err := s.reading.UpsertProgress(r.Context(), s.currentUser(r).ID, strings.TrimSpace(input.NoteID), input.Completed, input.Read); err != nil {
		if errors.Is(err, ErrInvalidNoteID) {
			writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteProgress(w http.ResponseWriter, r *http.Request) {
	noteID := strings.TrimSpace(r.URL.Query().Get("noteId"))
	if err := s.reading.DeleteProgress(r.Context(), s.currentUser(r).ID, noteID); err != nil {
		if errors.Is(err, ErrInvalidNoteID) {
			writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMergeProgress 供登录后把浏览器 localStorage 里的历史记录并入云端。
func (s *Server) handleMergeProgress(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Progress []struct {
			NoteID     string     `json:"noteId"`
			Completed  bool       `json:"completed"`
			LastReadAt *time.Time `json:"lastReadAt"`
			UpdatedAt  time.Time  `json:"updatedAt"`
		} `json:"progress"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	records := make([]ProgressRecord, 0, len(input.Progress))
	for _, item := range input.Progress {
		records = append(records, ProgressRecord{
			NoteID: item.NoteID, Completed: item.Completed,
			LastReadAt: item.LastReadAt, UpdatedAt: item.UpdatedAt,
		})
	}
	if err := s.reading.MergeProgress(r.Context(), s.currentUser(r).ID, records); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "merged": len(records)})
}

func (s *Server) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	var input struct {
		NoteID string `json:"noteId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	if err := s.reading.AddFavorite(r.Context(), s.currentUser(r).ID, strings.TrimSpace(input.NoteID)); err != nil {
		if errors.Is(err, ErrInvalidNoteID) {
			writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	noteID := strings.TrimSpace(r.URL.Query().Get("noteId"))
	if err := s.reading.RemoveFavorite(r.Context(), s.currentUser(r).ID, noteID); err != nil {
		if errors.Is(err, ErrInvalidNoteID) {
			writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMergeFavorites(w http.ResponseWriter, r *http.Request) {
	var input struct {
		NoteIDs []string `json:"noteIds"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	if err := s.reading.MergeFavorites(r.Context(), s.currentUser(r).ID, input.NoteIDs); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "merged": len(input.NoteIDs)})
}
