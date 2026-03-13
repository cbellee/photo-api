package handler

import (
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/cbellee/photo-api/internal/exif"
	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
	"github.com/cbellee/photo-api/internal/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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

		uploadStart := time.Now()

		slog.InfoContext(ctx, "upload request received",
			"method", r.Method,
			"content_length", r.ContentLength,
			"content_type", r.Header.Get("Content-Type"),
			"remote_addr", r.RemoteAddr,
			"user_agent", r.Header.Get("User-Agent"),
		)

		if r.Body == nil {
			slog.WarnContext(ctx, "upload rejected: nil request body")
			span.SetStatus(codes.Error, "nil request body")
			http.Error(w, "Multipart form not found", http.StatusBadRequest)
			return
		}

		// Keep files in memory rather than spilling to temp disk files.
		// The container image is FROM scratch so /tmp does not exist.
		// With the double-buffer copy eliminated and SPA concurrency
		// capped at 3, worst-case RSS ≈ 3 × 32 MiB = 96 MiB.
		memLimit := cfg.MemoryLimitMb << 20
		slog.DebugContext(ctx, "parsing multipart form",
			"memory_limit_bytes", memLimit,
			"memory_limit_mb", cfg.MemoryLimitMb,
		)

		parseStart := time.Now()
		err := r.ParseMultipartForm(memLimit)
		if err != nil {
			slog.ErrorContext(ctx, "multipart parse failed",
				"error", err,
				"content_length", r.ContentLength,
				"memory_limit_bytes", memLimit,
				"elapsed_ms", time.Since(parseStart).Milliseconds(),
			)
			span.SetStatus(codes.Error, "multipart parse failed")
			span.RecordError(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.DebugContext(ctx, "multipart form parsed",
			"elapsed_ms", time.Since(parseStart).Milliseconds(),
			"num_files", len(r.MultipartForm.File),
			"num_values", len(r.MultipartForm.Value),
		)

		it := models.ImageTags{}
		metadataValues, ok := r.MultipartForm.Value["metadata"]
		if !ok || len(metadataValues) == 0 {
			slog.WarnContext(ctx, "upload rejected: missing metadata field",
				"available_fields", mapKeys(r.MultipartForm.Value),
			)
			span.SetStatus(codes.Error, "missing metadata")
			http.Error(w, "metadata field is required", http.StatusBadRequest)
			return
		}
		err = json.Unmarshal([]byte(metadataValues[0]), &it)
		if err != nil {
			slog.ErrorContext(ctx, "error unmarshalling metadata json",
				"error", err,
				"raw_metadata", truncate(metadataValues[0], 500),
			)
			span.SetStatus(codes.Error, "invalid metadata JSON")
			span.RecordError(err)
			http.Error(w, "Invalid metadata", http.StatusBadRequest)
			return
		}

		slog.InfoContext(ctx, "upload metadata parsed",
			"collection", it.Collection,
			"album", it.Album,
			"type", it.Type,
			"description", truncate(it.Description, 100),
		)

		// Validate the declared content type.
		if !allowedImageTypes[it.Type] {
			slog.WarnContext(ctx, "upload rejected: unsupported image type",
				"type", it.Type,
				"allowed_types", allowedImageTypes,
			)
			span.SetStatus(codes.Error, "unsupported image type")
			http.Error(w, "Unsupported image type", http.StatusUnsupportedMediaType)
			return
		}

		fh, ok := r.MultipartForm.File["photo"]
		if !ok || len(fh) == 0 {
			slog.WarnContext(ctx, "upload rejected: missing photo file",
				"available_file_fields", mapKeys(r.MultipartForm.File),
			)
			span.SetStatus(codes.Error, "missing photo file")
			http.Error(w, "photo file is required", http.StatusBadRequest)
			return
		}

		fileNameWithPrefix := fmt.Sprintf("%s/%s/%s", it.Collection, it.Album, fh[0].Filename)
		span.SetAttributes(
			attribute.String("collection", it.Collection),
			attribute.String("album", it.Album),
			attribute.String("filename", fh[0].Filename),
			attribute.Int64("file.size", fh[0].Size),
			attribute.String("file.content_type", it.Type),
		)

		slog.InfoContext(ctx, "processing upload",
			"filename", fh[0].Filename,
			"blob_path", fileNameWithPrefix,
			"file_size", fh[0].Size,
			"declared_content_type", it.Type,
			"file_header_content_type", fh[0].Header.Get("Content-Type"),
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
			slog.ErrorContext(ctx, "error opening multipart file",
				"filename", fh[0].Filename,
				"file_size", fh[0].Size,
				"error", err,
				"file_type", reflect.TypeOf(file).String(),
			)
			span.SetStatus(codes.Error, "file open failed")
			span.RecordError(err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		slog.DebugContext(ctx, "multipart file opened",
			"filename", fh[0].Filename,
			"underlying_type", fmt.Sprintf("%T", file),
		)

		// multipart.File implements io.ReadSeeker so we can rewind between
		// operations without ever copying the full payload into a []byte.

		// 1. Decode image dimensions (reads only the header bytes).
		decodeStart := time.Now()
		img, imgFormat, err := image.DecodeConfig(file)
		if err != nil {
			slog.ErrorContext(ctx, "error decoding image config",
				"error", err,
				"filename", fh[0].Filename,
				"declared_type", it.Type,
			)
			span.SetStatus(codes.Error, "image decode failed")
			span.RecordError(err)
			http.Error(w, "Invalid image file", http.StatusBadRequest)
			return
		}
		slog.DebugContext(ctx, "image config decoded",
			"width", img.Width,
			"height", img.Height,
			"format", imgFormat,
			"elapsed_ms", time.Since(decodeStart).Milliseconds(),
		)

		// 2. Rewind and extract EXIF metadata.
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			slog.ErrorContext(ctx, "error seeking file for exif",
				"error", err,
				"filename", fh[0].Filename,
			)
			span.SetStatus(codes.Error, "seek failed")
			span.RecordError(err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		exifStart := time.Now()
		exifData := ""
		exifData, err = exif.GetExifJSON(file)
		if err != nil {
			slog.WarnContext(ctx, "exif extraction failed (non-fatal)",
				"error", err,
				"filename", fh[0].Filename,
			)
			span.AddEvent("exif_extraction_failed")
			// EXIF errors are non-fatal — continue without EXIF data
		} else {
			slog.DebugContext(ctx, "exif data extracted",
				"exif_length", len(exifData),
				"elapsed_ms", time.Since(exifStart).Milliseconds(),
			)
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
			slog.ErrorContext(ctx, "error seeking file for blob upload",
				"error", err,
				"filename", fh[0].Filename,
			)
			span.SetStatus(codes.Error, "seek failed")
			span.RecordError(err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		slog.InfoContext(ctx, "saving blob to storage",
			"blob_path", fileNameWithPrefix,
			"container", cfg.UploadsContainerName,
			"file_size", fh[0].Size,
			"content_type", it.Type,
			"num_tags", len(tags),
			"num_metadata", len(md),
		)

		saveStart := time.Now()
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
			slog.ErrorContext(ctx, "error saving blob to storage",
				"error", err,
				"blob_path", fileNameWithPrefix,
				"container", cfg.UploadsContainerName,
				"file_size", fh[0].Size,
				"elapsed_ms", time.Since(saveStart).Milliseconds(),
			)
			span.SetStatus(codes.Error, "blob save failed")
			span.RecordError(err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		totalElapsed := time.Since(uploadStart)
		slog.InfoContext(ctx, "upload completed successfully",
			"blob_path", fileNameWithPrefix,
			"file_size", fh[0].Size,
			"width", img.Width,
			"height", img.Height,
			"has_exif", exifData != "",
			"save_elapsed_ms", time.Since(saveStart).Milliseconds(),
			"total_elapsed_ms", totalElapsed.Milliseconds(),
		)
		span.SetAttributes(
			attribute.Int64("upload.total_ms", totalElapsed.Milliseconds()),
			attribute.Int64("upload.save_ms", time.Since(saveStart).Milliseconds()),
			attribute.Int("image.width", img.Width),
			attribute.Int("image.height", img.Height),
			attribute.Bool("image.has_exif", exifData != ""),
		)

		w.WriteHeader(http.StatusCreated)
	}
}

// mapKeys returns the keys of a map as a slice (for logging available form fields).
func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// truncate returns s trimmed to at most maxLen characters, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
