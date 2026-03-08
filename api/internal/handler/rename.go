package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cbellee/photo-api/internal/storage"
	"go.opentelemetry.io/otel/attribute"
)

// renameRequest is the JSON body for rename endpoints.
type renameRequest struct {
	NewName string `json:"newName"`
}

// RenameCollectionHandler handles PUT /api/rename/{collection}.
// It renames a collection by:
//  1. Finding all blobs with the given collection tag.
//  2. Copying each blob to a new path with the new collection name.
//  3. Updating tags on the new blob (collection + name).
//  4. Deleting the old blob.
func RenameCollectionHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.RenameCollection")
		defer span.End()

		collection := r.PathValue("collection")
		if err := validatePathParam("collection", collection); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		span.SetAttributes(attribute.String("collection", collection))

		if r.Body == nil {
			http.Error(w, "body is empty", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		var req renameRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := validatePathParam("newName", req.NewName); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.NewName == collection {
			http.Error(w, "new name is the same as current name", http.StatusBadRequest)
			return
		}
		span.SetAttributes(attribute.String("newName", req.NewName))

		// Find all blobs in this collection.
		query := fmt.Sprintf("@container='%s' and collection='%s'", cfg.ImagesContainerName, collection)
		blobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error querying blobs for rename", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if len(blobs) == 0 {
			http.Error(w, "collection not found", http.StatusNotFound)
			return
		}

		slog.InfoContext(ctx, "renaming collection", "from", collection, "to", req.NewName, "blobCount", len(blobs))

		var errors []string
		for _, blob := range blobs {
			// Build new blob name: replace the collection segment.
			// Blob names follow the pattern: collection/album/filename
			newBlobName := replaceFirstSegment(blob.Name, req.NewName)

			// Copy blob to new location.
			if err := store.CopyBlob(ctx, blob.Name, newBlobName, cfg.ImagesContainerName); err != nil {
				slog.ErrorContext(ctx, "error copying blob during rename", "src", blob.Name, "dest", newBlobName, "error", err)
				errors = append(errors, fmt.Sprintf("copy %s: %v", blob.Name, err))
				continue
			}

			// Update tags on the new blob.
			newTags := make(map[string]string, len(blob.Tags))
			for k, v := range blob.Tags {
				newTags[k] = v
			}
			newTags["collection"] = req.NewName
			newTags["name"] = newBlobName

			if err := store.SetBlobTags(ctx, newBlobName, cfg.ImagesContainerName, newTags); err != nil {
				slog.ErrorContext(ctx, "error setting tags on renamed blob", "blob", newBlobName, "error", err)
				errors = append(errors, fmt.Sprintf("set tags %s: %v", newBlobName, err))
				continue
			}

			// Delete old blob.
			if err := store.DeleteBlob(ctx, blob.Name, cfg.ImagesContainerName); err != nil {
				slog.ErrorContext(ctx, "error deleting old blob during rename", "blob", blob.Name, "error", err)
				errors = append(errors, fmt.Sprintf("delete %s: %v", blob.Name, err))
			}
		}

		if len(errors) > 0 {
			slog.ErrorContext(ctx, "rename completed with errors", "errors", errors)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPartialContent)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message":  "rename completed with errors",
				"errors":   errors,
				"newName":  req.NewName,
				"affected": len(blobs),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":  "collection renamed",
			"newName":  req.NewName,
			"affected": len(blobs),
		})
	}
}

// RenameAlbumHandler handles PUT /api/rename/{collection}/{album}.
// It renames an album by finding all matching blobs, copying, retagging,
// and deleting the originals.
func RenameAlbumHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.RenameAlbum")
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

		if r.Body == nil {
			http.Error(w, "body is empty", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		var req renameRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := validatePathParam("newName", req.NewName); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.NewName == album {
			http.Error(w, "new name is the same as current name", http.StatusBadRequest)
			return
		}
		span.SetAttributes(attribute.String("newName", req.NewName))

		// Find all blobs in this collection/album.
		query := fmt.Sprintf("@container='%s' and collection='%s' and album='%s'",
			cfg.ImagesContainerName, collection, album)
		blobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error querying blobs for album rename", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if len(blobs) == 0 {
			http.Error(w, "album not found", http.StatusNotFound)
			return
		}

		slog.InfoContext(ctx, "renaming album", "collection", collection, "from", album, "to", req.NewName, "blobCount", len(blobs))

		var errors []string
		for _, blob := range blobs {
			// Build new blob name: replace the album segment (second path segment).
			newBlobName := replaceSecondSegment(blob.Name, req.NewName)

			if err := store.CopyBlob(ctx, blob.Name, newBlobName, cfg.ImagesContainerName); err != nil {
				slog.ErrorContext(ctx, "error copying blob during album rename", "src", blob.Name, "dest", newBlobName, "error", err)
				errors = append(errors, fmt.Sprintf("copy %s: %v", blob.Name, err))
				continue
			}

			newTags := make(map[string]string, len(blob.Tags))
			for k, v := range blob.Tags {
				newTags[k] = v
			}
			newTags["album"] = req.NewName
			newTags["name"] = newBlobName

			if err := store.SetBlobTags(ctx, newBlobName, cfg.ImagesContainerName, newTags); err != nil {
				slog.ErrorContext(ctx, "error setting tags on renamed blob", "blob", newBlobName, "error", err)
				errors = append(errors, fmt.Sprintf("set tags %s: %v", newBlobName, err))
				continue
			}

			if err := store.DeleteBlob(ctx, blob.Name, cfg.ImagesContainerName); err != nil {
				slog.ErrorContext(ctx, "error deleting old blob during album rename", "blob", blob.Name, "error", err)
				errors = append(errors, fmt.Sprintf("delete %s: %v", blob.Name, err))
			}
		}

		if len(errors) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPartialContent)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message":  "album rename completed with errors",
				"errors":   errors,
				"newName":  req.NewName,
				"affected": len(blobs),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":  "album renamed",
			"newName":  req.NewName,
			"affected": len(blobs),
		})
	}
}

// replaceFirstSegment replaces the first path segment (collection) in a blob name.
// e.g. "old-collection/album/file.jpg" → "new-collection/album/file.jpg"
func replaceFirstSegment(blobName, newSegment string) string {
	parts := strings.SplitN(blobName, "/", 2)
	if len(parts) < 2 {
		return newSegment
	}
	return newSegment + "/" + parts[1]
}

// replaceSecondSegment replaces the second path segment (album) in a blob name.
// e.g. "collection/old-album/file.jpg" → "collection/new-album/file.jpg"
func replaceSecondSegment(blobName, newSegment string) string {
	parts := strings.SplitN(blobName, "/", 3)
	if len(parts) < 3 {
		return blobName
	}
	return parts[0] + "/" + newSegment + "/" + parts[2]
}
