package facestore

import "context"

// FaceStore abstracts storage of face detection and recognition data.
// Implementations exist for SQLite (local dev / blobemu) and Azure Table
// Storage (production).
type FaceStore interface {
	// ── Face CRUD ────────────────────────────────────────────────────

	// SaveFace persists a detected face and increments the owning person's
	// face count. If the Face.PersonID references a person that does not
	// yet exist, the implementation must create it.
	SaveFace(ctx context.Context, f Face) error

	// GetFacesByPerson returns all faces belonging to a person.
	GetFacesByPerson(ctx context.Context, personID string) ([]Face, error)

	// GetFacesByPhoto returns all faces detected in a specific photo.
	GetFacesByPhoto(ctx context.Context, ref PhotoRef) ([]Face, error)

	// ── Person CRUD ──────────────────────────────────────────────────

	// GetAllPersons returns every person ordered by name (unnamed last).
	GetAllPersons(ctx context.Context) ([]Person, error)

	// GetPersonByID returns a single person or an error if not found.
	GetPersonByID(ctx context.Context, personID string) (Person, error)

	// SetPersonName updates the display name for a person.
	SetPersonName(ctx context.Context, personID string, name string) error

	// MergePeople moves all faces from sourcePersonID into targetPersonID
	// and deletes the source person.
	MergePeople(ctx context.Context, sourcePersonID, targetPersonID string) error

	// SearchPeople returns persons whose name matches the given prefix
	// (case-insensitive). Pass an empty string to list all named persons.
	SearchPeople(ctx context.Context, namePrefix string) ([]Person, error)

	// ── Queries ──────────────────────────────────────────────────────

	// FindSimilarFaces searches for faces whose landmark fingerprint AND
	// perceptual hash are within the given tolerances. The two-pass
	// approach (geometric then perceptual) lets the implementation prune
	// candidates quickly.
	//
	//   landmarkTolerance – max Euclidean distance between fingerprint vectors
	//   hashMaxHamming    – max Hamming distance between dHash hex strings
	FindSimilarFaces(ctx context.Context, fingerprint []float64, hash string, landmarkTolerance float64, hashMaxHamming int) ([]Face, error)

	// GetPhotosByPerson returns photo refs for photos containing a given
	// person, ordered by creation date descending, with pagination.
	GetPhotosByPerson(ctx context.Context, personID string, offset, limit int) ([]PhotoRef, error)

	// HasPhotoBeenProcessed returns true if at least one face record
	// exists for the given photo (used by the cron backfill job).
	HasPhotoBeenProcessed(ctx context.Context, ref PhotoRef) (bool, error)

	// GetFaceOverlaysForPhoto returns the minimal overlay data needed by
	// the UI to render bounding boxes on a specific photo.
	GetFaceOverlaysForPhoto(ctx context.Context, ref PhotoRef) ([]FaceOverlay, error)

	// Close releases any resources held by the store.
	Close() error
}
