package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/cbellee/photo-api/internal/storage"
	"github.com/cbellee/photo-api/internal/telemetry"
	"github.com/cbellee/photo-api/internal/utils"
	daprd "github.com/dapr/go-sdk/service/grpc"
)

func main() {
	// ── Telemetry ────────────────────────────────────────────────────
	// Read OTel config with os.Getenv (not utils.GetEnvValue) to avoid
	// logging before the fanout logger is installed.
	ctx := context.Background()
	otelCfg := telemetry.Config{
		ServiceName:    "resize-api",
		ServiceVersion: "1.0.0",
		OTLPEndpoint:   envOr("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		EnableTraces:   envOr("OTEL_TRACES_ENABLED", "true") == "true",
		EnableMetrics:  envOr("OTEL_METRICS_ENABLED", "true") == "true",
		EnableLogs:     envOr("OTEL_LOGS_ENABLED", "true") == "true",
	}
	providers, err := telemetry.Init(ctx, otelCfg)
	if err != nil {
		slog.Error("failed to init telemetry", "error", err)
	} else {
		defer providers.Shutdown(ctx)
	}

	// ── Logging (stdout JSON + OTel fan-out) ─────────────────────────
	// Must be set up before any utils.GetEnvValue calls so their
	// slog.Warn messages flow through the OTel bridge.
	telemetry.SetupLogger("resize-api", providers)

	// ── Configuration ───────────────────────────────────────────────
	maxHeight, err := strconv.Atoi(utils.GetEnvValue("MAX_IMAGE_HEIGHT", "1200"))
	if err != nil {
		slog.Error("invalid MAX_IMAGE_HEIGHT", "error", err)
		return
	}
	maxWidth, err := strconv.Atoi(utils.GetEnvValue("MAX_IMAGE_WIDTH", "1600"))
	if err != nil {
		slog.Error("invalid MAX_IMAGE_WIDTH", "error", err)
		return
	}

	storageAccount := utils.GetEnvValue("STORAGE_ACCOUNT_NAME", "")
	storageSuffix := utils.GetEnvValue("STORAGE_ACCOUNT_SUFFIX", "blob.core.windows.net")

	cfg := &Config{
		ServiceName:         utils.GetEnvValue("SERVICE_NAME", ""),
		ServicePort:         utils.GetEnvValue("SERVICE_PORT", ""),
		HealthPort:          utils.GetEnvValue("HEALTH_PORT", "8081"),
		UploadsQueueBinding: utils.GetEnvValue("UPLOADS_QUEUE_BINDING", ""),
		AzureClientID:       utils.GetEnvValue("AZURE_CLIENT_ID", ""),
		ImagesContainerName: utils.GetEnvValue("IMAGES_CONTAINER_NAME", "images"),
		MaxImageHeight:      maxHeight,
		MaxImageWidth:       maxWidth,
		StorageAccount:      storageAccount,
		StorageSuffix:       storageSuffix,
		StorageContainer:    utils.GetEnvValue("STORAGE_CONTAINER_NAME", ""),
	}

	// ── Create blob store ────────────────────────────────────────────
	storageUrl := fmt.Sprintf("https://%s.%s", cfg.StorageAccount, cfg.StorageSuffix)
	store, err := storage.NewBlobStore(storageUrl, cfg.AzureClientID)
	if err != nil {
		slog.Error("error creating blob store", "error", err)
		return
	}

	// ── Create handler ──────────────────────────────────────────────
	h := NewHandler(store, cfg)

	// ── Dapr service ────────────────────────────────────────────────
	port := fmt.Sprintf(":%s", cfg.ServicePort)
	s, err := daprd.NewService(port)
	if err != nil {
		slog.Error("failed to create daprd service", "error", err)
		return
	}

	if err := s.AddBindingInvocationHandler(cfg.UploadsQueueBinding, h.Resize); err != nil {
		slog.Error("error adding binding handler", "error", err)
		return
	}
	slog.Info("added binding handler", "name", cfg.UploadsQueueBinding)

	// ── Health probes (separate HTTP server) ────────────────────────
	// The Dapr gRPC service does not expose HTTP endpoints, so we run
	// a lightweight HTTP server on a separate port for k8s probes.
	healthMux := http.NewServeMux()

	// Liveness probe – returns 200 if the process is running.
	healthMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	// Readiness probe – returns 200 when the service can reach blob storage.
	healthMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		readyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := store.FilterBlobsByTags(readyCtx,
			fmt.Sprintf("@container='%s' and collectionImage='true'", cfg.ImagesContainerName),
			cfg.ImagesContainerName)
		if err != nil {
			slog.Warn("readiness check failed", "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, `{"status":"unavailable"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	healthAddr := fmt.Sprintf(":%s", cfg.HealthPort)
	go func() {
		slog.Info("starting health probe server", "addr", healthAddr)
		if err := http.ListenAndServe(healthAddr, healthMux); err != nil {
			slog.Error("health probe server failed", "error", err)
		}
	}()

	// ── Start ───────────────────────────────────────────────────────
	slog.Info("starting service", "name", cfg.ServiceName, "port", cfg.ServicePort)
	if err := s.Start(); err != nil {
		slog.Error("server failed to start", "error", err)
		return
	}
}

// envOr reads an environment variable without logging (used before
// the fanout logger is installed).
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
