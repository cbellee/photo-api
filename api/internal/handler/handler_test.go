package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Test fixtures ───────────────────────────────────────────────────

func testConfig() *Config {
	return &Config{
		ServiceName:          "testService",
		ServicePort:          "8080",
		UploadsContainerName: "uploads",
		ImagesContainerName:  "images",
		StorageUrl:           "https://teststorage.blob.core.windows.net",
		MemoryLimitMb:        32,
		JwksURL:              "https://test.jwks.url",
		RoleName:             "photo.upload",
		CorsOrigins:          []string{"http://localhost:5173"},
	}
}

func sampleBlobs() []models.Blob {
	return []models.Blob{
		{
			Name: "nature/sunset/photo1.jpg",
			Path: "https://teststorage.blob.core.windows.net/images/nature/sunset/photo1.jpg",
			Tags: map[string]string{
				"collection":      "nature",
				"album":           "sunset",
				"description":     "A sunset photo",
				"isDeleted":       "false",
				"albumImage":      "true",
				"collectionImage": "true",
				"orientation":     "1",
			},
			MetaData: map[string]string{
				"Width":    "1920",
				"Height":   "1080",
				"ExifData": `{"camera":"Canon"}`,
			},
		},
		{
			Name: "nature/sunset/photo2.jpg",
			Path: "https://teststorage.blob.core.windows.net/images/nature/sunset/photo2.jpg",
			Tags: map[string]string{
				"collection":      "nature",
				"album":           "sunset",
				"description":     "Another sunset",
				"isDeleted":       "false",
				"albumImage":      "false",
				"collectionImage": "false",
				"orientation":     "0",
			},
			MetaData: map[string]string{
				"Width":  "1600",
				"Height": "900",
			},
		},
	}
}

// ── BlobsToPhotos tests ─────────────────────────────────────────────

func TestBlobsToPhotos_ConvertsCorrectly(t *testing.T) {
	blobs := sampleBlobs()
	photos := BlobsToPhotos(blobs)

	require.Len(t, photos, 2)

	// First photo
	assert.Equal(t, "nature/sunset/photo1.jpg", photos[0].Name)
	assert.Equal(t, blobs[0].Path, photos[0].Src)
	assert.Equal(t, 1920, photos[0].Width)
	assert.Equal(t, 1080, photos[0].Height)
	assert.Equal(t, "nature", photos[0].Collection)
	assert.Equal(t, "sunset", photos[0].Album)
	assert.Equal(t, "A sunset photo", photos[0].Description)
	assert.Equal(t, `{"camera":"Canon"}`, photos[0].ExifData)
	assert.False(t, photos[0].IsDeleted)
	assert.True(t, photos[0].AlbumImage)
	assert.True(t, photos[0].CollectionImage)
	assert.Equal(t, 1, photos[0].Orientation)

	// Second photo
	assert.Equal(t, 1600, photos[1].Width)
	assert.Equal(t, 900, photos[1].Height)
	assert.False(t, photos[1].AlbumImage)
	assert.False(t, photos[1].CollectionImage)
	assert.Equal(t, 0, photos[1].Orientation)
}

func TestBlobsToPhotos_EmptySlice(t *testing.T) {
	photos := BlobsToPhotos([]models.Blob{})
	assert.NotNil(t, photos) // should be empty slice, not nil
	assert.Empty(t, photos)
}

func TestBlobsToPhotos_InvalidMetadataDefaults(t *testing.T) {
	blobs := []models.Blob{
		{
			Name: "test/img.jpg",
			Path: "https://stor/images/test/img.jpg",
			Tags: map[string]string{
				"isDeleted":       "garbage",
				"albumImage":      "",
				"collectionImage": "xyz",
				"orientation":     "notanumber",
			},
			MetaData: map[string]string{
				"Width":  "abc",
				"Height": "",
			},
		},
	}

	photos := BlobsToPhotos(blobs)
	require.Len(t, photos, 1)
	assert.Equal(t, 0, photos[0].Width)
	assert.Equal(t, 0, photos[0].Height)
	assert.False(t, photos[0].IsDeleted)
	assert.False(t, photos[0].AlbumImage)
	assert.False(t, photos[0].CollectionImage)
	assert.Equal(t, 0, photos[0].Orientation)
}

// ── TagListHandler tests ────────────────────────────────────────────

