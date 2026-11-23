package anchor

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type Article struct {
	HTML     string `json:"html"`
	Revision string `json:"revision"`
}

// Normalize defines the character index shared by matching and the reader.
func Normalize(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '\uFEFF' {
			return -1
		}
		return r
	}, text)
}

// RenderedText reads the text a reader can select from a rendered article.
func RenderedText(fragment string) (string, error) {
	article := &html.Node{Type: html.ElementNode, Data: "article", DataAtom: atom.Article}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), article)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
			return
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "img", "svg", "script", "style", "template", "noscript":
				return
			}
			for _, attribute := range node.Attr {
				if attribute.Key == "hidden" {
					return
				}
			}
		}
		for child := range node.ChildNodes() {
			visit(child)
		}
	}
	for _, node := range nodes {
		visit(node)
	}
	return Normalize(text.String()), nil
}

func Revision(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:16])
}
