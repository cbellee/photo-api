package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/storage"
)

// CollectionHandler returns the list of collections (each represented by its
// collectionImage placeholder photo).
func CollectionHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// get photos with matching collection tags
		query := fmt.Sprintf("@container='%s' and collectionImage='true'", cfg.ImagesContainerName)
		slog.Debug("query", "query", query)

		filteredBlobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName, cfg.StorageUrl)
		if err != nil {
			slog.Error("error getting blobs by tags", "error", err)

			// try a query without the collectionImage tag as fallback
			slog.Error("no collection images found, trying query without 'collectionImage' & will set a default placeholder collectionImage", "query", query)

			fallbackQuery := fmt.Sprintf("@container='%s'", cfg.ImagesContainerName)
			filteredBlobs, err = store.FilterBlobsByTags(ctx, fallbackQuery, cfg.ImagesContainerName, cfg.StorageUrl)
			if err != nil {
				slog.Error("error getting blobs by tags (fallback)", "error", err)
				http.Error(w, "No collection images found", http.StatusNotFound)
				return
			}

			// set the first image as the collection image & write the tag back to the blob
			filteredBlobs[0].Tags["collectionImage"] = "true"
			err = store.SetBlobTags(ctx, filteredBlobs[0].Name, cfg.ImagesContainerName, cfg.StorageUrl, filteredBlobs[0].Tags)
			if err != nil {
				slog.Error("error setting collectionImage tag", "error", err)
			}
		}

		if len(filteredBlobs) == 0 {
			http.Error(w, "No collection images found", http.StatusNotFound)
			return
		}

		// For collection view we also fetch individual blob tags to get album/collection names
		for i, b := range filteredBlobs {
			tags, err := store.GetBlobTags(ctx, b.Name, cfg.ImagesContainerName, cfg.StorageUrl)
			if err != nil {
				slog.Error("error getting blob tags", "error", err, "blobpath", b.Path)
				continue
			}
			filteredBlobs[i].Tags = tags
		}

		photos := BlobsToPhotos(filteredBlobs)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}
