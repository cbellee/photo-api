package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── UploadHandler tests ─────────────────────────────────────────────

// createMultipartBody builds a multipart/form-data body with a metadata JSON field
// and a "photo" file field containing a valid JPEG.
func createMultipartBody(t *testing.T, metadata models.ImageTags, imageWidth, imageHeight int) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// metadata field
	mdJSON, _ := json.Marshal(metadata)
	_ = writer.WriteField("metadata", string(mdJSON))

	// photo file
	part, err := writer.CreateFormFile("photo", "test-photo.jpg")
	require.NoError(t, err)

	img := image.NewRGBA(image.Rect(0, 0, imageWidth, imageHeight))
	for x := 0; x < imageWidth; x++ {
		for y := 0; y < imageHeight; y++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	err = jpeg.Encode(part, img, nil)
	require.NoError(t, err)

	writer.Close()
	return body, writer.FormDataContentType()
}

func TestUploadHandler_Success(t *testing.T) {
	cfg := testConfig()
	var savedData []byte
	var savedTags map[string]string
	var savedMeta map[string]string

	mock := &storage.MockBlobStore{
		SaveBlobFunc: func(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
			savedData = data
			savedTags = tags
			savedMeta = metadata
			assert.Equal(t, "uploads", containerName)
			assert.Equal(t, "nature/sunset/test-photo.jpg", blobName)
			assert.Equal(t, "image/jpeg", contentType)
			return nil
		},
	}

	metadata := models.ImageTags{
		Collection:  "nature",
		Album:       "sunset",
		Description: "A test upload",
		Type:        "image/jpeg",
		IsDeleted:   false,
	}

	body, contentType := createMultipartBody(t, metadata, 100, 200)
	req := httptest.NewRequest("POST", "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	handler := UploadHandler(mock, cfg)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	require.NotNil(t, savedData)
	assert.NotEmpty(t, savedData)
	assert.Equal(t, "nature", savedTags["collection"])
	assert.Equal(t, "sunset", savedTags["album"])
	assert.Equal(t, "A test upload", savedTags["description"])
	assert.Equal(t, "100", savedMeta["width"])
	assert.Equal(t, "200", savedMeta["height"])
}

func TestUploadHandler_NilBody_Returns400(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{}

	req := httptest.NewRequest("POST", "/api/upload", nil)
	req.Body = nil
	w := httptest.NewRecorder()

	handler := UploadHandler(mock, cfg)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandler_NoMetadata_Returns400(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	// write a photo but no metadata
	part, _ := writer.CreateFormFile("photo", "test.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	jpeg.Encode(part, img, nil)
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	handler := UploadHandler(mock, cfg)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandler_NoPhotoFile_Returns400(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	mdJSON, _ := json.Marshal(models.ImageTags{Collection: "c", Album: "a"})
	writer.WriteField("metadata", string(mdJSON))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	handler := UploadHandler(mock, cfg)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandler_InvalidMetadataJSON_Returns400(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("metadata", "{invalid json")
	part, _ := writer.CreateFormFile("photo", "test.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	jpeg.Encode(part, img, nil)
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	handler := UploadHandler(mock, cfg)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandler_InvalidImage_Returns400(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	mdJSON, _ := json.Marshal(models.ImageTags{Collection: "c", Album: "a", Type: "image/jpeg"})
	writer.WriteField("metadata", string(mdJSON))
	part, _ := writer.CreateFormFile("photo", "test.jpg")
	part.Write([]byte("not-an-image"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	handler := UploadHandler(mock, cfg)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandler_SaveBlobError_Returns500(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		SaveBlobFunc: func(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
			return fmt.Errorf("storage failure")
		},
	}

	metadata := models.ImageTags{
		Collection: "nature",
		Album:      "sunset",
		Type:       "image/jpeg",
	}
	body, contentType := createMultipartBody(t, metadata, 50, 50)
	req := httptest.NewRequest("POST", "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	handler := UploadHandler(mock, cfg)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUploadHandler_TagStripping(t *testing.T) {
	cfg := testConfig()
	var savedTags map[string]string

	mock := &storage.MockBlobStore{
		SaveBlobFunc: func(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
			savedTags = tags
			return nil
		},
	}

	metadata := models.ImageTags{
		Collection:  "nature&forest",
		Album:       "sunset*beach",
		Description: "Photo with $pecial ch@rs!",
		Type:        "image/jpeg",
	}
	body, contentType := createMultipartBody(t, metadata, 20, 20)
	req := httptest.NewRequest("POST", "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	handler := UploadHandler(mock, cfg)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	// Invalid chars should be stripped
	assert.NotContains(t, savedTags["collection"], "&")
	assert.NotContains(t, savedTags["album"], "*")
	assert.NotContains(t, savedTags["description"], "$")
	assert.NotContains(t, savedTags["description"], "@")
	assert.NotContains(t, savedTags["description"], "!")
}

// ── AlbumHandler edge cases ─────────────────────────────────────────

func TestAlbumHandler_StorageError_Returns500(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			return map[string][]string{"nature": {"sunset"}}, nil
		},
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return nil, fmt.Errorf("storage error")
		},
	}

	handler := AlbumHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/nature", nil)
	req.SetPathValue("collection", "nature")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// When no blobs can be fetched the handler returns 404
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAlbumHandler_AutoAssignsAlbumImage(t *testing.T) {
	cfg := testConfig()
	candidate := models.Blob{
		Name: "nature/forest/p1.jpg",
		Path: "https://stor/images/nature/forest/p1.jpg",
		Tags: map[string]string{"collection": "nature", "album": "forest", "albumImage": "false"},
	}

	mock := &storage.MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			return map[string][]string{"nature": {"sunset", "forest"}}, nil
		},
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			if contains(query, "albumImage='true'") {
				// Only sunset has a marker; forest does not
				return []models.Blob{{
					Name: "nature/sunset/p2.jpg",
					Path: "https://stor/images/nature/sunset/p2.jpg",
					Tags: map[string]string{"collection": "nature", "album": "sunset", "albumImage": "true"},
				}}, nil
			}
			if contains(query, "album='forest'") {
				return []models.Blob{candidate}, nil
			}
			return nil, nil
		},
		SetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string, tags map[string]string) error {
			assert.Equal(t, "nature/forest/p1.jpg", blobName)
			assert.Equal(t, "true", tags["albumImage"])
			return nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			if blobName == "nature/forest/p1.jpg" {
				return candidate.Tags, nil
			}
			return map[string]string{"collection": "nature", "album": "sunset", "albumImage": "true"}, nil
		},
	}

	handler := AlbumHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/nature", nil)
	req.SetPathValue("collection", "nature")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, mock.SetBlobTagsCalls, 1, "should auto-assign albumImage for forest")
}

// contains is a test helper (strings.Contains).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexSubstring(s, substr) >= 0)
}

func indexSubstring(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ── PhotoHandler edge cases ─────────────────────────────────────────

func TestPhotoHandler_StorageError_Returns500(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return nil, fmt.Errorf("storage error")
		},
	}

	handler := PhotoHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api/nature/sunset", nil)
	req.SetPathValue("collection", "nature")
	req.SetPathValue("album", "sunset")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── CollectionHandler edge cases ────────────────────────────────────

func TestCollectionHandler_EmptyTagList_Returns404(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			return map[string][]string{}, nil
		},
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return nil, nil
		},
	}

	handler := CollectionHandler(mock, cfg)
	req := httptest.NewRequest("GET", "/api", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── UpdateHandler edge cases ────────────────────────────────────────

func TestUpdateHandler_GetBlobTagsError_Returns500(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return nil, fmt.Errorf("storage error")
		},
	}

	body := `{"name":"nature/sunset/p.jpg","collection":"nature","album":"sunset","description":"d"}`
	handler := UpdateHandler(mock, cfg)
	req := httptest.NewRequest("PUT", "/api/update/nature/sunset/p", io.NopCloser(bytes.NewBufferString(body)))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── RequireRole middleware tests ─────────────────────────────────────

// testHMACSecret is a symmetric key used to sign test JWTs.
var testHMACSecret = []byte("test-secret-key-at-least-32-bytes!")

// signTestJWT creates a signed JWT string with the given roles.
func signTestJWT(t *testing.T, roles []string, expiry time.Time) string {
	t.Helper()
	claims := models.MyClaims{
		Roles: roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiry),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(testHMACSecret)
	require.NoError(t, err)
	return signed
}

// testKeyfunc returns a jwt.Keyfunc that always provides the test HMAC key.
func testKeyfunc(token *jwt.Token) (interface{}, error) {
	if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	}
	return testHMACSecret, nil
}

