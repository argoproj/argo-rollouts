package trace

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type traceExport struct {
	request *collectortrace.ExportTraceServiceRequest
	headers metadata.MD
}

type traceCollector struct {
	collectortrace.UnimplementedTraceServiceServer
	exports chan traceExport
}

func (c *traceCollector) Export(ctx context.Context, request *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	headers, _ := metadata.FromIncomingContext(ctx)
	c.exports <- traceExport{request: request, headers: headers.Copy()}
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func startCollector(t *testing.T, opts ...grpc.ServerOption) (string, *traceCollector) {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(opts...)
	collector := &traceCollector{exports: make(chan traceExport, 10)}
	collectortrace.RegisterTraceServiceServer(server, collector)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String(), collector
}

func isolateOTel(t *testing.T) {
	t.Helper()
	provider, propagator, errorHandler := otel.GetTracerProvider(), otel.GetTextMapPropagator(), otel.GetErrorHandler()
	t.Cleanup(func() {
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagator)
		otel.SetErrorHandler(errorHandler)
	})
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
}

func TestInitTracerDisabled(t *testing.T) {
	isolateOTel(t)
	provider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(provider)
	propagator := propagation.Baggage{}
	otel.SetTextMapPropagator(propagator)
	var handled error
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) { handled = err }))
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", "/nonexistent/ca.crt")

	closeTracer, err := InitTracer(t.Context(), "argo-rollouts", "v1.0.0", Config{})

	require.NoError(t, err)
	require.NotNil(t, closeTracer)
	closeTracer()
	assert.Same(t, provider, otel.GetTracerProvider())
	assert.Equal(t, propagator, otel.GetTextMapPropagator())
	expected := errors.New("existing handler")
	otel.Handle(expected)
	assert.Same(t, expected, handled)
}

func TestInitTracerLogsAsynchronousExportErrors(t *testing.T) {
	isolateOTel(t)
	t.Setenv("OTEL_BSP_MAX_EXPORT_BATCH_SIZE", "1")
	logger := log.StandardLogger()
	hooks := logger.ReplaceHooks(log.LevelHooks{})
	t.Cleanup(func() { logger.ReplaceHooks(hooks) })
	hook := logtest.NewGlobal()
	address, _ := startCollector(t, grpc.UnaryInterceptor(func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
		return nil, status.Error(codes.InvalidArgument, "collector rejected spans")
	}))
	closeTracer, err := InitTracer(t.Context(), "argo-rollouts", "v1.0.0", Config{Address: address, Insecure: true, SampleRatio: 1})
	require.NoError(t, err)
	t.Cleanup(closeTracer)

	_, span := otel.Tracer("error-test").Start(t.Context(), "rejected.span")
	span.End()

	require.Eventually(t, func() bool { return len(hook.AllEntries()) > 0 }, 5*time.Second, 10*time.Millisecond)
	entry := hook.LastEntry()
	assert.Equal(t, log.ErrorLevel, entry.Level)
	assert.Equal(t, "OpenTelemetry error", entry.Message)
	loggedErr, ok := entry.Data[log.ErrorKey].(error)
	require.True(t, ok)
	assert.ErrorContains(t, loggedErr, "collector rejected spans")
}

func TestInitTracerExportsOnShutdown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     map[string]string
		headers map[string]string
		want    string
	}{
		{name: "generic environment headers", env: map[string]string{"OTEL_EXPORTER_OTLP_HEADERS": "authorization=Bearer generic"}, want: "Bearer generic"},
		{name: "trace environment headers", env: map[string]string{"OTEL_EXPORTER_OTLP_HEADERS": "authorization=Bearer generic", "OTEL_EXPORTER_OTLP_TRACES_HEADERS": "authorization=Bearer traces"}, want: "Bearer traces"},
		{name: "explicit headers override environment", env: map[string]string{"OTEL_EXPORTER_OTLP_TRACES_HEADERS": "authorization=Bearer traces"}, headers: map[string]string{"authorization": "Bearer configured"}, want: "Bearer configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateOTel(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			address, collector := startCollector(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			closeTracer, err := InitTracer(ctx, "argo-rollouts", "v1.0.0", Config{
				Address: address, Insecure: true, Headers: tc.headers, SampleRatio: 1,
				Attributes: []string{"deployment.environment.name:production", "url:https://example.com:4317", "service.name:override", "service.version:override"},
			})
			require.NoError(t, err)
			t.Cleanup(closeTracer)
			spanCtx, span := otel.Tracer("export-test").Start(ctx, "test.span")
			carrier := propagation.MapCarrier{}
			otel.GetTextMapPropagator().Inject(spanCtx, carrier)
			assert.NotEmpty(t, carrier.Get("traceparent"))
			extracted := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
			assert.Equal(t, span.SpanContext().TraceID(), oteltrace.SpanContextFromContext(extracted).TraceID())
			span.End()
			cancel()

			closeTracer()

			select {
			case exported := <-collector.exports:
				assert.Equal(t, []string{tc.want}, exported.headers.Get("authorization"))
				require.Len(t, exported.request.ResourceSpans, 1)
				resource := exported.request.ResourceSpans[0]
				attrs := make(map[string]string)
				for _, attr := range resource.Resource.Attributes {
					attrs[attr.Key] = attr.Value.GetStringValue()
				}
				assert.Equal(t, "argo-rollouts", attrs["service.name"])
				assert.Equal(t, "v1.0.0", attrs["service.version"])
				assert.Equal(t, "production", attrs["deployment.environment.name"])
				assert.Equal(t, "https://example.com:4317", attrs["url"])
				require.Len(t, resource.ScopeSpans, 1)
				require.Len(t, resource.ScopeSpans[0].Spans, 1)
				assert.Equal(t, "test.span", resource.ScopeSpans[0].Spans[0].Name)
			case <-time.After(time.Second):
				t.Fatal("collector received no span during shutdown")
			}
		})
	}
}

