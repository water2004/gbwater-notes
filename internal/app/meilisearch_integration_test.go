//go:build integration

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealMeilisearchFullTextAndFuzzy(t *testing.T) {
	key := os.Getenv("GBWATER_MEILISEARCH_KEY")
	if key == "" {
		t.Skip("GBWATER_MEILISEARCH_KEY is not configured")
	}
	store := NewContentStore(filepath.Join("..", "..", "content"))
	notes, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	documents := []SearchDocument{}
	for _, note := range notes {
		document, err := store.Read(note.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !note.Draft {
			documents = append(documents, searchDocument(document))
		}
	}
	client := NewSearchClient("http://127.0.0.1:7700", key)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := client.IndexDocuments(ctx, documents); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"傅里叶", "狄拉克", "薛定谔"} {
		var results []SearchResult
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
			results, err = client.Search(ctx, query, 10)
			if err == nil && len(results) > 0 {
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
		if err != nil || len(results) == 0 {
			t.Fatalf("query %q: results=%v, err=%v", query, results, err)
		}
	}
	fuzzy, err := client.Search(ctx, "傅立叶", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, result := range fuzzy {
		if result.Title == "Fourier分析" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fuzzy query did not find Fourier分析: %+v", fuzzy)
	}
}
