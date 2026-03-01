package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cbellee/photo-api/internal/models"
)

// LocalBlobStore implements BlobStore by talking to the blobemu service over HTTP.
// It is intended for local Docker / Kubernetes development where Azure Blob Storage
// is not available.
type LocalBlobStore struct {
	baseURL    string
	httpClient *http.Client
}

// NewLocalBlobStore creates a store that proxies all operations to the
// blob emulator at baseURL (e.g. "http://blobemu:10000").
func NewLocalBlobStore(baseURL string) *LocalBlobStore {
	return &LocalBlobStore{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// blobResponse mirrors the JSON shape returned by the emulator.
type blobResponse struct {
	Name      string            `json:"name"`
	Container string            `json:"container"`
	Tags      map[string]string `json:"tags"`
	Metadata  map[string]string `json:"metadata"`
}

// ---------- BlobStore implementation ----------

func (s *LocalBlobStore) FilterBlobsByTags(ctx context.Context, query string, containerName string, storageUrl string) ([]models.Blob, error) {
	body, _ := json.Marshal(map[string]string{"query": query})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/_query", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query failed (%d): %s", resp.StatusCode, string(b))
	}

	var items []blobResponse
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(items) == 0 {
		return nil, nil
	}

	blobs := make([]models.Blob, 0, len(items))
	for _, item := range items {
		blobs = append(blobs, models.Blob{
			Name:     item.Name,
			Path:     fmt.Sprintf("%s/%s/%s", storageUrl, containerName, item.Name),
			Tags:     item.Tags,
			MetaData: item.Metadata,
		})
	}
	return blobs, nil
}

func (s *LocalBlobStore) GetBlobTags(ctx context.Context, blobName string, containerName string, storageUrl string) (map[string]string, error) {
	u := s.blobURL(containerName, blobName) + "?comp=tags"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get tags failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get tags status %d", resp.StatusCode)
	}

	var tags map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}
	return tags, nil
}

func (s *LocalBlobStore) SetBlobTags(ctx context.Context, blobName string, containerName string, storageUrl string, tags map[string]string) error {
	body, _ := json.Marshal(tags)
	u := s.blobURL(containerName, blobName) + "?comp=tags"

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("set tags failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set tags status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (s *LocalBlobStore) GetBlobMetadata(ctx context.Context, blobName string, containerName string, storageUrl string) (map[string]string, error) {
	u := s.blobURL(containerName, blobName) + "?comp=metadata"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get metadata failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get metadata status %d", resp.StatusCode)
	}

	var md map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&md); err != nil {
		return nil, err
	}
	return md, nil
}

func (s *LocalBlobStore) GetBlobTagList(ctx context.Context, containerName string, storageUrl string) (map[string][]string, error) {
	u := fmt.Sprintf("%s/%s", s.baseURL, url.PathEscape(containerName))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list blobs failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list blobs status %d", resp.StatusCode)
	}

	var items []blobResponse
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}

	// Build the collection → albums map exactly like the Azure implementation.
	tagMap := make(map[string][]string)
	for _, item := range items {
		collection := item.Tags["collection"]
		album := item.Tags["album"]
		if !contains(tagMap[collection], album) {
			tagMap[collection] = append(tagMap[collection], album)
		}
	}
	return tagMap, nil
}

func (s *LocalBlobStore) GetBlob(ctx context.Context, blobName string, containerName string, storageUrl string) ([]byte, error) {
	u := s.blobURL(containerName, blobName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get blob failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get blob status %d: %s", resp.StatusCode, string(b))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading blob body: %w", err)
	}

	slog.Debug("downloaded blob via emulator", "container", containerName, "name", blobName, "bytes", len(data))
	return data, nil
}

func (s *LocalBlobStore) SaveBlob(ctx context.Context, data []byte, blobName string, containerName string, storageUrl string, tags map[string]string, metadata map[string]string, contentType string) error {
	u := s.blobURL(containerName, blobName)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(data))
	if err != nil {
		return err
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if tags != nil {
		j, _ := json.Marshal(tags)
		req.Header.Set("X-Blob-Tags", string(j))
	}
	if metadata != nil {
		j, _ := json.Marshal(metadata)
		req.Header.Set("X-Blob-Metadata", string(j))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("save blob failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("save blob status %d: %s", resp.StatusCode, string(b))
	}

	slog.Debug("saved blob via emulator", "container", containerName, "name", blobName)
	return nil
}

// ---------- helpers ----------

// blobURL builds a properly encoded URL for a blob, preserving
// slashes in the blob name as path separators.
func (s *LocalBlobStore) blobURL(container, blobName string) string {
	segments := strings.Split(blobName, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return fmt.Sprintf("%s/%s/%s", s.baseURL, url.PathEscape(container), strings.Join(segments, "/"))
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

// Compile-time check.
var _ BlobStore = (*LocalBlobStore)(nil)
