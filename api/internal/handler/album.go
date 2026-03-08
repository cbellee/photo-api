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
// Supports ?includeDeleted=true to also return soft-deleted albums.
func AlbumHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.Albums")
		defer span.End()

		collection := r.PathValue("collection")
		if err := validatePathParam("collection", collection); err != nil {
			slog.ErrorContext(ctx, "invalid path param", "name", "collection", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		span.SetAttributes(attribute.String("collection", collection))

		includeDeleted := r.URL.Query().Get("includeDeleted") == "true"

		// 1. Get the tag-list to know every album in this collection.
		tagList, err := store.GetBlobTagList(ctx, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error getting blob tag list", "error", err)
			http.Error(w, "No albums found", http.StatusNotFound)
			return
		}
		albums := tagList[collection] // []string of album names

		// 2. Fetch blobs already marked as albumImage for this collection (non-deleted only).
		query := fmt.Sprintf("@container='%s' and collection='%s' and albumImage='true' and isDeleted='false'", cfg.ImagesContainerName, collection)
		markedBlobs, _ := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)

		markedAlbums := make(map[string]bool)
		for _, b := range markedBlobs {
			if a, ok := b.Tags["album"]; ok {
				markedAlbums[a] = true
			}
		}

		// 3. For every album that is NOT yet marked, pick one non-deleted image and tag it.
		for _, album := range albums {
			if markedAlbums[album] {
				continue
			}

			pickQuery := fmt.Sprintf("@container='%s' and collection='%s' and album='%s' and isDeleted='false'", cfg.ImagesContainerName, collection, album)
			candidates, err := store.FilterBlobsByTags(ctx, pickQuery, cfg.ImagesContainerName)
			if err != nil || len(candidates) == 0 {
				// No non-deleted blobs → this is a deleted album; skip for now.
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
			markedAlbums[album] = true
		}

		// 4. If includeDeleted, also find deleted albums and pick a representative blob.
		if includeDeleted {
			for _, album := range albums {
				if markedAlbums[album] {
					continue
				}
				// This album has no non-deleted blobs → pick any deleted blob as representative.
				delQuery := fmt.Sprintf("@container='%s' and collection='%s' and album='%s' and isDeleted='true'",
					cfg.ImagesContainerName, collection, album)
				deletedBlobs, err := store.FilterBlobsByTags(ctx, delQuery, cfg.ImagesContainerName)
				if err != nil || len(deletedBlobs) == 0 {
					continue
				}
				markedBlobs = append(markedBlobs, deletedBlobs[0])
				markedAlbums[album] = true
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
