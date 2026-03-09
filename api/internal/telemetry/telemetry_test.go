package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviders_Shutdown_NilProviders(t *testing.T) {
	// All three providers are nil — Shutdown should not panic.
	p := &Providers{}

	assert.NotPanics(t, func() {
		p.Shutdown(context.Background())
	})
}

func TestProviders_Shutdown_PartialNilProviders(t *testing.T) {
	// Only TracerProvider is nil, others not set.
	p := &Providers{
		TracerProvider: nil,
		MeterProvider:  nil,
		LoggerProvider: nil,
	}

	assert.NotPanics(t, func() {
		p.Shutdown(context.Background())
	})
}

func TestInit_AllDisabled_ReturnsNil(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-svc",
		ServiceVersion: "0.0.1",
		OTLPEndpoint:   "localhost:4317",
		EnableTraces:   false,
		EnableMetrics:  false,
		EnableLogs:     false,
	}
	p, err := Init(context.Background(), cfg)
	require.NoError(t, err)
	assert.Nil(t, p, "expected nil Providers when all signals are disabled")
}

func TestInit_OnlyTracesEnabled(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-svc",
		ServiceVersion: "0.0.1",
		OTLPEndpoint:   "localhost:4317",
		EnableTraces:   true,
		EnableMetrics:  false,
		EnableLogs:     false,
	}
	p, err := Init(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, p)
	defer p.Shutdown(context.Background())

	assert.NotNil(t, p.TracerProvider, "TracerProvider should be set")
	assert.Nil(t, p.MeterProvider, "MeterProvider should be nil")
	assert.Nil(t, p.LoggerProvider, "LoggerProvider should be nil")
}

func TestInit_OnlyMetricsEnabled(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-svc",
		ServiceVersion: "0.0.1",
		OTLPEndpoint:   "localhost:4317",
		EnableTraces:   false,
		EnableMetrics:  true,
		EnableLogs:     false,
	}
	p, err := Init(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, p)
	defer p.Shutdown(context.Background())

	assert.Nil(t, p.TracerProvider, "TracerProvider should be nil")
	assert.NotNil(t, p.MeterProvider, "MeterProvider should be set")
	assert.Nil(t, p.LoggerProvider, "LoggerProvider should be nil")
}

func TestInit_OnlyLogsEnabled(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-svc",
		ServiceVersion: "0.0.1",
		OTLPEndpoint:   "localhost:4317",
		EnableTraces:   false,
		EnableMetrics:  false,
		EnableLogs:     true,
	}
	p, err := Init(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, p)
	defer p.Shutdown(context.Background())

	assert.Nil(t, p.TracerProvider, "TracerProvider should be nil")
	assert.Nil(t, p.MeterProvider, "MeterProvider should be nil")
	assert.NotNil(t, p.LoggerProvider, "LoggerProvider should be set")
}

func TestInit_AllEnabled(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-svc",
		ServiceVersion: "0.0.1",
		OTLPEndpoint:   "localhost:4317",
		EnableTraces:   true,
		EnableMetrics:  true,
		EnableLogs:     true,
	}
	p, err := Init(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, p)
	defer p.Shutdown(context.Background())

	assert.NotNil(t, p.TracerProvider, "TracerProvider should be set")
	assert.NotNil(t, p.MeterProvider, "MeterProvider should be set")
	assert.NotNil(t, p.LoggerProvider, "LoggerProvider should be set")
}

func TestInit_StripsSchemeFromEndpoint(t *testing.T) {
	// The exporter creation should succeed even if a URL scheme is passed.
	cfg := Config{
		ServiceName:    "test-svc",
		ServiceVersion: "0.0.1",
		OTLPEndpoint:   "http://localhost:4317",
		EnableTraces:   true,
		EnableMetrics:  false,
		EnableLogs:     false,
	}
	p, err := Init(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, p)
	defer p.Shutdown(context.Background())

	assert.NotNil(t, p.TracerProvider)
}
