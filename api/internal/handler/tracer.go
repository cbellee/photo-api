package handler

import "go.opentelemetry.io/otel"

// tracer is the shared OpenTelemetry tracer for all handler functions.
var tracer = otel.Tracer("photo-api")
