package storage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Construction ─────────────────────────────────────────────────────

func TestNewLocalBlobStore(t *testing.T) {
	store := NewLocalBlobStore("http://localhost:10000/", "http://public:10000/")
	assert.NotNil(t, store)
	assert.Equal(t, "http://localhost:10000", store.baseURL, "trailing slash should be trimmed")
	assert.Equal(t, "http://public:10000", store.publicURL, "trailing slash should be trimmed")
	assert.NotNil(t, store.httpClient)
}

func TestLocalBlobStore_blobURL(t *testing.T) {
	store := NewLocalBlobStore("http://localhost:10000", "http://pub:10000")

	t.Run("simple name", func(t *testing.T) {
		u := store.blobURL("images", "photo.jpg")
		assert.Equal(t, "http://localhost:10000/images/photo.jpg", u)
	})

	t.Run("nested path", func(t *testing.T) {
		u := store.blobURL("images", "nature/sunset/photo.jpg")
		assert.Equal(t, "http://localhost:10000/images/nature/sunset/photo.jpg", u)
	})

	t.Run("path with spaces", func(t *testing.T) {
		u := store.blobURL("images", "my album/photo 1.jpg")
		assert.Contains(t, u, "my%20album/photo%201.jpg")
	})
}

// ── FilterBlobsByTags ────────────────────────────────────────────────

func TestLocalBlobStore_FilterBlobsByTags_Success(t *testing.T) {
	items := []blobResponse{
		{Name: "nature/sunset/p1.jpg", Container: "images", Tags: map[string]string{"collection": "nature"}, Metadata: map[string]string{"Width": "1920"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/query", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		json.Unmarshal(body, &payload)
		assert.Contains(t, payload["query"], "collection")

		json.NewEncoder(w).Encode(items)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, "http://public:10000")
	blobs, err := store.FilterBlobsByTags(context.Background(), "collection='nature'", "images")

	require.NoError(t, err)
	require.Len(t, blobs, 1)
	assert.Equal(t, "nature/sunset/p1.jpg", blobs[0].Name)
	assert.Contains(t, blobs[0].Path, "http://public:10000/images/nature/sunset/p1.jpg")
	assert.Equal(t, "1920", blobs[0].MetaData["Width"])
}

func TestLocalBlobStore_FilterBlobsByTags_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]blobResponse{})
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	blobs, err := store.FilterBlobsByTags(context.Background(), "q", "c")
	assert.NoError(t, err)
	assert.Nil(t, blobs, "empty result should return nil")
}

func TestLocalBlobStore_FilterBlobsByTags_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	blobs, err := store.FilterBlobsByTags(context.Background(), "q", "c")
	assert.Error(t, err)
	assert.Nil(t, blobs)
	assert.Contains(t, err.Error(), "500")
}

// ── GetBlobTags ──────────────────────────────────────────────────────

func TestLocalBlobStore_GetBlobTags_Success(t *testing.T) {
	tags := map[string]string{"collection": "nature", "album": "sunset"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasSuffix(r.URL.RawQuery, "comp=tags"))
		assert.Equal(t, http.MethodGet, r.Method)
		json.NewEncoder(w).Encode(tags)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	result, err := store.GetBlobTags(context.Background(), "p.jpg", "images")
	require.NoError(t, err)
	assert.Equal(t, "nature", result["collection"])
}

func TestLocalBlobStore_GetBlobTags_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	_, err := store.GetBlobTags(context.Background(), "p.jpg", "images")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// ── SetBlobTags ──────────────────────────────────────────────────────

func TestLocalBlobStore_SetBlobTags_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.True(t, strings.Contains(r.URL.RawQuery, "comp=tags"))

		body, _ := io.ReadAll(r.Body)
		var tags map[string]string
		json.Unmarshal(body, &tags)
		assert.Equal(t, "true", tags["albumImage"])

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	err := store.SetBlobTags(context.Background(), "p.jpg", "images", map[string]string{"albumImage": "true"})
	assert.NoError(t, err)
}

func TestLocalBlobStore_SetBlobTags_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	err := store.SetBlobTags(context.Background(), "p.jpg", "images", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

// ── GetBlobMetadata ──────────────────────────────────────────────────

func TestLocalBlobStore_GetBlobMetadata_Success(t *testing.T) {
	md := map[string]string{"Width": "1920", "Height": "1080"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, "comp=metadata")
		json.NewEncoder(w).Encode(md)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	result, err := store.GetBlobMetadata(context.Background(), "p.jpg", "images")
	require.NoError(t, err)
	assert.Equal(t, "1920", result["Width"])
}

func TestLocalBlobStore_GetBlobMetadata_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	_, err := store.GetBlobMetadata(context.Background(), "p.jpg", "images")
	assert.Error(t, err)
}

