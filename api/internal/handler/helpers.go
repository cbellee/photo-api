package handler

import (
	"strconv"

	"github.com/cbellee/photo-api/internal/models"
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
