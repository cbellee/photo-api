package facestore

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/aztables"
)

// TableStore implements FaceStore backed by Azure Table Storage.
// Three tables are used:
//
//   - persons  – PK "person", RK <personID>
//   - faces    – PK <personID>, RK <faceID>
//   - photofaces – PK <collection/album/name>, RK <faceID>  (reverse index)
type TableStore struct {
	persons    *aztables.Client
	faces      *aztables.Client
	photofaces *aztables.Client
}

// NewTableStore creates clients for the three face tables.
// The credential must have "Storage Table Data Contributor" role.
func NewTableStore(serviceURL string, cred azcore.TokenCredential) (*TableStore, error) {
	svcClient, err := aztables.NewServiceClient(serviceURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("facestore: table service client: %w", err)
	}

	ts := &TableStore{
		persons:    svcClient.NewClient("persons"),
		faces:      svcClient.NewClient("faces"),
		photofaces: svcClient.NewClient("photofaces"),
	}

	// Ensure tables exist (idempotent).
	ctx := context.Background()
	for _, c := range []*aztables.Client{ts.persons, ts.faces, ts.photofaces} {
		if _, err := c.CreateTable(ctx, nil); err != nil {
			// Ignore "TableAlreadyExists".
			if !isTableExists(err) {
				return nil, fmt.Errorf("facestore: create table: %w", err)
			}
		}
	}
	return ts, nil
}

func (ts *TableStore) Close() error { return nil }

// ── entity types ────────────────────────────────────────────────────────────

type personEntity struct {
	aztables.Entity
	Name            string `json:"Name"`
	FaceCount       int    `json:"FaceCount"`
	ThumbnailFaceID string `json:"ThumbnailFaceID"`
}

type faceEntity struct {
	aztables.Entity
	PersonID        string  `json:"PersonID"`
	PhotoCollection string  `json:"PhotoCollection"`
	PhotoAlbum      string  `json:"PhotoAlbum"`
	PhotoName       string  `json:"PhotoName"`
	BBoxX           float64 `json:"BBoxX"`
	BBoxY           float64 `json:"BBoxY"`
	BBoxW           float64 `json:"BBoxW"`
	BBoxH           float64 `json:"BBoxH"`
	LandmarkFP      string  `json:"LandmarkFP"` // JSON-encoded []float64
	FaceHash        string  `json:"FaceHash"`
	Confidence      float32 `json:"Confidence"`
	DetectedAt      string  `json:"DetectedAt"` // RFC3339
}

type photofaceEntity struct {
	aztables.Entity
	PersonID string `json:"PersonID"`
}

// ── SaveFace ────────────────────────────────────────────────────────────────

func (ts *TableStore) SaveFace(ctx context.Context, f Face) error {
	fpJSON, _ := json.Marshal(f.LandmarkFingerprint)

	// 1. Upsert person (merge so we don't overwrite name).
	pe := personEntity{
		Entity: aztables.Entity{
			PartitionKey: "person",
			RowKey:       f.PersonID,
		},
		ThumbnailFaceID: f.FaceID,
	}
	peBytes, _ := json.Marshal(pe)
	_, err := ts.persons.UpsertEntity(ctx, peBytes, &aztables.UpsertEntityOptions{UpdateMode: aztables.UpdateModeMerge})
	if err != nil {
		return fmt.Errorf("facestore: upsert person: %w", err)
	}

	// 2. Insert face.
	fe := faceEntity{
		Entity: aztables.Entity{
			PartitionKey: f.PersonID,
			RowKey:       f.FaceID,
		},
		PersonID:        f.PersonID,
		PhotoCollection: f.PhotoRef.Collection,
		PhotoAlbum:      f.PhotoRef.Album,
		PhotoName:       f.PhotoRef.Name,
		BBoxX:           f.BBox.X,
		BBoxY:           f.BBox.Y,
		BBoxW:           f.BBox.W,
		BBoxH:           f.BBox.H,
		LandmarkFP:      string(fpJSON),
		FaceHash:        f.FaceHash,
		Confidence:      f.Confidence,
		DetectedAt:      f.CreatedAt.UTC().Format(time.RFC3339),
	}
	feBytes, _ := json.Marshal(fe)
	_, err = ts.faces.UpsertEntity(ctx, feBytes, &aztables.UpsertEntityOptions{UpdateMode: aztables.UpdateModeReplace})
	if err != nil {
		return fmt.Errorf("facestore: insert face: %w", err)
	}

	// 3. Insert reverse index.
	pfe := photofaceEntity{
		Entity: aztables.Entity{
			PartitionKey: f.PhotoRef.Key(),
			RowKey:       f.FaceID,
		},
		PersonID: f.PersonID,
	}
	pfeBytes, _ := json.Marshal(pfe)
	_, err = ts.photofaces.UpsertEntity(ctx, pfeBytes, &aztables.UpsertEntityOptions{UpdateMode: aztables.UpdateModeReplace})
	if err != nil {
		return fmt.Errorf("facestore: insert photoface: %w", err)
	}

	// 4. Update face count (read-modify-write).
	return ts.recalcFaceCount(ctx, f.PersonID)
}

