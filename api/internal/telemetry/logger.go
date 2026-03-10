package telemetry

import (
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// SetupLogger creates and installs an slog.Logger.
//
// When an OTel LoggerProvider is available the logger uses ONLY the OTel
// bridge handler. Structured JSON is NOT written to stdout in this case
// because Azure Container Apps' managed OpenTelemetry already captures
// container console output and forwards it via OTLP — writing to both
// the OTel bridge and stdout would produce duplicate log records in the
// collector.
//
// When OTel is not configured (local development) the logger falls back
// to a JSON handler on stdout.
func SetupLogger(serviceName string, providers *Providers) *slog.Logger {
	var logger *slog.Logger
	if providers != nil && providers.LoggerProvider != nil {
		otelHandler := otelslog.NewHandler(serviceName,
			otelslog.WithLoggerProvider(providers.LoggerProvider),
		)
		logger = slog.New(otelHandler)
	} else {
		stdoutHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelInfo,
		})
		logger = slog.New(stdoutHandler)
	}

	slog.SetDefault(logger)
	return logger
}
