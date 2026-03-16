package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/cbellee/photo-api/internal/facestore"
)

// FaceOverlaysHandler returns the face overlays for a specific photo.
// GET /api/faces/photo/{collection}/{album}/{name}
func FaceOverlaysHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.FaceOverlays")
		defer span.End()

		if cfg.FaceStore == nil {
			http.Error(w, "face detection not configured", http.StatusServiceUnavailable)
			return
		}

		collection := r.PathValue("collection")
		album := r.PathValue("album")
		name := r.PathValue("name")
		if collection == "" || album == "" || name == "" {
			http.Error(w, "collection, album, and name path parameters are required", http.StatusBadRequest)
			return
		}

		ref := facestore.PhotoRef{
			Collection: collection,
			Album:      album,
			Name:       name,
		}

		overlays, err := cfg.FaceStore.GetFaceOverlaysForPhoto(ctx, ref)
		if err != nil {
			slog.ErrorContext(ctx, "error getting face overlays", "photo", ref.Key(), "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(overlays)
	}
}

// FacesByPersonHandler returns all faces belonging to a person.
// GET /api/faces/person/{personID}
func FacesByPersonHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.FacesByPerson")
		defer span.End()

		if cfg.FaceStore == nil {
			http.Error(w, "face detection not configured", http.StatusServiceUnavailable)
			return
		}

		personID := r.PathValue("personID")
		if personID == "" {
			http.Error(w, "personID is required", http.StatusBadRequest)
			return
		}

		faces, err := cfg.FaceStore.GetFacesByPerson(ctx, personID)
		if err != nil {
			slog.ErrorContext(ctx, "error getting faces for person", "personID", personID, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(faces)
	}
}