func TestInitTracerSampling(t *testing.T) {
	for _, tc := range []struct {
		name         string
		ratio        float64
		parent       bool
		parentFlags  oteltrace.TraceFlags
		wantRecorded bool
	}{
		{name: "root ratio zero", ratio: 0},
		{name: "root ratio one", ratio: 1, wantRecorded: true},
		{name: "sampled parent with ratio zero", ratio: 0, parent: true, parentFlags: oteltrace.FlagsSampled, wantRecorded: true},
		{name: "unsampled parent with ratio one", ratio: 1, parent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateOTel(t)
			address, collector := startCollector(t)
			closeTracer, err := InitTracer(t.Context(), "argo-rollouts", "v1.0.0", Config{Address: address, Insecure: true, SampleRatio: tc.ratio})
			require.NoError(t, err)
			t.Cleanup(closeTracer)
			ctx := t.Context()
			if tc.parent {
				ctx = oteltrace.ContextWithRemoteSpanContext(ctx, oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
					TraceID: oteltrace.TraceID{1}, SpanID: oteltrace.SpanID{2}, TraceFlags: tc.parentFlags,
				}))
			}
			_, span := otel.Tracer("sampling-test").Start(ctx, "sampled.span")
			assert.Equal(t, tc.wantRecorded, span.IsRecording())
			span.End()
			closeTracer()
			assert.Equal(t, tc.wantRecorded, len(collector.exports) > 0)
		})
	}
}

func TestInitTracerRejectsInvalidConfiguration(t *testing.T) {
	for _, ratio := range []float64{-0.01, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		t.Run("sample ratio "+fmt.Sprint(ratio), func(t *testing.T) {
			isolateOTel(t)
			_, err := InitTracer(t.Context(), "argo-rollouts", "v1.0.0", Config{Address: "localhost:4317", SampleRatio: ratio})
			require.ErrorContains(t, err, "sample ratio must be between 0 and 1")
		})
	}
	for _, value := range []string{"missing-separator", ":missing-key", " :missing-key"} {
		t.Run("attribute "+value, func(t *testing.T) {
			isolateOTel(t)
			_, err := InitTracer(t.Context(), "argo-rollouts", "v1.0.0", Config{Address: "localhost:4317", Attributes: []string{value}, SampleRatio: 1})
			require.ErrorContains(t, err, "expected key:value")
		})
	}
}

type testCertificate struct {
	certificate tls.Certificate
	certPath    string
	keyPath     string
	pool        *x509.CertPool
}

func selfSignedCertificate(t *testing.T, usage x509.ExtKeyUsage) testCertificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	parsed, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certPath, certPEM, 0600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0600))
	return testCertificate{certificate: pair, certPath: certPath, keyPath: keyPath, pool: pool}
}

func TestInitTracerTLSCertificateAuthorities(t *testing.T) {
	for _, tc := range []struct {
		name    string
		traceCA bool
	}{
		{name: "generic CA"},
		{name: "trace CA overrides generic", traceCA: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateOTel(t)
			serverCert := selfSignedCertificate(t, x509.ExtKeyUsageServerAuth)
			if tc.traceCA {
				t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", "/nonexistent/ca.crt")
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE", serverCert.certPath)
			} else {
				t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", serverCert.certPath)
			}
			t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "true")
			address, collector := startCollector(t, grpc.Creds(credentials.NewServerTLSFromCert(&serverCert.certificate)))
			assert.True(t, exportTestSpan(t, address, false, collector), "collector received no TLS export")
		})
	}
}

