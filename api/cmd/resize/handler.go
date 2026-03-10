package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
	"github.com/cbellee/photo-api/internal/utils"
	"github.com/dapr/go-sdk/service/common"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("resize-api")

// blobRef holds the decomposed parts of a blob URL.
type blobRef struct {
	container  string
	path       string
	collection string
	album      string
}

// Handler processes resize events from the Dapr binding.
type Handler struct {
	store storage.BlobStore
	cfg   *Config
}

// NewHandler creates a new resize Handler.
func NewHandler(store storage.BlobStore, cfg *Config) *Handler {
	return &Handler{store: store, cfg: cfg}
}

// Resize is the Dapr binding invocation handler for image-resize events.
// It always returns nil so Dapr ACKs the message.  Returning a non-nil
// error causes Dapr to NACK the RabbitMQ message, which requeues it and
// creates an infinite retry loop for non-transient failures (e.g. 404).
func (h *Handler) Resize(ctx context.Context, in *common.BindingEvent) (out []byte, err error) {
	ctx, span := tracer.Start(ctx, "resize.Resize")
	defer span.End()
	defer func() {
		if err != nil {
			slog.ErrorContext(ctx, "resize failed (message acknowledged to prevent requeue)", "error", err)
			span.RecordError(err)
			err = nil // ACK — reprocessing will not fix non-transient errors
		}
	}()

	if in == nil {
		return nil, fmt.Errorf("received nil binding event")
	}

	// Parse the incoming event.
	evt, err := utils.ConvertToEvent(in)
	if err != nil {
		return nil, fmt.Errorf("converting binding event: %w", err)
	}
	h.logEvent(ctx, evt, in)

	// Decompose the blob URL.
	ref, err := parseBlobRef(evt.Data.URL)
	if err != nil {
		return nil, fmt.Errorf("parsing blob URL: %w", err)
	}
	span.SetAttributes(
		attribute.String("blob.container", ref.container),
		attribute.String("blob.path", ref.path),
		attribute.String("blob.collection", ref.collection),
		attribute.String("blob.album", ref.album),
	)
	slog.InfoContext(ctx, "processing blob", "container", ref.container, "path", ref.path, "album", ref.album, "collection", ref.collection)

	// Download the source blob.
	blobBytes, err := h.store.GetBlob(ctx, ref.path, ref.container)
	if err != nil {
		return nil, fmt.Errorf("downloading blob %s: %w", ref.path, err)
	}

	// Fetch existing tags and metadata.
	tags, err := h.store.GetBlobTags(ctx, ref.path, ref.container)
	if err != nil {
		return nil, fmt.Errorf("getting blob tags for %s: %w", ref.path, err)
	}
	metadata, err := h.store.GetBlobMetadata(ctx, ref.path, ref.container)
	if err != nil {
		return nil, fmt.Errorf("getting blob metadata for %s: %w", ref.path, err)
	}

	// Resize the image.
	imgBytes, err := utils.ResizeImage(blobBytes, evt.Data.ContentType, ref.path, h.cfg.MaxImageHeight, h.cfg.MaxImageWidth)
	if err != nil {
		return nil, fmt.Errorf("resizing image %s: %w", ref.path, err)
	}

	// Read the dimensions of the resized image.
	imgCfg, _, err := image.DecodeConfig(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, fmt.Errorf("decoding resized image config %s: %w", ref.path, err)
	}
	slog.InfoContext(ctx, "resized image", "path", ref.path, "height", imgCfg.Height, "width", imgCfg.Width, "bytes", len(imgBytes))

	// Enrich metadata with the new dimensions.
	metadata["Size"] = strconv.Itoa(len(imgBytes))
	metadata["Height"] = fmt.Sprint(imgCfg.Height)
	metadata["Width"] = fmt.Sprint(imgCfg.Width)

	// Save the resized image to the images container.
	err = h.store.SaveBlob(ctx, imgBytes, ref.path, h.cfg.ImagesContainerName, tags, metadata, evt.Data.ContentType)
	if err != nil {
		return nil, fmt.Errorf("saving resized blob %s: %w", ref.path, err)
	}

	// Copy EXIF sidecar blob from the source (uploads) to images container.
	// The sidecar follows the naming convention: <blobName>.exif.json.
	// Errors are non-fatal — the photo just won't have EXIF data.
	exifSidecarName := ref.path + ".exif.json"
	sidecarBytes, sidecarErr := h.store.GetBlob(ctx, exifSidecarName, ref.container)
	if sidecarErr == nil && len(sidecarBytes) > 0 {
		if err := h.store.SaveBlob(ctx, sidecarBytes, exifSidecarName, h.cfg.ImagesContainerName, nil, nil, "application/json"); err != nil {
			slog.WarnContext(ctx, "failed to copy exif sidecar to images container", "sidecar", exifSidecarName, "error", err)
		} else {
			slog.InfoContext(ctx, "copied exif sidecar to images container", "sidecar", exifSidecarName)
		}
	}

	return nil, nil
}

// parseBlobRef decomposes an Azure Blob Storage URL into its constituent parts.
// Expected URL format: https://<account>.<suffix>/<container>/<collection>/<album>/<file>
func parseBlobRef(rawURL string) (blobRef, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return blobRef{}, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 4 {
		return blobRef{}, fmt.Errorf("URL path %q requires at least 4 segments (container/collection/album/file), got %d", u.Path, len(parts))
	}

	return blobRef{
		container:  parts[0],
		collection: parts[1],
		album:      parts[2],
		path:       strings.Join(parts[1:], "/"),
	}, nil
}

// logEvent writes structured debug information about the incoming event.
func (h *Handler) logEvent(ctx context.Context, evt models.Event, in *common.BindingEvent) {
	slog.DebugContext(ctx, "input binding handler",
		"name", h.cfg.UploadsQueueBinding,
		"subject", evt.Subject,
		"topic", evt.Topic,
		"event_time", evt.EventTime,
		"id", evt.ID,
		"api", evt.Data.API,
		"type", evt.EventType,
		"content_length", evt.Data.ContentLength,
		"content_type", evt.Data.ContentType,
		"etag", evt.Data.ETag,
		"metadata", in.Metadata,
		"url", evt.Data.URL,
	)
}
