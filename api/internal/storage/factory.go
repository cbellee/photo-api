package storage

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/cbellee/photo-api/internal/utils"
)

// NewBlobStore creates the appropriate BlobStore implementation based on
// environment configuration. When BLOB_EMULATOR_URL is set it returns a
// LocalBlobStore; otherwise it creates an AzureBlobStore with the correct
// credential strategy.
func NewBlobStore(storageUrl string, azureClientID string) (BlobStore, error) {
	if blobEmuURL := os.Getenv("BLOB_EMULATOR_URL"); blobEmuURL != "" {
		slog.Info("using local blob emulator", "url", blobEmuURL)
		return NewLocalBlobStore(blobEmuURL, storageUrl), nil
	}

	isProduction := false
	if _, exists := os.LookupEnv("CONTAINER_APP_NAME"); exists {
		isProduction = true
	} else {
		slog.Info("'CONTAINER_APP_NAME' env var not found, running in local environment")
	}

	blobClient, err := utils.CreateAzureBlobClient(storageUrl, isProduction, azureClientID)
	if err != nil {
		return nil, fmt.Errorf("creating blob client: %w", err)
	}
	return NewAzureBlobStore(blobClient, storageUrl), nil
}
