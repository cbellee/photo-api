package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/storage"
)

// TagListHandler returns all collection→album tag mappings.
func TagListHandler(store storage.BlobStore, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.TagList")
		defer span.End()

		blobTagList, err := store.GetBlobTagList(ctx, cfg.ImagesContainerName)
		if err != nil {
			slog.ErrorContext(ctx, "error getting blob tag list", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		slog.DebugContext(ctx, "blob tag map", "value", blobTagList)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blobTagList)
	}
}
