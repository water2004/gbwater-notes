package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"time"
)

type packageConflictItem struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type packageNotePlan struct {
	Note   Note   `json:"note"`
	Action string `json:"action"`
}

type packageImportPlan struct {
	Digest         string                `json:"digest"`
	Notes          []packageNotePlan     `json:"notes"`
	NewNotes       int                   `json:"newNotes"`
	ReplaceNotes   int                   `json:"replaceNotes"`
	NewAssets      int                   `json:"newAssets"`
	ReusedAssets   int                   `json:"reusedAssets"`
	RemappedAssets []string              `json:"remappedAssets"`
	RecycledAssets []string              `json:"recycledAssets"`
	Categories     []string              `json:"categories"`
	Conflicts      []packageConflictItem `json:"conflicts"`
}

func (s *Server) exportDocumentPackage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(w, r, &input); err != nil || len(input.IDs) == 0 || len(input.IDs) >= maxPackageEntries {
		fail(w, invalidPackage("请选择要下载的笔记"))
		return
	}
	pack, err := s.collectDocumentPackage(r.Context(), input.IDs)
	if err != nil {
		fail(w, err)
		return
	}
	zip, err := encodeDocumentPackage(pack)
	if err != nil {
		fail(w, err)
		return
	}
	name := "GBwater-笔记-" + time.Now().Format("20060102")
	if len(pack.notes) == 1 {
		name = pack.notes[0].Note.Title
	}
	name += ".gbnotes.zip"
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(zip))
}

func (s *Server) collectDocumentPackage(ctx context.Context, ids []string) (documentPackage, error) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	s.assets.mu.Lock()
	defer s.assets.mu.Unlock()

	pack := documentPackage{categories: map[string]categorySettings{}, assets: map[string][]byte{}}
	incomplete := func(message string) error {
		return apiErr(http.StatusConflict, "document_package_incomplete", message)
	}
	registeredAssets, err := publicationAssetIDs(ctx, s.db)
	if err != nil {
		return pack, err
	}
	settings, err := s.history.PublicSettings(ctx)
	if err != nil {
		return pack, err
	}
	categories, err := configuredCategories(settings)
	if err != nil {
		return pack, err
	}
	registered := map[string]bool{}
	for _, id := range registeredAssets {
		registered[id] = true
	}
	assets := map[string]bool{}
	seen := map[string]bool{}
	size := 0
	for _, id := range ids {
		key := packagePathKey(id)
		if !validNoteID(id) || !validAssetPath(id) || strings.Count(id, "/") != 2 || seen[key] {
			return pack, invalidPackage("所选笔记包含无效或重复的路径")
		}
		seen[key] = true
		doc, err := s.store.read(id)
		if errors.Is(err, fs.ErrNotExist) {
			return pack, apiErr(http.StatusNotFound, "note_not_found", "找不到笔记 "+id)
		}
		if err != nil {
			return pack, err
		}
		content, links := packageAssetLinks(doc.Content, s.config.BasePath)
		for _, link := range links {
			if !registered[link] {
				return pack, incomplete("《" + doc.Note.Title + "》引用的配图不可用 " + link)
			}
			assets[link] = true
		}
		for _, asset := range registeredAssets {
			if strings.HasPrefix(asset, id+"/") {
				assets[asset] = true
			}
		}
		doc.Content = content
		size += len(content)
		if size > maxExpandedPackage || len(content) > maxPackageNote {
			return pack, oversizedPackage()
		}
		category := categories[doc.Note.Category]
		if category.Name == "" {
			category.Name = doc.Note.CategoryTitle
		}
		if category.Name == "" {
			category.Name = doc.Note.Category
		}
		pack.categories[doc.Note.Category] = category
		pack.notes = append(pack.notes, doc)
	}
	seen = map[string]bool{}
	for id := range assets {
		key := packagePathKey(id)
		if seen[key] {
			return pack, incomplete("所选图片中有同名路径 " + id)
		}
		seen[key] = true
		data, err := s.packageImageData(id, false)
		if err != nil {
			return pack, err
		}
		if !validPackageImage(id, data) {
			return pack, incomplete("图片格式不适合打包 " + id)
		}
		size += len(data)
		if size > maxExpandedPackage {
			return pack, oversizedPackage()
		}
		pack.assets[id] = data
	}
	return pack, nil
}

func (s *Server) packageImageData(id string, trash bool) ([]byte, error) {
	file, info, err := s.assets.open(id, trash)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info.Size() > maxAssetBytes {
		return nil, oversizedPackage()
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAssetBytes+1))
	if len(data) > maxAssetBytes {
		return nil, oversizedPackage()
	}
	return data, err
}

