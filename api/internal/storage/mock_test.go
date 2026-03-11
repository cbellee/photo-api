package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Default behaviour (no Func configured) ──────────────────────────

func TestMock_FilterBlobsByTags_DefaultError(t *testing.T) {
	m := &MockBlobStore{}
	blobs, err := m.FilterBlobsByTags(context.Background(), "q", "c")
	assert.Nil(t, blobs)
	assert.Error(t, err)
	assert.Len(t, m.FilterBlobsByTagsCalls, 1)
}

func TestMock_GetBlobTags_DefaultError(t *testing.T) {
	m := &MockBlobStore{}
	tags, err := m.GetBlobTags(context.Background(), "b", "c")
	assert.Nil(t, tags)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "GetBlobTags not configured")
	assert.Len(t, m.GetBlobTagsCalls, 1)
	assert.Equal(t, "b", m.GetBlobTagsCalls[0].BlobName)
	assert.Equal(t, "c", m.GetBlobTagsCalls[0].ContainerName)
}

func TestMock_SetBlobTags_DefaultNilError(t *testing.T) {
	m := &MockBlobStore{}
	err := m.SetBlobTags(context.Background(), "b", "c", map[string]string{"k": "v"})
	assert.NoError(t, err) // default is nil error
	assert.Len(t, m.SetBlobTagsCalls, 1)
	assert.Equal(t, "v", m.SetBlobTagsCalls[0].Tags["k"])
}

func TestMock_GetBlobMetadata_DefaultError(t *testing.T) {
	m := &MockBlobStore{}
	md, err := m.GetBlobMetadata(context.Background(), "b", "c")
	assert.Nil(t, md)
	assert.Error(t, err)
	assert.Len(t, m.GetBlobMetadataCalls, 1)
}

func TestMock_GetBlobTagList_DefaultError(t *testing.T) {
	m := &MockBlobStore{}
	list, err := m.GetBlobTagList(context.Background(), "c")
	assert.Nil(t, list)
	assert.Error(t, err)
	assert.Len(t, m.GetBlobTagListCalls, 1)
}

func TestMock_SaveBlob_DefaultNilError(t *testing.T) {
	m := &MockBlobStore{}
	err := m.SaveBlob(context.Background(), strings.NewReader("data"), 4, "b", "c", nil, nil, "ct")
	assert.NoError(t, err) // default is nil error
	require.Len(t, m.SaveBlobCalls, 1)
	assert.Equal(t, []byte("data"), m.SaveBlobCalls[0].Data)
	assert.Equal(t, "b", m.SaveBlobCalls[0].BlobName)
	assert.Equal(t, "c", m.SaveBlobCalls[0].ContainerName)
	assert.Equal(t, "ct", m.SaveBlobCalls[0].ContentType)
}

func TestMock_GetBlob_DefaultError(t *testing.T) {
	m := &MockBlobStore{}
	data, err := m.GetBlob(context.Background(), "b", "c")
	assert.Nil(t, data)
	assert.Error(t, err)
	assert.Len(t, m.GetBlobCalls, 1)
}

// ── Custom Func delegates ───────────────────────────────────────────

func TestMock_FilterBlobsByTags_CustomFunc(t *testing.T) {
	expected := []models.Blob{{Name: "a"}}
	m := &MockBlobStore{
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return expected, nil
		},
	}
	blobs, err := m.FilterBlobsByTags(context.Background(), "q", "c")
	assert.NoError(t, err)
	assert.Equal(t, expected, blobs)
}

func TestMock_GetBlob_CustomFunc(t *testing.T) {
	m := &MockBlobStore{
		GetBlobFunc: func(ctx context.Context, blobName string, containerName string) ([]byte, error) {
			return []byte("hello"), nil
		},
	}
	data, err := m.GetBlob(context.Background(), "b", "c")
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello"), data)
	assert.Len(t, m.GetBlobCalls, 1)
}

func TestMock_SaveBlob_CustomFunc_ReturnsError(t *testing.T) {
	m := &MockBlobStore{
		SaveBlobFunc: func(ctx context.Context, reader io.ReadSeeker, size int64, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
			return fmt.Errorf("disk full")
		},
	}
	err := m.SaveBlob(context.Background(), strings.NewReader(""), 0, "b", "c", nil, nil, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

func TestMock_GetBlobTagList_CustomFunc(t *testing.T) {
	m := &MockBlobStore{
		GetBlobTagListFunc: func(ctx context.Context, containerName string) (map[string][]string, error) {
			return map[string][]string{"nature": {"sunset", "forest"}}, nil
		},
	}
	list, err := m.GetBlobTagList(context.Background(), "images")
	assert.NoError(t, err)
	assert.Equal(t, []string{"sunset", "forest"}, list["nature"])
}

func TestMock_SetBlobTags_CustomFunc(t *testing.T) {
	m := &MockBlobStore{
		SetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string, tags map[string]string) error {
			assert.Equal(t, "photo.jpg", blobName)
			return nil
		},
	}
	err := m.SetBlobTags(context.Background(), "photo.jpg", "images", map[string]string{"album": "sunset"})
	assert.NoError(t, err)
}

func TestMock_GetBlobMetadata_CustomFunc(t *testing.T) {
	m := &MockBlobStore{
		GetBlobMetadataFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{"Width": "1920"}, nil
		},
	}
	md, err := m.GetBlobMetadata(context.Background(), "photo.jpg", "images")
	assert.NoError(t, err)
	assert.Equal(t, "1920", md["Width"])
}

func TestMock_GetBlobTags_CustomFunc(t *testing.T) {
	m := &MockBlobStore{
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{"collection": "nature"}, nil
		},
	}
	tags, err := m.GetBlobTags(context.Background(), "photo.jpg", "images")
	assert.NoError(t, err)
	assert.Equal(t, "nature", tags["collection"])
}

// ── Thread safety ───────────────────────────────────────────────────

func TestMock_ConcurrentCallTracking(t *testing.T) {
	m := &MockBlobStore{
		FilterBlobsByTagsFunc: func(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
			return nil, nil
		},
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = m.FilterBlobsByTags(context.Background(), fmt.Sprintf("q%d", n), "c")
		}(i)
	}
	wg.Wait()
	assert.Len(t, m.FilterBlobsByTagsCalls, 50)
}

// ── Compile-time interface check ─────────────────────────────────────

var _ BlobStore = (*MockBlobStore)(nil)

func TestMock_ImplementsBlobStore(t *testing.T) {
	// This test exists to document that MockBlobStore implements BlobStore.
	// The compile-time check above is the real assertion.
	var store BlobStore = &MockBlobStore{}
	assert.NotNil(t, store)
}
