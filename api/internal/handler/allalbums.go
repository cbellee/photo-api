package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
)

// AllAlbumsHandler returns every album across all collections in a single
// response. Each album is represented by its albumImage placeholder photo.
// This avoids the N+1 request pattern of calling AlbumHandler per collection.
// Supports ?includeDeleted=true to also return soft-deleted albums.
func AllAlbumsHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.AllAlbums")
		defer span.End()

		includeDeleted := r.URL.Query().Get("includeDeleted") == "true"

		// 1. Get the tag-list (collection → []album) to know every album.
		tagList, err := store.GetBlobTagList(ctx, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error getting blob tag list", "error", err)
			http.Error(w, "No albums found", http.StatusNotFound)
			return
		}

		// 2. Fetch ALL blobs marked as albumImage (non-deleted) in one query.
		query := fmt.Sprintf("@container='%s' and albumImage='true' and isDeleted='false'", cfg.ImagesContainerName)
		allMarked, _ := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)

		// Deduplicate: keep only the first blob per collection/album pair and
		// clear stale albumImage tags on extras.
		type collAlbum struct{ collection, album string }
		markedSet := make(map[collAlbum]bool)
		var markedBlobs []models.Blob
		for _, b := range allMarked {
			key := collAlbum{b.Tags["collection"], b.Tags["album"]}
			if markedSet[key] {
				b.Tags["albumImage"] = "false"
				if err := store.SetBlobTags(ctx, b.Name, cfg.ImagesContainerName, b.Tags); err != nil {
					slog.ErrorContext(ctx, "error clearing stale albumImage tag", "blob", b.Name, "error", err)
				}
				continue
			}
			markedSet[key] = true
			markedBlobs = append(markedBlobs, b)
		}

		// 3. For every album that is NOT yet marked, pick one non-deleted image.
		for collection, albums := range tagList {
			for _, album := range albums {
				key := collAlbum{collection, album}
				if markedSet[key] {
					continue
				}

				pickQuery := fmt.Sprintf(
					"@container='%s' and collection='%s' and album='%s' and isDeleted='false'",
					cfg.ImagesContainerName, collection, album,
				)
				candidates, err := store.FilterBlobsByTags(ctx, pickQuery, cfg.ImagesContainerName)
				if err != nil || len(candidates) == 0 {
					continue
				}

				pick := candidates[0]
				pick.Tags["albumImage"] = "true"
				if err := store.SetBlobTags(ctx, pick.Name, cfg.ImagesContainerName, pick.Tags); err != nil {
					slog.ErrorContext(ctx, "error setting albumImage tag", "blob", pick.Name, "error", err)
				} else {
					slog.InfoContext(ctx, "auto-assigned albumImage", "collection", collection, "album", album, "blob", pick.Name)
				}
				markedBlobs = append(markedBlobs, pick)
				markedSet[key] = true
			}
		}

		// 4. If includeDeleted, find deleted albums and pick a representative blob.
		if includeDeleted {
			for collection, albums := range tagList {
				for _, album := range albums {
					key := collAlbum{collection, album}
					if markedSet[key] {
						continue
					}
					delQuery := fmt.Sprintf(
						"@container='%s' and collection='%s' and album='%s' and isDeleted='true'",
						cfg.ImagesContainerName, collection, album,
					)
					deletedBlobs, err := store.FilterBlobsByTags(ctx, delQuery, cfg.ImagesContainerName)
					if err != nil || len(deletedBlobs) == 0 {
						continue
					}
					markedBlobs = append(markedBlobs, deletedBlobs[0])
					markedSet[key] = true
				}
			}
		}

		if len(markedBlobs) == 0 {
			http.Error(w, "No album images found", http.StatusNotFound)
			return
		}

		// Refresh tags for all blobs.
		for i, b := range markedBlobs {
			tags, err := store.GetBlobTags(ctx, b.Name, cfg.ImagesContainerName)
			if err != nil {
				slog.ErrorContext(ctx, "error getting blob tags", "error", err, "blobpath", b.Path)
				continue
			}
			markedBlobs[i].Tags = tags
		}

		photos := BlobsToPhotos(markedBlobs)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}
