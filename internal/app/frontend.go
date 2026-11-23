package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"gbwater/internal/anchor"
)

const frontendManifest = "site-publication.json"

var accountPages = map[string]string{
	"login": "登录", "register": "注册", "forgot": "找回密码",
	"admin": "工作台", "admin/notes": "笔记管理", "admin/assets": "图床",
	"admin/publish": "发布", "admin/settings": "站点设置", "admin/users": "用户管理",
	"admin/security": "注册与安全", "admin/editor": "编辑笔记",
}

type siteBundle struct {
	shell string
	base  string
	files map[string]siteFile
}

type siteFile struct {
	key    string
	source string
	page   func() ([]byte, error)
}

type siteNote struct {
	Title, Slug, Category, CategoryTitle, Course, CourseSlug, Excerpt string
	Draft                                                             bool
}

type pageLink struct{ Title, Href, Summary string }

type pageContent struct {
	Base, Brand, Title, Description, Revision string
	Links                                     []pageLink
	Article                                   template.HTML
}

var publicPage = template.Must(template.New("page").Parse(`
<div class="page-shell">
  <nav aria-label="主导航"><a href="{{.Base}}">{{.Brand}}</a> · <a href="{{.Base}}library/">笔记库</a></nav>
  <main id="main-content">
    <h1>{{.Title}}</h1>
    {{if .Description}}<p>{{.Description}}</p>{{end}}
    {{if .Revision}}<article class="note-article" data-annotation-revision="{{.Revision}}">{{.Article}}</article>{{end}}
    {{if .Links}}<ul>{{range .Links}}<li><a href="{{.Href}}">{{.Title}}</a>{{if .Summary}}<p>{{.Summary}}</p>{{end}}</li>{{end}}</ul>{{end}}
  </main>
</div>`))

func fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func frontendBundle(root string) string {
	if configured := os.Getenv("GBWATER_FRONTEND_BUNDLE"); configured != "" {
		return configured
	}
	return filepath.Join(root, "web", "compiled")
}

func readSiteBundle(root, base, api string) (siteBundle, error) {
	base, api, err := publicPaths(base, api)
	if err != nil {
		return siteBundle{}, err
	}
	dir := frontendBundle(root)
	html, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		return siteBundle{}, fmt.Errorf("读取 Vue 编译模板: %w", err)
	}
	shell := string(html)
	if !strings.Contains(shell, `<div id="app"></div>`) || !strings.Contains(shell, `<html lang="zh-CN">`) {
		return siteBundle{}, errors.New("Vue 编译模板缺少应用挂载点或页面配置")
	}
	shell = strings.NewReplacer(
		`<html lang="zh-CN">`, `<html lang="zh-CN" data-site-base="`+base+`" data-api-base="`+api+`">`,
		`"./assets/`, `"`+base+`assets/`,
		`"./favicon.svg"`, `"`+base+`favicon.svg"`,
	).Replace(shell)
	bundle := siteBundle{shell: shell, base: base, files: map[string]siteFile{}}
	err = fs.WalkDir(os.DirFS(dir), "assets", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if !entry.Type().IsRegular() || !validAssetPath(name) {
			return fmt.Errorf("无效的 Vue 资源路径 %q", name)
		}
		bundle.files[name] = siteFile{key: name, source: filepath.Join(dir, filepath.FromSlash(name))}
		return nil
	})
	if err != nil {
		return siteBundle{}, err
	}
	for name, title := range accountPages {
		bundle.addPage(name, pageContent{Title: title}, "", "")
	}
	return bundle, nil
}

