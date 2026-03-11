package handler

import (
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/cbellee/photo-api/internal/exif"
	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
	"github.com/cbellee/photo-api/internal/utils"
	"go.opentelemetry.io/otel/attribute"
)

// allowedImageTypes is the set of MIME types accepted for photo uploads.
var allowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// UploadHandler handles multipart file uploads, extracts EXIF data and image
// dimensions, and saves the blob to the uploads container.
//
// Memory optimisation: the multipart file is used directly as an io.ReadSeeker
// so we never allocate a second in-memory copy of the file data. With a low
// ParseMultipartForm memory limit (1 MiB) large files are spilled to a temp
// file on disk, keeping per-request RSS near zero.
func UploadHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.Upload")
		defer span.End()

		if r.Body == nil {
			http.Error(w, "Multipart form not found", http.StatusBadRequest)
			return
		}

		// Use a small in-memory limit so large uploads spill to temp files,
		// keeping per-request memory low even under high concurrency.
		const multipartMemLimit = 1 << 20 // 1 MiB
		err := r.ParseMultipartForm(multipartMemLimit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		it := models.ImageTags{}
		metadataValues, ok := r.MultipartForm.Value["metadata"]
		if !ok || len(metadataValues) == 0 {
			http.Error(w, "metadata field is required", http.StatusBadRequest)
			return
		}
		err = json.Unmarshal([]byte(metadataValues[0]), &it)
		if err != nil {
			slog.ErrorContext(ctx, "error unmarshalling metadata json", "error", err)
			http.Error(w, "Invalid metadata", http.StatusBadRequest)
			return
		}

		// Validate the declared content type.
		if !allowedImageTypes[it.Type] {
			http.Error(w, "Unsupported image type", http.StatusUnsupportedMediaType)
			return
		}

		fh, ok := r.MultipartForm.File["photo"]
		if !ok || len(fh) == 0 {
			http.Error(w, "photo file is required", http.StatusBadRequest)
			return
		}

		fileNameWithPrefix := fmt.Sprintf("%s/%s/%s", it.Collection, it.Album, fh[0].Filename)
		span.SetAttributes(
			attribute.String("collection", it.Collection),
			attribute.String("album", it.Album),
			attribute.String("filename", fh[0].Filename),
		)

		tags := make(map[string]string)
		tags["name"] = fileNameWithPrefix
		tags["description"] = it.Description
		tags["collection"] = it.Collection
		tags["album"] = it.Album
		tags["isDeleted"] = strconv.FormatBool(it.IsDeleted)
		tags["collectionImage"] = strconv.FormatBool(it.CollectionImage)
		tags["albumImage"] = strconv.FormatBool(it.AlbumImage)

		// strip invalid characters from tag values
		for k, v := range tags {
			tags[k] = utils.StripInvalidTagCharacters(v)
		}

		file, err := fh[0].Open()
		if err != nil {
			slog.ErrorContext(ctx, "error opening file", "filename", fh[0].Filename, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// multipart.File implements io.ReadSeeker so we can rewind between
		// operations without ever copying the full payload into a []byte.

		// 1. Decode image dimensions (reads only the header bytes).
		img, _, err := image.DecodeConfig(file)
		if err != nil {
			slog.ErrorContext(ctx, "error decoding image config", "error", err)
			http.Error(w, "Invalid image file", http.StatusBadRequest)
			return
		}

		// 2. Rewind and extract EXIF metadata.
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			slog.ErrorContext(ctx, "error seeking file", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		exifData := ""
		exifData, err = exif.GetExifJSON(file)
		if err != nil {
			slog.ErrorContext(ctx, "error getting exif data", "error", err)
			// EXIF errors are non-fatal — continue without EXIF data
		}

		md := make(map[string]string)
		md["height"] = fmt.Sprint(img.Height)
		md["width"] = fmt.Sprint(img.Width)
		md["size"] = strconv.Itoa(int(fh[0].Size))

		if exifData != "" {
			md["exifData"] = exifData
		}

		// 3. Rewind and stream to blob storage — no second buffer needed.
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			slog.ErrorContext(ctx, "error seeking file for upload", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = store.SaveBlob(
			ctx,
			file,
			fh[0].Size,
			fileNameWithPrefix,
			cfg.UploadsContainerName,
			tags,
			md,
			it.Type,
		)
		if err != nil {
			slog.ErrorContext(ctx, "error saving blob", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}
