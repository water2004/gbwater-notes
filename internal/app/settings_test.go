package app

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

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
