package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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
