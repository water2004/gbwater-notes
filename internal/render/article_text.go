package render

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gbwater/internal/app"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func parseArticle(fragment string) ([]*html.Node, error) {
	context := &html.Node{Type: html.ElementNode, Data: "article", DataAtom: atom.Article}
	return html.ParseFragment(strings.NewReader(fragment), context)
}

func prose(node *html.Node, text *strings.Builder) {
	if node.Type == html.TextNode {
		text.WriteString(node.Data)
		return
	}
	if node.Type != html.ElementNode {
		return
	}
	switch node.DataAtom {
	case atom.Code, atom.Pre, atom.Figure, atom.Img, atom.Svg, atom.Script, atom.Style, atom.Template, atom.Noscript:
		return
	}
	for _, attribute := range node.Attr {
		if attribute.Key == "hidden" || (attribute.Key == "role" && attribute.Val == "math") {
			return
		}
	}
	switch node.DataAtom {
	case atom.A, atom.Strong, atom.Em, atom.Span, atom.Mark, atom.Del, atom.Sup, atom.Sub:
	default:
		text.WriteByte(' ')
		defer text.WriteByte(' ')
	}
	for child := range node.ChildNodes() {
		prose(child, text)
	}
}

func headingLevel(node *html.Node) int {
	if node.Type == html.ElementNode && len(node.Data) == 2 && node.Data[0] == 'h' && '1' <= node.Data[1] && node.Data[1] <= '6' {
		return int(node.Data[1] - '0')
	}
	return 0
}

func headingTitle(node *html.Node) string {
	var text strings.Builder
	prose(node, &text)
	title := strings.Join(strings.Fields(text.String()), " ")
	if title == "" {
		return "章节"
	}
	return title
}

// Normalize IDs from rendered labels before publishing so that fragment URLs
// cannot pick up TeX or Markdown link destinations from the source heading.
func normalizeArticleHeadings(fragment string) (string, error) {
	nodes, err := parseArticle(fragment)
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	redirect := map[string]string{}
	for _, root := range nodes {
		setHeadingID(root, used, redirect)
		for node := range root.Descendants() {
			setHeadingID(node, used, redirect)
		}
	}
	var output bytes.Buffer
	for _, node := range nodes {
		rewriteHeadingLink(node, redirect)
		for child := range node.Descendants() {
			rewriteHeadingLink(child, redirect)
		}
		if err := html.Render(&output, node); err != nil {
			return "", err
		}
	}
	return output.String(), nil
}

func setHeadingID(node *html.Node, used map[string]bool, redirect map[string]string) {
	if headingLevel(node) == 0 {
		return
	}
	base := strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' {
			return unicode.ToLower(character)
		}
		return '-'
	}, headingTitle(node))
	base = strings.Join(strings.FieldsFunc(base, func(character rune) bool { return character == '-' }), "-")
	if base == "" {
		base = "section"
	}
	id := base
	for number := 1; used[id]; number++ {
		id = base + "-" + strconv.Itoa(number)
	}
	used[id] = true
	for index, attribute := range node.Attr {
		if attribute.Key == "id" {
			redirect["#"+attribute.Val] = "#" + id
			node.Attr[index].Val = id
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: "id", Val: id})
}

func rewriteHeadingLink(node *html.Node, redirect map[string]string) {
	if node.DataAtom != atom.A {
		return
	}
	for index, attribute := range node.Attr {
		if href, ok := redirect[attribute.Val]; attribute.Key == "href" && ok {
			node.Attr[index].Val = href
		}
	}
}

func buildCatalog(note app.Note, fragment string) (app.CatalogNote, error) {
	nodes, err := parseArticle(fragment)
	if err != nil {
		return app.CatalogNote{}, err
	}
	var text strings.Builder
	headings := []app.Heading{}
	for _, root := range nodes {
		prose(root, &text)
		headings = appendHeading(headings, root)
		for node := range root.Descendants() {
			headings = appendHeading(headings, node)
		}
	}
	content := strings.Join(strings.Fields(text.String()), " ")
	characters := utf8.RuneCountInString(content)
	excerpt := content
	if characters > 180 {
		excerpt = string([]rune(content)[:180])
	}
	return app.CatalogNote{
		ID: note.ID, Title: note.Title, Category: note.Category, CategoryTitle: note.CategoryTitle,
		Course: note.Course, Slug: note.Slug, CourseSlug: note.CourseSlug, Order: note.Order, Draft: note.Draft,
		Excerpt: excerpt, Characters: characters, ReadingMinutes: max(1, (characters+399)/400), Headings: headings,
	}, nil
}

func appendHeading(headings []app.Heading, node *html.Node) []app.Heading {
	level := headingLevel(node)
	if level == 0 || level > 3 {
		return headings
	}
	for _, attribute := range node.Attr {
		if attribute.Key == "id" {
			return append(headings, app.Heading{Level: level, Title: headingTitle(node), Slug: attribute.Val})
		}
	}
	return headings
}
