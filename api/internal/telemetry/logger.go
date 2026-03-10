package telemetry

import (
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// SetupLogger creates and installs an slog.Logger.
//
// When an OTel LoggerProvider is available the logger fans out to both:
//   - a structured JSON handler on stdout  (for ACA log stream / local dev)
//   - the OTel bridge handler              (for OTLP export to the collector)
//
// When OTel is not configured (local development) the logger falls back
// to a JSON handler on stdout only.
func SetupLogger(serviceName string, providers *Providers) *slog.Logger {
	stdoutHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	})

	var logger *slog.Logger
	if providers != nil && providers.LoggerProvider != nil {
		otelHandler := otelslog.NewHandler(serviceName,
			otelslog.WithLoggerProvider(providers.LoggerProvider),
		)
		logger = slog.New(NewFanoutHandler(stdoutHandler, otelHandler))
	} else {
		logger = slog.New(stdoutHandler)
	}

	slog.SetDefault(logger)
	return logger
}
