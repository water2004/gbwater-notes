package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSearchSyncOnlySubmitsChangedDocuments(t *testing.T) {
	unchanged := SearchDocument{ID: "same", Text: "原正文"}
	before := SearchDocument{ID: "changed", Text: "上一版"}
	after := SearchDocument{ID: before.ID, Text: "新版"}
	var uploaded []indexedSearchDocument
	var removed []string
	var settings int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/indexes/notes/documents":
			if r.URL.Query().Get("fields") != "id,contentDigest" {
				t.Errorf("expected only ID and digest, got %s", r.URL.RawQuery)
			}
			entries := []map[string]string{
				{"id": unchanged.ID, "contentDigest": searchableDocument(unchanged).Digest},
				{"id": before.ID, "contentDigest": searchableDocument(before).Digest},
				{"id": "draft"},
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": entries, "total": len(entries)})
		case r.Method == http.MethodPatch && r.URL.Path == "/indexes/notes/settings":
			settings++
		case r.Method == http.MethodPost && r.URL.Path == "/indexes/notes/documents":
			if err := json.NewDecoder(r.Body).Decode(&uploaded); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte("{}"))
		case r.Method == http.MethodPost && r.URL.Path == "/indexes/notes/documents/delete-batch":
			if err := json.NewDecoder(r.Body).Decode(&removed); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client := NewSearchClient(server.URL, "")
	if err := client.syncDocuments(t.Context(), []SearchDocument{unchanged, before}, []string{"absent"}); err != nil {
		t.Fatal(err)
	}
	if settings != 0 || len(uploaded) != 0 || len(removed) != 0 {
		t.Fatal("no-op synchronization issued a write")
	}
	if err := client.syncDocuments(t.Context(), []SearchDocument{unchanged, after}, []string{"draft", "absent"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(uploaded, []indexedSearchDocument{searchableDocument(after)}) ||
		!reflect.DeepEqual(removed, []string{"draft"}) || settings != 1 {
		t.Fatalf("uploaded=%+v removed=%v settings=%d", uploaded, removed, settings)
	}
}
