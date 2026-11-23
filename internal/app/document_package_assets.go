package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

func (s *Server) planPackageImages(assets map[string][]byte, registered []string, plan *packageImportPlan, changes *packageImportChanges) (map[string]string, error) {
	active := map[string]string{}
	reserved := map[string]bool{}
	for _, id := range registered {
		active[packagePathKey(id)] = id
	}
	ids := make([]string, 0, len(assets))
	for id := range assets {
		ids = append(ids, id)
		reserved[packagePathKey(id)] = true
	}
	slices.Sort(ids)
	mapped := map[string]string{}
	for _, id := range ids {
		data := assets[id]
		candidate := id
		ext := path.Ext(id)
		stem := strings.TrimSuffix(id, ext) + "-" + packageHash(data)[:16]
		for suffix := 1; ; suffix++ {
			key := packagePathKey(candidate)
			if candidate == id || !reserved[key] {
				free, identical, err := s.packageImageCandidate(candidate, data, active[key])
				if err != nil {
					return nil, err
				}
				if free || identical {
					mapped[id] = candidate
					reserved[key] = true
					if candidate != id {
						plan.RemappedAssets = append(plan.RemappedAssets, candidate)
					}
					if identical && active[key] != "" {
						plan.ReusedAssets++
					} else {
						plan.NewAssets++
						changes.assets = append(changes.assets, candidate)
						if free {
							changes.writeFiles[assetDir+"/"+candidate] = data
						}
					}
					break
				}
			}
			candidate = stem + ext
			if suffix > 1 {
				candidate = fmt.Sprintf("%s-%d%s", stem, suffix, ext)
			}
			if !validAssetPath(candidate) || suffix >= maxPackageEntries {
				return nil, packageConflict("无法为图片分配新路径 " + id)
			}
		}
	}
	return mapped, nil
}

func (s *Server) packageImageCandidate(id string, data []byte, registered string) (free, identical bool, err error) {
	if registered != "" && registered != id {
		return false, false, nil
	}
	existing, err := s.packageImageData(id, false)
	if err == nil {
		return false, bytes.Equal(existing, data), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		if _, ok := asAPIError(err); ok {
			return false, false, nil
		}
		return false, false, err
	}
	if registered != "" {
		return false, false, nil
	}
	file, _, err := s.assets.open(id, true)
	if err == nil {
		file.Close()
		return false, false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, false, err
	}
	return true, false, nil
}

func (s *Server) packageRetiredImages(ctx context.Context, tx *sql.Tx, registered []string, mapped map[string]string, notes []packageNoteChange) ([]string, error) {
	retired := []string{}
	keep := map[string]bool{}
	for _, id := range mapped {
		keep[id] = true
	}
	candidates := map[string]bool{}
	replaced := []string{}
	for _, change := range notes {
		if change.before == nil {
			continue
		}
		id := change.before.Note.ID
		replaced = append(replaced, id)
		for _, asset := range registered {
			if strings.HasPrefix(asset, id+"/") && !keep[asset] {
				candidates[asset] = true
			}
		}
	}
	if len(candidates) == 0 {
		return retired, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT content FROM notes WHERE NOT (id = ANY($1::text[]))`, replaced)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		for id := range candidates {
			if hasAssetReference(content, id) {
				delete(candidates, id)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id := range candidates {
		file, _, err := s.assets.open(id, false)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		file.Close()
		trash, _, err := s.assets.open(id, true)
		if err == nil {
			trash.Close()
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		retired = append(retired, id)
	}
	slices.Sort(retired)
	return retired, nil
}