// ── GetFaceByID ─────────────────────────────────────────────────────────────

func (ts *TableStore) GetFaceByID(ctx context.Context, faceID string) (Face, error) {
	// Face entities are partitioned by personID. Since we only have faceID
	// we must scan all faces. For a small dataset this is acceptable;
	// for scale, consider a secondary index table.
	filter := fmt.Sprintf("RowKey eq '%s'", escapeOData(faceID))
	faces, err := ts.queryFaces(ctx, filter)
	if err != nil {
		return Face{}, err
	}
	if len(faces) == 0 {
		return Face{}, fmt.Errorf("facestore: face %q not found", faceID)
	}
	return faces[0], nil
}

// ── GetFacesByPerson ────────────────────────────────────────────────────────

func (ts *TableStore) GetFacesByPerson(ctx context.Context, personID string) ([]Face, error) {
	filter := fmt.Sprintf("PartitionKey eq '%s'", escapeOData(personID))
	return ts.queryFaces(ctx, filter)
}

// ── GetFacesByPhoto ─────────────────────────────────────────────────────────

func (ts *TableStore) GetFacesByPhoto(ctx context.Context, ref PhotoRef) ([]Face, error) {
	// Query the photofaces reverse index to get faceIDs, then fetch from faces table.
	filter := fmt.Sprintf("PartitionKey eq '%s'", escapeOData(ref.Key()))
	pager := ts.photofaces.NewListEntitiesPager(&aztables.ListEntitiesOptions{Filter: &filter})

	var faces []Face
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, raw := range page.Entities {
			var pfe photofaceEntity
			if err := json.Unmarshal(raw, &pfe); err != nil {
				continue
			}
			// Fetch the full face entity.
			resp, err := ts.faces.GetEntity(ctx, pfe.PersonID, pfe.RowKey, nil)
			if err != nil {
				continue
			}
			var fe faceEntity
			if err := json.Unmarshal(resp.Value, &fe); err != nil {
				continue
			}
			faces = append(faces, faceEntityToFace(fe))
		}
	}
	return faces, nil
}

// ── GetAllPersons ───────────────────────────────────────────────────────────

func (ts *TableStore) GetAllPersons(ctx context.Context) ([]Person, error) {
	filter := "PartitionKey eq 'person'"
	return ts.queryPersons(ctx, filter)
}

// ── GetPersonByID ───────────────────────────────────────────────────────────

func (ts *TableStore) GetPersonByID(ctx context.Context, personID string) (Person, error) {
	resp, err := ts.persons.GetEntity(ctx, "person", personID, nil)
	if err != nil {
		return Person{}, fmt.Errorf("facestore: person %q not found: %w", personID, err)
	}
	var pe personEntity
	if err := json.Unmarshal(resp.Value, &pe); err != nil {
		return Person{}, err
	}
	return Person{
		PersonID:        pe.RowKey,
		Name:            pe.Name,
		FaceCount:       pe.FaceCount,
		ThumbnailFaceID: pe.ThumbnailFaceID,
	}, nil
}

// ── SetPersonName ───────────────────────────────────────────────────────────

func (ts *TableStore) SetPersonName(ctx context.Context, personID, name string) error {
	patch := map[string]any{
		"PartitionKey": "person",
		"RowKey":       personID,
		"Name":         name,
	}
	b, _ := json.Marshal(patch)
	_, err := ts.persons.UpdateEntity(ctx, b, &aztables.UpdateEntityOptions{UpdateMode: aztables.UpdateModeMerge})
	return err
}

// ── DeletePerson ────────────────────────────────────────────────────────────

func (ts *TableStore) DeletePerson(ctx context.Context, personID string) error {
	// 1. Delete all face entities belonging to this person.
	faces, err := ts.GetFacesByPerson(ctx, personID)
	if err != nil {
		return err
	}
	for _, f := range faces {
		_, _ = ts.faces.DeleteEntity(ctx, personID, f.FaceID, nil)
		// Also clean up the photofaces reverse index.
		_, _ = ts.photofaces.DeleteEntity(ctx, f.PhotoRef.Key(), f.FaceID, nil)
	}

	// 2. Delete the person entity.
	_, err = ts.persons.DeleteEntity(ctx, "person", personID, nil)
	return err
}

