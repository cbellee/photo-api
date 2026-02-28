package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/cbellee/photo-api/internal/storage"
	"github.com/cbellee/photo-api/internal/telemetry"
	"github.com/cbellee/photo-api/internal/utils"
	daprd "github.com/dapr/go-sdk/service/grpc"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

func main() {
	// ── Telemetry ────────────────────────────────────────────────────
	ctx := context.Background()
	otelCfg := telemetry.Config{
		ServiceName:    "resize-service",
		ServiceVersion: "1.0.0",
		OTLPEndpoint:   utils.GetEnvValue("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
	}
	providers, err := telemetry.Init(ctx, otelCfg)
	if err != nil {
		slog.Error("failed to init telemetry", "error", err)
	} else {
		defer providers.Shutdown(ctx)
	}

	// ── Logging (bridged to OTel) ────────────────────────────────────
	var logger *slog.Logger
	if providers != nil {
		logger = otelslog.NewLogger("resize-service",
			otelslog.WithLoggerProvider(providers.LoggerProvider),
		)
	} else {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelInfo,
		}))
	}
	slog.SetDefault(logger)

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
	var store storage.BlobStore
	var storageUrl string

	if emuURL := utils.GetEnvValue("EMULATED_STORAGE_URL", ""); emuURL != "" {
		// Local / emulator mode: use plain HTTP client to talk to blobemu.
		slog.Info("using local blob emulator", "url", emuURL)
		store = storage.NewLocalBlobStore(emuURL)
		storageUrl = emuURL
	} else {
		storageUrl = fmt.Sprintf("https://%s.%s", cfg.StorageAccount, cfg.StorageSuffix)
		slog.Info("storage url", "url", storageUrl)

		isProduction := false
		if _, exists := os.LookupEnv("CONTAINER_APP_NAME"); exists {
			isProduction = true
		} else {
			slog.Info("'CONTAINER_APP_NAME' env var not found, running in local environment")
		}

		blobClient, blobErr := utils.CreateAzureBlobClient(storageUrl, isProduction, cfg.AzureClientID)
		if blobErr != nil {
			slog.Error("error creating blob client", "error", blobErr)
			return
		}
		store = storage.NewAzureBlobStore(blobClient)
	}

	// ── Create handler ──────────────────────────────────────────────
	h := NewHandler(store, storageUrl, cfg)

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

	// ── Start ───────────────────────────────────────────────────────
	slog.Info("starting service", "name", cfg.ServiceName, "port", cfg.ServicePort)
	if err := s.Start(); err != nil {
		slog.Error("server failed to start", "error", err)
		return
	}
}