func TestInitTracerMutualTLS(t *testing.T) {
	for _, tc := range []struct {
		name         string
		clientPrefix string
		strayCert    bool
	}{
		{name: "generic mutual TLS", clientPrefix: "OTEL_EXPORTER_OTLP_"},
		{name: "trace mutual TLS overrides generic", clientPrefix: "OTEL_EXPORTER_OTLP_TRACES_"},
		{name: "complete generic client pair with stray trace certificate", clientPrefix: "OTEL_EXPORTER_OTLP_", strayCert: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateOTel(t)
			serverCert := selfSignedCertificate(t, x509.ExtKeyUsageServerAuth)
			clientCert := selfSignedCertificate(t, x509.ExtKeyUsageClientAuth)
			serverTLS := &tls.Config{
				Certificates: []tls.Certificate{serverCert.certificate},
				ClientAuth:   tls.RequireAndVerifyClientCert,
				ClientCAs:    clientCert.pool,
			}
			t.Setenv(tc.clientPrefix+"CLIENT_CERTIFICATE", clientCert.certPath)
			t.Setenv(tc.clientPrefix+"CLIENT_KEY", clientCert.keyPath)
			if tc.clientPrefix == "OTEL_EXPORTER_OTLP_TRACES_" {
				t.Setenv("OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE", "/nonexistent/client.crt")
				t.Setenv("OTEL_EXPORTER_OTLP_CLIENT_KEY", "/nonexistent/client.key")
			}
			if tc.strayCert {
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_CLIENT_CERTIFICATE", "/nonexistent/client.crt")
			}
			t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", serverCert.certPath)
			t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "true")
			address, collector := startCollector(t, grpc.Creds(credentials.NewTLS(serverTLS)))
			assert.True(t, exportTestSpan(t, address, false, collector), "collector received no TLS export")
		})
	}
}

func exportTestSpan(t *testing.T, address string, insecure bool, collector *traceCollector) bool {
	t.Helper()
	closeTracer, err := InitTracer(t.Context(), "argo-rollouts", "v1.0.0", Config{Address: address, Insecure: insecure, SampleRatio: 1})
	require.NoError(t, err)
	t.Cleanup(closeTracer)
	_, span := otel.Tracer("tls-test").Start(t.Context(), "tls.span")
	span.End()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_ = otel.GetTracerProvider().(*sdktrace.TracerProvider).ForceFlush(ctx)
	closeTracer()
	return len(collector.exports) > 0
}

func TestInitTracerTransportVerification(t *testing.T) {
	t.Run("system roots reject untrusted collector", func(t *testing.T) {
		isolateOTel(t)
		cert := selfSignedCertificate(t, x509.ExtKeyUsageServerAuth)
		address, collector := startCollector(t, grpc.Creds(credentials.NewServerTLSFromCert(&cert.certificate)))
		assert.False(t, exportTestSpan(t, address, false, collector))
	})
	t.Run("TLS cannot be disabled through environment", func(t *testing.T) {
		isolateOTel(t)
		t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "true")
		address, collector := startCollector(t)
		assert.False(t, exportTestSpan(t, address, false, collector))
	})
	t.Run("explicit plaintext ignores certificate environment", func(t *testing.T) {
		isolateOTel(t)
		cert := selfSignedCertificate(t, x509.ExtKeyUsageServerAuth)
		t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "false")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "false")
		t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", cert.certPath)
		address, collector := startCollector(t)
		assert.True(t, exportTestSpan(t, address, true, collector))
	})
}

func TestInitTracerRejectsInvalidTLSConfiguration(t *testing.T) {
	cert := selfSignedCertificate(t, x509.ExtKeyUsageClientAuth)
	notPEM := filepath.Join(t.TempDir(), "invalid.crt")
	require.NoError(t, os.WriteFile(notPEM, []byte("not PEM"), 0600))
	for name, env := range map[string]map[string]string{
		"unreadable CA":            {"OTEL_EXPORTER_OTLP_CERTIFICATE": "/nonexistent/ca.crt"},
		"invalid trace CA":         {"OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE": notPEM},
		"client cert without key":  {"OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE": cert.certPath},
		"client key without cert":  {"OTEL_EXPORTER_OTLP_TRACES_CLIENT_KEY": cert.keyPath},
		"mismatched client pair":   {"OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE": cert.certPath, "OTEL_EXPORTER_OTLP_CLIENT_KEY": cert.certPath},
		"crossed incomplete pairs": {"OTEL_EXPORTER_OTLP_TRACES_CLIENT_CERTIFICATE": cert.certPath, "OTEL_EXPORTER_OTLP_CLIENT_KEY": cert.keyPath},
	} {
		t.Run(name, func(t *testing.T) {
			isolateOTel(t)
			t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
			for key, value := range env {
				t.Setenv(key, value)
			}
			_, err := InitTracer(t.Context(), "argo-rollouts", "v1.0.0", Config{Address: "localhost:4317", SampleRatio: 1})
			require.Error(t, err)
		})
	}
}