func TestTagListHandler_ReturnsTagMap(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			assert.Equal(t, "images", containerName)
			return map[string][]string{
				"nature":       {"sunset", "mountains"},
				"architecture": {"bridges"},
			}, nil
		},
	}

	handler := TagListHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/tags", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result map[string][]string
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, []string{"sunset", "mountains"}, result["nature"])
}

func TestTagListHandler_ErrorReturns500(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			return nil, fmt.Errorf("storage error")
		},
	}

	handler := TagListHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/tags", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── PhotoHandler tests ──────────────────────────────────────────────

func TestPhotoHandler_ReturnsPhotos(t *testing.T) {
	cfg := testConfig()
	blobs := sampleBlobs()
	mock := &storage.MockBlobStore{
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			assert.Contains(t, query, "collection='nature'")
			assert.Contains(t, query, "album='sunset'")
			assert.Contains(t, query, "isDeleted='false'")
			return blobs, nil
		},
	}

	handler := PhotoHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/nature/sunset", nil)
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "sunset")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var photos []models.Photo
	err := json.Unmarshal(w.Body.Bytes(), &photos)
	require.NoError(t, err)
	assert.Len(t, photos, 2)
	assert.Equal(t, "nature/sunset/photo1.jpg", photos[0].Name)
}

func TestPhotoHandler_IncludeDeleted_OmitsDeletedFilter(t *testing.T) {
	cfg := testConfig()
	blobs := sampleBlobs()
	mock := &storage.MockBlobStore{
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			assert.Contains(t, query, "collection='nature'")
			assert.Contains(t, query, "album='sunset'")
			assert.NotContains(t, query, "isDeleted")
			return blobs, nil
		},
	}

	handler := PhotoHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/nature/sunset?includeDeleted=true", nil)
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "sunset")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var photos []models.Photo
	err := json.Unmarshal(w.Body.Bytes(), &photos)
	require.NoError(t, err)
	assert.Len(t, photos, 2)
}

func TestPhotoHandler_MissingCollection_Returns400(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{}

	handler := PhotoHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api//sunset", nil)
	req.SetPathValue("collection", "")
	req.SetPathValue("album", "sunset")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPhotoHandler_MissingAlbum_Returns400(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{}

	handler := PhotoHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/nature/", nil)
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPhotoHandler_NoBlobsFound_Returns404(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return nil, nil // unified contract: empty results return nil, nil
		},
	}

	handler := PhotoHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/nature/sunset", nil)
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "sunset")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── AlbumHandler tests ──────────────────────────────────────────────

func TestAlbumHandler_ReturnsAlbums(t *testing.T) {
	cfg := testConfig()
	blobs := sampleBlobs()[:1] // just one album image
	mock := &storage.MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			return map[string][]string{"nature": {"sunset"}}, nil
		},
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			if strings.Contains(query, "albumImage='true'") {
				return blobs, nil
			}
			return blobs, nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return blobs[0].Tags, nil
		},
	}

	handler := AlbumHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/nature", nil)
	req.SetPathValue("collection", "nature")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var photos []models.Photo
	err := json.Unmarshal(w.Body.Bytes(), &photos)
	require.NoError(t, err)
	assert.Len(t, photos, 1)
	assert.Equal(t, "sunset", photos[0].Album)
}

func TestAlbumHandler_MissingCollection_Returns400(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{}

	handler := AlbumHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/", nil)
	req.SetPathValue("collection", "")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAlbumHandler_NoBlobsFound_Returns404(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			return nil, fmt.Errorf("no tags")
		},
	}

	handler := AlbumHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/nature", nil)
	req.SetPathValue("collection", "nature")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── CollectionHandler tests ─────────────────────────────────────────

func TestCollectionHandler_ReturnsCollections(t *testing.T) {
	cfg := testConfig()
	blobs := sampleBlobs()[:1]
	mock := &storage.MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			return map[string][]string{"nature": {"sunset"}}, nil
		},
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			if strings.Contains(query, "collectionImage='true'") {
				return blobs, nil // nature already marked
			}
			return blobs, nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return blobs[0].Tags, nil
		},
	}

	handler := CollectionHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var photos []models.Photo
	err := json.Unmarshal(w.Body.Bytes(), &photos)
	require.NoError(t, err)
	assert.Len(t, photos, 1)
	assert.Equal(t, "nature", photos[0].Collection)
}

