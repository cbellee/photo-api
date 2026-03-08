package storage

import (
	"context"

	"github.com/cbellee/photo-api/internal/models"
)

// BlobStore abstracts blob storage operations so handlers can be tested with mock implementations.
// The storage URL is provided at construction time so callers only need to pass the container name.
type BlobStore interface {
	// FilterBlobsByTags queries blobs using a tag-based filter expression and returns matching blobs
	// with their tags and metadata fully populated.
	FilterBlobsByTags(ctx context.Context, query string, containerName string) ([]models.Blob, error)

	// GetBlobTags returns the index tags for a single blob.
	GetBlobTags(ctx context.Context, blobName string, containerName string) (map[string]string, error)

	// SetBlobTags writes index tags for a single blob.
	SetBlobTags(ctx context.Context, blobName string, containerName string, tags map[string]string) error

	// GetBlobMetadata returns custom metadata for a single blob.
	GetBlobMetadata(ctx context.Context, blobName string, containerName string) (map[string]string, error)

	// GetBlobTagList returns a map of collection to album list built from all blobs in a container.
	GetBlobTagList(ctx context.Context, containerName string) (map[string][]string, error)

	// GetBlob downloads blob content and returns the raw bytes.
	GetBlob(ctx context.Context, blobName string, containerName string) ([]byte, error)

	// SaveBlob uploads bytes as a blob with tags, metadata, and content type.
	SaveBlob(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error

	// CopyBlob copies a blob from srcBlobName to destBlobName within the same container,
	// preserving tags and metadata.
	CopyBlob(ctx context.Context, srcBlobName string, destBlobName string, containerName string) error

	// DeleteBlob permanently deletes a blob from storage.
	DeleteBlob(ctx context.Context, blobName string, containerName string) error
}
