package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestMeilisearchSearchAndCache(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/indexes/notes/search" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		requests.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["q"] != "傅立叶" || body["matchingStrategy"] != "last" {
			t.Fatalf("unexpected search body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[{"id":"fourier-fen-xi","noteId":"math/数学分析B2/Fourier分析","title":"Fourier分析","course":"数学分析B2","category":"math","excerpt":"函数的 Fourier 级数","_formatted":{"title":"<mark>Fourier</mark>分析"},"_rankingScore":0.85}]}`))
	}))
	defer server.Close()
	client := NewSearchClient(server.URL, "search-key")
	results, err := client.Search(context.Background(), "傅立叶", 20)
	if err != nil || len(results) != 1 || results[0].Slug != "fourier-fen-xi" || results[0].Title != "Fourier分析" {
		t.Fatalf("results: %+v, %v", results, err)
	}
	if _, err := client.Search(context.Background(), "傅立叶", 20); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("expected one upstream request, got %d", requests.Load())
	}
}

func TestSearchDocumentPlainText(t *testing.T) {
	document := Document{Note: Note{ID: "physics/量子力学/狄拉克符号", Slug: "dirac", Title: "狄拉克符号", Course: "量子力学"}, Content: "---\ntitle: test\n---\n\n# 狄拉克符号\n\n内积 $\\inp{a}{b}$ 与 **算符**。"}
	indexed := searchDocument(document)
	if indexed.ID != "dirac" || indexed.NoteID != document.Note.ID {
		t.Fatalf("unexpected identifiers: %+v", indexed)
	}
	if indexed.Text == "" || contains(indexed.Text, "frontmatter") || contains(indexed.Text, "\\inp") {
		t.Fatalf("unexpected plain text: %q", indexed.Text)
	}
}

func contains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}
