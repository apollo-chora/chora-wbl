// Package main wires the chora-wbl Go service.
//
// CHO-23 — Work-Based Learning. Connects internships + apprenticeships +
// employer rosters + competency milestones + work-evidence submissions
// into a single hexagonal Go service. Per the locked architecture, WBL is
// a sub-context of Content Delivery (Tier 1 D1) — it shares the
// chora_delivery DB at M12.
//
// Per CLAUDE.md §6 the production stack is OTLP-everywhere. The full OTel
// SDK wiring lands here at M12 (2026-05-26, N8 of the O+ M12-backlog wave)
// via the canonical chora-common/{otel,observability,tracing} packages.
// Spans are exported over OTLP as service.name=chora-wbl.
//
// Hexagonal layout:
//
//	internal/domain/internship      (pure domain — Listing + Application)
//	internal/domain/apprenticeship  (pure domain — Program)
//	internal/domain/employer        (pure domain — Employer + Roster)
//	internal/domain/milestone       (pure domain — Milestone + dual sign-off)
//	internal/domain/evidence        (pure domain — Evidence + state machine)
//	internal/adapter/inmem          (in-memory composite store)
//	internal/adapter/http           (HTTP/JSON adapter)
//	internal/events                 (envelope-conforming event bus)
//	internal/observability          (log + traceparent shim)
//
// Per CLAUDE.md §6 "no inline config" — all values from env vars
// (PORT, CHORA_SOURCE_PROJECT, etc.).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpapi "github.com/apollo-chora/chora-wbl/internal/adapter/http"
	"github.com/apollo-chora/chora-wbl/internal/adapter/inmem"
	"github.com/apollo-chora/chora-wbl/internal/events"
	"github.com/apollo-chora/chora-wbl/internal/observability"
)

const (
	serviceName    = "chora-wbl"
	serviceVersion = "0.1.0"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// OTLP wiring per Tier 3 D13 — OTLP endpoint in prod, stdout
	// fallback in dev. Init MUST run before any handler emits a span. The
	// returned shutdown is deferred to flush pending spans on SIGTERM.
	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer initCancel()
	otlpShutdown, err := observability.Init(initCtx)
	if err != nil {
		log.Printf("observability: Init returned error (continuing fail-soft): %v", err)
	}
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if otlpShutdown != nil {
			if err := otlpShutdown(shutdownCtx); err != nil {
				log.Printf("observability: trace shutdown error: %v", err)
			}
		}
	}()

	store := inmem.NewStore()
	bus := events.NewBus()
	handler := observability.HTTPMiddleware()(httpapi.NewServer(httpapi.Deps{Store: store, Bus: bus}))

	addr := ":" + port
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("service=%s version=%s listening on %s", serviceName, serviceVersion, addr)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	case sig := <-sigCh:
		log.Printf("received %s — shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}
}
