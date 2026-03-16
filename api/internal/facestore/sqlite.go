package facestore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements FaceStore backed by a local SQLite database.
// It is used for local development (alongside blobemu) and for unit tests.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the SQLite database at dbPath and
// initialises the schema.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("facestore: open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("facestore: WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, fmt.Errorf("facestore: busy timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("facestore: foreign keys: %w", err)
	}
	if err := initFaceSchema(db); err != nil {
		return nil, fmt.Errorf("facestore: init schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func initFaceSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS persons (
			id               TEXT PRIMARY KEY,
			name             TEXT NOT NULL DEFAULT '',
			face_count       INTEGER NOT NULL DEFAULT 0,
			thumbnail_face_id TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS faces (
			id                TEXT PRIMARY KEY,
			person_id         TEXT NOT NULL,
			photo_collection  TEXT NOT NULL,
			photo_album       TEXT NOT NULL,
			photo_name        TEXT NOT NULL,
			bbox_x            REAL NOT NULL,
			bbox_y            REAL NOT NULL,
			bbox_w            REAL NOT NULL,
			bbox_h            REAL NOT NULL,
			landmark_fp       TEXT NOT NULL DEFAULT '[]',
			face_hash         TEXT NOT NULL DEFAULT '',
			confidence        REAL NOT NULL DEFAULT 0,
			created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (person_id) REFERENCES persons(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_faces_person  ON faces(person_id);
		CREATE INDEX IF NOT EXISTS idx_faces_photo   ON faces(photo_collection, photo_album, photo_name);
	`)
	return err
}

// Close releases the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// ── SaveFace ────────────────────────────────────────────────────────────────

func (s *SQLiteStore) SaveFace(ctx context.Context, f Face) error {
	fpJSON, err := json.Marshal(f.LandmarkFingerprint)
	if err != nil {
		return fmt.Errorf("facestore: marshal fingerprint: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Upsert person (create if not exists).
	_, err = tx.ExecContext(ctx, `
		INSERT INTO persons (id, name, face_count, thumbnail_face_id)
		VALUES (?, '', 0, ?)
		ON CONFLICT(id) DO NOTHING
	`, f.PersonID, f.FaceID)
	if err != nil {
		return fmt.Errorf("facestore: upsert person: %w", err)
	}

	// Insert face.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO faces (id, person_id, photo_collection, photo_album, photo_name,
			bbox_x, bbox_y, bbox_w, bbox_h, landmark_fp, face_hash, confidence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, f.FaceID, f.PersonID,
		f.PhotoRef.Collection, f.PhotoRef.Album, f.PhotoRef.Name,
		f.BBox.X, f.BBox.Y, f.BBox.W, f.BBox.H,
		string(fpJSON), f.FaceHash, f.Confidence, f.CreatedAt)
	if err != nil {
		return fmt.Errorf("facestore: insert face: %w", err)
	}

	// Bump face count.
	_, err = tx.ExecContext(ctx, `
		UPDATE persons SET face_count = face_count + 1 WHERE id = ?
	`, f.PersonID)
	if err != nil {
		return fmt.Errorf("facestore: bump count: %w", err)
	}

	return tx.Commit()
}

// ── GetFacesByPerson ────────────────────────────────────────────────────────

func (s *SQLiteStore) GetFacesByPerson(ctx context.Context, personID string) ([]Face, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, person_id, photo_collection, photo_album, photo_name,
			bbox_x, bbox_y, bbox_w, bbox_h, landmark_fp, face_hash, confidence, created_at
		FROM faces WHERE person_id = ? ORDER BY created_at DESC
	`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFaces(rows)
}

// ── GetFacesByPhoto ─────────────────────────────────────────────────────────

func (s *SQLiteStore) GetFacesByPhoto(ctx context.Context, ref PhotoRef) ([]Face, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, person_id, photo_collection, photo_album, photo_name,
			bbox_x, bbox_y, bbox_w, bbox_h, landmark_fp, face_hash, confidence, created_at
		FROM faces WHERE photo_collection = ? AND photo_album = ? AND photo_name = ?
		ORDER BY bbox_x ASC
	`, ref.Collection, ref.Album, ref.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFaces(rows)
}

// ── GetAllPersons ───────────────────────────────────────────────────────────

func (s *SQLiteStore) GetAllPersons(ctx context.Context) ([]Person, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, face_count, thumbnail_face_id
		FROM persons
		ORDER BY CASE WHEN name = '' THEN 1 ELSE 0 END, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPersons(rows)
}

// ── GetPersonByID ───────────────────────────────────────────────────────────

func (s *SQLiteStore) GetPersonByID(ctx context.Context, personID string) (Person, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, face_count, thumbnail_face_id
		FROM persons WHERE id = ?
	`, personID)
	var p Person
	err := row.Scan(&p.PersonID, &p.Name, &p.FaceCount, &p.ThumbnailFaceID)
	if err == sql.ErrNoRows {
		return p, fmt.Errorf("facestore: person %q not found", personID)
	}
	return p, err
}

// ── SetPersonName ───────────────────────────────────────────────────────────

func (s *SQLiteStore) SetPersonName(ctx context.Context, personID, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE persons SET name = ? WHERE id = ?`, name, personID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("facestore: person %q not found", personID)
	}
	return nil
}

// ── MergePeople ─────────────────────────────────────────────────────────────

func (s *SQLiteStore) MergePeople(ctx context.Context, sourceID, targetID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Move faces.
	_, err = tx.ExecContext(ctx, `UPDATE faces SET person_id = ? WHERE person_id = ?`, targetID, sourceID)
	if err != nil {
		return err
	}

	// Recompute target face count.
	_, err = tx.ExecContext(ctx, `
		UPDATE persons SET face_count = (SELECT COUNT(*) FROM faces WHERE person_id = ?) WHERE id = ?
	`, targetID, targetID)
	if err != nil {
		return err
	}

	// Delete source person.
	_, err = tx.ExecContext(ctx, `DELETE FROM persons WHERE id = ?`, sourceID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ── SearchPeople ────────────────────────────────────────────────────────────

func (s *SQLiteStore) SearchPeople(ctx context.Context, namePrefix string) ([]Person, error) {
	var rows *sql.Rows
	var err error
	if namePrefix == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, name, face_count, thumbnail_face_id
			FROM persons WHERE name != '' ORDER BY name ASC
		`)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, name, face_count, thumbnail_face_id
			FROM persons WHERE name LIKE ? COLLATE NOCASE ORDER BY name ASC
		`, namePrefix+"%")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPersons(rows)
}

// ── FindSimilarFaces ────────────────────────────────────────────────────────

func (s *SQLiteStore) FindSimilarFaces(ctx context.Context, fingerprint []float64, hash string, landmarkTol float64, hashMaxHamming int) ([]Face, error) {
	// SQLite has no vector operations, so we load all faces and do brute-force
	// comparison in Go. This is fine for <10 K faces.
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, person_id, photo_collection, photo_album, photo_name,
			bbox_x, bbox_y, bbox_w, bbox_h, landmark_fp, face_hash, confidence, created_at
		FROM faces
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	allFaces, err := scanFaces(rows)
	if err != nil {
		return nil, err
	}

	var matches []Face
	for _, f := range allFaces {
		// Pass 1: geometric fingerprint distance.
		if euclidean(fingerprint, f.LandmarkFingerprint) > landmarkTol {
			continue
		}
		// Pass 2: perceptual hash Hamming distance.
		if hammingHex(hash, f.FaceHash) > hashMaxHamming {
			continue
		}
		matches = append(matches, f)
	}
	return matches, nil
}

// ── GetPhotosByPerson ───────────────────────────────────────────────────────

func (s *SQLiteStore) GetPhotosByPerson(ctx context.Context, personID string, offset, limit int) ([]PhotoRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT photo_collection, photo_album, photo_name
		FROM faces WHERE person_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, personID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []PhotoRef
	for rows.Next() {
		var r PhotoRef
		if err := rows.Scan(&r.Collection, &r.Album, &r.Name); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// ── HasPhotoBeenProcessed ───────────────────────────────────────────────────

func (s *SQLiteStore) HasPhotoBeenProcessed(ctx context.Context, ref PhotoRef) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM faces
		WHERE photo_collection = ? AND photo_album = ? AND photo_name = ?
	`, ref.Collection, ref.Album, ref.Name).Scan(&count)
	return count > 0, err
}

