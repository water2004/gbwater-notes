package app

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSiteIconSVG(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	source := pngDataPrefix + base64.StdEncoding.EncodeToString(imageData.Bytes())
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	icon, err := siteIconSVG(data)
	if err != nil || !strings.Contains(icon, `href="`+source+`"`) {
		t.Fatalf("expected embedded PNG: %q, %v", icon, err)
	}
	for _, invalid := range []string{`"https://example.com/favicon.svg"`, `"data:image/svg+xml;base64,PHN2Zz4="`, `"data:image/png;base64,PHN2Zz4="`} {
		if _, err := siteIconSVG(json.RawMessage(invalid)); err == nil {
			t.Fatalf("accepted invalid icon: %s", invalid)
		}
	}
}

func TestPublicSettingsStorageAndExport(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	history := &History{db: db}
	defer history.Close()
	ctx := t.Context()
	mock.ExpectQuery(`SELECT payload FROM site_settings`).WillReturnError(sql.ErrNoRows)
	payload, err := history.PublicSettings(ctx)
	if err != nil || string(payload) != "{}" {
		t.Fatalf("default settings: %s, %v", payload, err)
	}
	mock.ExpectExec(`INSERT INTO site_settings`).WithArgs(json.RawMessage(`{"home":{"title":"自定义"}}`), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := history.SavePublicSettings(ctx, json.RawMessage(`{"home":{"title":"自定义"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := history.SavePublicSettings(ctx, json.RawMessage(`[]`)); err == nil {
		t.Fatal("expected non-object settings to be rejected")
	}
	mock.ExpectQuery(`SELECT payload FROM site_settings`).WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(`{"home":{"title":"自定义"}}`))
	path := filepath.Join(t.TempDir(), "generated", "site-settings.json")
	if err := history.ExportPublicSettings(ctx, path); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "{\"home\":{\"title\":\"自定义\"}}\n" {
		t.Fatalf("exported settings: %q, %v", content, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