func TestCollectionHandler_FallbackQuery(t *testing.T) {
	cfg := testConfig()
	blobs := sampleBlobs()[:1]
	// Blob has collectionImage='true' but there's a second collection 'sport'
	// that has no marker yet — so the handler should auto-assign one.
	sportBlob := models.Blob{
		Name: "sport/ravens/photo1.jpg",
		Path: "https://teststorage.blob.core.windows.net/images/sport/ravens/photo1.jpg",
		Tags: map[string]string{"collection": "sport", "album": "ravens", "collectionImage": "false"},
	}
	callCount := 0
	mock := &storage.MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			return map[string][]string{"nature": {"sunset"}, "sport": {"ravens"}}, nil
		},
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			callCount++
			if strings.Contains(query, "collectionImage='true'") {
				// nature already marked
				return blobs, nil
			}
			if strings.Contains(query, "collection='sport'") {
				return []models.Blob{sportBlob}, nil
			}
			return nil, fmt.Errorf("no blobs found")
		},
		SetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string, tags map[string]string) error {
			assert.Equal(t, "true", tags["collectionImage"])
			return nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			if blobName == "sport/ravens/photo1.jpg" {
				return sportBlob.Tags, nil
			}
			return blobs[0].Tags, nil
		},
	}

	handler := CollectionHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, mock.SetBlobTagsCalls, 1) // should auto-assign collectionImage for 'sport'

	var photos []models.Photo
	err := json.Unmarshal(w.Body.Bytes(), &photos)
	require.NoError(t, err)
	assert.Len(t, photos, 2) // nature + sport
}

func TestCollectionHandler_NoBlobs_Returns404(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			return nil, fmt.Errorf("no tags")
		},
	}

	handler := CollectionHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── UpdateHandler tests ─────────────────────────────────────────────

func TestUpdateHandler_UpdatesTags(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{
				"name":            "nature/sunset/photo1.jpg",
				"collection":      "nature",
				"album":           "sunset",
				"description":     "Old description",
				"isDeleted":       "false",
				"collectionImage": "false",
				"albumImage":      "false",
			}, nil
		},
		SetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string, tags map[string]string) error {
			return nil
		},
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return nil, fmt.Errorf("no collection image")
		},
	}

	body := `{"name":"nature/sunset/photo1.jpg","collection":"nature","album":"sunset","description":"Updated description","isDeleted":"false","collectionImage":"false","albumImage":"false"}`
	handler := UpdateHandler(mock, cfg)
	req := httptest.NewRequest("PUT", "/api/update/nature/sunset/photo1.jpg", strings.NewReader(body))
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "sunset")
	req.SetPathValue("id", "photo1.jpg")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, mock.SetBlobTagsCalls, 1)
	assert.Equal(t, "Updated description", mock.SetBlobTagsCalls[0].Tags["description"])
}

func TestUpdateHandler_EmptyBody_Returns400(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{}

	handler := UpdateHandler(mock, cfg)
	req := httptest.NewRequest("PUT", "/api/update/nature/sunset/photo1.jpg", nil)
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "sunset")
	req.SetPathValue("id", "photo1.jpg")
	req.Body = nil
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateHandler_InvalidJSON_Returns400(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{}

	handler := UpdateHandler(mock, cfg)
	req := httptest.NewRequest("PUT", "/api/update/nature/sunset/photo1.jpg", strings.NewReader("{invalid"))
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "sunset")
	req.SetPathValue("id", "photo1.jpg")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateHandler_TagsUnchanged_Returns304(t *testing.T) {
	cfg := testConfig()
	tags := map[string]string{
		"name":            "nature/sunset/photo1.jpg",
		"collection":      "nature",
		"album":           "sunset",
		"description":     "Same description",
		"isDeleted":       "false",
		"collectionImage": "false",
		"albumImage":      "false",
	}
	mock := &storage.MockBlobStore{
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			// Return same tags (simulating no change)
			return tags, nil
		},
	}

	body, _ := json.Marshal(tags)
	handler := UpdateHandler(mock, cfg)
	req := httptest.NewRequest("PUT", "/api/update/nature/sunset/photo1.jpg", strings.NewReader(string(body)))
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "sunset")
	req.SetPathValue("id", "photo1.jpg")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotModified, w.Code)
	assert.Empty(t, mock.SetBlobTagsCalls) // no SetBlobTags call
}

