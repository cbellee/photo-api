package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
)

// PeopleListHandler returns all persons, ordered by name.
// GET /api/people
func PeopleListHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.PeopleList")
		defer span.End()

		if cfg.FaceStore == nil {
			http.Error(w, "face detection not configured", http.StatusServiceUnavailable)
			return
		}

		persons, err := cfg.FaceStore.GetAllPersons(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "error listing persons", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(persons)
	}
}

// PersonByIDHandler returns a single person by ID.
// GET /api/people/{personID}
func PersonByIDHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.PersonByID")
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

		person, err := cfg.FaceStore.GetPersonByID(ctx, personID)
		if err != nil {
			slog.ErrorContext(ctx, "person not found", "personID", personID, "error", err)
			http.Error(w, "Person not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(person)
	}
}

// PersonPhotosHandler returns photo refs for a given person with pagination.
// GET /api/people/{personID}/photos?offset=0&limit=50
func PersonPhotosHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.PersonPhotos")
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

		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 200 {
			limit = 50
		}

		refs, err := cfg.FaceStore.GetPhotosByPerson(ctx, personID, offset, limit)
		if err != nil {
			slog.ErrorContext(ctx, "error getting photos for person", "personID", personID, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(refs)
	}
}

// SetPersonNameHandler updates a person's display name. Requires auth.
// PUT /api/people/{personID}/name   body: {"name":"..."}
func SetPersonNameHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.SetPersonName")
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

		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			http.Error(w, "request body must include a non-empty \"name\" field", http.StatusBadRequest)
			return
		}

		// Set the name first.
		if err := cfg.FaceStore.SetPersonName(ctx, personID, body.Name); err != nil {
			slog.ErrorContext(ctx, "error setting person name", "personID", personID, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Auto-merge: if another person already has this exact name, merge into them.
		resultPersonID := personID
		merged := false
		existing, err := cfg.FaceStore.FindPersonByName(ctx, body.Name)
		if err == nil && existing.PersonID != personID {
			// Merge the just-named person into the pre-existing one.
			if mergeErr := cfg.FaceStore.MergePeople(ctx, personID, existing.PersonID); mergeErr != nil {
				slog.WarnContext(ctx, "auto-merge failed", "source", personID, "target", existing.PersonID, "error", mergeErr)
			} else {
				slog.InfoContext(ctx, "auto-merged persons by name", "source", personID, "target", existing.PersonID, "name", body.Name)
				resultPersonID = existing.PersonID
				merged = true
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message":  "ok",
			"personID": resultPersonID,
			"name":     body.Name,
			"merged":   merged,
		})
	}
}

// DeletePersonHandler deletes a person and all their faces. Requires auth.
// DELETE /api/people/{personID}
func DeletePersonHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.DeletePerson")
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

		if err := cfg.FaceStore.DeletePerson(ctx, personID); err != nil {
			slog.ErrorContext(ctx, "error deleting person", "personID", personID, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "deleted", "personID": personID})
	}
}

// MergePeopleHandler merges two persons (moves all faces from source to target).
// Requires auth.
// POST /api/people/merge   body: {"sourcePersonID":"...","targetPersonID":"..."}
func MergePeopleHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.MergePeople")
		defer span.End()

		if cfg.FaceStore == nil {
			http.Error(w, "face detection not configured", http.StatusServiceUnavailable)
			return
		}

		var body struct {
			SourcePersonID string `json:"sourcePersonID"`
			TargetPersonID string `json:"targetPersonID"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if body.SourcePersonID == "" || body.TargetPersonID == "" {
			http.Error(w, "sourcePersonID and targetPersonID are required", http.StatusBadRequest)
			return
		}
		if body.SourcePersonID == body.TargetPersonID {
			http.Error(w, "source and target must differ", http.StatusBadRequest)
			return
		}

		if err := cfg.FaceStore.MergePeople(ctx, body.SourcePersonID, body.TargetPersonID); err != nil {
			slog.ErrorContext(ctx, "error merging people", "source", body.SourcePersonID, "target", body.TargetPersonID, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message":        "merged",
			"sourcePersonID": body.SourcePersonID,
			"targetPersonID": body.TargetPersonID,
		})
	}
}

// SearchPeopleHandler searches persons by name prefix.
// GET /api/people/search?q=...
func SearchPeopleHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "handler.SearchPeople")
		defer span.End()

		if cfg.FaceStore == nil {
			http.Error(w, "face detection not configured", http.StatusServiceUnavailable)
			return
		}

		q := r.URL.Query().Get("q")
		persons, err := cfg.FaceStore.SearchPeople(ctx, q)
		if err != nil {
			slog.ErrorContext(ctx, "error searching people", "query", q, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(persons)
	}
}
