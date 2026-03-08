package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/cbellee/photo-api/internal/storage"
	"go.opentelemetry.io/otel/attribute"
)

// thumbnailRequest is the JSON body for thumbnail endpoints.
type thumbnailRequest struct {
	// ImageName selects a new thumbnail image. If empty, the current
	// thumbnail is kept and only orientation is updated.
	ImageName string `json:"imageName,omitempty"`
	// Orientation sets the rotation in degrees (0, 90, 180, 270).
	// Only applied when present (non-zero or explicitly set).
	Orientation *int `json:"orientation,omitempty"`
}

// ThumbnailCollectionHandler handles PUT /api/thumbnail/{collection}.
// It can:
//   - Change which image is the collection thumbnail (set imageName).
//   - Rotate the current collection thumbnail (set orientation).
//   - Both at once.
func ThumbnailCollectionHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.ThumbnailCollection")
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

		var req thumbnailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.ImageName == "" && req.Orientation == nil {
			http.Error(w, "imageName or orientation is required", http.StatusBadRequest)
			return
		}

		// If changing the thumbnail image, clear the old one first.
		if req.ImageName != "" {
			// Find current collection thumbnail.
			currentImages, err := GetCollectionImage(store, ctx, cfg, collection)
			if err == nil && len(currentImages) > 0 {
				for _, img := range currentImages {
					if img.Name != req.ImageName {
						img.Tags["collectionImage"] = "false"
						if err := store.SetBlobTags(ctx, img.Name, cfg.ImagesContainerName, img.Tags); err != nil {
							slog.ErrorContext(ctx, "error clearing old collectionImage", "blob", img.Name, "error", err)
						}
					}
				}
			}

			// Set the new image as collection thumbnail.
			newTags, err := store.GetBlobTags(ctx, req.ImageName, cfg.ImagesContainerName)
			if err != nil {
				slog.ErrorContext(ctx, "error getting tags for new thumbnail", "blob", req.ImageName, "error", err)
				http.Error(w, "image not found", http.StatusNotFound)
				return
			}

			newTags["collectionImage"] = "true"

			if req.Orientation != nil {
				newTags["orientation"] = strconv.Itoa(*req.Orientation)
			}

			if err := store.SetBlobTags(ctx, req.ImageName, cfg.ImagesContainerName, newTags); err != nil {
				slog.ErrorContext(ctx, "error setting new collectionImage", "blob", req.ImageName, "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			slog.InfoContext(ctx, "collection thumbnail updated", "collection", collection, "newImage", req.ImageName)
		} else if req.Orientation != nil {
			// Only rotating, find the current collection thumbnail.
			currentImages, err := GetCollectionImage(store, ctx, cfg, collection)
			if err != nil || len(currentImages) == 0 {
				http.Error(w, "no collection thumbnail found", http.StatusNotFound)
				return
			}

			img := currentImages[0]
			img.Tags["orientation"] = strconv.Itoa(*req.Orientation)
			if err := store.SetBlobTags(ctx, img.Name, cfg.ImagesContainerName, img.Tags); err != nil {
				slog.ErrorContext(ctx, "error rotating collection thumbnail", "blob", img.Name, "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			slog.InfoContext(ctx, "collection thumbnail rotated", "collection", collection, "orientation", *req.Orientation)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "collection thumbnail updated",
		})
	}
}

// ThumbnailAlbumHandler handles PUT /api/thumbnail/{collection}/{album}.
// It can change which image is the album thumbnail and/or rotate it.
func ThumbnailAlbumHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.ThumbnailAlbum")
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

		var req thumbnailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.ImageName == "" && req.Orientation == nil {
			http.Error(w, "imageName or orientation is required", http.StatusBadRequest)
			return
		}

		if req.ImageName != "" {
			// Find current album thumbnails and clear them.
			albumQuery := fmt.Sprintf("@container='%s' and collection='%s' and album='%s' and albumImage='true'",
				cfg.ImagesContainerName, collection, album)
			currentAlbumImages, _ := store.FilterBlobsByTags(ctx, albumQuery, cfg.ImagesContainerName)

			for _, img := range currentAlbumImages {
				if img.Name != req.ImageName {
					img.Tags["albumImage"] = "false"
					if err := store.SetBlobTags(ctx, img.Name, cfg.ImagesContainerName, img.Tags); err != nil {
						slog.ErrorContext(ctx, "error clearing old albumImage", "blob", img.Name, "error", err)
					}
				}
			}

			// Set the new image as album thumbnail.
			newTags, err := store.GetBlobTags(ctx, req.ImageName, cfg.ImagesContainerName)
			if err != nil {
				slog.ErrorContext(ctx, "error getting tags for new album thumbnail", "blob", req.ImageName, "error", err)
				http.Error(w, "image not found", http.StatusNotFound)
				return
			}

			newTags["albumImage"] = "true"

			if req.Orientation != nil {
				newTags["orientation"] = strconv.Itoa(*req.Orientation)
			}

			if err := store.SetBlobTags(ctx, req.ImageName, cfg.ImagesContainerName, newTags); err != nil {
				slog.ErrorContext(ctx, "error setting new albumImage", "blob", req.ImageName, "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			slog.InfoContext(ctx, "album thumbnail updated", "collection", collection, "album", album, "newImage", req.ImageName)
		} else if req.Orientation != nil {
			// Only rotating, find the current album thumbnail.
			albumQuery := fmt.Sprintf("@container='%s' and collection='%s' and album='%s' and albumImage='true'",
				cfg.ImagesContainerName, collection, album)
			currentAlbumImages, err := store.FilterBlobsByTags(ctx, albumQuery, cfg.ImagesContainerName)
			if err != nil || len(currentAlbumImages) == 0 {
				http.Error(w, "no album thumbnail found", http.StatusNotFound)
				return
			}

			img := currentAlbumImages[0]
			img.Tags["orientation"] = strconv.Itoa(*req.Orientation)
			if err := store.SetBlobTags(ctx, img.Name, cfg.ImagesContainerName, img.Tags); err != nil {
				slog.ErrorContext(ctx, "error rotating album thumbnail", "blob", img.Name, "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			slog.InfoContext(ctx, "album thumbnail rotated", "collection", collection, "album", album, "orientation", *req.Orientation)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "album thumbnail updated",
		})
	}
}

// CollectionPhotosHandler handles GET /api/photos/{collection}.
// It returns ALL photos in a collection (for the thumbnail picker UI).
func CollectionPhotosHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.CollectionPhotos")
		defer span.End()

		collection := r.PathValue("collection")
		if err := validatePathParam("collection", collection); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		span.SetAttributes(attribute.String("collection", collection))

		query := fmt.Sprintf("@container='%s' and collection='%s' and isDeleted='false'",
			cfg.ImagesContainerName, collection)
		blobs, err := store.FilterBlobsByTags(ctx, query, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error querying blobs", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if len(blobs) == 0 {
			http.Error(w, "no photos found", http.StatusNotFound)
			return
		}

		photos := BlobsToPhotos(blobs)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}