func TestUpdateHandler_SetsBlobTagsError_Returns500(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{
				"name":            "nature/sunset/photo1.jpg",
				"collection":      "nature",
				"description":     "Old",
				"collectionImage": "false",
			}, nil
		},
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return nil, fmt.Errorf("no collection image")
		},
		SetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string, tags map[string]string) error {
			return fmt.Errorf("storage write error")
		},
	}

	body := `{"name":"nature/sunset/photo1.jpg","collection":"nature","description":"New","collectionImage":"false"}`
	handler := UpdateHandler(mock, cfg)
	req := httptest.NewRequest("PUT", "/api/update/nature/sunset/photo1.jpg", strings.NewReader(body))
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "sunset")
	req.SetPathValue("id", "photo1.jpg")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUpdateHandler_SwapsCollectionImage(t *testing.T) {
	cfg := testConfig()
	existingCollectionImageBlob := models.Blob{
		Name: "nature/sunset/old-collection-image.jpg",
		Path: "https://teststorage.blob.core.windows.net/images/nature/sunset/old-collection-image.jpg",
		Tags: map[string]string{
			"name":            "nature/sunset/old-collection-image.jpg",
			"collection":      "nature",
			"collectionImage": "true",
		},
	}

	setBlobTagsCalls := []storage.SetBlobTagsCall{}
	mock := &storage.MockBlobStore{
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{
				"name":            "nature/sunset/photo1.jpg",
				"collection":      "nature",
				"album":           "sunset",
				"description":     "Old",
				"isDeleted":       "false",
				"collectionImage": "false",
				"albumImage":      "false",
			}, nil
		},
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return []models.Blob{existingCollectionImageBlob}, nil
		},
		SetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string, tags map[string]string) error {
			setBlobTagsCalls = append(setBlobTagsCalls, storage.SetBlobTagsCall{
				BlobName: blobName, Tags: tags,
			})
			return nil
		},
	}

	body := `{"name":"nature/sunset/photo1.jpg","collection":"nature","album":"sunset","description":"New","isDeleted":"false","collectionImage":"true","albumImage":"false"}`
	handler := UpdateHandler(mock, cfg)
	req := httptest.NewRequest("PUT", "/api/update/nature/sunset/photo1.jpg", strings.NewReader(body))
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "sunset")
	req.SetPathValue("id", "photo1.jpg")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// Should have 2 SetBlobTags calls: one to clear old, one to set new
	require.Len(t, setBlobTagsCalls, 2)
	assert.Equal(t, "nature/sunset/old-collection-image.jpg", setBlobTagsCalls[0].BlobName)
	assert.Equal(t, "false", setBlobTagsCalls[0].Tags["collectionImage"])
	assert.Equal(t, "nature/sunset/photo1.jpg", setBlobTagsCalls[1].BlobName)
}

// ── GetCollectionImage tests ────────────────────────────────────────

func TestGetCollectionImage_Found(t *testing.T) {
	cfg := testConfig()
	blobs := sampleBlobs()[:1]
	mock := &storage.MockBlobStore{
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			assert.Contains(t, query, "collection='nature'")
			assert.Contains(t, query, "collectionImage='true'")
			return blobs, nil
		},
	}

	result, err := GetCollectionImage(mock, context.Background(), cfg, "nature")

	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "nature/sunset/photo1.jpg", result[0].Name)
}

func TestGetCollectionImage_NotFound(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return nil, fmt.Errorf("no blobs found")
		},
	}

	result, err := GetCollectionImage(mock, context.Background(), cfg, "nonexistent")

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestGetCollectionImage_EmptyResults(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return []models.Blob{}, nil // empty slice, no error
		},
	}

	result, err := GetCollectionImage(mock, context.Background(), cfg, "empty-collection")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no collection image found")
}

// ── Config tests ────────────────────────────────────────────────────

func TestConfig_Defaults(t *testing.T) {
	cfg := testConfig()
	assert.Equal(t, "testService", cfg.ServiceName)
	assert.Equal(t, "8080", cfg.ServicePort)
	assert.Equal(t, "uploads", cfg.UploadsContainerName)
	assert.Equal(t, "images", cfg.ImagesContainerName)
	assert.Contains(t, cfg.StorageUrl, "https://")
	assert.Equal(t, int64(32), cfg.MemoryLimitMb)
}
