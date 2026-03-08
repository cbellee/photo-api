package telemetry

import (
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// SetupLogger creates and installs an slog.Logger that fans out to both
// JSON stdout and (when providers is non-nil) the OTel log bridge.
// It returns the logger for callers that want to hold a reference.
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
