// Package telemetry provides a shared OpenTelemetry bootstrap for all services
// in the photo-api solution. It initialises trace, metric, and log providers
// that export via OTLP/gRPC.
package telemetry

import (
	"context"
	"fmt"
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
// The caller must defer Providers.Shutdown().
func Init(ctx context.Context, cfg Config) (*Providers, error) {
	// ── Resource (shared service identity) ──────────────────────────
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTel resource: %w", err)
	}

	// ── Trace exporter → provider ───────────────────────────────────
	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
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
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// ── Metric exporter → provider ──────────────────────────────────
	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
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

	// ── Log exporter → provider ─────────────────────────────────────
	logExp, err := otlploggrpc.New(ctx,
		otlploggrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlploggrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating log exporter: %w", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)

	return &Providers{
		TracerProvider: tp,
		MeterProvider:  mp,
		LoggerProvider: lp,
	}, nil
}
