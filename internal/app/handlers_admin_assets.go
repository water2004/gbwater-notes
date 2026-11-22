package app

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func (s *Server) listAssets(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	noteID := query.Get("noteId")
	if noteID != "" && (strings.Count(noteID, "/") != 2 || !validAssetPath(noteID)) {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记路径无效")
		return
	}
	assets, err := s.assets.list(query.Get("trash") == "true", noteID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": assets})
}

func (s *Server) uploadAsset(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	name := query.Get("name")
	if !validUploadName(name) {
		writeError(w, http.StatusBadRequest, "invalid_name", "图片文件名无效")
		return
	}
	folder := "uploads"
	if noteID := query.Get("noteId"); noteID != "" {
		if !validAssetPath(noteID) {
			writeError(w, http.StatusBadRequest, "invalid_note", "笔记路径无效")
			return
		}
		if _, err := s.store.Read(noteID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_note", "请先保存笔记，再上传到该笔记的目录")
			return
		}
		folder = noteID
	}
	data, err := readAssetUpload(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	asset, err := s.assets.add(folder, name, data)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"asset": asset})
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	contentType := imageTypes[strings.ToLower(path.Ext(id))]
	if !validAssetPath(id) || contentType == "" {
		writeError(w, http.StatusBadRequest, "invalid_asset", "图片路径无效")
		return
	}
	file, info, err := s.assets.open(id, r.URL.Query().Get("trash") == "true")
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusNotFound, "asset_not_found", "图片不存在")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) listAssetReferences(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !validAssetPath(id) || imageTypes[strings.ToLower(path.Ext(id))] == "" {
		writeError(w, http.StatusBadRequest, "invalid_asset", "图片路径无效")
		return
	}
	s.store.mu.RLock()
	references, err := s.store.assetReferences(id, false)
	s.store.mu.RUnlock()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"references": references})
}

func (s *Server) moveAsset(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID      string `json:"id"`
		Restore bool   `json:"restore"`
	}
	if err := decodeJSON(w, r, &input); err != nil || !validAssetPath(input.ID) || imageTypes[strings.ToLower(path.Ext(input.ID))] == "" {
		writeError(w, http.StatusBadRequest, "invalid_asset", "图片路径无效")
		return
	}
	if !input.Restore {
		s.store.mu.RLock()
		defer s.store.mu.RUnlock()
		references, err := s.store.assetReferences(input.ID, true)
		if err != nil {
			fail(w, err)
			return
		}
		if len(references) != 0 {
			writeError(w, http.StatusConflict, "asset_in_use", "《"+references[0].Title+"》仍在使用这张图片")
			return
		}
	}
	if err := s.assets.move(input.ID, input.Restore); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, "asset_not_found", "图片不存在")
		} else {
			fail(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
