package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/storage"
)

// PhotoHandler returns all photos within a specific collection/album.
func PhotoHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		collection := r.PathValue("collection")
		if collection == "" {
			slog.Error("empty path value", "name", "collection")
			http.Error(w, "collection is required", http.StatusBadRequest)
			return
		}

		album := r.PathValue("album")
		if album == "" {
			slog.Error("empty path value", "name", "album")
			http.Error(w, "album is required", http.StatusBadRequest)
			return
		}

		// get photos with matching collection & album tags
		query := fmt.Sprintf("@container='%s' AND collection='%s' AND album='%s' AND isDeleted='false'", cfg.ImagesContainerName, collection, album)
		filteredBlobs, err := store.FilterBlobsByTags(r.Context(), query, cfg.ImagesContainerName, cfg.StorageUrl)
		if err != nil {
			slog.Error("error getting blobs by tags", "error", err)
			http.Error(w, "No photos found", http.StatusNotFound)
			return
		}

		photos := BlobsToPhotos(filteredBlobs)

		slog.Debug("filtered photos", "metadata", photos)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}
