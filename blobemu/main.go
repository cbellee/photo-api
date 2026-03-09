// Package main implements a lightweight blob-storage emulator with
// automatic tag indexing, designed to replace Azure Blob Storage for
// local Docker / Kubernetes development.
//
// REST API
//
//	POST  /query                         Filter blobs by tag query
//	GET   /{container}                    List blobs in a container
//	GET   /{container}/{blob...}          Download blob (or ?comp=tags / ?comp=metadata)
//	PUT   /{container}/{blob...}          Upload blob  (or ?comp=tags to set tags)
package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	dataDir := env("DATA_DIR", "/data")
	port := env("PORT", "10000")

	store, err := NewStore(dataDir)
	if err != nil {
		slog.Error("failed to initialise store", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// ── RabbitMQ publisher (optional) ────────────────────────────────
	var pub *Publisher
	if amqpURL := env("RABBITMQ_URL", ""); amqpURL != "" {
		pubCfg := &PublisherConfig{
			URL:        amqpURL,
			Exchange:   env("RABBITMQ_EXCHANGE", "blob-events"),
			RoutingKey: env("RABBITMQ_ROUTING_KEY", "blob.created"),
			QueueName:  env("RABBITMQ_QUEUE", "blob-events"),
			BaseURL:    env("BLOB_PUBLIC_URL", "http://blobemu:10000"),
		}
		pub, err = NewPublisher(pubCfg)
		if err != nil {
			slog.Error("failed to connect to RabbitMQ", "error", err)
			os.Exit(1)
		}
		defer pub.Close()
	} else {
		slog.Info("RABBITMQ_URL not set - event publishing disabled")
	}

	mux := http.NewServeMux()

	// Maximum upload size in MB (default 100).
	maxBodyMB, err := strconv.ParseInt(env("MAX_BODY_SIZE_MB", "100"), 10, 64)
	if err != nil || maxBodyMB <= 0 {
		maxBodyMB = 100
	}
	maxBodySize := maxBodyMB << 20

	// Allowed blob content types.
	allowedContentTypes := map[string]bool{
		"image/jpeg":               true,
		"image/png":                true,
		"image/gif":                true,
		"image/webp":               true,
		"application/octet-stream": true,
	}

	// CORS origins from env (default: restrictive for local dev).
	corsOrigins := env("CORS_ORIGINS", "http://localhost:5173,http://localhost:8080")

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /query", queryHandler(store))
	mux.HandleFunc("GET /{container}", listHandler(store))
	mux.HandleFunc("GET /{container}/{blob...}", blobGetHandler(store))
	publishContainer := env("PUBLISH_CONTAINER", "uploads")
	mux.HandleFunc("PUT /{container}/{blob...}", blobPutHandler(store, pub, publishContainer, maxBodySize, allowedContentTypes))
	mux.HandleFunc("DELETE /{container}/{blob...}", blobDeleteHandler(store))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           corsHandler(mux, corsOrigins),
		MaxHeaderBytes:    1 << 20, // 1 MB (metadata can be large)
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown: listen for SIGINT/SIGTERM, then drain connections.
	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("blobemu listening", "port", port, "dataDir", dataDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-shutdownCtx.Done()
	slog.Info("shutting down blobemu")

	drainCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
	slog.Info("blobemu stopped")
}

// ---------- handlers ----------

// validateBlobPath checks that a blob path is safe (no directory traversal).
func validateBlobPath(blob string) bool {
	// Reject empty paths and paths containing traversal sequences.
	if blob == "" {
		return false
	}
	cleaned := filepath.Clean(blob)
	// Reject if Clean produced a path starting with .. or an absolute path.
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return false
	}
	// Also reject if any segment is literally "..".
	for _, seg := range strings.Split(cleaned, string(filepath.Separator)) {
		if seg == ".." {
			return false
		}
	}
	return true
}

func queryHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Limit query body to 64 KB.
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}

		blobs, err := store.FilterByTags(req.Query)
		if err != nil {
			slog.Error("filter error", "query", req.Query, "error", err)
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		slog.Debug("query", "query", req.Query, "results", len(blobs))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blobs)
	}
}

func listHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		container := r.PathValue("container")
		if !validateBlobPath(container) {
			http.Error(w, "invalid container name", http.StatusBadRequest)
			return
		}

		blobs, err := store.ListBlobs(container)
		if err != nil {
			slog.Error("list error", "container", container, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blobs)
	}
}

func blobGetHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		container := r.PathValue("container")
		blob := r.PathValue("blob")
		if !validateBlobPath(container) || !validateBlobPath(blob) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		comp := r.URL.Query().Get("comp")
		slog.Info("GET blob", "container", container, "blob", blob, "comp", comp, "rawPath", r.URL.RawPath)

		switch comp {
		case "tags":
			tags, err := store.GetTags(container, blob)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tags)

		case "metadata":
			md, err := store.GetMetadata(container, blob)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(md)

		default:
			data, ct, err := store.GetBlob(container, blob)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", ct)
			w.Write(data)
		}
	}
}

func blobPutHandler(store *Store, pub *Publisher, publishContainer string, maxBodySize int64, allowedCT map[string]bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		container := r.PathValue("container")
		blob := r.PathValue("blob")
		if !validateBlobPath(container) || !validateBlobPath(blob) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		comp := r.URL.Query().Get("comp")
		slog.Info("PUT blob", "container", container, "blob", blob, "comp", comp, "rawPath", r.URL.RawPath)

		switch comp {
		case "tags":
			var tags map[string]string
			if err := json.NewDecoder(r.Body).Decode(&tags); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			if err := store.SetTags(container, blob, tags); err != nil {
				slog.Error("set tags error", "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)

		default:
			// Enforce body size limit.
			r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "error reading body", http.StatusBadRequest)
				return
			}

			ct := r.Header.Get("Content-Type")
			if ct == "" {
				ct = "application/octet-stream"
			}

			// Validate content type.
			if !allowedCT[ct] {
				http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
				return
			}

			var tags map[string]string
			if h := r.Header.Get("X-Blob-Tags"); h != "" {
				json.NewDecoder(strings.NewReader(h)).Decode(&tags)
			}

			var metadata map[string]string
			if h := r.Header.Get("X-Blob-Metadata"); h != "" {
				json.NewDecoder(strings.NewReader(h)).Decode(&metadata)
			}

			if err := store.SaveBlob(container, blob, data, tags, metadata, ct); err != nil {
				slog.Error("save error", "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Publish a BlobCreated event to RabbitMQ only for the watched
			// container to avoid an infinite loop (resize writes to images).
			if pub != nil && container == publishContainer {
				if err := pub.PublishBlobCreated(container, blob, ct, len(data)); err != nil {
					slog.Error("failed to publish blob event", "container", container, "blob", blob, "error", err)
					// Non-fatal: the blob is saved, just the event failed.
				}
			}

			w.WriteHeader(http.StatusCreated)
		}
	}
}

func blobDeleteHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		container := r.PathValue("container")
		blob := r.PathValue("blob")
		if !validateBlobPath(container) || !validateBlobPath(blob) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		if err := store.DeleteBlob(container, blob); err != nil {
			slog.Error("delete blob error", "container", container, "blob", blob, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------- helpers ----------

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// corsHandler adds CORS headers scoped to the configured origins.
func corsHandler(next http.Handler, allowedCSV string) http.Handler {
	allowed := make(map[string]bool)
	for _, o := range strings.Split(allowedCSV, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			allowed[o] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Blob-Tags, X-Blob-Metadata")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Type, Content-Length")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
