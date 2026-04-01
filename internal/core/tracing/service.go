package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/MrEthical07/superapi/internal/core/config"
)

// Service owns tracer construction and shutdown for the process.
type Service struct {
	enabled    bool
	tracer     trace.Tracer
	shutdownFn func(context.Context) error
}

// New configures OpenTelemetry tracing and installs global providers.
func New(ctx context.Context, cfg config.TracingConfig, env string) (*Service, error) {
	if !cfg.Enabled {
		return &Service{enabled: false}, nil
	}

	if cfg.Exporter != "otlpgrpc" {
		return nil, fmt.Errorf("unsupported tracing exporter: %s", cfg.Exporter)
	}

	exportOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
	}
	if cfg.Insecure {
		exportOpts = append(exportOpts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, exportOpts...)
	if err != nil {
		return nil, fmt.Errorf("init otlpgrpc exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			attribute.String("deployment.environment", env),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("init tracing resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(selectSampler(cfg)),
	)

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagator)

	return NewWithProvider(tp, tp.Shutdown), nil
}

// NewWithProvider wraps an existing tracer provider in Service API.
func NewWithProvider(provider trace.TracerProvider, shutdownFn func(context.Context) error) *Service {
	if provider == nil {
		return &Service{}
	}
	return &Service{
		enabled:    true,
		tracer:     provider.Tracer("superapi/http"),
		shutdownFn: shutdownFn,
	}
}

// Enabled reports whether tracing is active.
func (s *Service) Enabled() bool {
	return s != nil && s.enabled && s.tracer != nil
}

// Tracer returns the HTTP tracer used by middleware and handlers.
func (s *Service) Tracer() trace.Tracer {
	if s == nil {
		return otel.Tracer("superapi/http")
	}
	if s.tracer == nil {
		return otel.Tracer("superapi/http")
	}
	return s.tracer
}

// Shutdown flushes telemetry and closes exporter resources.
func (s *Service) Shutdown(ctx context.Context) error {
	if s == nil || s.shutdownFn == nil {
		return nil
	}
	return s.shutdownFn(ctx)
}

func selectSampler(cfg config.TracingConfig) sdktrace.Sampler {
	switch cfg.Sampler {
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(cfg.SampleRatio)
	default:
		return sdktrace.TraceIDRatioBased(0.05)
	}
}
