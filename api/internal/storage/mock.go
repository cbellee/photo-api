package storage

import (
	"context"
	"fmt"
	"sync"

	"github.com/cbellee/photo-api/internal/models"
)

// MockBlobStore is a test double for BlobStore. It lets tests configure return values
// and tracks calls for verification.
type MockBlobStore struct {
	mu sync.Mutex

	// FilterBlobsByTags configuration
	FilterBlobsByTagsFunc  func(ctx context.Context, query string, containerName string) ([]models.Blob, error)
	FilterBlobsByTagsCalls []FilterBlobsByTagsCall

	// GetBlobTags configuration
	GetBlobTagsFunc  func(ctx context.Context, blobName string, containerName string) (map[string]string, error)
	GetBlobTagsCalls []GetBlobTagsCall

	// SetBlobTags configuration
	SetBlobTagsFunc  func(ctx context.Context, blobName string, containerName string, tags map[string]string) error
	SetBlobTagsCalls []SetBlobTagsCall

	// GetBlobMetadata configuration
	GetBlobMetadataFunc  func(ctx context.Context, blobName string, containerName string) (map[string]string, error)
	GetBlobMetadataCalls []GetBlobMetadataCall

	// GetBlobTagList configuration
	GetBlobTagListFunc  func(ctx context.Context, containerName string) (map[string][]string, error)
	GetBlobTagListCalls []GetBlobTagListCall

	// SaveBlob configuration
	SaveBlobFunc  func(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error
	SaveBlobCalls []SaveBlobCall

	// GetBlob configuration
	GetBlobFunc  func(ctx context.Context, blobName string, containerName string) ([]byte, error)
	GetBlobCalls []GetBlobCall
}

// Call tracking structs

type FilterBlobsByTagsCall struct {
	Query         string
	ContainerName string
}

type GetBlobTagsCall struct {
	BlobName      string
	ContainerName string
}

type SetBlobTagsCall struct {
	BlobName      string
	ContainerName string
	Tags          map[string]string
}

type GetBlobMetadataCall struct {
	BlobName      string
	ContainerName string
}

type GetBlobTagListCall struct {
	ContainerName string
}

type SaveBlobCall struct {
	Data          []byte
	BlobName      string
	ContainerName string
	Tags          map[string]string
	Metadata      map[string]string
	ContentType   string
}

type GetBlobCall struct {
	BlobName      string
	ContainerName string
}

func (m *MockBlobStore) FilterBlobsByTags(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
	m.mu.Lock()
	m.FilterBlobsByTagsCalls = append(m.FilterBlobsByTagsCalls, FilterBlobsByTagsCall{
		Query: query, ContainerName: containerName,
	})
	m.mu.Unlock()

	if m.FilterBlobsByTagsFunc != nil {
		return m.FilterBlobsByTagsFunc(ctx, query, containerName)
	}
	return nil, fmt.Errorf("FilterBlobsByTags not configured")
}

func (m *MockBlobStore) GetBlobTags(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
	m.mu.Lock()
	m.GetBlobTagsCalls = append(m.GetBlobTagsCalls, GetBlobTagsCall{
		BlobName: blobName, ContainerName: containerName,
	})
	m.mu.Unlock()

	if m.GetBlobTagsFunc != nil {
		return m.GetBlobTagsFunc(ctx, blobName, containerName)
	}
	return nil, fmt.Errorf("GetBlobTags not configured")
}

func (m *MockBlobStore) SetBlobTags(ctx context.Context, blobName string, containerName string, tags map[string]string) error {
	m.mu.Lock()
	m.SetBlobTagsCalls = append(m.SetBlobTagsCalls, SetBlobTagsCall{
		BlobName: blobName, ContainerName: containerName, Tags: tags,
	})
	m.mu.Unlock()

	if m.SetBlobTagsFunc != nil {
		return m.SetBlobTagsFunc(ctx, blobName, containerName, tags)
	}
	return nil
}

func (m *MockBlobStore) GetBlobMetadata(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
	m.mu.Lock()
	m.GetBlobMetadataCalls = append(m.GetBlobMetadataCalls, GetBlobMetadataCall{
		BlobName: blobName, ContainerName: containerName,
	})
	m.mu.Unlock()

	if m.GetBlobMetadataFunc != nil {
		return m.GetBlobMetadataFunc(ctx, blobName, containerName)
	}
	return nil, fmt.Errorf("GetBlobMetadata not configured")
}

func (m *MockBlobStore) GetBlobTagList(ctx context.Context, containerName string) (map[string][]string, error) {
	m.mu.Lock()
	m.GetBlobTagListCalls = append(m.GetBlobTagListCalls, GetBlobTagListCall{
		ContainerName: containerName,
	})
	m.mu.Unlock()

	if m.GetBlobTagListFunc != nil {
		return m.GetBlobTagListFunc(ctx, containerName)
	}
	return nil, fmt.Errorf("GetBlobTagList not configured")
}

func (m *MockBlobStore) SaveBlob(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
	m.mu.Lock()
	m.SaveBlobCalls = append(m.SaveBlobCalls, SaveBlobCall{
		Data: data, BlobName: blobName, ContainerName: containerName, Tags: tags, Metadata: metadata, ContentType: contentType,
	})
	m.mu.Unlock()

	if m.SaveBlobFunc != nil {
		return m.SaveBlobFunc(ctx, data, blobName, containerName, tags, metadata, contentType)
	}
	return nil
}

func (m *MockBlobStore) GetBlob(ctx context.Context, blobName string, containerName string) ([]byte, error) {
	m.mu.Lock()
	m.GetBlobCalls = append(m.GetBlobCalls, GetBlobCall{
		BlobName: blobName, ContainerName: containerName,
	})
	m.mu.Unlock()

	if m.GetBlobFunc != nil {
		return m.GetBlobFunc(ctx, blobName, containerName)
	}
	return nil, fmt.Errorf("GetBlob not configured")
}

// Compile-time check that MockBlobStore implements BlobStore.
var _ BlobStore = (*MockBlobStore)(nil)
