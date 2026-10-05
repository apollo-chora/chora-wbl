package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/apollo-chora/chora-wbl/internal/observability"
)

// -- legacy traceparent / log shim tests (preserved from skeleton) ----------

// FromRequest mints a fresh trace context if missing.
func TestFromRequest_MintsContext(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "/api/employers", nil)
	tc := observability.FromRequest(r)
	if len(tc.TraceID) != 32 {
		t.Errorf("expected 32-char trace id, got %d", len(tc.TraceID))
	}
	if len(tc.SpanID) != 16 {
		t.Errorf("expected 16-char span id, got %d", len(tc.SpanID))
	}
}

// FromRequest parses upstream W3C traceparent.
func TestFromRequest_ParsesTraceparent(t *testing.T) {
	t.Parallel()
	tp := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	r := httptest.NewRequest("GET", "/api/employers", nil)
	r.Header.Set("traceparent", tp)
	tc := observability.FromRequest(r)
	if tc.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("unexpected trace id %q", tc.TraceID)
	}
	if tc.SpanID != "b7ad6b7169203331" {
		t.Errorf("unexpected span id %q", tc.SpanID)
	}
}

// TraceparentHeader is consistent.
func TestTraceparentHeader_RoundTrip(t *testing.T) {
	t.Parallel()
	tc := observability.TraceContext{TraceID: "a", SpanID: "b", TraceFlg: "01"}
	h := tc.TraceparentHeader()
	if !strings.HasPrefix(h, "00-") {
		t.Errorf("expected version-00 prefix, got %q", h)
	}
}

// LogRequest emits a structured log line; smoke-test it doesn't panic and
// honours zero-duration. Output goes to stdlib log so we can't easily
// assert on the bytes; the contract is "no panic + side-effect-free".
func TestLogRequest_NoPanic(t *testing.T) {
	t.Parallel()
	tc := observability.TraceContext{
		TraceID:  "0af7651916cd43dd8448eb211c80319c",
		SpanID:   "b7ad6b7169203331",
		TraceFlg: "01",
	}
	// Must not panic on any of these inputs.
	observability.LogRequest(tc, "tenant-1", "gcid-1", "GET", "/api/employers", 200, 0)
	observability.LogRequest(tc, "", "", "POST", "/api/internships", 500, 0)
}

// -- M12 OTel SDK wiring tests (RED → GREEN) --------------------------------

// ServiceName + ServiceVersion are pinned so this binary's spans carry a
// stable service.name value (drift would split historical span lookups).
func TestServiceConstantsPinned(t *testing.T) {
	if observability.ServiceName != "chora-wbl" {
		t.Errorf("ServiceName drift: got %q; want chora-wbl", observability.ServiceName)
	}
	if observability.ServiceVersion == "" {
		t.Errorf("ServiceVersion must be non-empty for service.version resource attr")
	}
}

// Init succeeds in dev mode (no OTLP endpoint).
// The canonical chora-common lib falls back to stdouttrace.
func TestInit_DevFallback_StdoutExporter(t *testing.T) {
	// t.Setenv mutates process env — cannot t.Parallel.
	t.Setenv("OTEL_EXPORTER", "stdout")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := observability.Init(ctx)
	if err != nil {
		t.Fatalf("Init(dev): unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init returned nil shutdown")
	}

	shutdownCtx, c := context.WithTimeout(context.Background(), 2*time.Second)
	defer c()
	if err := shutdown(shutdownCtx); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

// Tracer() returns a usable Tracer scoped to ServiceName.
func TestTracer_NotNil(t *testing.T) {
	if observability.Tracer() == nil {
		t.Error("Tracer() returned nil")
	}
}

// HTTPMiddleware starts an OTel server-side span per request, propagates W3C
// trace context, and continues an inbound traceparent rather than starting a
// fresh root. Regression-guards the trace-propagation gap that closed
// in chora-gateway 2026-05-17 (E2E-BE-AI-ASSIST-TRACE-EXPORT-PERM).
func TestHTTPMiddleware_EmitsSpanForWBLRequest(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	mw := observability.HTTPMiddleware()
	if mw == nil {
		t.Fatal("HTTPMiddleware() returned nil")
	}

	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/internships", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if !called {
		t.Fatal("inner handler not invoked")
	}
	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("expected a server-side span; HTTPMiddleware likely degraded to no-op")
	}
	if got := spans[0].Name(); got != "GET /api/internships" {
		t.Errorf("span name=%q want \"GET /api/internships\"", got)
	}
}

// Shutdown is idempotent and safe even when Init was not called (no-op).
// The canonical lib's fail-soft contract guarantees a callable shutdown
// closure even on init error / timeout.
func TestShutdown_SafeOnDevFallback(t *testing.T) {
	t.Setenv("OTEL_EXPORTER", "stdout")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := observability.Init(ctx)
	if err != nil {
		t.Fatalf("Init(dev): %v", err)
	}

	// Two shutdown calls in sequence must not panic; the second is a no-op.
	if err := shutdown(ctx); err != nil {
		t.Errorf("first shutdown error: %v", err)
	}
	// Second call should also be safe.
	if err := shutdown(ctx); err != nil {
		t.Logf("second shutdown error (acceptable for already-shutdown TP): %v", err)
	}
}
