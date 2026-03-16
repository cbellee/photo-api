package facestore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempDB(t *testing.T) *SQLiteStore {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "facetest-*.db")
	require.NoError(t, err)
	f.Close()

	store, err := NewSQLiteStore(f.Name())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSaveFaceCreatesPersonAndFace(t *testing.T) {
	store := tempDB(t)
	ctx := context.Background()

	face := Face{
		FaceID:              "face-1",
		PersonID:            "person-1",
		PhotoRef:            PhotoRef{Collection: "trips", Album: "paris", Name: "img001.jpg"},
		BBox:                BBox{X: 0.1, Y: 0.2, W: 0.3, H: 0.4},
		LandmarkFingerprint: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
		FaceHash:            "abcdef0123456789",
		Confidence:          0.95,
		CreatedAt:           time.Now(),
	}

	err := store.SaveFace(ctx, face)
	require.NoError(t, err)

	person, err := store.GetPersonByID(ctx, "person-1")
	require.NoError(t, err)
	assert.Equal(t, "person-1", person.PersonID)
	assert.Equal(t, 1, person.FaceCount)
	assert.Equal(t, "face-1", person.ThumbnailFaceID)

	faces, err := store.GetFacesByPerson(ctx, "person-1")
	require.NoError(t, err)
	assert.Len(t, faces, 1)
	assert.Equal(t, "face-1", faces[0].FaceID)
}

func TestGetFacesByPhoto(t *testing.T) {
	store := tempDB(t)
	ctx := context.Background()

	ref := PhotoRef{Collection: "trips", Album: "paris", Name: "img002.jpg"}
	for i, fid := range []string{"f1", "f2"} {
		err := store.SaveFace(ctx, Face{
			FaceID:              fid,
			PersonID:            "p1",
			PhotoRef:            ref,
			BBox:                BBox{X: float64(i) * 0.3, Y: 0.1, W: 0.2, H: 0.2},
			LandmarkFingerprint: []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			FaceHash:            "0000000000000000",
			Confidence:          0.90,
			CreatedAt:           time.Now(),
		})
		require.NoError(t, err)
	}

	faces, err := store.GetFacesByPhoto(ctx, ref)
	require.NoError(t, err)
	assert.Len(t, faces, 2)
}

