package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

type SearchDocument struct {
	ID       string `json:"id"`
	NoteID   string `json:"noteId"`
	Title    string `json:"title"`
	Course   string `json:"course"`
	Category string `json:"category"`
	Text     string `json:"text"`
	Excerpt  string `json:"excerpt"`
}

type SearchResult struct {
	ID       string  `json:"id"`
	Slug     string  `json:"slug"`
	Title    string  `json:"title"`
	Course   string  `json:"course"`
	Category string  `json:"category"`
	Excerpt  string  `json:"excerpt"`
	Score    float64 `json:"score"`
}

type searchHit struct {
	SearchDocument
	Formatted map[string]string `json:"_formatted"`
	Score     float64           `json:"_rankingScore"`
}

type cachedSearch struct {
	results []SearchResult
	expires time.Time
}

type SearchClient struct {
	baseURL string
	key     string
	http    *http.Client
	mu      sync.Mutex
	cache   map[string]cachedSearch
}

func NewSearchClient(baseURL, key string) *SearchClient {
	return &SearchClient{
		baseURL: strings.TrimRight(baseURL, "/"), key: key, cache: map[string]cachedSearch{},
		http: &http.Client{Timeout: 1200 * time.Millisecond, Transport: &http.Transport{
			MaxIdleConns: 20, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second,
		}},
	}
}

func (c *SearchClient) request(ctx context.Context, method, path string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		request.Header.Set("Authorization", "Bearer "+c.key)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Meilisearch %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target)
}

func (c *SearchClient) Configure(ctx context.Context) error {
	settings := map[string]any{
		"searchableAttributes": []string{"title", "course", "text"},
		"displayedAttributes":  []string{"id", "noteId", "title", "course", "category", "excerpt"},
		"rankingRules":         []string{"words", "typo", "proximity", "attribute", "sort", "exactness"},
		"typoTolerance": map[string]any{
			"enabled":             true,
			"minWordSizeForTypos": map[string]int{"oneTypo": 2, "twoTypos": 5},
		},
		"pagination": map[string]int{"maxTotalHits": 100},
		"synonyms": map[string][]string{
			"傅里叶": {"傅立叶", "Fourier"}, "傅立叶": {"傅里叶", "Fourier"}, "Fourier": {"傅里叶", "傅立叶"},
			"狄拉克": {"Dirac"}, "Dirac": {"狄拉克"}, "薛定谔": {"Schrödinger", "Schrodinger"}, "Schrodinger": {"薛定谔"},
		},
	}
	return c.request(ctx, http.MethodPatch, "/indexes/notes/settings", settings, nil)
}

func (c *SearchClient) IndexDocuments(ctx context.Context, documents []SearchDocument) error {
	if len(documents) == 0 {
		return nil
	}
	if err := c.Configure(ctx); err != nil {
		return err
	}
	var target any
	return c.request(ctx, http.MethodPost, "/indexes/notes/documents?primaryKey=id", documents, &target)
}

func (c *SearchClient) DeleteDocuments(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return c.request(ctx, http.MethodPost, "/indexes/notes/documents/delete-batch", ids, nil)
}

func (c *SearchClient) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	query = strings.Join(strings.Fields(query), " ")
	if query == "" || limit <= 0 {
		return []SearchResult{}, nil
	}
	if len([]rune(query)) > 100 {
		return nil, errors.New("搜索关键词过长")
	}
	if limit > 40 {
		limit = 40
	}
	cacheKey := fmt.Sprintf("%s\x00%d", query, limit)
	c.mu.Lock()
	if cached, ok := c.cache[cacheKey]; ok && time.Now().Before(cached.expires) {
		results := cached.results
		c.mu.Unlock()
		return results, nil
	}
	c.mu.Unlock()
	body := map[string]any{
		"q": query, "limit": limit, "matchingStrategy": "last", "showRankingScore": true,
		"attributesToRetrieve":  []string{"id", "noteId", "title", "course", "category", "excerpt"},
		"attributesToHighlight": []string{"title"}, "highlightPreTag": "<mark>", "highlightPostTag": "</mark>",
		"attributesToCrop": []string{"excerpt"}, "cropLength": 32, "cropMarker": "…",
	}
	var response struct {
		Hits []searchHit `json:"hits"`
	}
	if err := c.request(ctx, http.MethodPost, "/indexes/notes/search", body, &response); err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(response.Hits))
	for _, hit := range response.Hits {
		title := hit.Title
		if formatted := hit.Formatted["title"]; formatted != "" {
			title = strings.ReplaceAll(strings.ReplaceAll(formatted, "<mark>", ""), "</mark>", "")
		}
		results = append(results, SearchResult{
			ID: hit.NoteID, Slug: hit.ID, Title: title, Course: hit.Course,
			Category: hit.Category, Excerpt: hit.Excerpt, Score: hit.Score,
		})
	}
	if len(results) == 0 {
		return results, nil
	}
	c.mu.Lock()
	if len(c.cache) >= 256 {
		for key := range c.cache {
			delete(c.cache, key)
			break
		}
	}
	c.cache[cacheKey] = cachedSearch{results: results, expires: time.Now().Add(2 * time.Minute)}
	c.mu.Unlock()
	return results, nil
}

var (
	frontmatterPattern = regexp.MustCompile(`(?s)\A---\n.*?\n---`)
	fencePattern       = regexp.MustCompile("(?s)```.*?```")
	imagePattern       = regexp.MustCompile(`!\[([^]]*)\]\([^)]+\)`)
	linkPattern        = regexp.MustCompile(`\[([^]]+)\]\([^)]+\)`)
	texCommandPattern  = regexp.MustCompile(`\\[A-Za-z]+`)
	markupPattern      = regexp.MustCompile(`[$#*_>` + "`" + `|{}\[\]]+`)
	containerPattern   = regexp.MustCompile(`(?m)^\s*(?::+\s*[^\n]*|-{3,}\s*)$`)
	spacePattern       = regexp.MustCompile(`\s+`)
)

func documentPlainText(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	text := frontmatterPattern.ReplaceAllString(normalized, " ")
	text = containerPattern.ReplaceAllString(text, " ")
	text = fencePattern.ReplaceAllString(text, " ")
	text = imagePattern.ReplaceAllString(text, " $1 ")
	text = linkPattern.ReplaceAllString(text, " $1 ")
	text = texCommandPattern.ReplaceAllString(text, " ")
	text = markupPattern.ReplaceAllString(text, " ")
	return strings.TrimSpace(spacePattern.ReplaceAllString(text, " "))
}

func searchDocument(document Document) SearchDocument {
	plain := documentPlainText(document.Content)
	excerpt := []rune(plain)
	if len(excerpt) > 260 {
		excerpt = excerpt[:260]
	}
	return SearchDocument{
		ID: document.Note.Slug, NoteID: document.Note.ID, Title: document.Note.Title,
		Course: document.Note.Course, Category: document.Note.Category,
		Text: plain, Excerpt: string(excerpt),
	}
}
