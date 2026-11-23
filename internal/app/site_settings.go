package app

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed default_categories.json
var defaultCategoriesJSON []byte

type categorySettings struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

var categoryIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var reservedCategoryID = regexp.MustCompile(`^(con|prn|aux|nul|com[1-9]|lpt[1-9]|constructor|prototype)$`)

const pngDataPrefix = "data:image/png;base64,"

func siteIconSVG(raw json.RawMessage) (string, error) {
	var source string
	if len(raw) == 0 {
		return "", nil
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return "", errors.New("站点图标格式无效")
	}
	if source == "" {
		return "", nil
	}
	encoded, ok := strings.CutPrefix(source, pngDataPrefix)
	if !ok || len(encoded) > base64.StdEncoding.EncodedLen(80<<10) {
		return "", errors.New("站点图标必须是 80 KiB 以内的 PNG")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) > 80<<10 {
		return "", errors.New("站点图标格式无效")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width > 128 || config.Height > 128 {
		return "", errors.New("站点图标必须是 128 × 128 像素以内的 PNG")
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return "", errors.New("站点图标 PNG 已损坏")
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128"><image width="128" height="128" href="` +
		pngDataPrefix + base64.StdEncoding.EncodeToString(data) + `"/></svg>`, nil
}

func validCategoryID(id string) bool {
	return categoryIDPattern.MatchString(id) && !reservedCategoryID.MatchString(id)
}

func configuredCategories(payload json.RawMessage) (map[string]categorySettings, error) {
	var categories map[string]categorySettings
	var settings struct {
		Categories map[string]json.RawMessage `json:"categories"`
	}
	if err := json.Unmarshal(defaultCategoriesJSON, &categories); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &settings); err != nil {
		return nil, err
	}
	for id, raw := range settings.Categories {
		category := categories[id]
		if err := json.Unmarshal(raw, &category); err != nil {
			return nil, err
		}
		category.Name = strings.TrimSpace(category.Name)
		if !validCategoryID(id) || category.Name == "" {
			return nil, errors.New("学科需要有效的网址标识和显示名称")
		}
		categories[id] = category
	}
	return categories, nil
}

func (h *History) PublicSettings(ctx context.Context) (json.RawMessage, error) {
	var payload []byte
	err := h.db.QueryRowContext(ctx, `SELECT payload FROM site_settings WHERE settings_key = 'public'`).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, err
	}
	if !json.Valid(payload) {
		return nil, errors.New("站点设置 JSON 已损坏")
	}
	return json.RawMessage(payload), nil
}

func (h *History) SavePublicSettings(ctx context.Context, payload json.RawMessage) error {
	if len(payload) == 0 || len(payload) > 256<<10 {
		return errors.New("站点设置为空或超过 256 KB")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil || object == nil {
		return errors.New("站点设置必须是 JSON 对象")
	}
	if _, err := configuredCategories(payload); err != nil {
		return err
	}
	if _, err := siteIconSVG(object["favicon"]); err != nil {
		return err
	}
	_, err := h.db.ExecContext(ctx, `INSERT INTO site_settings(settings_key, payload, updated_at) VALUES('public', $1, $2)
		ON CONFLICT(settings_key) DO UPDATE SET payload=EXCLUDED.payload, updated_at=EXCLUDED.updated_at`, payload, time.Now())
	return err
}

func (h *History) ExportPublicSettings(ctx context.Context, path string) error {
	payload, err := h.PublicSettings(ctx)
	if err != nil {
		return err
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(payload, &settings); err != nil {
		return err
	}
	icon, err := siteIconSVG(settings["favicon"])
	if err != nil {
		return err
	}
	iconPath := filepath.Join(filepath.Dir(path), "site-icon.svg")
	if icon == "" {
		if err := os.Remove(iconPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	} else if err := exportSettingsFile(iconPath, []byte(icon)); err != nil {
		return err
	}
	delete(settings, "favicon")
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return exportSettingsFile(path, append(data, '\n'))
}

func exportSettingsFile(path string, data []byte) error {
	if previous, err := os.ReadFile(path); err == nil && bytes.Equal(previous, data) {
		return nil
	}
	return atomicWrite(path, string(data))
}
