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
				errors = append(errors, fmt.Sprintf("set tags %s: %v", blob.Name, err))
			}
		}

		if len(errors) > 0 {
			slog.ErrorContext(ctx, "soft-delete collection completed with errors", "collection", collection, "errors", errors)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPartialContent)
			json.NewEncoder(w).Encode(mutationResponse{
				Message:  "soft-delete completed with errors",
				Errors:   errors,
				Affected: len(blobs),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mutationResponse{
			Message:  "collection soft-deleted",
			Affected: len(blobs),
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
			blob.Tags["collectionImage"] = "false"

			if err := store.SetBlobTags(ctx, blob.Name, cfg.ImagesContainerName, blob.Tags); err != nil {
				errors = append(errors, fmt.Sprintf("set tags %s: %v", blob.Name, err))
			}
		}

		// Check whether the entire collection is now soft-deleted.
		remainingQuery := fmt.Sprintf("@container='%s' and collection='%s' and isDeleted='false'",
			cfg.ImagesContainerName, collection)
		remaining, _ := store.FilterBlobsByTags(ctx, remainingQuery, cfg.ImagesContainerName)
		collectionDeleted := len(remaining) == 0
		if collectionDeleted {
			slog.InfoContext(ctx, "all albums deleted, collection is now soft-deleted", "collection", collection)
		}

		if len(errors) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPartialContent)
			json.NewEncoder(w).Encode(mutationResponse{
				Message:           "album soft-delete completed with errors",
				Errors:            errors,
				Affected:          len(blobs),
				CollectionDeleted: collectionDeleted,
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mutationResponse{
			Message:           "album soft-deleted",
			Affected:          len(blobs),
			CollectionDeleted: collectionDeleted,
		})
	}
}

// RestoreAlbumHandler handles PATCH /api/{collection}/{album}.
// It sets isDeleted='false' on every blob in the album and re-assigns an albumImage.
func RestoreAlbumHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.RestoreAlbum")
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

		// Find all deleted blobs in this album.
		query := fmt.Sprintf("@container='%s' and collection='%s' and album='%s' and isDeleted='true'",
			cfg.ImagesContainerName, collection, album)
		blobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error querying blobs for album restore", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if len(blobs) == 0 {
			http.Error(w, "album not found or not deleted", http.StatusNotFound)
			return
		}

		slog.InfoContext(ctx, "restoring album", "collection", collection, "album", album, "blobCount", len(blobs))

		var errors []string
		albumImageAssigned := false
		for _, blob := range blobs {
			blob.Tags["isDeleted"] = "false"
			// Re-assign albumImage to the first blob.
			if !albumImageAssigned {
				blob.Tags["albumImage"] = "true"
				albumImageAssigned = true
			}

			if err := store.SetBlobTags(ctx, blob.Name, cfg.ImagesContainerName, blob.Tags); err != nil {
				errors = append(errors, fmt.Sprintf("set tags %s: %v", blob.Name, err))
			}
		}

		if len(errors) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPartialContent)
			json.NewEncoder(w).Encode(mutationResponse{
				Message:  "album restore completed with errors",
				Errors:   errors,
				Affected: len(blobs),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mutationResponse{
			Message:  "album restored",
			Affected: len(blobs),
		})
	}
}

// RestoreCollectionHandler handles PATCH /api/{collection}.
// It sets isDeleted='false' on every blob in the collection and re-assigns
// collectionImage and albumImage tags.
func RestoreCollectionHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.RestoreCollection")
		defer span.End()

		collection := r.PathValue("collection")
		if err := validatePathParam("collection", collection); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		span.SetAttributes(attribute.String("collection", collection))

		// Find all deleted blobs in this collection.
		query := fmt.Sprintf("@container='%s' and collection='%s' and isDeleted='true'",
			cfg.ImagesContainerName, collection)
		blobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error querying blobs for collection restore", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if len(blobs) == 0 {
			http.Error(w, "collection not found or not deleted", http.StatusNotFound)
			return
		}

		slog.InfoContext(ctx, "restoring collection", "collection", collection, "blobCount", len(blobs))

		// Track which albums have had albumImage re-assigned.
		albumImageAssigned := make(map[string]bool)
		collectionImageAssigned := false

		var errors []string
		for _, blob := range blobs {
			blob.Tags["isDeleted"] = "false"

			// Re-assign collectionImage to the first blob overall.
			if !collectionImageAssigned {
				blob.Tags["collectionImage"] = "true"
				collectionImageAssigned = true
			}

			// Re-assign albumImage to the first blob per album.
			album := blob.Tags["album"]
			if album != "" && !albumImageAssigned[album] {
				blob.Tags["albumImage"] = "true"
				albumImageAssigned[album] = true
			}

			if err := store.SetBlobTags(ctx, blob.Name, cfg.ImagesContainerName, blob.Tags); err != nil {
				errors = append(errors, fmt.Sprintf("set tags %s: %v", blob.Name, err))
			}
		}

		if len(errors) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPartialContent)
			json.NewEncoder(w).Encode(mutationResponse{
				Message:  "collection restore completed with errors",
				Errors:   errors,
				Affected: len(blobs),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mutationResponse{
			Message:  "collection restored",
			Affected: len(blobs),
		})
	}
}
