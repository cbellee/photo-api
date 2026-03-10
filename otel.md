# OpenTelemetry Implementation

This document describes the changes made to add OpenTelemetry (OTel) observability to the `/api` Go backend. The implementation covers **distributed tracing**, **metrics**, and **structured log bridging** across both services: the Photo HTTP API (`cmd/photo`) and the Resize Dapr worker (`cmd/resize`).

---

## Dependencies Added

The following OTel packages were added to `go.mod`:

| Package | Version | Purpose |
|---|---|---|
| `go.opentelemetry.io/otel` | v1.40.0 | Core OTel API (tracer, meter, propagation) |
| `go.opentelemetry.io/otel/sdk` | v1.40.0 | Trace SDK (TracerProvider, span processors) |
| `go.opentelemetry.io/otel/sdk/metric` | v1.40.0 | Metric SDK (MeterProvider, periodic reader) |
| `go.opentelemetry.io/otel/sdk/log` | v0.16.0 | Log SDK (LoggerProvider) |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | v1.21.0 | OTLP/gRPC trace exporter |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc` | v1.40.0 | OTLP/gRPC metric exporter |
| `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc` | v0.16.0 | OTLP/gRPC log exporter |
| `go.opentelemetry.io/otel/semconv/v1.26.0` | (transitive) | Semantic conventions for resource attributes |
| `go.opentelemetry.io/contrib/bridges/otelslog` | v0.15.0 | Bridges `log/slog` → OTel LoggerProvider |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | v0.65.0 | Auto-instruments `net/http` with trace spans |

> Go version was upgraded from 1.22.0 → **1.24.0** because `otelslog` v0.15.0 requires it.

---

## New File: `internal/telemetry/telemetry.go`

Shared bootstrap package used by both services. Key exports:

- **`Config`** — holds `ServiceName`, `ServiceVersion`, and `OTLPEndpoint`.
- **`Providers`** — groups the initialised `TracerProvider`, `MeterProvider`, and `LoggerProvider`.
- **`Init(ctx, cfg) (*Providers, error)`** — creates OTLP/gRPC exporters for traces, metrics, and logs, builds SDK providers with a merged `resource.Resource` (service name + version via semconv), sets them as the global OTel providers, and configures `TraceContext` + `Baggage` propagation.
- **`Providers.Shutdown(ctx)`** — flushes pending telemetry and releases resources.

### Configuration

| Environment Variable | Default | Description |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | gRPC endpoint of the OTLP collector |
| `OTEL_SERVICE_NAME` | set in code | Overridden per-service in `main()` |

---

## Service Entry Point Changes

### `cmd/photo/main.go` (Photo HTTP API)

1. **Telemetry init** — calls `telemetry.Init()` at the top of `main()` with `ServiceName: "photo-api"`. Defers `providers.Shutdown()`.
2. **Logging bridge** — replaced the plain `slog.NewJSONHandler` with `otelslog.NewLogger("photo-api")`, which bridges all `slog` calls to the OTel LoggerProvider. Falls back to the JSON handler if telemetry init fails.
3. **HTTP instrumentation** — wrapped the final `cors → mux` handler chain with `otelhttp.NewHandler(c.Handler(api), "photo-api")`, which automatically creates a root span for every inbound HTTP request and records standard HTTP metrics.

### `cmd/resize/main.go` (Resize Dapr Worker)

1. **Telemetry init** — calls `telemetry.Init()` at the top of `main()` with `ServiceName: "resize-api"`. Defers `providers.Shutdown()`.
2. **Logging bridge** — replaced the plain `slog.NewJSONHandler` with `otelslog.NewLogger("resize-api")`, with the same JSON fallback.

---

## Handler Span Instrumentation

A package-level tracer is declared in `internal/handler/collection.go`:

```go
var tracer = otel.Tracer("photo-api")
```

All handler files in `internal/handler/` share this tracer. Each handler function creates a child span with `tracer.Start()` and defers `span.End()`. Relevant attributes (collection, album, filename, etc.) are attached to spans via `span.SetAttributes()`.

### `internal/handler/collection.go` — `CollectionHandler`

- **Span name:** `handler.Collections`
- Creates a traced `ctx` that is passed to all downstream `store` calls.

### `internal/handler/album.go` — `AlbumHandler`

- **Span name:** `handler.Albums`
- **Attributes:** `collection`

### `internal/handler/photo.go` — `PhotoHandler`

- **Span name:** `handler.Photos`
- **Attributes:** `collection`, `album`

### `internal/handler/upload.go` — `UploadHandler`

- **Span name:** `handler.Upload`
- **Attributes:** `collection`, `album`, `filename`

### `internal/handler/update.go` — `UpdateHandler`

- **Span name:** `handler.Update`
- **Attributes:** `blob.name`

### `internal/handler/tags.go` — `TagListHandler`

- **Span name:** `handler.TagList`

### `internal/handler/middleware.go` — `RequireRole`

- **Span name:** `middleware.RequireRole`
- **Attributes:** `auth.required_role`

---

## Resize Handler Span Instrumentation

A separate package-level tracer is declared in `cmd/resize/handler.go`:

```go
var tracer = otel.Tracer("resize-api")
```

### `cmd/resize/handler.go` — `Handler.Resize`

- **Span name:** `resize.Resize`
- **Attributes:** `blob.container`, `blob.path`, `blob.collection`, `blob.album`
- The enriched `ctx` is propagated to blob download/upload calls.

---

## Trace Hierarchy (Photo API)

For an authenticated request like `PUT /api/update/{collection}/{album}/{id}`:

```
photo-api (otelhttp root span)
  └─ middleware.RequireRole
       └─ handler.Update
            └─ (storage calls use the traced ctx)
```

For a public request like `GET /api`:

```
photo-api (otelhttp root span)
  └─ handler.Collections
       └─ (storage calls use the traced ctx)
```

---

## Deployment Requirements

To collect telemetry in a deployed environment:

1. **Run an OTLP-compatible collector** as a sidecar or standalone service. Options include:
   - Azure Monitor OpenTelemetry Collector (exports to Application Insights)
   - Grafana Alloy / OpenTelemetry Collector (exports to Tempo, Loki, Prometheus)
   - Jaeger with OTLP receiver

2. **Set environment variables** on each container app:
   ```
   OTEL_EXPORTER_OTLP_ENDPOINT=<collector-host>:4317
   ```

3. The services are resilient to collector unavailability — if `telemetry.Init()` fails, logging falls back to the standard JSON handler and no spans are emitted.
