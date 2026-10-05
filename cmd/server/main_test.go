package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	commonotel "github.com/apollo-chora/chora-common/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// setupBootEnv pins dev-mode observability and swaps the exporter factory for
// one that errors, so main's fail-soft branch (Init error -> log + continue)
// is exercised deterministically and Init returns fast.
func setupBootEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER", "stdout")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	restore := commonotel.SwapExporterFactoryForTest(func(context.Context, string, string, string) (sdktrace.SpanExporter, error) {
		return nil, errors.New("forced exporter failure (test)")
	})
	t.Cleanup(restore)
}

// waitUntilHealthy polls the health endpoint on 127.0.0.1:port until it
// returns 200 or the deadline expires.
func waitUntilHealthy(t *testing.T, port int) {
	t.Helper()
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/healthz"
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = errors.New("unexpected status: " + resp.Status)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server never became healthy (last error: %v)", lastErr)
}

// Boot smoke test: start the real main() in-process on a free port and verify
// the full stack (observability middleware + mux + inmem store + event bus)
// answers /healthz. main() then blocks in its signal select for the remainder
// of the test process — acceptable for a boot test on Windows, where
// signal-based shutdown is unsupported.
func TestMain_BootsAndServesHealth(t *testing.T) {
	setupBootEnv(t)

	// Reserve a free port, release it, and hand the number to main() via PORT.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	t.Setenv("PORT", strconv.Itoa(port))

	go main()

	waitUntilHealthy(t, port)
}

// RED — with PORT unset, main falls back to the default 8080 and still boots.
// Skipped when 8080 is already taken (binding failure would reach main's
// log.Fatalf arm, which exits the test process).
func TestMain_UsesDefaultPortWhenUnset(t *testing.T) {
	setupBootEnv(t)
	t.Setenv("PORT", "")

	probe, err := net.Listen("tcp", ":8080")
	if err != nil {
		t.Skipf("port 8080 already in use: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}

	go main()

	waitUntilHealthy(t, 8080)
}
