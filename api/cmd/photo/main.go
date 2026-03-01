package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cbellee/photo-api/internal/handler"
	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
	"github.com/cbellee/photo-api/internal/telemetry"
	"github.com/cbellee/photo-api/internal/utils"
	"github.com/rs/cors"

	azlog "github.com/Azure/azure-sdk-for-go/sdk/azcore/log"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/MicahParks/keyfunc/v3"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	// ── Telemetry ────────────────────────────────────────────────────
	ctx := context.Background()
	otelCfg := telemetry.Config{
		ServiceName:    "photo-api",
		ServiceVersion: "1.0.0",
		OTLPEndpoint:   utils.GetEnvValue("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
	}
	providers, err := telemetry.Init(ctx, otelCfg)
	if err != nil {
		slog.Error("failed to init telemetry", "error", err)
	}

	// ── Configuration ───────────────────────────────────────────────
	// EMULATED_STORAGE_URL overrides the computed Azure storage URL (used with the blob emulator).
	storageUrl := utils.GetEnvValue("EMULATED_STORAGE_URL", "")
	if storageUrl == "" {
		storageAccount := utils.GetEnvValue("STORAGE_ACCOUNT_NAME", "stor6aq2g56sfcosi")
		storageAccountSuffix := utils.GetEnvValue("STORAGE_ACCOUNT_SUFFIX", "blob.core.windows.net")
		storageUrl = fmt.Sprintf("https://%s.%s", storageAccount, storageAccountSuffix)
	}
	azureClientId := utils.GetEnvValue("AZURE_CLIENT_ID", "")

	cfg := &handler.Config{
		ServiceName:          utils.GetEnvValue("SERVICE_NAME", "photoService"),
		ServicePort:          utils.GetEnvValue("SERVICE_PORT", "8080"),
		UploadsContainerName: utils.GetEnvValue("UPLOADS_CONTAINER_NAME", "uploads"),
		ImagesContainerName:  utils.GetEnvValue("IMAGES_CONTAINER_NAME", "images"),
		StorageUrl:           storageUrl,
		MemoryLimitMb:        32,
		JwksURL:              utils.GetEnvValue("JWKS_URL", "https://0cd02bb5-3c24-4f77-8b19-99223d65aa67.ciamlogin.com/0cd02bb5-3c24-4f77-8b19-99223d65aa67/discovery/v2.0/keys?appid=689078c3-c0ad-4c10-a0d3-1c430c2e471d"),
		RoleName:             utils.GetEnvValue("ROLE_NAME", "photo.upload"),
		CorsOrigins:          strings.Split(utils.GetEnvValue("CORS_ORIGINS", "http://localhost:5173,https://photo-dev.bellee.net,https://photo.bellee.net"), ","),
	}

	// ── JWKS keyfunc (cached, refreshed in background) ─────────────
	jwksCtx, jwksCancel := context.WithCancel(context.Background())
	k, err := keyfunc.NewDefaultCtx(jwksCtx, []string{cfg.JwksURL})
	if err != nil {
		slog.Warn("failed to create JWKS keyfunc, JWT verification will fall back to per-request fetch", "error", err)
	} else {
		cfg.JWTKeyfunc = k.Keyfunc
	}

	// ── Logging (bridged to OTel) ────────────────────────────────────
	var logger *slog.Logger
	if providers != nil {
		logger = otelslog.NewLogger("photo-api",
			otelslog.WithLoggerProvider(providers.LoggerProvider),
		)
	} else {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelInfo,
		}))
	}
	slog.SetDefault(logger)
	slog.Info("cors origins", "origins", cfg.CorsOrigins)
	slog.Info("storage url", "url", storageUrl)

	// Enable Azure SDK logging (identity events only).
	azlog.SetListener(func(event azlog.Event, s string) {
		slog.Info("azlog", "event", event, "message", s)
	})
	azlog.SetEvents(azidentity.EventAuthentication)

	// ── Create blob store ───────────────────────────────────────────
	var store storage.BlobStore
	if blobEmuURL := utils.GetEnvValue("BLOB_EMULATOR_URL", ""); blobEmuURL != "" {
		slog.Info("using local blob emulator", "url", blobEmuURL)
		store = storage.NewLocalBlobStore(blobEmuURL, storageUrl)
	} else {
		isProduction := false
		if _, exists := os.LookupEnv("CONTAINER_APP_NAME"); exists {
			isProduction = true
		} else {
			slog.Info("'CONTAINER_APP_NAME' env var not found, running in local environment")
		}

		blobClient, err := utils.CreateAzureBlobClient(storageUrl, isProduction, azureClientId)
		if err != nil {
			slog.Error("error creating blob client", "error", err)
			return
		}
		store = storage.NewAzureBlobStore(blobClient, storageUrl)
	}

	// ── Routes ──────────────────────────────────────────────────────
	port := fmt.Sprintf(":%s", cfg.ServicePort)
	api := http.NewServeMux()

	// Liveness probe – returns 200 if the process is running.
	api.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	// Readiness probe – returns 200 when the service can handle traffic.
	// Uses a short-lived context and a lightweight single-blob check rather
	// than listing every blob in the container.
	api.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		readyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Attempt a cheap tag-filter query that returns at most one row.
		_, err := store.FilterBlobsByTags(readyCtx,
			fmt.Sprintf("@container='%s'", cfg.ImagesContainerName),
			cfg.ImagesContainerName)
		if err != nil {
			slog.Warn("readiness check failed", "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"unavailable","error":%q}`+"\n", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	api.HandleFunc("GET /api", handler.CollectionHandler(store, cfg))
	api.HandleFunc("GET /api/{collection}", handler.AlbumHandler(store, cfg))
	api.HandleFunc("GET /api/{collection}/{album}", handler.PhotoHandler(store, cfg))
	api.HandleFunc("POST /api/upload", handler.RequireRole(cfg, handler.UploadHandler(store, cfg)))
	api.HandleFunc("PUT /api/update/{collection}/{album}/{id}", handler.RequireRole(cfg, handler.UpdateHandler(store, cfg)))
	api.HandleFunc("GET /api/tags", handler.TagListHandler(store, cfg))

	slog.Info("server listening", "name", cfg.ServiceName, "port", port)

	c := cors.New(cors.Options{
		AllowedOrigins:   cfg.CorsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD"},
		AllowedHeaders:   []string{"Origin", "X-Requested-With", "Content-Type", "Accept", "Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	})

	otelHandler := otelhttp.NewHandler(c.Handler(api), "photo-api")

	srv := &http.Server{
		Addr:              port,
		Handler:           otelHandler,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown: listen for SIGINT/SIGTERM, then drain connections.
	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-shutdownCtx.Done()
	slog.Info("shutting down server")

	drainCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	// Flush telemetry before exit.
	if providers != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer flushCancel()
		providers.Shutdown(flushCtx)
	}

	// Stop JWKS background refresh.
	jwksCancel()

	slog.Info("server stopped")

	// Keep models import used (StorageConfig is referenced in config for backwards compat).
	_ = models.StorageConfig{}
}
