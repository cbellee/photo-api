package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blobURL mirrors the encoding logic in api/internal/storage/local.go
// to reproduce the exact HTTP paths the photo-api sends to blobemu.
func blobURL(base, container, blobName string) string {
	segments := strings.Split(blobName, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return base + "/" + url.PathEscape(container) + "/" + strings.Join(segments, "/")
}

// newTestMux creates an http.ServeMux wired to the given Store, identical
// to the production setup in main() but without CORS or body-size limits.
func newTestMux(store *Store) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /query", queryHandler(store))
	mux.HandleFunc("GET /{container}", listHandler(store))
	mux.HandleFunc("GET /{container}/{blob...}", blobGetHandler(store))
	allowedCT := map[string]bool{
		"image/jpeg":               true,
		"application/octet-stream": true,
	}
	mux.HandleFunc("PUT /{container}/{blob...}", blobPutHandler(store, nil, "uploads", 100<<20, allowedCT))
	mux.HandleFunc("DELETE /{container}/{blob...}", blobDeleteHandler(store))
	return mux
}

// TestBlobRoundTrip_SpacesInName verifies that blobs whose names contain
// spaces can be PUT and then GET back via properly percent-encoded URLs.
func TestBlobRoundTrip_SpacesInName(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	mux := newTestMux(store)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	container := "uploads"
	blobName := "sport/ravens vs stingrays/Ravens vs Stingrays - August 2025-71.jpg"
	body := "fake jpeg data"
	tags := map[string]string{"collection": "sport", "album": "ravens vs stingrays"}
	tagsJSON, _ := json.Marshal(tags)

	// ── PUT the blob via percent-encoded URL (same as LocalBlobStore.SaveBlob) ──
	putURL := blobURL(ts.URL, container, blobName)
	t.Logf("PUT URL: %s", putURL)

	req, err := http.NewRequest(http.MethodPut, putURL, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "image/jpeg")
	req.Header.Set("X-Blob-Tags", string(tagsJSON))

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode, "PUT should succeed")

	// ── GET the blob back ──
	getURL := blobURL(ts.URL, container, blobName)
	t.Logf("GET URL: %s", getURL)

	resp2, err := http.Get(getURL)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode, "GET should find the blob")

	data, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(data))
	assert.Equal(t, "image/jpeg", resp2.Header.Get("Content-Type"))

	// ── GET tags ──
	tagsURL := blobURL(ts.URL, container, blobName) + "?comp=tags"
	resp3, err := http.Get(tagsURL)
	require.NoError(t, err)
	defer resp3.Body.Close()
	require.Equal(t, http.StatusOK, resp3.StatusCode, "GET tags should succeed")

	var gotTags map[string]string
	require.NoError(t, json.NewDecoder(resp3.Body).Decode(&gotTags))
	assert.Equal(t, "sport", gotTags["collection"])
	assert.Equal(t, "ravens vs stingrays", gotTags["album"])
}

// TestBlobRoundTrip_NoSpaces is a baseline test showing the round-trip
// works for simple blob names with no special characters.
func TestBlobRoundTrip_NoSpaces(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	mux := newTestMux(store)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	container := "uploads"
	blobName := "sport/soccer/goal.jpg"
	body := "simple image data"

	putURL := blobURL(ts.URL, container, blobName)
	req, err := http.NewRequest(http.MethodPut, putURL, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "image/jpeg")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	getURL := blobURL(ts.URL, container, blobName)
	resp2, err := http.Get(getURL)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	data, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(data))
}

// TestBlobRoundTrip_SpecialChars tests blob names containing parentheses,
// ampersands, and other characters that need percent-encoding.
func TestBlobRoundTrip_SpecialChars(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	mux := newTestMux(store)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	container := "uploads"
	blobName := "events/party (2025)/photo #1 & friends.jpg"
	body := "special chars data"

	putURL := blobURL(ts.URL, container, blobName)
	req, err := http.NewRequest(http.MethodPut, putURL, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "image/jpeg")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	getURL := blobURL(ts.URL, container, blobName)
	resp2, err := http.Get(getURL)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	data, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(data))
}

// TestDeleteRemovesFile verifies that deleting a blob removes the file
// from disk and the DB row (matching Azure Blob Storage behaviour).
func TestDeleteRemovesFile(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	mux := newTestMux(store)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	container := "uploads"
	blobName := "sport/soccer/goal.jpg"
	body := "staging blob data"

	// PUT the blob.
	putURL := blobURL(ts.URL, container, blobName)
	req, err := http.NewRequest(http.MethodPut, putURL, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "image/jpeg")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Verify file exists on disk.
	diskPath := store.blobPath(container, blobName)
	_, err = os.Stat(diskPath)
	require.NoError(t, err, "file should exist on disk after PUT")

	// DELETE the blob.
	delURL := blobURL(ts.URL, container, blobName)
	delReq, err := http.NewRequest(http.MethodDelete, delURL, nil)
	require.NoError(t, err)
	delResp, err := http.DefaultClient.Do(delReq)
	require.NoError(t, err)
	defer delResp.Body.Close()
	require.Equal(t, http.StatusNoContent, delResp.StatusCode)

	// Verify file is gone from disk.
	_, err = os.Stat(diskPath)
	assert.True(t, os.IsNotExist(err), "file should be removed from disk after DELETE")

	// GET should 404.
	getResp, err := http.Get(blobURL(ts.URL, container, blobName))
	require.NoError(t, err)
	defer getResp.Body.Close()
	assert.Equal(t, http.StatusNotFound, getResp.StatusCode)
}

// TestPublisherURLEncoding verifies that encodeBlobPath produces valid
// URL paths that round-trip correctly through url.Parse → u.Path.
func TestPublisherURLEncoding(t *testing.T) {
	tests := []struct {
		name     string
		blobName string
		wantPath string // the decoded path expected from url.Parse
	}{
		{
			name:     "simple name",
			blobName: "sport/soccer/goal.jpg",
			wantPath: "sport/soccer/goal.jpg",
		},
		{
			name:     "spaces in segments",
			blobName: "sport/ravens vs stingrays/Ravens vs Stingrays - August 2025-71.jpg",
			wantPath: "sport/ravens vs stingrays/Ravens vs Stingrays - August 2025-71.jpg",
		},
		{
			name:     "special characters",
			blobName: "events/party (2025)/photo #1 & friends.jpg",
			wantPath: "events/party (2025)/photo #1 & friends.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := encodeBlobPath(tt.blobName)
			rawURL := "http://blobemu:10000/uploads/" + encoded

			u, err := url.Parse(rawURL)
			require.NoError(t, err)

			// The decoded path should contain the original blob name.
			path := strings.TrimPrefix(u.Path, "/uploads/")
			assert.Equal(t, tt.wantPath, path)
		})
	}
}
