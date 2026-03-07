package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
	"go.opentelemetry.io/otel/attribute"
)

// UpdateHandler handles PUT requests to update blob tags for a photo.
func UpdateHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.Update")
		defer span.End()

		// Derive the blob name from the validated URL path parameters
		// instead of trusting a client-supplied "name" field in the body.
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
		blobID := r.PathValue("id")
		if blobID == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}
		blobName := fmt.Sprintf("%s/%s/%s", collection, album, blobID)

		if r.Body == nil {
			http.Error(w, "body is empty", http.StatusBadRequest)
			return
		}

		// Limit request body size to 1 MB (tags payload should be small).
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		// get new tags from request body
		newTags := map[string]string{}
		err := json.NewDecoder(r.Body).Decode(&newTags)
		if err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Override the name with the server-derived value to prevent
		// a client from targeting a different blob.
		newTags["name"] = blobName

		// get current blob tags from storage and compare with updated tags
		span.SetAttributes(attribute.String("blob.name", blobName))
		currTags, err := store.GetBlobTags(ctx, blobName, cfg.ImagesContainerName)
		if err != nil {
			slog.Error("error getting blob tags", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// remove 'Url' tag from comparison
		delete(currTags, "Url")

		if maps.Equal(currTags, newTags) {
			slog.Info("tags not modified", "tags", currTags)
			http.Error(w, "Tags not modified", http.StatusNotModified)
			return
		}

		// get current image with collectionImage tag set to 'true'
		currentCollectionImage, err := GetCollectionImage(store, ctx, cfg, currTags["collection"])
		if err != nil {
			slog.Error("error getting collection image", "error", err)
			// not fatal — the collection may not have a collectionImage yet
		}

		// set CollectionImage tag to 'false' on existing image if it has been set to 'true' on the current image
		if newTags["collectionImage"] == "true" && len(currentCollectionImage) > 0 && currentCollectionImage[0].Tags["name"] != newTags["name"] {
			slog.Info("collection image set on another image. Setting 'collectionImage'",
				"collection", currTags["collection"], "image", currentCollectionImage[0].Name)

			currentCollectionImage[0].Tags["collectionImage"] = "false"

			err = store.SetBlobTags(ctx, currentCollectionImage[0].Name, cfg.ImagesContainerName, currentCollectionImage[0].Tags)
			if err != nil {
				slog.Error("error setting collectionImage tag", "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}

		// update blob tags
		err = store.SetBlobTags(ctx, blobName, cfg.ImagesContainerName, newTags)
		if err != nil {
			slog.Error("error updating blob tags", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// GetCollectionImage returns the blobs that have collectionImage='true' for the
// given collection. Exported so tests can call it directly.
func GetCollectionImage(store storage.BlobStore, ctx context.Context, cfg *Config, collection string) ([]models.Blob, error) {
	query := fmt.Sprintf("@container='%s' and collection='%s' and collectionImage='true'", cfg.ImagesContainerName, collection)
	filteredBlobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)
	if err != nil {
		slog.Error("error getting blobs by tags", "error", err)
		return nil, err
	}

	if len(filteredBlobs) == 0 {
		return nil, fmt.Errorf("no collection image found for collection: %s", collection)
	}

	return filteredBlobs, nil
}
