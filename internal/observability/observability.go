// Package observability wires OTel SDK traces over OTLP per CLAUDE.md §6
// ("OTLP everywhere — every service emits traces from day-1; no opt-in
// flags; no OTel Collector").
//
// M12 wire-up (2026-05-26, N8 of the O+ M12-backlog wave): the skeleton
// previously emitted structured log lines with W3C trace context only. The
// full OTel SDK wiring is now plumbed via the canonical
// chora-common/{otel,tracing} packages — the same delegation pattern
// chora-gateway / chora-governance / chora-observability use. Spans are
// exported as service.name=chora-wbl.
//
// Backward-compatible public API:
//
//   - TraceContext + FromRequest + LogRequest + TraceparentHeader were the
//     pre-M12 skeleton primitives the HTTP adapter (internal/adapter/http)
//     already calls. Preserved verbatim so handlers.go doesn't churn.
//
//   - Init(ctx) + Shutdown via the returned closure is the new entry point
//     main.go MUST call. Returns a shutdown func that flushes pending spans
//     on process exit.
//
//   - HTTPMiddleware() returns net/http middleware that starts an OTel
//     server-side span per inbound request and propagates W3C trace
//     context. Mount as the OUTERMOST middleware around the chora-wbl
//     mux so route handlers + sub-handlers run inside the request span.
//
//   - Tracer() returns the tracer scoped to ServiceName for in-handler
//     manual span creation.
//
// Per CLAUDE.md "no inline config" — endpoint + environment come from env
// vars (OTEL_EXPORTER_OTLP_ENDPOINT, DEPLOYMENT_ENVIRONMENT, CLOUD_REGION).
// The canonical lib falls back to stdouttrace in dev (no endpoint).
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	commonotel "github.com/apollo-chora/chora-common/otel"
	commontracing "github.com/apollo-chora/chora-common/tracing"
)

// ServiceName is the canonical OTLP service.name attribute. Pinned so
// span lookups stay stable across releases (drift would split historical
// traces by service).
const ServiceName = "chora-wbl"

// ServiceVersion follows semver per OpenInference convention. Bumped per
// release; emitted as the service.version resource attribute.
const ServiceVersion = "0.1.0"

// Init wires the OTel TracerProvider via the canonical lib and registers
// global propagators (W3C TraceContext + Baggage). Returns a shutdown func
// the caller (cmd/server/main.go) MUST defer to flush pending spans on
// process exit. Dev fallback (no OTLP endpoint) routes spans to stdouttrace.
func Init(ctx context.Context) (shutdown func(context.Context) error, err error) {
	return commonotel.Init(ctx, ServiceName, ServiceVersion)
}

// Tracer returns a Tracer scoped to this service — sugar for
// otel.Tracer(ServiceName). Safe to call before Init (the OTel SDK falls
// back to a no-op TracerProvider).
func Tracer() trace.Tracer { return otel.Tracer(ServiceName) }

// HTTPMiddleware returns net/http middleware that starts an OTel
// server-side span per inbound request, extracts inbound W3C TraceContext
// via the OTel propagator, and stamps response headers with the outgoing
// traceparent. Delegates to the shared chora-common tracing.Middleware
// — the same middleware chora-gateway / chora-identity / chora-tenancy
// use. Mount as the OUTERMOST middleware in main.go.
func HTTPMiddleware() func(next http.Handler) http.Handler {
	return commontracing.Middleware()
}

// -----------------------------------------------------------------------------
// Legacy skeleton primitives (preserved so the HTTP adapter doesn't churn)
// -----------------------------------------------------------------------------

// TraceContext holds the W3C traceparent fields the skeleton shim threads
// through each request. Preserved verbatim for backward-compat with the
// internal/adapter/http handlers + events.NewEnvelope call sites.
type TraceContext struct {
	TraceID  string // 16-byte hex (32 chars)
	SpanID   string // 8-byte hex (16 chars)
	TraceFlg string // 2-char hex
}

// FromRequest extracts traceparent (W3C trace context) or mints a fresh one.
// Preserved verbatim for backward-compat with logging middleware. New
// callers should prefer HTTPMiddleware which extracts via the OTel
// propagator + starts a real server-side span.
func FromRequest(r *http.Request) TraceContext {
	if h := r.Header.Get("traceparent"); h != "" {
		parts := strings.Split(h, "-")
		if len(parts) == 4 && parts[0] == "00" {
			return TraceContext{TraceID: parts[1], SpanID: parts[2], TraceFlg: parts[3]}
		}
	}
	return TraceContext{
		TraceID:  randHex(16),
		SpanID:   randHex(8),
		TraceFlg: "01",
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> uint(8*(i%8)))
		}
	}
	return hex.EncodeToString(b)
}

// LogRequest emits a single structured-log line per request. Preserved for
// backward-compat with the existing logging middleware in
// internal/adapter/http.
func LogRequest(tc TraceContext, tenantID, gcid, method, path string, status int, dur time.Duration) {
	log.Printf(
		"service=%s trace_id=%s span_id=%s tenant_id=%s gcid=%s method=%s path=%s status=%d duration_ms=%d",
		ServiceName, tc.TraceID, tc.SpanID, tenantID, gcid, method, path, status, dur.Milliseconds(),
	)
}

// TraceparentHeader formats a TraceContext as a W3C traceparent string.
func (tc TraceContext) TraceparentHeader() string {
	return fmt.Sprintf("00-%s-%s-%s", tc.TraceID, tc.SpanID, tc.TraceFlg)
}
