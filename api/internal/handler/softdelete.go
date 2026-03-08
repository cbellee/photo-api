package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/storage"
	"go.opentelemetry.io/otel/attribute"
)

// SoftDeleteCollectionHandler handles DELETE /api/{collection}.
// It sets isDeleted='true' on every blob in the collection.
func SoftDeleteCollectionHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.SoftDeleteCollection")
		defer span.End()

		collection := r.PathValue("collection")
		if err := validatePathParam("collection", collection); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		span.SetAttributes(attribute.String("collection", collection))

		// Find all non-deleted blobs in this collection.
		query := fmt.Sprintf("@container='%s' and collection='%s' and isDeleted='false'",
			cfg.ImagesContainerName, collection)
		blobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error querying blobs for soft-delete", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if len(blobs) == 0 {
			http.Error(w, "collection not found or already deleted", http.StatusNotFound)
			return
		}

		slog.InfoContext(ctx, "soft-deleting collection", "collection", collection, "blobCount", len(blobs))

		var errors []string
		for _, blob := range blobs {
			blob.Tags["isDeleted"] = "true"
			blob.Tags["collectionImage"] = "false"
			blob.Tags["albumImage"] = "false"

			if err := store.SetBlobTags(ctx, blob.Name, cfg.ImagesContainerName, blob.Tags); err != nil {
				slog.ErrorContext(ctx, "error soft-deleting blob", "blob", blob.Name, "error", err)
				errors = append(errors, fmt.Sprintf("set tags %s: %v", blob.Name, err))
			}
		}

		if len(errors) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPartialContent)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message":  "soft-delete completed with errors",
				"errors":   errors,
				"affected": len(blobs),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":  "collection soft-deleted",
			"affected": len(blobs),
		})
	}
}

// SoftDeleteAlbumHandler handles DELETE /api/{collection}/{album}.
// It sets isDeleted='true' on every blob in the album.
func SoftDeleteAlbumHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.SoftDeleteAlbum")
		defer span.End()

		collection := r.PathValue("collection")
		if err := validatePathParam("collection", collection); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		album := r.PathValue("album")
		if err := validatePathParam("album", album); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		span.SetAttributes(
			attribute.String("collection", collection),
			attribute.String("album", album),
		)

		query := fmt.Sprintf("@container='%s' and collection='%s' and album='%s' and isDeleted='false'",
			cfg.ImagesContainerName, collection, album)
		blobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error querying blobs for album soft-delete", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if len(blobs) == 0 {
			http.Error(w, "album not found or already deleted", http.StatusNotFound)
			return
		}

		slog.InfoContext(ctx, "soft-deleting album", "collection", collection, "album", album, "blobCount", len(blobs))

		var errors []string
		for _, blob := range blobs {
			blob.Tags["isDeleted"] = "true"
			blob.Tags["albumImage"] = "false"

			if err := store.SetBlobTags(ctx, blob.Name, cfg.ImagesContainerName, blob.Tags); err != nil {
				slog.ErrorContext(ctx, "error soft-deleting blob", "blob", blob.Name, "error", err)
				errors = append(errors, fmt.Sprintf("set tags %s: %v", blob.Name, err))
			}
		}

		if len(errors) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPartialContent)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message":  "album soft-delete completed with errors",
				"errors":   errors,
				"affected": len(blobs),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":  "album soft-deleted",
			"affected": len(blobs),
		})
	}
}
