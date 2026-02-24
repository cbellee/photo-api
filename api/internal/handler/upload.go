package handler

import (
	"bytes"
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
)

// UploadHandler handles multipart file uploads, extracts EXIF data and image
// dimensions, and saves the blob to the uploads container.
func UploadHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if r.Body == nil {
			http.Error(w, "Multipart form not found", http.StatusBadRequest)
			return
		}

		err := r.ParseMultipartForm(cfg.MemoryLimitMb << 20)
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
			slog.Error("error unmarshalling metadata json", "error", err)
			http.Error(w, "Invalid metadata", http.StatusBadRequest)
			return
		}

		fh, ok := r.MultipartForm.File["photo"]
		if !ok || len(fh) == 0 {
			http.Error(w, "photo file is required", http.StatusBadRequest)
			return
		}

		fileNameWithPrefix := fmt.Sprintf("%s/%s/%s", it.Collection, it.Album, fh[0].Filename)

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
			slog.Error("error opening file", "filename", fh[0].Filename, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		buf := bytes.NewBuffer(nil)
		if _, err := io.Copy(buf, file); err != nil {
			slog.Error("error copying to buffer", "filename", fh[0].Filename, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		img, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
		if err != nil {
			slog.Error("error decoding image config", "error", err)
			http.Error(w, "Invalid image file", http.StatusBadRequest)
			return
		}

		exifData := ""
		exifData, err = exif.GetExifJSON(*buf)
		if err != nil {
			slog.Error("error getting exif data", "error", err)
			// EXIF errors are non-fatal — continue without EXIF data
		}

		md := make(map[string]string)
		md["height"] = fmt.Sprint(img.Height)
		md["width"] = fmt.Sprint(img.Width)
		md["size"] = strconv.Itoa(int(fh[0].Size))
		md["exifData"] = exifData

		err = store.SaveBlob(
			ctx,
			buf.Bytes(),
			fileNameWithPrefix,
			cfg.UploadsContainerName,
			cfg.StorageUrl,
			tags,
			md,
			it.Type,
		)
		if err != nil {
			slog.Error("error saving blob", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}