// ── GetFaceOverlaysForPhoto ─────────────────────────────────────────────────

func (s *SQLiteStore) GetFaceOverlaysForPhoto(ctx context.Context, ref PhotoRef) ([]FaceOverlay, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.person_id, COALESCE(p.name, ''), f.bbox_x, f.bbox_y, f.bbox_w, f.bbox_h
		FROM faces f
		LEFT JOIN persons p ON p.id = f.person_id
		WHERE f.photo_collection = ? AND f.photo_album = ? AND f.photo_name = ?
		ORDER BY f.bbox_x ASC
	`, ref.Collection, ref.Album, ref.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var overlays []FaceOverlay
	for rows.Next() {
		var o FaceOverlay
		if err := rows.Scan(&o.FaceID, &o.PersonID, &o.PersonName,
			&o.BBox.X, &o.BBox.Y, &o.BBox.W, &o.BBox.H); err != nil {
			return nil, err
		}
		overlays = append(overlays, o)
	}
	return overlays, rows.Err()
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func scanFaces(rows *sql.Rows) ([]Face, error) {
	var faces []Face
	for rows.Next() {
		var f Face
		var fpJSON string
		var createdAt time.Time
		if err := rows.Scan(
			&f.FaceID, &f.PersonID,
			&f.PhotoRef.Collection, &f.PhotoRef.Album, &f.PhotoRef.Name,
			&f.BBox.X, &f.BBox.Y, &f.BBox.W, &f.BBox.H,
			&fpJSON, &f.FaceHash, &f.Confidence, &createdAt,
		); err != nil {
			return nil, err
		}
		f.CreatedAt = createdAt
		if err := json.Unmarshal([]byte(fpJSON), &f.LandmarkFingerprint); err != nil {
			f.LandmarkFingerprint = nil
		}
		faces = append(faces, f)
	}
	return faces, rows.Err()
}

func scanPersons(rows *sql.Rows) ([]Person, error) {
	var persons []Person
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.PersonID, &p.Name, &p.FaceCount, &p.ThumbnailFaceID); err != nil {
			return nil, err
		}
		persons = append(persons, p)
	}
	return persons, rows.Err()
}

// euclidean computes the Euclidean distance between two float64 vectors.
// If lengths differ the shorter one is zero-padded.
func euclidean(a, b []float64) float64 {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	var sum float64
	for i := 0; i < n; i++ {
		va, vb := 0.0, 0.0
		if i < len(a) {
			va = a[i]
		}
		if i < len(b) {
			vb = b[i]
		}
		d := va - vb
		sum += d * d
	}
	return math.Sqrt(sum)
}

// hammingHex computes the Hamming distance between two hex-encoded hash strings.
// If the strings have different lengths the result is max-int (no match).
func hammingHex(a, b string) int {
	ba, err1 := hex.DecodeString(a)
	bb, err2 := hex.DecodeString(b)
	if err1 != nil || err2 != nil || len(ba) != len(bb) {
		return math.MaxInt
	}
	dist := 0
	for i := range ba {
		xor := ba[i] ^ bb[i]
		for xor != 0 {
			dist += int(xor & 1)
			xor >>= 1
		}
	}
	return dist
}

// Ensure SQLiteStore satisfies FaceStore at compile time.
var _ FaceStore = (*SQLiteStore)(nil)

// Suppress unused import warning for strings — used for future query building.
var _ = strings.HasPrefix
