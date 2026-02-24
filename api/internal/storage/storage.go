package storage

import (
	"context"

	"github.com/cbellee/photo-api/internal/models"
)

// BlobStore abstracts blob storage operations so handlers can be tested with mock implementations.
type BlobStore interface {
	// FilterBlobsByTags queries blobs using a tag-based filter expression and returns matching blobs
	// with their tags and metadata fully populated.
	FilterBlobsByTags(ctx context.Context, query string, containerName string, storageUrl string) ([]models.Blob, error)

	// GetBlobTags returns the index tags for a single blob.
	GetBlobTags(ctx context.Context, blobName string, containerName string, storageUrl string) (map[string]string, error)

	// SetBlobTags writes index tags for a single blob.
	SetBlobTags(ctx context.Context, blobName string, containerName string, storageUrl string, tags map[string]string) error

	// GetBlobMetadata returns custom metadata for a single blob.
	GetBlobMetadata(ctx context.Context, blobName string, containerName string, storageUrl string) (map[string]string, error)

	// GetBlobTagList returns a map of collection to album list built from all blobs in a container.
	GetBlobTagList(ctx context.Context, containerName string, storageUrl string) (map[string][]string, error)

	// SaveBlob uploads bytes as a blob with tags, metadata, and content type.
	SaveBlob(ctx context.Context, data []byte, blobName string, containerName string, storageUrl string, tags map[string]string, metadata map[string]string, contentType string) error
}
