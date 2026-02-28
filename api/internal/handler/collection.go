package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/storage"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("photo-api")

// CollectionHandler returns the list of collections (each represented by its
// collectionImage placeholder photo).
func CollectionHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.Collections")
		defer span.End()

		// 1. Get the tag-list (collection → albums) to know every collection.
		tagList, err := store.GetBlobTagList(ctx, cfg.ImagesContainerName, cfg.StorageUrl)
		if err != nil {
			slog.Error("error getting blob tag list", "error", err)
			http.Error(w, "No collections found", http.StatusNotFound)
			return
		}

		// 2. Fetch blobs already marked as collectionImage.
		query := fmt.Sprintf("@container='%s' and collectionImage='true'", cfg.ImagesContainerName)
		slog.Debug("query", "query", query)

		markedBlobs, _ := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName, cfg.StorageUrl)

		// Build a set of collections that already have a collectionImage.
		markedCollections := make(map[string]bool)
		for _, b := range markedBlobs {
			if c, ok := b.Tags["collection"]; ok {
				markedCollections[c] = true
			}
		}

		// 3. For every collection that is NOT yet marked, find one image and
		//    tag it as collectionImage so the UI can display it.
		for collection := range tagList {
			if markedCollections[collection] {
				continue
			}

			pickQuery := fmt.Sprintf("@container='%s' and collection='%s'", cfg.ImagesContainerName, collection)
			candidates, err := store.FilterBlobsByTags(ctx, pickQuery, cfg.ImagesContainerName, cfg.StorageUrl)
			if err != nil || len(candidates) == 0 {
				slog.Warn("no blobs found for collection, skipping", "collection", collection)
				continue
			}

			pick := candidates[0]
			pick.Tags["collectionImage"] = "true"
			if err := store.SetBlobTags(ctx, pick.Name, cfg.ImagesContainerName, cfg.StorageUrl, pick.Tags); err != nil {
				slog.Error("error setting collectionImage tag", "blob", pick.Name, "error", err)
			} else {
				slog.Info("auto-assigned collectionImage", "collection", collection, "blob", pick.Name)
			}
			markedBlobs = append(markedBlobs, pick)
			markedCollections[collection] = true
		}

		if len(markedBlobs) == 0 {
			http.Error(w, "No collection images found", http.StatusNotFound)
			return
		}

		// Refresh tags for all blobs (may have just been updated).
		for i, b := range markedBlobs {
			tags, err := store.GetBlobTags(ctx, b.Name, cfg.ImagesContainerName, cfg.StorageUrl)
			if err != nil {
				slog.Error("error getting blob tags", "error", err, "blobpath", b.Path)
				continue
			}
			markedBlobs[i].Tags = tags
		}

		photos := BlobsToPhotos(markedBlobs)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}