func TestSetPersonName(t *testing.T) {
	store := tempDB(t)
	ctx := context.Background()

	err := store.SaveFace(ctx, Face{
		FaceID:              "f1",
		PersonID:            "p1",
		PhotoRef:            PhotoRef{Collection: "c", Album: "a", Name: "n.jpg"},
		LandmarkFingerprint: []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		FaceHash:            "0000000000000000",
		Confidence:          0.9,
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	err = store.SetPersonName(ctx, "p1", "Alice")
	require.NoError(t, err)

	person, err := store.GetPersonByID(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, "Alice", person.Name)
}

func TestSearchPeople(t *testing.T) {
	store := tempDB(t)
	ctx := context.Background()

	for _, pair := range []struct{ pid, name string }{{"p1", "Alice"}, {"p2", "Bob"}} {
		err := store.SaveFace(ctx, Face{
			FaceID:              "f-" + pair.pid,
			PersonID:            pair.pid,
			PhotoRef:            PhotoRef{Collection: "c", Album: "a", Name: pair.pid + ".jpg"},
			LandmarkFingerprint: []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			FaceHash:            "0000000000000000",
			Confidence:          0.9,
			CreatedAt:           time.Now(),
		})
		require.NoError(t, err)
		err = store.SetPersonName(ctx, pair.pid, pair.name)
		require.NoError(t, err)
	}

	results, err := store.SearchPeople(ctx, "ali")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Alice", results[0].Name)
}

func TestMergePeople(t *testing.T) {
	store := tempDB(t)
	ctx := context.Background()

	for _, pair := range []struct{ fid, pid string }{{"f1", "p1"}, {"f2", "p2"}} {
		err := store.SaveFace(ctx, Face{
			FaceID:              pair.fid,
			PersonID:            pair.pid,
			PhotoRef:            PhotoRef{Collection: "c", Album: "a", Name: pair.fid + ".jpg"},
			LandmarkFingerprint: []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			FaceHash:            "0000000000000000",
			Confidence:          0.9,
			CreatedAt:           time.Now(),
		})
		require.NoError(t, err)
	}

	err := store.MergePeople(ctx, "p2", "p1")
	require.NoError(t, err)

	person, err := store.GetPersonByID(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, 2, person.FaceCount)

	_, err = store.GetPersonByID(ctx, "p2")
	assert.Error(t, err)
}

func TestHasPhotoBeenProcessed(t *testing.T) {
	store := tempDB(t)
	ctx := context.Background()

	ref := PhotoRef{Collection: "c", Album: "a", Name: "test.jpg"}
	processed, err := store.HasPhotoBeenProcessed(ctx, ref)
	require.NoError(t, err)
	assert.False(t, processed)

	err = store.SaveFace(ctx, Face{
		FaceID:              "f1",
		PersonID:            "p1",
		PhotoRef:            ref,
		LandmarkFingerprint: []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		FaceHash:            "0000000000000000",
		Confidence:          0.9,
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	processed, err = store.HasPhotoBeenProcessed(ctx, ref)
	require.NoError(t, err)
	assert.True(t, processed)
}

func TestFindSimilarFaces(t *testing.T) {
	store := tempDB(t)
	ctx := context.Background()

	fp := []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}
	hash := "abcdef0123456789"

	err := store.SaveFace(ctx, Face{
		FaceID:              "f1",
		PersonID:            "p1",
		PhotoRef:            PhotoRef{Collection: "c", Album: "a", Name: "test.jpg"},
		BBox:                BBox{X: 0.1, Y: 0.2, W: 0.3, H: 0.4},
		LandmarkFingerprint: fp,
		FaceHash:            hash,
		Confidence:          0.95,
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	matches, err := store.FindSimilarFaces(ctx, fp, hash, 0.35, 10)
	require.NoError(t, err)
	assert.Len(t, matches, 1)
	assert.Equal(t, "f1", matches[0].FaceID)

	diffFP := []float64{9, 9, 9, 9, 9, 9, 9, 9, 9, 9}
	matches, err = store.FindSimilarFaces(ctx, diffFP, hash, 0.35, 10)
	require.NoError(t, err)
	assert.Len(t, matches, 0)
}

func TestGetFaceOverlaysForPhoto(t *testing.T) {
	store := tempDB(t)
	ctx := context.Background()

	ref := PhotoRef{Collection: "c", Album: "a", Name: "overlay.jpg"}
	err := store.SaveFace(ctx, Face{
		FaceID:              "f1",
		PersonID:            "p1",
		PhotoRef:            ref,
		BBox:                BBox{X: 0.1, Y: 0.2, W: 0.3, H: 0.4},
		LandmarkFingerprint: []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		FaceHash:            "0000000000000000",
		Confidence:          0.9,
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)
	_ = store.SetPersonName(ctx, "p1", "Alice")

	overlays, err := store.GetFaceOverlaysForPhoto(ctx, ref)
	require.NoError(t, err)
	assert.Len(t, overlays, 1)
	assert.Equal(t, "Alice", overlays[0].PersonName)
	assert.Equal(t, "f1", overlays[0].FaceID)
	assert.InDelta(t, 0.1, overlays[0].BBox.X, 0.001)
}

func TestGetPhotosByPerson(t *testing.T) {
	store := tempDB(t)
	ctx := context.Background()

	for i, name := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		err := store.SaveFace(ctx, Face{
			FaceID:              name,
			PersonID:            "p1",
			PhotoRef:            PhotoRef{Collection: "c", Album: "a", Name: name},
			LandmarkFingerprint: []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			FaceHash:            "0000000000000000",
			Confidence:          float32(0.9 - float64(i)*0.01),
			CreatedAt:           time.Now().Add(-time.Duration(i) * time.Hour),
		})
		require.NoError(t, err)
	}

	refs, err := store.GetPhotosByPerson(ctx, "p1", 0, 2)
	require.NoError(t, err)
	assert.Len(t, refs, 2)

	refs, err = store.GetPhotosByPerson(ctx, "p1", 2, 10)
	require.NoError(t, err)
	assert.Len(t, refs, 1)
}
