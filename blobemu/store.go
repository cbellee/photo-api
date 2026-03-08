package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// capitaliseKey uppercases the first letter of a metadata key to match
// Azure Blob Storage behaviour (the SDK always capitalises metadata keys).
func capitaliseKey(key string) string {
	if key == "" {
		return key
	}
	r, size := utf8.DecodeRuneInString(key)
	return string(unicode.ToUpper(r)) + key[size:]
}

// joinStrings joins a string slice with a separator (avoids importing strings
// in hot path for a tiny helper).
func joinStrings(elems []string, sep string) string {
	return strings.Join(elems, sep)
}

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

	// Serialise all access through a single connection to avoid
	// SQLITE_BUSY when concurrent HTTP handlers write.
	db.SetMaxOpenConns(1)

	// WAL mode gives better concurrent-read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}
	// Wait up to 5 s for the write lock instead of failing immediately.
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, fmt.Errorf("setting busy timeout: %w", err)
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
			deleted_at   TIMESTAMP,
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
	err = tx.QueryRow("SELECT id FROM blobs WHERE container = ? AND name = ? AND deleted_at IS NULL", container, name).Scan(&blobID)
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
		normKey := capitaliseKey(k)
		if _, err := tx.Exec("INSERT INTO metadata (blob_id, key, value) VALUES (?, ?, ?)", blobID, normKey, v); err != nil {
			return fmt.Errorf("inserting metadata %s: %w", normKey, err)
		}
	}

	slog.Debug("saved blob", "container", container, "name", name, "tags", len(tags), "metadata", len(metadata))
	return tx.Commit()
}

