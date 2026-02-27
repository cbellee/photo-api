package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/cbellee/photo-api/internal/handler"
	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
	"github.com/cbellee/photo-api/internal/telemetry"
	"github.com/cbellee/photo-api/internal/utils"
	"github.com/rs/cors"

	azlog "github.com/Azure/azure-sdk-for-go/sdk/azcore/log"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
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
	} else {
		defer providers.Shutdown(ctx)
	}

	// ── Configuration ───────────────────────────────────────────────
	storageAccount := utils.GetEnvValue("STORAGE_ACCOUNT_NAME", "stor6aq2g56sfcosi")
	storageAccountSuffix := utils.GetEnvValue("STORAGE_ACCOUNT_SUFFIX", "blob.core.windows.net")
	storageUrl := fmt.Sprintf("https://%s.%s", storageAccount, storageAccountSuffix)
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
	slog.Info("current storage account", "name", storageAccount)
	slog.Info("storage url", "url", storageUrl)

	// Enable Azure SDK logging (identity events only).
	azlog.SetListener(func(event azlog.Event, s string) {
		slog.Info("azlog", "event", event, "message", s)
	})
	azlog.SetEvents(azidentity.EventAuthentication)

	// ── Detect environment ──────────────────────────────────────────
	isProduction := false
	if _, exists := os.LookupEnv("CONTAINER_APP_NAME"); exists {
		isProduction = true
	} else {
		slog.Info("'CONTAINER_APP_NAME' env var not found, running in local environment")
	}

	// ── Create blob client & store ──────────────────────────────────
	blobClient, err := utils.CreateAzureBlobClient(storageUrl, isProduction, azureClientId)
	if err != nil {
		slog.Error("error creating blob client", "error", err)
		return
	}
	store := storage.NewAzureBlobStore(blobClient)

	// ── Routes ──────────────────────────────────────────────────────
	port := fmt.Sprintf(":%s", cfg.ServicePort)
	api := http.NewServeMux()

	api.HandleFunc("GET /api", handler.CollectionHandler(store, cfg))
	api.HandleFunc("GET /api/{collection}", handler.AlbumHandler(store, cfg))
	api.HandleFunc("GET /api/{collection}/{album}", handler.PhotoHandler(store, cfg))
	api.HandleFunc("POST /api/upload", handler.RequireRole(cfg.RoleName, cfg.JwksURL, handler.UploadHandler(store, cfg)))
	api.HandleFunc("PUT /api/update/{collection}/{album}/{id}", handler.RequireRole(cfg.RoleName, cfg.JwksURL, handler.UpdateHandler(store, cfg)))
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
	log.Fatal(http.ListenAndServe(port, otelHandler))

	// Keep models import used (StorageConfig is referenced in config for backwards compat).
	_ = models.StorageConfig{}
}
