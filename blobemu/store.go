package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// BlobInfo is the JSON-serialisable representation of a stored blob.
type BlobInfo struct {
	Name      string            `json:"name"`
	Container string            `json:"container"`
	Tags      map[string]string `json:"tags,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Store manages blobs on disk and their tags/metadata in SQLite.
type Store struct {
	db      *sql.DB
	dataDir string
}

// NewStore opens (or creates) the SQLite database and blob directory tree.
func NewStore(dataDir string) (*Store, error) {
	blobDir := filepath.Join(dataDir, "blobs")
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		return nil, fmt.Errorf("creating blob directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "blobstore.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// WAL mode gives better concurrent-read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	if err := initSchema(db); err != nil {
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	return &Store{db: db, dataDir: dataDir}, nil
}

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS blobs (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			container    TEXT NOT NULL,
			name         TEXT NOT NULL,
			content_type TEXT DEFAULT 'application/octet-stream',
			size         INTEGER DEFAULT 0,
			created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(container, name)
		);
		CREATE TABLE IF NOT EXISTS tags (
			blob_id INTEGER NOT NULL,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL,
			FOREIGN KEY (blob_id) REFERENCES blobs(id) ON DELETE CASCADE,
			UNIQUE(blob_id, key)
		);
		CREATE TABLE IF NOT EXISTS metadata (
			blob_id INTEGER NOT NULL,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL,
			FOREIGN KEY (blob_id) REFERENCES blobs(id) ON DELETE CASCADE,
			UNIQUE(blob_id, key)
		);
		CREATE INDEX IF NOT EXISTS idx_tags_key_value        ON tags(key, value);
		CREATE INDEX IF NOT EXISTS idx_blobs_container       ON blobs(container);
		CREATE INDEX IF NOT EXISTS idx_blobs_container_name  ON blobs(container, name);
	`)
	return err
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// blobPath returns the on-disk path for a given blob.
func (s *Store) blobPath(container, name string) string {
	return filepath.Join(s.dataDir, "blobs", container, name)
}

// ---------- write operations ----------

// SaveBlob stores blob data on disk and upserts the metadata/tag rows.
func (s *Store) SaveBlob(container, name string, data []byte, tags, metadata map[string]string, contentType string) error {
	// Persist bytes to disk.
	fpath := s.blobPath(container, name)
	if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		return fmt.Errorf("writing blob data: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	var blobID int64
	err = tx.QueryRow("SELECT id FROM blobs WHERE container = ? AND name = ?", container, name).Scan(&blobID)
	if err == sql.ErrNoRows {
		res, err := tx.Exec(
			"INSERT INTO blobs (container, name, content_type, size) VALUES (?, ?, ?, ?)",
			container, name, contentType, len(data),
		)
		if err != nil {
			return fmt.Errorf("inserting blob: %w", err)
		}
		blobID, _ = res.LastInsertId()
	} else if err != nil {
		return fmt.Errorf("querying blob: %w", err)
	} else {
		if _, err := tx.Exec("UPDATE blobs SET content_type = ?, size = ? WHERE id = ?", contentType, len(data), blobID); err != nil {
			return fmt.Errorf("updating blob: %w", err)
		}
		tx.Exec("DELETE FROM tags WHERE blob_id = ?", blobID)
		tx.Exec("DELETE FROM metadata WHERE blob_id = ?", blobID)
	}

	for k, v := range tags {
		if _, err := tx.Exec("INSERT INTO tags (blob_id, key, value) VALUES (?, ?, ?)", blobID, k, v); err != nil {
			return fmt.Errorf("inserting tag %s: %w", k, err)
		}
	}
	for k, v := range metadata {
		if _, err := tx.Exec("INSERT INTO metadata (blob_id, key, value) VALUES (?, ?, ?)", blobID, k, v); err != nil {
			return fmt.Errorf("inserting metadata %s: %w", k, err)
		}
	}

	slog.Debug("saved blob", "container", container, "name", name, "tags", len(tags), "metadata", len(metadata))
	return tx.Commit()
}

// SetTags replaces all tags on a blob.
func (s *Store) SetTags(container, name string, tags map[string]string) error {
	var blobID int64
	if err := s.db.QueryRow("SELECT id FROM blobs WHERE container = ? AND name = ?", container, name).Scan(&blobID); err != nil {
		return fmt.Errorf("blob not found: %s/%s", container, name)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tx.Exec("DELETE FROM tags WHERE blob_id = ?", blobID)
	for k, v := range tags {
		if _, err := tx.Exec("INSERT INTO tags (blob_id, key, value) VALUES (?, ?, ?)", blobID, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- read operations ----------

// GetBlob returns the raw bytes and content-type.
func (s *Store) GetBlob(container, name string) ([]byte, string, error) {
	data, err := os.ReadFile(s.blobPath(container, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("blob not found: %s/%s", container, name)
		}
		return nil, "", fmt.Errorf("reading blob: %w", err)
	}

	var ct string
	if err := s.db.QueryRow("SELECT content_type FROM blobs WHERE container = ? AND name = ?", container, name).Scan(&ct); err != nil {
		ct = "application/octet-stream"
	}
	return data, ct, nil
}

// GetTags returns the index tags for a blob.
func (s *Store) GetTags(container, name string) (map[string]string, error) {
	var blobID int64
	if err := s.db.QueryRow("SELECT id FROM blobs WHERE container = ? AND name = ?", container, name).Scan(&blobID); err != nil {
		return nil, fmt.Errorf("blob not found: %s/%s", container, name)
	}
	return s.tagsByID(blobID)
}

// GetMetadata returns custom metadata for a blob.
func (s *Store) GetMetadata(container, name string) (map[string]string, error) {
	var blobID int64
	if err := s.db.QueryRow("SELECT id FROM blobs WHERE container = ? AND name = ?", container, name).Scan(&blobID); err != nil {
		return nil, fmt.Errorf("blob not found: %s/%s", container, name)
	}
	return s.metadataByID(blobID)
}

// ListBlobs returns every blob in a container with its tags.
func (s *Store) ListBlobs(container string) ([]BlobInfo, error) {
	rows, err := s.db.Query("SELECT id, name FROM blobs WHERE container = ?", container)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BlobInfo
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		tags, _ := s.tagsByID(id)
		out = append(out, BlobInfo{Name: name, Container: container, Tags: tags})
	}
	if out == nil {
		out = []BlobInfo{}
	}
	return out, nil
}

// FilterByTags parses an Azure-style tag query and returns matching blobs
// with tags and metadata fully populated.
func (s *Store) FilterByTags(query string) ([]BlobInfo, error) {
	conditions, err := ParseTagQuery(query)
	if err != nil {
		return nil, fmt.Errorf("parsing query: %w", err)
	}

	sqlText, args := BuildFilterSQL(conditions)
	rows, err := s.db.Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("executing filter: %w", err)
	}
	defer rows.Close()

	var out []BlobInfo
	for rows.Next() {
		var id int64
		var container, name string
		if err := rows.Scan(&id, &container, &name); err != nil {
			return nil, err
		}
		tags, _ := s.tagsByID(id)
		md, _ := s.metadataByID(id)
		out = append(out, BlobInfo{Name: name, Container: container, Tags: tags, Metadata: md})
	}
	if out == nil {
		out = []BlobInfo{}
	}
	return out, nil
}

// ---------- helpers ----------

func (s *Store) tagsByID(blobID int64) (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM tags WHERE blob_id = ?", blobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}

func (s *Store) metadataByID(blobID int64) (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM metadata WHERE blob_id = ?", blobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}
