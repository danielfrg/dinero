package store

import (
	"context"
	"github.com/danielfrg/dinero/internal/ledger"
)

// SQLiteSourcePath asks the connection for the main database filename, so URI
// normalization and environment configuration cannot diverge from the source.
func (s *Store) SQLiteSourcePath(ctx context.Context) (string, error) {
	if s.dialect != "sqlite" {
		return "", nil
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return "", &ledger.ErrUnavailable{}
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, path string
		if err := rows.Scan(&seq, &name, &path); err != nil {
			return "", &ledger.ErrUnavailable{}
		}
		if name == "main" {
			return path, nil
		}
	}
	if rows.Err() != nil {
		return "", &ledger.ErrUnavailable{}
	}
	return "", nil
}
