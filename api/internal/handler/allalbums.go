package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/storage"
)

// AllAlbumsHandler returns every album across all collections in a single
// response. Each album is represented by its albumImage placeholder photo.
// This avoids the N+1 request pattern of calling AlbumHandler per collection.
//
// Albums without a thumbnail are not returned here; AlbumHandler auto-assigns
// thumbnails lazily when a user visits a specific collection.
//
// Supports ?includeDeleted=true to also return soft-deleted album thumbnails.
func AllAlbumsHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.AllAlbums")
		defer span.End()

		includeDeleted := r.URL.Query().Get("includeDeleted") == "true"

		// Single query: fetch all blobs marked as album thumbnails.
		query := fmt.Sprintf(
			"@container='%s' and albumImage='true' and isDeleted='false'",
			cfg.ImagesContainerName,
		)
		allMarked, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error querying album images", "error", err)
			http.Error(w, "Failed to query albums", http.StatusInternalServerError)
			return
		}

		// Deduplicate: keep only the first blob per collection/album pair.
		type collAlbum struct{ collection, album string }
		seen := make(map[collAlbum]bool)
		deduped := allMarked[:0]
		for _, b := range allMarked {
			key := collAlbum{b.Tags["collection"], b.Tags["album"]}
			if seen[key] {
				continue
			}
			seen[key] = true
			deduped = append(deduped, b)
		}

		// Optionally include soft-deleted album thumbnails.
		if includeDeleted {
			delQuery := fmt.Sprintf(
				"@container='%s' and albumImage='true' and isDeleted='true'",
				cfg.ImagesContainerName,
			)
			deletedMarked, _ := store.FilterBlobsByTags(ctx, delQuery, cfg.ImagesContainerName)
			for _, b := range deletedMarked {
				key := collAlbum{b.Tags["collection"], b.Tags["album"]}
				if seen[key] {
					continue
				}
				seen[key] = true
				deduped = append(deduped, b)
			}
		}

		if len(deduped) == 0 {
			http.Error(w, "No album images found", http.StatusNotFound)
			return
		}

		photos := BlobsToPhotos(deduped)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}
