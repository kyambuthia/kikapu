package obs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.25.0"
	otel_trace "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/exp/slog"
)

type Tracer interface {
	Start(ctx context.Context, name string, opts ...otel_trace.SpanStartOption) (context.Context, otel_trace.Span)
}

type TracerFactory struct {
	mu        sync.RWMutex
	tracer    otel_trace.Tracer
	initOnce  sync.Once
	initialized bool
}

var globalTracer = &TracerFactory{}

func initTracer() {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()

	if globalTracer.initialized {
		return
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		globalTracer.tracer = noop.NewTracerProvider().Tracer("kikapu")
		globalTracer.initialized = true
		return
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("kikapu"),
	)

	otlpConn, err := otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		slog.Error("failed to create OTLP connection", "error", err)
		globalTracer.tracer = noop.NewTracerProvider().Tracer("kikapu")
		globalTracer.initialized = true
		return
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(otlpConn),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	globalTracer.tracer = tp.Tracer("kikapu")
	globalTracer.initialized = true
}

func GetTracer() Tracer {
	globalTracer.initOnce.Do(initTracer)

	globalTracer.mu.RLock()
	defer globalTracer.mu.RUnlock()

	return &tracerAdapter{globalTracer.tracer}
}

type tracerAdapter struct {
	otel_tracer otel_trace.Tracer
}

func (t *tracerAdapter) Start(ctx context.Context, name string, opts ...otel_trace.SpanStartOption) (context.Context, otel_trace.Span) {
	return t.otel_tracer.Start(ctx, name, opts...)
}
