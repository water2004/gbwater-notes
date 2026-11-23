package app

import (
	"context"
	"database/sql"
)

// InitializeContent registers the initial deployment's content once. Later
// note and image mutations keep the publication inputs current through the API.
func (s *Server) InitializeContent(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO publication_inventory (initialized) VALUES (TRUE) ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return err
	}
	notes, err := s.store.List()
	if err != nil {
		return err
	}
	for _, note := range notes {
		document, err := s.store.Read(note.ID)
		if err != nil {
			return err
		}
		if err := s.history.indexDocument(tx, document); err != nil {
			return err
		}
	}
	assets, err := s.assets.list(false, "")
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if _, err := tx.ExecContext(ctx, `INSERT INTO publication_assets (id) VALUES ($1) ON CONFLICT DO NOTHING`, asset.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (h *History) recordPublicationAsset(id string, present bool) error {
	var err error
	if present {
		_, err = h.db.Exec(`INSERT INTO publication_assets (id) VALUES ($1) ON CONFLICT DO NOTHING`, id)
	} else {
		_, err = h.db.Exec(`DELETE FROM publication_assets WHERE id = $1`, id)
	}
	return err
}

func (s *Server) publicationInputs(ctx context.Context) ([]Note, []string, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	notes, err := publicationNotes(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	assets, err := publicationAssetIDs(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	return notes, assets, tx.Commit()
}

func publicationAssetIDs(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM publication_assets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		assets = append(assets, id)
	}
	return assets, rows.Err()
}
