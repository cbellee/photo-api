package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/storage"
	"go.opentelemetry.io/otel/attribute"
)

// AlbumHandler returns the albums within a collection (each represented by its
// albumImage placeholder photo).
func AlbumHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.Albums")
		defer span.End()

		collection := r.PathValue("collection")
		if collection == "" {
			slog.Error("empty path value", "name", "collection")
			http.Error(w, "collection is required", http.StatusBadRequest)
			return
		}
		span.SetAttributes(attribute.String("collection", collection))

		// get album placeholder photos with matching tags
		query := fmt.Sprintf("@container='%s' and collection='%s' and albumImage='true'", cfg.ImagesContainerName, collection)
		filteredBlobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName, cfg.StorageUrl)
		if err != nil {
			slog.Error("error getting blobs by tags", "error", err)
			http.Error(w, "No album images found", http.StatusNotFound)
			return
		}

		// Fetch individual blob tags for album/collection names
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