// ── MergePeople ─────────────────────────────────────────────────────────────

func (ts *TableStore) MergePeople(ctx context.Context, sourceID, targetID string) error {
	// 1. List all faces belonging to sourceID.
	srcFaces, err := ts.GetFacesByPerson(ctx, sourceID)
	if err != nil {
		return err
	}

	// 2. For each face: delete from old partition, re-insert under target.
	for _, f := range srcFaces {
		// Delete old face entity.
		_, _ = ts.faces.DeleteEntity(ctx, sourceID, f.FaceID, nil)

		// Re-insert under target.
		f.PersonID = targetID
		fe := faceToEntity(f)
		feBytes, _ := json.Marshal(fe)
		_, _ = ts.faces.UpsertEntity(ctx, feBytes, &aztables.UpsertEntityOptions{UpdateMode: aztables.UpdateModeReplace})

		// Update reverse index.
		patch := map[string]any{
			"PartitionKey": f.PhotoRef.Key(),
			"RowKey":       f.FaceID,
			"PersonID":     targetID,
		}
		pb, _ := json.Marshal(patch)
		_, _ = ts.photofaces.UpdateEntity(ctx, pb, &aztables.UpdateEntityOptions{UpdateMode: aztables.UpdateModeMerge})
	}

	// 3. Delete source person.
	_, _ = ts.persons.DeleteEntity(ctx, "person", sourceID, nil)

	// 4. Recount target.
	return ts.recalcFaceCount(ctx, targetID)
}

// ── FindPersonByName ────────────────────────────────────────────────────────

func (ts *TableStore) FindPersonByName(ctx context.Context, name string) (Person, error) {
	filter := fmt.Sprintf("PartitionKey eq 'person' and Name eq '%s'", escapeOData(name))
	persons, err := ts.queryPersons(ctx, filter)
	if err != nil {
		return Person{}, err
	}
	if len(persons) == 0 {
		return Person{}, fmt.Errorf("facestore: no person named %q", name)
	}
	return persons[0], nil
}

// ── SearchPeople ────────────────────────────────────────────────────────────

func (ts *TableStore) SearchPeople(ctx context.Context, namePrefix string) ([]Person, error) {
	var filter string
	if namePrefix == "" {
		filter = "PartitionKey eq 'person' and Name ne ''"
	} else {
		// Table Storage doesn't support "starts with" natively.
		// We use a range: Name >= prefix AND Name < prefix+1 (lexicographic).
		upper := namePrefix[:len(namePrefix)-1] + string(rune(namePrefix[len(namePrefix)-1]+1))
		filter = fmt.Sprintf("PartitionKey eq 'person' and Name ge '%s' and Name lt '%s'",
			escapeOData(namePrefix), escapeOData(upper))
	}
	return ts.queryPersons(ctx, filter)
}

// ── FindSimilarFaces ────────────────────────────────────────────────────────

func (ts *TableStore) FindSimilarFaces(ctx context.Context, fingerprint []float64, hash string, landmarkTol float64, hashMaxHamming int) ([]Face, error) {
	// Brute-force: page through all faces and compare in memory.
	allFaces, err := ts.queryFaces(ctx, "")
	if err != nil {
		return nil, err
	}

	var matches []Face
	for _, f := range allFaces {
		if euclidean(fingerprint, f.LandmarkFingerprint) > landmarkTol {
			continue
		}
		if hammingHex(hash, f.FaceHash) > hashMaxHamming {
			continue
		}
		matches = append(matches, f)
	}
	return matches, nil
}

// ── GetPhotosByPerson ───────────────────────────────────────────────────────

func (ts *TableStore) GetPhotosByPerson(ctx context.Context, personID string, offset, limit int) ([]PhotoRef, error) {
	faces, err := ts.GetFacesByPerson(ctx, personID)
	if err != nil {
		return nil, err
	}

	// Deduplicate photo refs maintaining order.
	seen := map[string]bool{}
	var refs []PhotoRef
	for _, f := range faces {
		key := f.PhotoRef.Key()
		if seen[key] {
			continue
		}
		seen[key] = true
		refs = append(refs, f.PhotoRef)
	}

	// Apply offset/limit.
	if offset >= len(refs) {
		return nil, nil
	}
	end := offset + limit
	if end > len(refs) {
		end = len(refs)
	}
	return refs[offset:end], nil
}

// ── HasPhotoBeenProcessed ───────────────────────────────────────────────────

func (ts *TableStore) HasPhotoBeenProcessed(ctx context.Context, ref PhotoRef) (bool, error) {
	filter := fmt.Sprintf("PartitionKey eq '%s'", escapeOData(ref.Key()))
	top := int32(1)
	pager := ts.photofaces.NewListEntitiesPager(&aztables.ListEntitiesOptions{Filter: &filter, Top: &top})
	if pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return false, err
		}
		return len(page.Entities) > 0, nil
	}
	return false, nil
}

