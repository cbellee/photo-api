package storage

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/utils"
)

// AzureBlobStore is the production BlobStore backed by Azure Blob Storage.
type AzureBlobStore struct {
	client     *azblob.Client
	storageUrl string
}

// NewAzureBlobStore creates a new AzureBlobStore. The storageUrl is the base
// URL of the storage account (e.g. "https://myaccount.blob.core.windows.net").
func NewAzureBlobStore(client *azblob.Client, storageUrl string) *AzureBlobStore {
	return &AzureBlobStore{client: client, storageUrl: storageUrl}
}

func (s *AzureBlobStore) FilterBlobsByTags(ctx context.Context, query string, containerName string) ([]models.Blob, error) {
	var blobs []models.Blob

	resp, err := s.client.ServiceClient().FilterBlobs(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	for _, _blob := range resp.Blobs {
		blobPath := fmt.Sprintf("%s/%s/%s", s.storageUrl, containerName, *_blob.Name)

		tags, err := s.GetBlobTags(ctx, *_blob.Name, containerName)
		if err != nil {
			return nil, err
		}

		md, err := s.GetBlobMetadata(ctx, *_blob.Name, *_blob.ContainerName)
		if err != nil {
			slog.Warn("error getting metadata", "blobPath", blobPath, "error", err)
		}

		b := models.Blob{
			Name:     *_blob.Name,
			Path:     fmt.Sprintf("%s/%s/%s", s.storageUrl, containerName, *_blob.Name),
			Tags:     tags,
			MetaData: md,
		}

		blobs = append(blobs, b)
	}

	if len(blobs) == 0 {
		slog.Debug("no blobs found", "query", query)
		return nil, nil
	}

	slog.Info("found blobs by tag query", "query", query, "num_blobs", len(blobs))
	return blobs, nil
}

func (s *AzureBlobStore) GetBlobTags(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
	return utils.GetBlobTags(s.client, blobName, containerName, s.storageUrl)
}

func (s *AzureBlobStore) SetBlobTags(ctx context.Context, blobName string, containerName string, tags map[string]string) error {
	return utils.SetBlobTags(s.client, blobName, containerName, s.storageUrl, tags)
}

func (s *AzureBlobStore) GetBlobMetadata(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
	return utils.GetBlobMetadata(s.client, blobName, containerName, s.storageUrl)
}

func (s *AzureBlobStore) GetBlobTagList(ctx context.Context, containerName string) (map[string][]string, error) {
	return utils.GetBlobTagList(s.client, containerName, s.storageUrl, ctx)
}

func (s *AzureBlobStore) GetBlob(ctx context.Context, blobName string, containerName string) ([]byte, error) {
	buf, err := utils.GetBlobStream(s.client, ctx, blobName, containerName, s.storageUrl)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *AzureBlobStore) SaveBlob(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
	blobUrl := fmt.Sprintf("%s/%s/%s", s.storageUrl, containerName, blobName)
	blockBlob := s.client.ServiceClient().NewContainerClient(containerName).NewBlockBlobClient(blobName)

	md := make(map[string]*string)
	for key, value := range metadata {
		v := value
		md[key] = &v
	}

	_, err := blockBlob.UploadStream(ctx, bytes.NewReader(data), &blockblob.UploadStreamOptions{
		Tags:     tags,
		Metadata: md,
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: &contentType,
		},
	})
	if err != nil {
		return fmt.Errorf("uploading blob %s: %w", blobUrl, err)
	}

	slog.Debug("uploaded blob stream", "blob_url", blobUrl, "tags", tags, "metadata", metadata)
	return nil
}

func (s *AzureBlobStore) CopyBlob(ctx context.Context, srcBlobName string, destBlobName string, containerName string) error {
	srcURL := fmt.Sprintf("%s/%s/%s", s.storageUrl, containerName, srcBlobName)
	container := s.client.ServiceClient().NewContainerClient(containerName)
	destBlob := container.NewBlockBlobClient(destBlobName)

	_, err := destBlob.StartCopyFromURL(ctx, srcURL, nil)
	if err != nil {
		return fmt.Errorf("copying blob %s to %s: %w", srcBlobName, destBlobName, err)
	}

	slog.Debug("copied blob", "src", srcBlobName, "dest", destBlobName)
	return nil
}

func (s *AzureBlobStore) DeleteBlob(ctx context.Context, blobName string, containerName string) error {
	container := s.client.ServiceClient().NewContainerClient(containerName)
	blobClient := container.NewBlockBlobClient(blobName)

	_, err := blobClient.Delete(ctx, nil)
	if err != nil {
		return fmt.Errorf("deleting blob %s: %w", blobName, err)
	}

	slog.Debug("deleted blob", "blob", blobName)
	return nil
}

// Compile-time check that AzureBlobStore implements BlobStore.
var _ BlobStore = (*AzureBlobStore)(nil)
