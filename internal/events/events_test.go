package events_test

import (
	"strings"
	"testing"

	"github.com/apollo-chora/chora-wbl/internal/events"
)

// RED — envelope contains all mandatory fields.
func TestNewEnvelope_MandatoryFields(t *testing.T) {
	t.Parallel()
	env := events.NewEnvelope("", "", "t-1", "gcid-1")
	if env.EventID == "" {
		t.Errorf("event_id required")
	}
	if env.IdempotencyKey == "" {
		t.Errorf("idempotency_key required")
	}
	if env.IdempotencyKey != env.EventID {
		t.Errorf("idempotency_key default should equal event_id, got %q vs %q", env.IdempotencyKey, env.EventID)
	}
	if env.TenantID != "t-1" {
		t.Errorf("tenant_id mismatch")
	}
	if env.GCID != "gcid-1" {
		t.Errorf("gcid mismatch")
	}
	if env.OccurredAt.IsZero() {
		t.Errorf("occurred_at required")
	}
	if env.PublishedAt.IsZero() {
		t.Errorf("published_at required")
	}
	if env.Traceparent == "" {
		t.Errorf("traceparent required")
	}
	if env.SourceProject == "" {
		t.Errorf("source_project required")
	}
	if env.SourceService != events.SourceService {
		t.Errorf("source_service should be %q", events.SourceService)
	}
	if env.SchemaVersion < 1 {
		t.Errorf("schema_version >=1 required")
	}
}

// RED — accepts upstream traceparent.
func TestNewEnvelope_PreservesTraceparent(t *testing.T) {
	t.Parallel()
	tp := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	env := events.NewEnvelope(tp, "key-A", "t", "g")
	if env.Traceparent != tp {
		t.Errorf("expected upstream traceparent preserved")
	}
	if env.IdempotencyKey != "key-A" {
		t.Errorf("expected idempotency override")
	}
}

// RED — Publish records on the bus, malformed topic panics.
func TestBus_PublishAndDrain(t *testing.T) {
	t.Parallel()
	b := events.NewBus()
	env := events.NewEnvelope("", "", "t-1", "g")
	b.Publish("chora.wbl.internship.listing_created.v1", env, map[string]interface{}{"listing_id": "lst-1"})
	got := b.Drain()
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Topic != "chora.wbl.internship.listing_created.v1" {
		t.Errorf("unexpected topic %q", got[0].Topic)
	}
	if !strings.HasPrefix(got[0].Topic, "chora.wbl.") {
		t.Errorf("topic must start with chora.wbl.")
	}
}

// RED — malformed topic panics.
func TestBus_MalformedTopicPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on malformed topic")
		}
	}()
	b := events.NewBus()
	env := events.NewEnvelope("", "", "t", "g")
	b.Publish("not.a.real.topic", env, nil)
}

// RED — Subscribe gets live events.
func TestBus_Subscribe(t *testing.T) {
	t.Parallel()
	b := events.NewBus()
	ch := b.Subscribe()
	env := events.NewEnvelope("", "", "t", "g")
	b.Publish("chora.wbl.evidence.submitted.v1", env, nil)
	select {
	case e := <-ch:
		if e.Topic != "chora.wbl.evidence.submitted.v1" {
			t.Errorf("unexpected topic")
		}
	default:
		t.Errorf("subscriber should have received event")
	}
}
