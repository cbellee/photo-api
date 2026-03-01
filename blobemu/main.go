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
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
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

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /query", queryHandler(store))
	mux.HandleFunc("GET /{container}", listHandler(store))
	mux.HandleFunc("GET /{container}/{blob...}", blobGetHandler(store))
	publishContainer := env("PUBLISH_CONTAINER", "uploads")
	mux.HandleFunc("PUT /{container}/{blob...}", blobPutHandler(store, pub, publishContainer))

	srv := &http.Server{
		Addr:           ":" + port,
		Handler:        cors(mux),
		MaxHeaderBytes: 1 << 20, // 1 MB (metadata can be large)
	}

	slog.Info("blobemu listening", "port", port, "dataDir", dataDir)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

// ---------- handlers ----------

func queryHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		comp := r.URL.Query().Get("comp")

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

func blobPutHandler(store *Store, pub *Publisher, publishContainer string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		container := r.PathValue("container")
		blob := r.PathValue("blob")
		comp := r.URL.Query().Get("comp")

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
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "error reading body", http.StatusBadRequest)
				return
			}

			ct := r.Header.Get("Content-Type")
			if ct == "" {
				ct = "application/octet-stream"
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

// ---------- helpers ----------

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// cors adds permissive CORS headers for local development.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Type, Content-Length")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
