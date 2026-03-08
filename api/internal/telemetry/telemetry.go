// Package telemetry provides a shared OpenTelemetry bootstrap for all services
// in the photo-api solution. It initialises trace, metric, and log providers
// that export via OTLP/gRPC.
package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds the minimal configuration needed to set up telemetry.
type Config struct {
	ServiceName    string
	ServiceVersion string
	OTLPEndpoint   string // e.g. "localhost:4317"
	EnableTraces   bool   // when false, traces are not exported
	EnableMetrics  bool   // when false, metrics are not exported
	EnableLogs     bool   // when false, logs are not exported via OTLP
}

// Providers groups the three initialised OTel providers.
type Providers struct {
	TracerProvider *trace.TracerProvider
	MeterProvider  *metric.MeterProvider
	LoggerProvider *sdklog.LoggerProvider
}

// Shutdown flushes pending telemetry and releases resources.
// Pass a context with a deadline to bound the flush time.
func (p *Providers) Shutdown(ctx context.Context) {
	if p.TracerProvider != nil {
		_ = p.TracerProvider.Shutdown(ctx)
	}
	if p.MeterProvider != nil {
		_ = p.MeterProvider.Shutdown(ctx)
	}
	if p.LoggerProvider != nil {
		_ = p.LoggerProvider.Shutdown(ctx)
	}
}

// Init creates OTLP/gRPC exporters and returns fully-wired Providers.
// Individual signals (traces, metrics, logs) are only initialised when
// the corresponding Enable* flag in Config is true.
// The caller must defer Providers.Shutdown().
func Init(ctx context.Context, cfg Config) (*Providers, error) {
	// If nothing is enabled, return nil so callers can skip telemetry.
	if !cfg.EnableTraces && !cfg.EnableMetrics && !cfg.EnableLogs {
		return nil, nil
	}

	// WithEndpoint() expects "host:port", not a URL.  Strip any scheme
	// that may have been injected via OTEL_EXPORTER_OTLP_ENDPOINT.
	endpoint := cfg.OTLPEndpoint
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")

	// ── Resource (shared service identity) ──────────────────────────
	// Avoid resource.Default() and the With*() detectors – they embed the
	// SDK's built-in semconv schema URL (v1.39.0) which conflicts with our
	// explicitly imported semconv/v1.26.0.  Supply only plain attributes so
	// there is a single, consistent schema URL on the resource.
	res := resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
	)

	p := &Providers{}

	// ── Trace exporter → provider ───────────────────────────────────
	if cfg.EnableTraces {
		traceExp, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, fmt.Errorf("creating trace exporter: %w", err)
		}
		tp := trace.NewTracerProvider(
			trace.WithBatcher(traceExp, trace.WithBatchTimeout(5*time.Second)),
			trace.WithResource(res),
		)
		otel.SetTracerProvider(tp)
		p.TracerProvider = tp
	}

	// Always set the propagator so trace context is forwarded even when
	// local trace export is disabled.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// ── Metric exporter → provider ──────────────────────────────────
	if cfg.EnableMetrics {
		metricExp, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(endpoint),
			otlpmetricgrpc.WithInsecure(),
		)
		if err != nil {
			return nil, fmt.Errorf("creating metric exporter: %w", err)
		}
		mp := metric.NewMeterProvider(
			metric.WithReader(metric.NewPeriodicReader(metricExp, metric.WithInterval(30*time.Second))),
			metric.WithResource(res),
		)
		otel.SetMeterProvider(mp)
		p.MeterProvider = mp
	}

	// ── Log exporter → provider ─────────────────────────────────────
	if cfg.EnableLogs {
		logExp, err := otlploggrpc.New(ctx,
			otlploggrpc.WithEndpoint(endpoint),
			otlploggrpc.WithInsecure(),
		)
		if err != nil {
			return nil, fmt.Errorf("creating log exporter: %w", err)
		}
		lp := sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
			sdklog.WithResource(res),
		)
		p.LoggerProvider = lp
	}

	return p, nil
}
