package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("photo-api")

// CollectionHandler returns the list of collections (each represented by its
// collectionImage placeholder photo).
// Supports ?includeDeleted=true to also return soft-deleted collections.
func CollectionHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.Collections")
		defer span.End()

		includeDeleted := r.URL.Query().Get("includeDeleted") == "true"

		// 1. Get the tag-list (collection → albums) to know every collection.
		tagList, err := store.GetBlobTagList(ctx, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error getting blob tag list", "error", err)
			http.Error(w, "No collections found", http.StatusNotFound)
			return
		}

		// 2. Fetch blobs already marked as collectionImage (non-deleted only).
		query := fmt.Sprintf("@container='%s' and collectionImage='true' and isDeleted='false'", cfg.ImagesContainerName)
		slog.DebugContext(ctx, "query", "query", query)

		allMarked, _ := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)

		// Deduplicate: keep only the first blob per collection and clear
		// the stale collectionImage tag on any extras.
		markedCollections := make(map[string]bool)
		var markedBlobs []models.Blob
		for _, b := range allMarked {
			c := b.Tags["collection"]
			if markedCollections[c] {
				// Stale duplicate — clear the tag asynchronously.
				b.Tags["collectionImage"] = "false"
				if err := store.SetBlobTags(ctx, b.Name, cfg.ImagesContainerName, b.Tags); err != nil {
					slog.ErrorContext(ctx, "error clearing stale collectionImage tag", "blob", b.Name, "error", err)
				} else {
					slog.InfoContext(ctx, "cleared stale collectionImage", "collection", c, "blob", b.Name)
				}
				continue
			}
			markedCollections[c] = true
			markedBlobs = append(markedBlobs, b)
		}

		// 3. For every collection that is NOT yet marked, find one image and
		//    tag it as collectionImage so the UI can display it.
		for collection := range tagList {
			if markedCollections[collection] {
				continue
			}

			pickQuery := fmt.Sprintf("@container='%s' and collection='%s' and isDeleted='false'", cfg.ImagesContainerName, collection)
			candidates, err := store.FilterBlobsByTags(ctx, pickQuery, cfg.ImagesContainerName)
			if err != nil || len(candidates) == 0 {
				slog.WarnContext(ctx, "no blobs found for collection, skipping", "collection", collection)
				continue
			}

			pick := candidates[0]
			pick.Tags["collectionImage"] = "true"
			if err := store.SetBlobTags(ctx, pick.Name, cfg.ImagesContainerName, pick.Tags); err != nil {
				slog.ErrorContext(ctx, "error setting collectionImage tag", "blob", pick.Name, "error", err)
			} else {
				slog.InfoContext(ctx, "auto-assigned collectionImage", "collection", collection, "blob", pick.Name)
			}
			markedBlobs = append(markedBlobs, pick)
			markedCollections[collection] = true
		}

		// 4. If includeDeleted, also find deleted collections and pick a representative blob.
		if includeDeleted {
			for collection := range tagList {
				if markedCollections[collection] {
					continue
				}
				// This collection has no non-deleted blobs → pick any deleted blob as representative.
				delQuery := fmt.Sprintf("@container='%s' and collection='%s' and isDeleted='true'",
					cfg.ImagesContainerName, collection)
				deletedBlobs, err := store.FilterBlobsByTags(ctx, delQuery, cfg.ImagesContainerName)
				if err != nil || len(deletedBlobs) == 0 {
					continue
				}
				markedBlobs = append(markedBlobs, deletedBlobs[0])
				markedCollections[collection] = true
			}
		}

		if len(markedBlobs) == 0 {
			http.Error(w, "No collection images found", http.StatusNotFound)
			return
		}

		// Refresh tags for all blobs (may have just been updated).
		for i, b := range markedBlobs {
			tags, err := store.GetBlobTags(ctx, b.Name, cfg.ImagesContainerName)
			if err != nil {
				slog.ErrorContext(ctx, "error getting blob tags", "error", err, "blobpath", b.Path)
				continue
			}
			markedBlobs[i].Tags = tags
		}

		// Final safety filter: exclude any blobs that are now marked as deleted
		// (handles race conditions and stale collectionImage tags).
		// When includeDeleted is set, keep deleted blobs in the result.
		var resultBlobs []models.Blob
		for _, b := range markedBlobs {
			if b.Tags["isDeleted"] != "true" || includeDeleted {
				resultBlobs = append(resultBlobs, b)
			}
		}

		if len(resultBlobs) == 0 {
			http.Error(w, "No collection images found", http.StatusNotFound)
			return
		}

		photos := BlobsToPhotos(resultBlobs)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}