func (b siteBundle) addPage(route string, page pageContent, articlePath, articleKey string) {
	page.Base = b.base
	key, _ := json.Marshal([]any{fingerprint([]byte(b.shell)), page, articleKey})
	b.files[path.Join(route, "index.html")] = siteFile{key: fingerprint(key), page: func() ([]byte, error) {
		content := page
		if articlePath != "" {
			var article anchor.Article
			if err := readSiteJSON(articlePath, &article); err != nil {
				return nil, err
			}
			if len(article.Revision) != 32 || article.HTML == "" {
				return nil, fmt.Errorf("正文缺少内容或版本: %s", articlePath)
			}
			content.Revision = article.Revision
			// This HTML comes from the Folia render worker after asset injection.
			content.Article = template.HTML(article.HTML)
		}
		title := content.Title
		if content.Brand != "" {
			title += " · " + content.Brand
		}
		html := b.shell
		if start := strings.Index(html, "<title>"); start >= 0 {
			end := start + strings.Index(html[start:], "</title>")
			if end > start {
				html = html[:start] + "<title>" + template.HTMLEscapeString(title) + html[end:]
			}
		}
		if content.Description != "" {
			const opening = `<meta name="description" content="`
			if start := strings.Index(html, opening); start >= 0 {
				start += len(opening)
				if end := strings.IndexByte(html[start:], '"'); end >= 0 {
					html = html[:start] + template.HTMLEscapeString(content.Description) + html[start+end:]
				}
			}
		}
		if content.Brand != "" {
			var body bytes.Buffer
			if err := publicPage.Execute(&body, content); err != nil {
				return nil, err
			}
			html = strings.Replace(html, `<div id="app"></div>`, `<div id="app">`+body.String()+"</div>", 1)
		}
		return []byte(html), nil
	}}
}

func readSiteJSON(filename string, output any) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}

func InstallFrontend(config Config) error {
	bundle, err := readSiteBundle(config.Root, config.BasePath, config.APIBasePath)
	if err != nil {
		return err
	}
	dist := filepath.Join(config.Root, "web", "dist")
	for name, file := range bundle.files {
		target := filepath.Join(dist, filepath.FromSlash(name))
		if file.source != "" && regularFile(target) {
			continue
		}
		if err := installSiteFile(target, file, true); err != nil {
			return err
		}
	}
	icon := filepath.Join(dist, "favicon.svg")
	if !regularFile(icon) {
		return installSiteFile(icon, siteFile{source: filepath.Join(config.Root, "web", "public", "favicon.svg")}, false)
	}
	return nil
}

