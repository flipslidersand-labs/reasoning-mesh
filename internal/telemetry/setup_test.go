package telemetry_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"

	"github.com/flipslidersand/reasoning-mesh/internal/telemetry"
)

func TestSetup_NoOTLPEndpoint(t *testing.T) {
	// Without OTEL_EXPORTER_OTLP_ENDPOINT, should use stdout exporter and not error.
	ctx := context.Background()
	shutdown, err := telemetry.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup() error: %v", err)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	}()

	// Tracer should be callable after setup.
	tracer := telemetry.Tracer("test")
	_, span := tracer.Start(ctx, "smoke-test")
	span.End()
}

func TestTracer_GlobalProviderIsSet(t *testing.T) {
	ctx := context.Background()
	shutdown, err := telemetry.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup() error: %v", err)
	}
	defer shutdown(ctx) //nolint:errcheck

	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Error("global TracerProvider should be non-nil after Setup")
	}
}
