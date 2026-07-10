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

		// 1. Get the tag-list so we know every collection/album pair.
		tagList, err := store.GetBlobTagList(ctx, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error getting blob tag list", "error", err)
			http.Error(w, "No albums found", http.StatusNotFound)
			return
		}

		// 2. Fetch all blobs already marked as album thumbnails.
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

		// 3. For every collection/album pair that is not explicitly marked,
		// pick one non-deleted blob as an ephemeral representative.
		for collection, albums := range tagList {
			for _, album := range albums {
				key := collAlbum{collection, album}
				if seen[key] {
					continue
				}

				pickQuery := fmt.Sprintf(
					"@container='%s' and collection='%s' and album='%s' and isDeleted='false'",
					cfg.ImagesContainerName,
					collection,
					album,
				)
				candidates, err := store.FilterBlobsByTags(ctx, pickQuery, cfg.ImagesContainerName)
				if err != nil || len(candidates) == 0 {
					continue
				}

				seen[key] = true
				deduped = append(deduped, candidates[0])
			}
		}

		// 4. Optionally include deleted albums that still have no representative.
		if includeDeleted {
			for collection, albums := range tagList {
				for _, album := range albums {
					key := collAlbum{collection, album}
					if seen[key] {
						continue
					}

					delQuery := fmt.Sprintf(
						"@container='%s' and collection='%s' and album='%s' and isDeleted='true'",
						cfg.ImagesContainerName,
						collection,
						album,
					)
					deletedMarked, err := store.FilterBlobsByTags(ctx, delQuery, cfg.ImagesContainerName)
					if err != nil || len(deletedMarked) == 0 {
						continue
					}

					seen[key] = true
					deduped = append(deduped, deletedMarked[0])
				}
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