func planPublicPages(b siteBundle, publicDir string, rendered RenderedPublication) error {
	var settings struct {
		Brand          struct{ Name string }
		Home           struct{ Title, Description string }
		Library, About struct{ Title, Description, Lead string }
	}
	var notes []siteNote
	rawSettings, err := os.ReadFile(filepath.Join(publicDir, "site-settings.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(rawSettings, &settings); err != nil {
		return err
	}
	categories, err := configuredCategories(rawSettings)
	if err != nil {
		return err
	}
	if err := readSiteJSON(filepath.Join(publicDir, "catalog.json"), &notes); err != nil {
		return err
	}
	brand := settings.Brand.Name
	if brand == "" {
		brand = "碧山秋云"
	}
	var subjects = map[string]pageLink{}
	var courses = map[string][]pageLink{}
	var subjectCourses = map[string][]pageLink{}
	var courseTitles = map[string]string{}
	for _, note := range notes {
		if note.Draft || !validSlug(note.Slug) || !validSlug(note.CourseSlug) || !validCategoryID(note.Category) {
			return fmt.Errorf("公开目录含有无效的笔记: %q", note.Slug)
		}
		subject := categories[note.Category].Name
		if subject == "" {
			subject = note.CategoryTitle
		}
		subjects[note.Category] = pageLink{Title: subject, Href: b.base + "library/" + note.Category + "/"}
		if _, exists := courses[note.CourseSlug]; !exists {
			subjectCourses[note.Category] = append(subjectCourses[note.Category], pageLink{Title: note.Course, Href: b.base + "courses/" + note.CourseSlug + "/"})
		}
		courses[note.CourseSlug] = append(courses[note.CourseSlug], pageLink{Title: note.Title, Href: b.base + "notes/" + note.Slug + "/", Summary: note.Excerpt})
		courseTitles[note.CourseSlug] = note.Course
		article := "note-content/" + note.Slug + ".json"
		digest := rendered.Files[article]
		if digest == "" {
			return fmt.Errorf("缺少正文产物: %s", note.Slug)
		}
		b.addPage("notes/"+note.Slug, pageContent{
			Brand: brand, Title: note.Title, Description: note.Excerpt,
			Links: []pageLink{{Title: note.Course, Href: b.base + "courses/" + note.CourseSlug + "/"}},
		}, filepath.Join(publicDir, filepath.FromSlash(article)), digest)
	}
	for id, configured := range categories {
		subjects[id] = pageLink{Title: configured.Name, Href: b.base + "library/" + id + "/"}
	}
	var categoryIDs []string
	for id := range subjects {
		categoryIDs = append(categoryIDs, id)
	}
	slices.Sort(categoryIDs)
	links := make([]pageLink, 0, len(subjects))
	for _, id := range categoryIDs {
		links = append(links, subjects[id])
		b.addPage("library/"+id, pageContent{Brand: brand, Title: subjects[id].Title, Links: subjectCourses[id]}, "", "")
	}
	for slug, notes := range courses {
		b.addPage("courses/"+slug, pageContent{Brand: brand, Title: courseTitles[slug], Links: notes}, "", "")
	}
	homeTitle := settings.Home.Title
	if homeTitle == "" {
		homeTitle = brand
	}
	libraryTitle := settings.Library.Title
	if libraryTitle == "" {
		libraryTitle = "笔记库"
	}
	aboutTitle := settings.About.Title
	if aboutTitle == "" {
		aboutTitle = "关于"
	}
	homeLinks := links
	if len(notes) > 0 {
		note := notes[0]
		homeLinks = append([]pageLink{{Title: note.Title, Href: b.base + "notes/" + note.Slug + "/", Summary: note.Excerpt}}, links...)
	}
	b.addPage("", pageContent{Brand: brand, Title: homeTitle, Description: settings.Home.Description, Links: homeLinks}, "", "")
	b.addPage("library", pageContent{Brand: brand, Title: libraryTitle, Description: settings.Library.Description, Links: links}, "", "")
	b.addPage("about", pageContent{Brand: brand, Title: aboutTitle, Description: settings.About.Lead}, "", "")
	b.addPage("search", pageContent{Brand: brand, Title: "搜索"}, "", "")
	b.addPage("account", pageContent{Brand: brand, Title: "我的账号"}, "", "")
	return nil
}

func regularFile(filename string) bool {
	stat, err := os.Stat(filename)
	return err == nil && stat.Mode().IsRegular()
}

func installSiteFile(target string, file siteFile, compare bool) error {
	var reader io.Reader
	if file.source != "" {
		input, err := os.Open(file.source)
		if err != nil {
			return err
		}
		defer input.Close()
		reader = input
	} else {
		data, err := file.page()
		if err != nil {
			return err
		}
		if compare {
			if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, data) {
				return nil
			}
		}
		reader = bytes.NewReader(data)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".publish-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, copyErr := io.Copy(temp, reader)
	if err := errors.Join(copyErr, temp.Chmod(0o644), temp.Close()); err != nil {
		return err
	}
	return os.Rename(temp.Name(), target)
}

func publishFrontend(ctx context.Context, root, base, api string, rendered RenderedPublication, assets []string, log io.Writer) error {
	bundle, err := readSiteBundle(root, base, api)
	if err != nil {
		return err
	}
	publicDir := filepath.Join(root, "web", "public")
	if err := planPublicPages(bundle, publicDir, rendered); err != nil {
		return err
	}
	for name, key := range rendered.Files {
		if !validAssetPath(name) || key == "" ||
			(name != "catalog.json" && !strings.HasPrefix(name, "note-content/") && !strings.HasPrefix(name, "generated/")) {
			return fmt.Errorf("无效的渲染产物路径: %s", name)
		}
		bundle.files[name] = siteFile{key: key, source: filepath.Join(publicDir, filepath.FromSlash(name))}
	}
	for _, id := range assets {
		if !validAssetPath(id) {
			return fmt.Errorf("无效的图床路径: %s", id)
		}
		name := "content-assets/" + id
		bundle.files[name] = siteFile{key: name, source: filepath.Join(publicDir, filepath.FromSlash(name))}
	}
	iconSource := filepath.Join(publicDir, "site-icon.svg")
	if !regularFile(iconSource) {
		iconSource = filepath.Join(publicDir, "favicon.svg")
	}
	for name, source := range map[string]string{
		"site-settings.json": filepath.Join(publicDir, "site-settings.json"), "favicon.svg": iconSource,
	} {
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		bundle.files[name] = siteFile{key: fingerprint(data), source: source}
	}
	dist := filepath.Join(root, "web", "dist")
	state := filepath.Join(root, "web", "src", "generated", frontendManifest)
	return applyPublication(ctx, dist, state, bundle.files, log)
}

func applyPublication(ctx context.Context, dist, state string, files map[string]siteFile, log io.Writer) error {
	previous := map[string]string{}
	if err := readSiteJSON(state, &previous); errors.Is(err, fs.ErrNotExist) {
		// The mounted output directory is owned by the publisher. Reconcile
		// existing artifact names once when there is no publication manifest.
		err := fs.WalkDir(os.DirFS(dist), ".", func(name string, entry fs.DirEntry, err error) error {
			if err == nil && entry.Type().IsRegular() {
				previous[name] = ""
			}
			return err
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	} else if err != nil {
		return err
	}
	next := make(map[string]string, len(files))
	var updates, obsolete []string
	pages, reused := 0, 0
	for name, file := range files {
		next[name] = file.key
		update := previous[name] != file.key || !regularFile(filepath.Join(dist, filepath.FromSlash(name)))
		if update {
			updates = append(updates, name)
		}
		if file.page != nil {
			pages++
			if !update {
				reused++
			}
		}
	}
	for name := range previous {
		if _, kept := next[name]; kept || name == "." {
			continue
		}
		if _, err := filepath.Localize(name); err == nil {
			obsolete = append(obsolete, name)
		}
	}
	if len(updates) > 0 || len(obsolete) > 0 {
		slices.SortFunc(updates, func(a, b string) int {
			if (files[a].page == nil) == (files[b].page == nil) {
				return strings.Compare(a, b)
			}
			if files[a].page == nil {
				return -1
			}
			return 1
		})
		if err := commitSiteFiles(ctx, dist, state, files, updates, obsolete); err != nil {
			return err
		}
		data, err := json.Marshal(next)
		if err != nil {
			return err
		}
		if err := atomicWrite(state, string(data)); err != nil {
			return err
		}
	}
	fmt.Fprintf(log, "[pages] rendered %d, reused %d\n[publish] updated %d, removed %d, kept %d\n",
		pages-reused, reused, len(updates), len(obsolete), len(files)-len(updates))
	return nil
}

func commitSiteFiles(ctx context.Context, dist, state string, files map[string]siteFile, updates, obsolete []string) error {
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(dist, ".publish-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, name := range updates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := installSiteFile(filepath.Join(stage, filepath.FromSlash(name)), files[name], false); err != nil {
			return fmt.Errorf("准备发布 %s: %w", name, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(state); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// Assets are installed before the pages that refer to them.
	for _, name := range updates {
		local := filepath.FromSlash(name)
		target := filepath.Join(dist, local)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(stage, local), target); err != nil {
			return fmt.Errorf("发布 %s: %w", name, err)
		}
	}
	for _, name := range obsolete {
		target := filepath.Join(dist, filepath.FromSlash(name))
		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		for dir := filepath.Dir(target); dir != dist; dir = filepath.Dir(dir) {
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 || os.Remove(dir) != nil {
				break
			}
		}
	}
	return nil
}
