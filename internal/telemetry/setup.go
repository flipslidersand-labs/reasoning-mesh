// Package telemetry initialises the OpenTelemetry TracerProvider.
//
// Selection logic (checked in order):
//  1. OTEL_EXPORTER_OTLP_ENDPOINT is set → OTLP/HTTP exporter
//  2. otherwise → stdout exporter (human-readable, dev mode)
//
// The returned shutdown function must be deferred in main.
package telemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const ServiceName = "reasoning-mesh"

// Setup initialises the global TracerProvider.
// Returns a shutdown function that must be called before the process exits.
func Setup(ctx context.Context) (shutdown func(context.Context) error, err error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	exp, err := newExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("otel exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

// Tracer returns the package-level tracer for the given instrumentation scope.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

func newExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		return otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(endpoint),
			otlptracehttp.WithInsecure(),
		)
	}
	// stdout exporter: pretty-prints spans to os.Stderr for dev visibility
	return stdouttrace.New(stdouttrace.WithWriter(os.Stderr))
}
