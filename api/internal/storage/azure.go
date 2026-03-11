package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/cbellee/photo-api/internal/models"
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
	blockBlob := s.client.ServiceClient().NewContainerClient(containerName).NewBlockBlobClient(blobName)

	tagResponse, err := blockBlob.GetTags(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("getting blob tags %s/%s: %w", containerName, blobName, err)
	}

	tags := make(map[string]string)
	for _, t := range tagResponse.BlobTags.BlobTagSet {
		tags[*t.Key] = *t.Value
	}
	slog.Debug("got blob tags", "blob", blobName, "tags", tags)
	return tags, nil
}

func (s *AzureBlobStore) SetBlobTags(ctx context.Context, blobName string, containerName string, tags map[string]string) error {
	blockBlob := s.client.ServiceClient().NewContainerClient(containerName).NewBlockBlobClient(blobName)

	slog.Info("setting blob tags", "blob", blobName)
	slog.Debug("tags", "blob", blobName, "tags", tags)
	_, err := blockBlob.SetTags(ctx, tags, nil)
	if err != nil {
		return fmt.Errorf("setting blob tags %s/%s: %w", containerName, blobName, err)
	}
	return nil
}

func (s *AzureBlobStore) GetBlobMetadata(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
	blobClient := s.client.ServiceClient().NewContainerClient(containerName).NewBlockBlobClient(blobName)

	mdResponse, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("getting blob metadata %s/%s: %w", containerName, blobName, err)
	}

	m := make(map[string]string)
	for key, value := range mdResponse.Metadata {
		m[key] = *value
	}
	slog.Debug("got blob metadata", "blob", blobName, "metadata", m)
	return m, nil
}

func (s *AzureBlobStore) GetBlobTagList(ctx context.Context, containerName string) (map[string][]string, error) {
	pager := s.client.NewListBlobsFlatPager(containerName, &azblob.ListBlobsFlatOptions{
		Include: container.ListBlobsInclude{
			Deleted:  false,
			Versions: false,
			Metadata: false,
			Tags:     true,
		},
	})

	blobTagMap := make(map[string][]string)

	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			slog.Error("error while listing blobs", "error", err)
			break
		}

		for _, _blob := range resp.Segment.BlobItems {
			if _blob.BlobTags == nil {
				continue // sidecar / untagged blobs — skip
			}
			tags := *_blob.BlobTags
			var album, collection string

			for _, t := range tags.BlobTagSet {
				if *t.Key == "collection" {
					collection = *t.Value
				}
				if *t.Key == "album" {
					album = *t.Value
				}
			}

			if !slices.Contains(blobTagMap[collection], album) {
				blobTagMap[collection] = append(blobTagMap[collection], album)
			}
		}
	}
	return blobTagMap, nil
}

func (s *AzureBlobStore) GetBlob(ctx context.Context, blobName string, containerName string) ([]byte, error) {
	blockBlob := s.client.ServiceClient().NewContainerClient(containerName).NewBlockBlobClient(blobName)

	// Ensure blob exists.
	_, err := blockBlob.GetProperties(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("blob not found %s/%s: %w", containerName, blobName, err)
	}

	blobStream, err := blockBlob.DownloadStream(ctx, &blob.DownloadStreamOptions{})
	if err != nil {
		return nil, fmt.Errorf("downloading blob %s/%s: %w", containerName, blobName, err)
	}

	var buf bytes.Buffer
	_, err = buf.ReadFrom(blobStream.NewRetryReader(ctx, &azblob.RetryReaderOptions{}))
	if err != nil {
		return nil, fmt.Errorf("reading blob stream %s/%s: %w", containerName, blobName, err)
	}

	slog.Info("got blob stream", "blob", blobName, "bytes", buf.Len())
	return buf.Bytes(), nil
}

func (s *AzureBlobStore) SaveBlob(ctx context.Context, reader io.ReadSeeker, size int64, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
	blobUrl := fmt.Sprintf("%s/%s/%s", s.storageUrl, containerName, blobName)
	blockBlob := s.client.ServiceClient().NewContainerClient(containerName).NewBlockBlobClient(blobName)

	md := make(map[string]*string)
	for key, value := range metadata {
		v := value
		md[key] = &v
	}

	_, err := blockBlob.Upload(ctx, streaming.NopCloser(reader), &blockblob.UploadOptions{
		Tags:     tags,
		Metadata: md,
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: &contentType,
		},
	})
	if err != nil {
		return fmt.Errorf("uploading blob %s: %w", blobUrl, err)
	}

	slog.Debug("uploaded blob", "blob_url", blobUrl, "tags", tags, "metadata", metadata)
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
