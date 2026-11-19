package trace

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Config controls trace export. An empty Address disables tracing.
type Config struct {
	Address     string
	Insecure    bool
	Headers     map[string]string
	Attributes  []string
	SampleRatio float64
}

// InitTracer installs an OTLP/gRPC trace provider and W3C trace-context propagation.
// The returned closer flushes pending spans before shutting down the provider.
func InitTracer(ctx context.Context, serviceName, serviceVersion string, cfg Config) (func(), error) {
	if cfg.Address == "" {
		return func() {}, nil
	}
	if math.IsNaN(cfg.SampleRatio) || cfg.SampleRatio < 0 || cfg.SampleRatio > 1 {
		return nil, errors.New("OTLP sample ratio must be between 0 and 1")
	}
	attrs, err := resourceAttributes(cfg.Attributes)
	if err != nil {
		return nil, err
	}
	// Controller identity takes precedence over configured resource attributes.
	attrs = append(attrs, attribute.String("service.name", serviceName))
	if serviceVersion != "" {
		attrs = append(attrs, attribute.String("service.version", serviceVersion))
	}
	res := resource.NewSchemaless(attrs...)

	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Address)}
	if cfg.Insecure {
		// Explicit credentials keep certificate environment variables from enabling TLS.
		opts = append(opts, otlptracegrpc.WithTLSCredentials(insecure.NewCredentials()))
	} else {
		creds, err := collectorTLSCredentials()
		if err != nil {
			return nil, err
		}
		opts = append(opts, otlptracegrpc.WithTLSCredentials(creds))
	}
	// Preserve standard OTLP header environment variables when no headers are supplied.
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
	}
	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	// Route asynchronous SDK errors through the controller's configured logger.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		log.WithError(err).Error("OpenTelemetry error")
	}))
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetTracerProvider(provider)
	return func() {
		// Cancellation of ctx normally triggers shutdown, so use a fresh context to flush.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := provider.Shutdown(shutdownCtx); err != nil {
			log.WithError(err).Error("Failed to shut down OpenTelemetry tracing")
		}
	}, nil
}

func resourceAttributes(values []string) ([]attribute.KeyValue, error) {
	attrs := make([]attribute.KeyValue, 0, len(values)+2)
	for _, value := range values {
		key, attrValue, found := strings.Cut(value, ":")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("invalid OTLP resource attribute %q: expected key:value", value)
		}
		attrs = append(attrs, attribute.String(key, strings.TrimSpace(attrValue)))
	}
	return attrs, nil
}

// collectorTLSCredentials follows the standard OTLP certificate environment variables.
// Trace-specific settings take precedence over generic settings. Explicit TLS
// credentials prevent OTEL_EXPORTER_OTLP_INSECURE from overriding Config.Insecure.
func collectorTLSCredentials() (credentials.TransportCredentials, error) {
	env := func(name string) string {
		return strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_" + name))
	}
	cfg := &tls.Config{}
	caPath := env("TRACES_CERTIFICATE")
	if caPath == "" {
		caPath = env("CERTIFICATE")
	}
	if caPath != "" {
		data, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read OTLP collector CA: %w", err)
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("no certificates found in OTLP collector CA %s", caPath)
		}
	}
	var certPath, keyPath string
	sc, sk := env("TRACES_CLIENT_CERTIFICATE"), env("TRACES_CLIENT_KEY")
	gc, gk := env("CLIENT_CERTIFICATE"), env("CLIENT_KEY")
	switch {
	case sc != "" && sk != "":
		certPath, keyPath = sc, sk
	case gc != "" && gk != "":
		certPath, keyPath = gc, gk
	case sc != "" || sk != "" || gc != "" || gk != "":
		return nil, errors.New("OTLP client certificate and key must both be set")
	}
	if certPath != "" {
		pair, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("load OTLP client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	return credentials.NewTLS(cfg), nil
}