func (s *Server) importDocumentPackage(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPackageBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, oversizedPackage())
		} else {
			fail(w, invalidPackage("无法读取上传的文档包"))
		}
		return
	}
	digest := packageHash(raw)
	preview := r.URL.Query().Get("preview") == "true"
	expected := r.URL.Query().Get("digest")
	if !preview && len(expected) != 64 {
		fail(w, invalidPackage("请先预览本次选择的文档包"))
		return
	}
	pack, err := decodeDocumentPackage(raw)
	if err != nil {
		fail(w, err)
		return
	}
	plan, err := s.applyDocumentPackage(r.Context(), pack, digest, expected, preview, r.URL.Query().Get("replace") == "true")
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, plan)
}

type packageImportChanges struct {
	notes      []packageNoteChange
	assets     []string
	writeFiles map[string][]byte
	settings   json.RawMessage
}

type packageNoteChange struct {
	document Document
	before   *Document
}

func (s *Server) applyDocumentPackage(ctx context.Context, pack documentPackage, digest, expected string, preview, replace bool) (result packageImportPlan, err error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.assets.mu.Lock()
	defer s.assets.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: preview})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	plan, changes, err := s.planDocumentPackage(ctx, tx, pack, digest, preview)
	if preview || err != nil {
		return plan, err
	}
	if len(plan.Conflicts) > 0 {
		return plan, packageConflict("导入前的检查发现冲突，请重新预览")
	}
	if plan.Digest != expected {
		return plan, packageConflict("文档包或目标内容已变化，请重新预览")
	}
	if plan.ReplaceNotes > 0 && !replace {
		return plan, packageConflict("请确认替换已有笔记")
	}
	root, err := os.OpenRoot(s.config.Root)
	if err != nil {
		return plan, err
	}
	defer root.Close()
	var rollback []func() error
	defer func() {
		if err != nil {
			for index := len(rollback) - 1; index >= 0; index-- {
				err = errors.Join(err, rollback[index]())
			}
		}
	}()
	for target, data := range changes.writeFiles {
		if err := writePackageFile(root, target, data, false); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return plan, packageConflict("目标文件已存在 " + target)
			}
			return plan, err
		}
		rollback = append(rollback, func() error { return root.Remove(target) })
	}
	for _, change := range changes.notes {
		action := "create"
		if change.before != nil {
			action = "update"
			target := "content/" + change.document.Note.ID + ".md"
			if err := writePackageFile(root, target, []byte(change.document.Content), true); err != nil {
				return plan, err
			}
			rollback = append(rollback, func() error {
				return writePackageFile(root, target, []byte(change.before.Content), true)
			})
		}
		saved, err := s.store.read(change.document.Note.ID)
		if err != nil {
			return plan, err
		}
		if err := s.history.recordRevision(tx, saved, action, change.before); err != nil {
			return plan, err
		}
	}
	for _, id := range changes.assets {
		if _, err := tx.ExecContext(ctx, `INSERT INTO publication_assets (id) VALUES ($1) ON CONFLICT DO NOTHING`, id); err != nil {
			return plan, err
		}
	}
	for _, id := range plan.RecycledAssets {
		source, trash := path.Join(assetDir, id), path.Join(assetTrashDir, id)
		if err := root.MkdirAll(path.Dir(trash), 0o755); err != nil {
			return plan, err
		}
		if err := root.Rename(source, trash); err != nil {
			return plan, err
		}
		rollback = append(rollback, func() error { return root.Rename(trash, source) })
		if _, err := tx.ExecContext(ctx, `DELETE FROM publication_assets WHERE id=$1`, id); err != nil {
			return plan, err
		}
	}
	if len(changes.settings) != 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE site_settings SET payload=$1, updated_at=now() WHERE settings_key='public'`, changes.settings); err != nil {
			return plan, err
		}
	}
	return plan, tx.Commit()
}

func (s *Server) planDocumentPackage(ctx context.Context, tx *sql.Tx, pack documentPackage, digest string, preview bool) (packageImportPlan, packageImportChanges, error) {
	plan := packageImportPlan{Notes: []packageNotePlan{}, Categories: []string{}, Conflicts: []packageConflictItem{},
		RemappedAssets: []string{}, RecycledAssets: []string{}}
	changes := packageImportChanges{writeFiles: map[string][]byte{}}
	conflict := func(kind, id, reason string) {
		plan.Conflicts = append(plan.Conflicts, packageConflictItem{Kind: kind, ID: id, Reason: reason})
	}
	existing, err := publicationNotes(ctx, tx)
	if err != nil {
		return plan, changes, err
	}
	registeredAssets, err := publicationAssetIDs(ctx, tx)
	if err != nil {
		return plan, changes, err
	}
	mapped, err := s.planPackageImages(pack.assets, registeredAssets, &plan, &changes)
	if err != nil {
		return plan, changes, err
	}
	paths := map[string]string{}
	slugs := map[string]string{}
	for _, note := range existing {
		paths[packagePathKey(note.ID)] = note.ID
		slugs[note.Slug] = note.ID
	}
	existingVersions := map[string]string{}
	for _, doc := range pack.notes {
		id := doc.Note.ID
		doc.Content, _ = rewritePackageAssetLinks(doc.Content, "/", mapped)
		doc.Note.Revision = revision(doc.Content)
		key := packagePathKey(id)
		action := "create"
		var reason string
		var before *Document
		pathID, registered := paths[key]
		other := slugs[doc.Note.Slug]
		switch {
		case registered && pathID != id:
			reason = "与现有笔记路径仅大小写或字符形式不同"
		case other != "" && other != id:
			reason = "网址已被另一篇笔记使用"
		default:
			current, err := s.store.read(id)
			switch {
			case err == nil:
				existingVersions[id] = current.Note.Revision
				content, _ := packageAssetLinks(current.Content, s.config.BasePath)
				if registered {
					if content == doc.Content {
						action = "reuse"
					} else {
						action, before = "replace", &current
					}
				} else if content != doc.Content {
					reason = "同路径已有未登记文件，请先通过后台保存"
				}
			case errors.Is(err, fs.ErrNotExist) && !registered:
				changes.writeFiles["content/"+id+".md"] = []byte(doc.Content)
			case errors.Is(err, fs.ErrNotExist):
				reason = "该笔记已登记，请先恢复原文件"
			default:
				reason = "该笔记路径无法写入，请检查现有文件"
			}
		}
		if reason != "" {
			action = "conflict"
			conflict("note", id, reason)
		} else if action == "create" {
			plan.NewNotes++
			changes.notes = append(changes.notes, packageNoteChange{document: doc})
		} else if action == "replace" {
			plan.ReplaceNotes++
			changes.notes = append(changes.notes, packageNoteChange{document: doc, before: before})
		}
		plan.Notes = append(plan.Notes, packageNotePlan{Note: doc.Note, Action: action})
	}

	plan.RecycledAssets, err = s.packageRetiredImages(ctx, tx, registeredAssets, mapped, changes.notes)
	if err != nil {
		return plan, changes, err
	}
	changes.settings, plan.Categories, err = mergePackageCategories(ctx, tx, pack.categories, preview)
	if err != nil {
		return plan, changes, err
	}
	input, err := json.Marshal([4]any{digest, plan, existingVersions, mapped})
	if err == nil {
		plan.Digest = packageHash(input)
	}
	return plan, changes, err
}

func mergePackageCategories(ctx context.Context, tx *sql.Tx, incoming map[string]categorySettings, preview bool) (json.RawMessage, []string, error) {
	payload := json.RawMessage(`{}`)
	selectSQL := `SELECT payload FROM site_settings WHERE settings_key='public'`
	if !preview {
		if _, err := tx.ExecContext(ctx, `INSERT INTO site_settings (settings_key, payload, updated_at)
			VALUES ('public', '{}', now()) ON CONFLICT DO NOTHING`); err != nil {
			return nil, nil, err
		}
		selectSQL += " FOR UPDATE"
	}
	if err := tx.QueryRowContext(ctx, selectSQL).Scan(&payload); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	current, err := configuredCategories(payload)
	if err != nil {
		return nil, nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		return nil, nil, err
	}
	custom := map[string]json.RawMessage{}
	if raw := object["categories"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &custom); err != nil {
			return nil, nil, err
		}
	}
	if custom == nil {
		custom = map[string]json.RawMessage{}
	}
	added := []string{}
	for id, category := range incoming {
		if _, found := current[id]; found {
			continue
		}
		custom[id], _ = json.Marshal(category)
		added = append(added, id)
	}
	slices.Sort(added)
	if len(added) == 0 {
		return nil, added, nil
	}
	object["categories"], _ = json.Marshal(custom)
	result, err := json.Marshal(object)
	if len(result) > 256<<10 {
		return nil, added, invalidPackage("新增学科后，站点设置超过 256 KB")
	}
	return result, added, err
}

func writePackageFile(root *os.Root, target string, data []byte, replace bool) error {
	parent := path.Dir(target)
	if err := root.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	key, err := newID()
	if err != nil {
		return err
	}
	tmp := path.Join(parent, ".package-"+key)
	file, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if replace {
		return root.Rename(tmp, target)
	}
	return root.Link(tmp, target)
}
