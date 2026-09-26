package observability

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

func sampleRatioFromEnv() float64 {
	raw := os.Getenv("OTEL_TRACE_SAMPLE_RATIO")
	if raw == "" {
		return 0.1
	}
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil || ratio < 0 || ratio > 1 {
		log.Printf("invalid OTEL_TRACE_SAMPLE_RATIO=%q, using default 0.1", raw)
		return 0.1
	}
	return ratio
}

func otlpEndpointFromEnv() string {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		return "localhost:4317"
	}
	return endpoint
}

func NewTracerProvider() *sdktrace.TracerProvider {
	return InitJaeger("observastack")
}

func InitJaeger(serviceName string) *sdktrace.TracerProvider {
	ctx := context.Background()

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(otlpEndpointFromEnv()),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		log.Printf("failed to create OTLP exporter: %v", err)
		return nil
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(sampleRatioFromEnv())),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		)),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)

	return tp
}

func TraceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.SpanContext().IsValid() {
		return ""
	}
	return span.SpanContext().TraceID().String()
}

func TracerMiddleware(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http.request")
}