// SetTags replaces all tags on a blob.
func (s *Store) SetTags(container, name string, tags map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var blobID int64
	if err := tx.QueryRow("SELECT id FROM blobs WHERE container = ? AND name = ? AND deleted_at IS NULL", container, name).Scan(&blobID); err != nil {
		return fmt.Errorf("blob not found: %s/%s", container, name)
	}

	if _, err := tx.Exec("DELETE FROM tags WHERE blob_id = ?", blobID); err != nil {
		return fmt.Errorf("deleting old tags: %w", err)
	}
	for k, v := range tags {
		if _, err := tx.Exec("INSERT INTO tags (blob_id, key, value) VALUES (?, ?, ?)", blobID, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteBlob soft-deletes a blob by setting its deleted_at timestamp.
// The blob data remains on disk and can be restored later.
// Returns nil if the blob does not exist (idempotent).
func (s *Store) DeleteBlob(container, name string) error {
	result, err := s.db.Exec(
		"UPDATE blobs SET deleted_at = CURRENT_TIMESTAMP WHERE container = ? AND name = ? AND deleted_at IS NULL",
		container, name,
	)
	if err != nil {
		return fmt.Errorf("soft-deleting blob: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		slog.Debug("blob already deleted or not found", "container", container, "name", name)
	} else {
		slog.Debug("soft-deleted blob", "container", container, "name", name)
	}
	return nil
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
	if err := s.db.QueryRow("SELECT content_type FROM blobs WHERE container = ? AND name = ? AND deleted_at IS NULL", container, name).Scan(&ct); err != nil {
		ct = "application/octet-stream"
	}
	return data, ct, nil
}

// GetTags returns the index tags for a blob.
func (s *Store) GetTags(container, name string) (map[string]string, error) {
	var blobID int64
	if err := s.db.QueryRow("SELECT id FROM blobs WHERE container = ? AND name = ? AND deleted_at IS NULL", container, name).Scan(&blobID); err != nil {
		return nil, fmt.Errorf("blob not found: %s/%s", container, name)
	}
	return s.tagsByID(blobID)
}

// GetMetadata returns custom metadata for a blob.
func (s *Store) GetMetadata(container, name string) (map[string]string, error) {
	var blobID int64
	if err := s.db.QueryRow("SELECT id FROM blobs WHERE container = ? AND name = ? AND deleted_at IS NULL", container, name).Scan(&blobID); err != nil {
		return nil, fmt.Errorf("blob not found: %s/%s", container, name)
	}
	return s.metadataByID(blobID)
}

// ListBlobs returns every blob in a container with its tags.
// Uses a single JOIN to avoid per-blob round-trips.
func (s *Store) ListBlobs(container string) ([]BlobInfo, error) {
	const q = `
		SELECT b.id, b.name, COALESCE(t.key, ''), COALESCE(t.value, '')
		FROM blobs b
		LEFT JOIN tags t ON t.blob_id = b.id
		WHERE b.container = ? AND b.deleted_at IS NULL
		ORDER BY b.id`

	rows, err := s.db.Query(q, container)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Coalesce rows into BlobInfo structs keyed by blob id.
	type entry struct {
		index int
	}
	index := make(map[int64]entry)
	var out []BlobInfo

	for rows.Next() {
		var id int64
		var name, tagKey, tagVal string
		if err := rows.Scan(&id, &name, &tagKey, &tagVal); err != nil {
			return nil, err
		}

		e, ok := index[id]
		if !ok {
			e = entry{index: len(out)}
			index[id] = e
			out = append(out, BlobInfo{Name: name, Container: container, Tags: map[string]string{}})
		}
		if tagKey != "" {
			out[e.index].Tags[tagKey] = tagVal
		}
	}
	if out == nil {
		out = []BlobInfo{}
	}
	return out, nil
}

// FilterByTags parses an Azure-style tag query and returns matching blobs
// with tags and metadata fully populated.
// Uses JOINs to fetch tags and metadata in bulk instead of per-blob queries.
func (s *Store) FilterByTags(query string) ([]BlobInfo, error) {
	conditions, err := ParseTagQuery(query)
	if err != nil {
		return nil, fmt.Errorf("parsing query: %w", err)
	}

	// Step 1: find matching blob IDs.
	sqlText, args := BuildFilterSQL(conditions)
	rows, err := s.db.Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("executing filter: %w", err)
	}

	type blobRow struct {
		id        int64
		container string
		name      string
	}
	var matched []blobRow
	for rows.Next() {
		var br blobRow
		if err := rows.Scan(&br.id, &br.container, &br.name); err != nil {
			rows.Close()
			return nil, err
		}
		matched = append(matched, br)
	}
	rows.Close()

	if len(matched) == 0 {
		return []BlobInfo{}, nil
	}

	// Build id list for bulk fetch.
	ids := make([]interface{}, len(matched))
	placeholders := make([]string, len(matched))
	for i, br := range matched {
		ids[i] = br.id
		placeholders[i] = "?"
	}
	inClause := "(" + joinStrings(placeholders, ",") + ")"

	// Step 2: bulk-fetch tags.
	tagMap := make(map[int64]map[string]string)
	tagRows, err := s.db.Query("SELECT blob_id, key, value FROM tags WHERE blob_id IN "+inClause, ids...)
	if err == nil {
		defer tagRows.Close()
		for tagRows.Next() {
			var bid int64
			var k, v string
			if err := tagRows.Scan(&bid, &k, &v); err == nil {
				if tagMap[bid] == nil {
					tagMap[bid] = make(map[string]string)
				}
				tagMap[bid][k] = v
			}
		}
	}

	// Step 3: bulk-fetch metadata.
	mdMap := make(map[int64]map[string]string)
	mdRows, err := s.db.Query("SELECT blob_id, key, value FROM metadata WHERE blob_id IN "+inClause, ids...)
	if err == nil {
		defer mdRows.Close()
		for mdRows.Next() {
			var bid int64
			var k, v string
			if err := mdRows.Scan(&bid, &k, &v); err == nil {
				if mdMap[bid] == nil {
					mdMap[bid] = make(map[string]string)
				}
				mdMap[bid][k] = v
			}
		}
	}

	// Step 4: assemble results.
	out := make([]BlobInfo, 0, len(matched))
	for _, br := range matched {
		tags := tagMap[br.id]
		if tags == nil {
			tags = map[string]string{}
		}
		md := mdMap[br.id]
		if md == nil {
			md = map[string]string{}
		}
		out = append(out, BlobInfo{Name: br.name, Container: br.container, Tags: tags, Metadata: md})
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
