package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Settings 目前只有阅读偏好，保留 JSON 结构便于后续扩展。
type UserSettings struct {
	Reading json.RawMessage `json:"reading"`
}

type UserSettingsStore struct{ db *sql.DB }

func NewUserSettingsStore(db *sql.DB) *UserSettingsStore { return &UserSettingsStore{db: db} }

func (s *UserSettingsStore) Get(ctx context.Context, userID string) (json.RawMessage, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT reading FROM user_settings WHERE user_id = $1`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(raw), nil
}

func (s *UserSettingsStore) Put(ctx context.Context, userID string, reading json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_settings(user_id, reading, updated_at)
		VALUES($1, $2, $3) ON CONFLICT (user_id) DO UPDATE
		SET reading = EXCLUDED.reading, updated_at = EXCLUDED.updated_at`,
		userID, string(reading), time.Now())
	return err
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	reading, err := s.settings.Get(r.Context(), s.currentUser(r).ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, UserSettings{Reading: reading})
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Reading json.RawMessage `json:"reading"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	reading := strings.TrimSpace(string(input.Reading))
	if reading == "" || reading == "null" {
		reading = "{}"
	}
	if len(reading) > 4096 {
		writeError(w, http.StatusBadRequest, "settings_too_large", "设置内容过大")
		return
	}
	if !json.Valid([]byte(reading)) {
		writeError(w, http.StatusBadRequest, "invalid_request", "设置格式无效")
		return
	}
	if err := s.settings.Put(r.Context(), s.currentUser(r).ID, json.RawMessage(reading)); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, UserSettings{Reading: json.RawMessage(reading)})
}