// ── GetFaceOverlaysForPhoto ─────────────────────────────────────────────────

func (ts *TableStore) GetFaceOverlaysForPhoto(ctx context.Context, ref PhotoRef) ([]FaceOverlay, error) {
	faces, err := ts.GetFacesByPhoto(ctx, ref)
	if err != nil {
		return nil, err
	}

	overlays := make([]FaceOverlay, 0, len(faces))
	for _, f := range faces {
		name := ""
		if p, err := ts.GetPersonByID(ctx, f.PersonID); err == nil {
			name = p.Name
		}
		overlays = append(overlays, FaceOverlay{
			FaceID:     f.FaceID,
			PersonID:   f.PersonID,
			PersonName: name,
			BBox:       f.BBox,
		})
	}
	return overlays, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (ts *TableStore) recalcFaceCount(ctx context.Context, personID string) error {
	filter := fmt.Sprintf("PartitionKey eq '%s'", escapeOData(personID))
	pager := ts.faces.NewListEntitiesPager(&aztables.ListEntitiesOptions{Filter: &filter})

	count := 0
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return err
		}
		count += len(page.Entities)
	}

	patch := map[string]any{
		"PartitionKey": "person",
		"RowKey":       personID,
		"FaceCount":    count,
	}
	b, _ := json.Marshal(patch)
	_, err := ts.persons.UpdateEntity(ctx, b, &aztables.UpdateEntityOptions{UpdateMode: aztables.UpdateModeMerge})
	return err
}

func (ts *TableStore) queryFaces(ctx context.Context, filter string) ([]Face, error) {
	opts := &aztables.ListEntitiesOptions{}
	if filter != "" {
		opts.Filter = &filter
	}
	pager := ts.faces.NewListEntitiesPager(opts)

	var faces []Face
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, raw := range page.Entities {
			var fe faceEntity
			if err := json.Unmarshal(raw, &fe); err != nil {
				continue
			}
			faces = append(faces, faceEntityToFace(fe))
		}
	}
	return faces, nil
}

func (ts *TableStore) queryPersons(ctx context.Context, filter string) ([]Person, error) {
	opts := &aztables.ListEntitiesOptions{}
	if filter != "" {
		opts.Filter = &filter
	}
	pager := ts.persons.NewListEntitiesPager(opts)

	var persons []Person
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, raw := range page.Entities {
			var pe personEntity
			if err := json.Unmarshal(raw, &pe); err != nil {
				continue
			}
			persons = append(persons, Person{
				PersonID:        pe.RowKey,
				Name:            pe.Name,
				FaceCount:       pe.FaceCount,
				ThumbnailFaceID: pe.ThumbnailFaceID,
			})
		}
	}
	return persons, nil
}

func faceEntityToFace(fe faceEntity) Face {
	var fp []float64
	_ = json.Unmarshal([]byte(fe.LandmarkFP), &fp)
	t, _ := time.Parse(time.RFC3339, fe.DetectedAt)
	return Face{
		FaceID:              fe.RowKey,
		PersonID:            fe.PartitionKey,
		PhotoRef:            PhotoRef{Collection: fe.PhotoCollection, Album: fe.PhotoAlbum, Name: fe.PhotoName},
		BBox:                BBox{X: fe.BBoxX, Y: fe.BBoxY, W: fe.BBoxW, H: fe.BBoxH},
		LandmarkFingerprint: fp,
		FaceHash:            fe.FaceHash,
		Confidence:          fe.Confidence,
		CreatedAt:           t,
	}
}

func faceToEntity(f Face) faceEntity {
	fpJSON, _ := json.Marshal(f.LandmarkFingerprint)
	return faceEntity{
		Entity: aztables.Entity{
			PartitionKey: f.PersonID,
			RowKey:       f.FaceID,
		},
		PersonID:        f.PersonID,
		PhotoCollection: f.PhotoRef.Collection,
		PhotoAlbum:      f.PhotoRef.Album,
		PhotoName:       f.PhotoRef.Name,
		BBoxX:           f.BBox.X,
		BBoxY:           f.BBox.Y,
		BBoxW:           f.BBox.W,
		BBoxH:           f.BBox.H,
		LandmarkFP:      string(fpJSON),
		FaceHash:        f.FaceHash,
		Confidence:      f.Confidence,
		DetectedAt:      f.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func escapeOData(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func isTableExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "TableAlreadyExists")
}

// Compile-time check.
var _ FaceStore = (*TableStore)(nil)

// Suppress unused import warning for math.
var _ = math.MaxInt