// ── GetBlobTagList ───────────────────────────────────────────────────

func TestLocalBlobStore_GetBlobTagList_Success(t *testing.T) {
	items := []blobResponse{
		{Name: "nature/sunset/p1.jpg", Tags: map[string]string{"collection": "nature", "album": "sunset"}},
		{Name: "nature/forest/p2.jpg", Tags: map[string]string{"collection": "nature", "album": "forest"}},
		{Name: "nature/sunset/p3.jpg", Tags: map[string]string{"collection": "nature", "album": "sunset"}}, // duplicate
		{Name: "sport/surf/p4.jpg", Tags: map[string]string{"collection": "sport", "album": "surf"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(items)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	result, err := store.GetBlobTagList(context.Background(), "images")
	require.NoError(t, err)
	require.Len(t, result, 2, "should have nature + sport")
	assert.ElementsMatch(t, []string{"sunset", "forest"}, result["nature"])
	assert.Equal(t, []string{"surf"}, result["sport"])
}

func TestLocalBlobStore_GetBlobTagList_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	_, err := store.GetBlobTagList(context.Background(), "images")
	assert.Error(t, err)
}

// ── GetBlob ──────────────────────────────────────────────────────────

func TestLocalBlobStore_GetBlob_Success(t *testing.T) {
	blobData := []byte("fake-image-data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Write(blobData)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	data, err := store.GetBlob(context.Background(), "nature/sunset/p1.jpg", "images")
	require.NoError(t, err)
	assert.Equal(t, blobData, data)
}

func TestLocalBlobStore_GetBlob_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	_, err := store.GetBlob(context.Background(), "missing.jpg", "images")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// ── SaveBlob ─────────────────────────────────────────────────────────

func TestLocalBlobStore_SaveBlob_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "image/jpeg", r.Header.Get("Content-Type"))

		// Verify tags and metadata headers
		var tags map[string]string
		json.Unmarshal([]byte(r.Header.Get("X-Blob-Tags")), &tags)
		assert.Equal(t, "nature", tags["collection"])

		var md map[string]string
		json.Unmarshal([]byte(r.Header.Get("X-Blob-Metadata")), &md)
		assert.Equal(t, "1920", md["Width"])

		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, "image-data", string(body))

		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	err := store.SaveBlob(
		context.Background(),
		strings.NewReader("image-data"),
		int64(len("image-data")),
		"nature/sunset/p1.jpg",
		"images",
		map[string]string{"collection": "nature"},
		map[string]string{"Width": "1920"},
		"image/jpeg",
	)
	assert.NoError(t, err)
}

func TestLocalBlobStore_SaveBlob_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "disk full", http.StatusInternalServerError)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	err := store.SaveBlob(context.Background(), strings.NewReader("data"), 4, "b", "c", nil, nil, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestLocalBlobStore_SaveBlob_NoContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// When no content type, the header should not be "application/json" or similar
		assert.Empty(t, r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	err := store.SaveBlob(context.Background(), strings.NewReader("data"), 4, "b", "c", nil, nil, "")
	assert.NoError(t, err)
}

// ── Context cancellation ─────────────────────────────────────────────

func TestLocalBlobStore_CancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := store.GetBlob(ctx, "b", "c")
	assert.Error(t, err)
}

// Compile-time check
func TestLocalBlobStore_ImplementsBlobStore(t *testing.T) {
	var _ BlobStore = (*LocalBlobStore)(nil)
}

// ── Malformed JSON response tests ────────────────────────────────────

func TestLocalBlobStore_FilterBlobsByTags_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{not valid json"))
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	blobs, err := store.FilterBlobsByTags(context.Background(), "q", "c")
	assert.Error(t, err)
	assert.Nil(t, blobs)
	assert.Contains(t, err.Error(), "decoding response")
}

func TestLocalBlobStore_GetBlobTags_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	_, err := store.GetBlobTags(context.Background(), "p.jpg", "images")
	assert.Error(t, err)
}

func TestLocalBlobStore_GetBlobMetadata_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[broken"))
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	_, err := store.GetBlobMetadata(context.Background(), "p.jpg", "images")
	assert.Error(t, err)
}

func TestLocalBlobStore_GetBlobTagList_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{bad"))
	}))
	defer srv.Close()

	store := NewLocalBlobStore(srv.URL, srv.URL)
	_, err := store.GetBlobTagList(context.Background(), "images")
	assert.Error(t, err)
}