func TestRequireRole_NoAuthHeader_Returns401(t *testing.T) {
	cfg := testConfig()
	cfg.JWTKeyfunc = testKeyfunc

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/upload", nil)
	w := httptest.NewRecorder()

	handler := RequireRole(cfg, next)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, called, "next handler should not have been called")
}

func TestRequireRole_InvalidToken_Returns401(t *testing.T) {
	cfg := testConfig()
	cfg.JWTKeyfunc = testKeyfunc

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-string")
	w := httptest.NewRecorder()

	handler := RequireRole(cfg, next)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, called)
}

func TestRequireRole_WrongRole_Returns403(t *testing.T) {
	cfg := testConfig()
	cfg.JWTKeyfunc = testKeyfunc

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	token := signTestJWT(t, []string{"other.role"}, time.Now().Add(time.Hour))
	req := httptest.NewRequest("GET", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler := RequireRole(cfg, next)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, called)
}

func TestRequireRole_ValidToken_CallsNext(t *testing.T) {
	cfg := testConfig()
	cfg.JWTKeyfunc = testKeyfunc

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	token := signTestJWT(t, []string{"photo.upload"}, time.Now().Add(time.Hour))
	req := httptest.NewRequest("GET", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler := RequireRole(cfg, next)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called, "next handler should have been called")
}

func TestRequireRole_ExpiredToken_Returns401(t *testing.T) {
	cfg := testConfig()
	cfg.JWTKeyfunc = testKeyfunc

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	token := signTestJWT(t, []string{"photo.upload"}, time.Now().Add(-time.Hour))
	req := httptest.NewRequest("GET", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler := RequireRole(cfg, next)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, called)
}
