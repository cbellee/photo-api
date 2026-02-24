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
		ctx := r.Context()

		blobTagList, err := store.GetBlobTagList(ctx, cfg.ImagesContainerName, cfg.StorageUrl)
		if err != nil {
			slog.Error("error getting blob tag list", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		slog.Debug("blob tag map", "value", blobTagList)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blobTagList)
	}
}
