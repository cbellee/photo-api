package handler

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
)

// BlobsToPhotos converts a slice of Blob models into a slice of Photo models.
// It centralises the repeated mapping logic that was previously duplicated across
// collectionHandler, albumHandler, and photoHandler.
func BlobsToPhotos(blobs []models.Blob) []models.Photo {
	photos := make([]models.Photo, 0, len(blobs))

	for _, b := range blobs {
		width, _ := strconv.ParseInt(b.MetaData["Width"], 10, 32)
		height, _ := strconv.ParseInt(b.MetaData["Height"], 10, 32)

		isDeleted, err := strconv.ParseBool(b.Tags["isDeleted"])
		if err != nil {
			isDeleted = false
		}

		albumImage, err := strconv.ParseBool(b.Tags["albumImage"])
		if err != nil {
			albumImage = false
		}

		collectionImage, err := strconv.ParseBool(b.Tags["collectionImage"])
		if err != nil {
			collectionImage = false
		}

		orientation, err := strconv.Atoi(b.Tags["orientation"])
		if err != nil {
			orientation = 0
		}

		photo := models.Photo{
			Src:             b.Path,
			Name:            b.Name,
			Width:           int(width),
			Height:          int(height),
			Album:           b.Tags["album"],
			Collection:      b.Tags["collection"],
			Description:     b.Tags["description"],
			ExifData:        b.MetaData["ExifData"],
			IsDeleted:       isDeleted,
			Orientation:     orientation,
			AlbumImage:      albumImage,
			CollectionImage: collectionImage,
		}

		photos = append(photos, photo)
	}

	return photos
}

// ExifSidecarName returns the conventional sidecar blob name for a given image blob.
func ExifSidecarName(blobName string) string {
	return blobName + ".exif.json"
}

// HydrateExifData loads EXIF sidecar blobs for photos that don't already
// have inline ExifData (backward compat for blobs uploaded before the
// sidecar migration). Errors are silently ignored — a missing sidecar
// simply means no EXIF data is available.
func HydrateExifData(ctx context.Context, photos []models.Photo, store storage.BlobStore, containerName string) {
	for i := range photos {
		if photos[i].ExifData != "" {
			continue // already has inline EXIF (legacy)
		}
		sidecarName := ExifSidecarName(photos[i].Name)
		data, err := store.GetBlob(ctx, sidecarName, containerName)
		if err != nil {
			continue // no sidecar, that's fine
		}
		photos[i].ExifData = string(data)
		slog.DebugContext(ctx, "loaded exif sidecar", "photo", photos[i].Name, "sidecar", sidecarName)
	}
}
