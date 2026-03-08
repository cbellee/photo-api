package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/storage"
	"go.opentelemetry.io/otel/attribute"
)

// PhotoHandler returns all photos within a specific collection/album.
func PhotoHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.Photos")
		defer span.End()

		collection := r.PathValue("collection")
		if err := validatePathParam("collection", collection); err != nil {
			slog.ErrorContext(ctx, "invalid path param", "name", "collection", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		album := r.PathValue("album")
		if err := validatePathParam("album", album); err != nil {
			slog.ErrorContext(ctx, "invalid path param", "name", "album", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		span.SetAttributes(
			attribute.String("collection", collection),
			attribute.String("album", album),
		)

		// get photos with matching collection & album tags
		// When ?includeDeleted=true is passed, return all photos (including soft-deleted ones)
		includeDeleted := r.URL.Query().Get("includeDeleted") == "true"
		var query string
		if includeDeleted {
			query = fmt.Sprintf("@container='%s' AND collection='%s' AND album='%s'", cfg.ImagesContainerName, collection, album)
		} else {
			query = fmt.Sprintf("@container='%s' AND collection='%s' AND album='%s' AND isDeleted='false'", cfg.ImagesContainerName, collection, album)
		}
		filteredBlobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error getting blobs by tags", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if len(filteredBlobs) == 0 {
			http.Error(w, "No photos found", http.StatusNotFound)
			return
		}

		photos := BlobsToPhotos(filteredBlobs)

		slog.DebugContext(ctx, "filtered photos", "metadata", photos)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}
