// Package events is the in-memory envelope-conforming event bus the
// chora-wbl service emits to.
//
// Production wiring (M12+) replaces this with the shared NATS JetStream
// publisher (chora-common/eventbus) validating against the Protobuf Schema
// Registry. Until then, the bus exposes the same envelope shape so handlers
// + tests can validate the taxonomy + envelope mandatory fields without
// standing up NATS.
//
// Envelope mandatory fields (per CLAUDE.md §6 + .claude/rules/ddd-enforcement.md):
//
//	event_id        UUIDv7
//	idempotency_key (defaults to event_id)
//	tenant_id
//	gcid            (actor — empty for system events)
//	occurred_at
//	published_at
//	traceparent     (W3C — required across the event bus)
//	tracestate
//	source_project  (logical source label, e.g. chora-local)
//	source_service  (chora-wbl)
//	schema_version
//
// Topic taxonomy: chora.wbl.{aggregate}.{event_type}.v{N}.
//
// Note: WBL is sub-domain under Content Delivery — the locked architecture
// puts it inside chora_delivery DB; the topic prefix `chora.wbl.*` keeps
// the WBL events clearly attributable while still being subscribed by the
// Delivery domain at M12.
package events

import (
	"crypto/rand"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"
)

// SourceService — only chora-wbl emits via this bus.
const SourceService = "chora-wbl"

// Envelope mirrors chora-contracts/proto/common/envelope.proto in pure Go.
type Envelope struct {
	EventID        string    `json:"event_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	TenantID       string    `json:"tenant_id"`
	GCID           string    `json:"gcid"`
	OccurredAt     time.Time `json:"occurred_at"`
	PublishedAt    time.Time `json:"published_at"`
	Traceparent    string    `json:"traceparent"`
	Tracestate     string    `json:"tracestate"`
	SourceProject  string    `json:"source_project"`
	SourceService  string    `json:"source_service"`
	SchemaVersion  int32     `json:"schema_version"`
}

// NewEnvelope constructs an Envelope with all mandatory fields populated.
//
//   - traceparent: pass "" to mint a fresh one; an upstream-supplied
//     W3C-format value is accepted as-is.
//   - idempotencyKey: empty string -> defaults to event_id.
func NewEnvelope(traceparent, idempotencyKey, tenantID, gcid string) Envelope {
	now := time.Now().UTC()
	eventID := newUUIDv7()
	if idempotencyKey == "" {
		idempotencyKey = eventID
	}
	if traceparent == "" {
		traceparent = mintTraceparent()
	}
	src := os.Getenv("CHORA_SOURCE_PROJECT")
	if src == "" {
		src = "chora-local"
	}
	return Envelope{
		EventID:        eventID,
		IdempotencyKey: idempotencyKey,
		TenantID:       tenantID,
		GCID:           gcid,
		OccurredAt:     now,
		PublishedAt:    now,
		Traceparent:    traceparent,
		Tracestate:     "",
		SourceProject:  src,
		SourceService:  SourceService,
		SchemaVersion:  1,
	}
}

// Event bundles an envelope with its topic + payload.
type Event struct {
	Topic    string                 `json:"topic"`
	Envelope Envelope               `json:"envelope"`
	Payload  map[string]interface{} `json:"payload"`
}

// Bus is an in-memory FIFO event bus.
type Bus struct {
	mu      sync.Mutex
	events  []Event
	subs    []chan Event
	subsCap int
}

// NewBus returns an empty bus.
func NewBus() *Bus { return &Bus{subsCap: 64} }

// topicRe enforces chora.{domain}.{aggregate}.{event_type}.v{N}.
var topicRe = regexp.MustCompile(`^chora\.[a-z_]+\.[a-z_]+\.[a-z_]+\.v[0-9]+$`)

// Publish records an event on the bus.
//
// Malformed topics panic (programmer error — Schema Registry would reject
// in production).
func (b *Bus) Publish(topic string, env Envelope, payload map[string]interface{}) {
	if !topicRe.MatchString(topic) {
		panic(fmt.Sprintf("events.Bus: malformed topic %q (must match chora.{domain}.{aggregate}.{event_type}.v{N})", topic))
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	e := Event{Topic: topic, Envelope: env, Payload: payload}

	b.mu.Lock()
	b.events = append(b.events, e)
	subs := make([]chan Event, len(b.subs))
	copy(subs, b.subs)
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Drain returns and clears the buffered events.
func (b *Bus) Drain() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.events
	b.events = nil
	return out
}

// Subscribe returns a buffered channel of live events.
func (b *Bus) Subscribe() <-chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, b.subsCap)
	b.subs = append(b.subs, ch)
	return ch
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// newUUIDv7 — RFC 9562 §5.7.
func newUUIDv7() string {
	const buflen = 16
	var b [buflen]byte
	now := uint64(time.Now().UnixMilli())
	b[0] = byte(now >> 40)
	b[1] = byte(now >> 32)
	b[2] = byte(now >> 24)
	b[3] = byte(now >> 16)
	b[4] = byte(now >> 8)
	b[5] = byte(now)
	if _, err := rand.Read(b[6:]); err != nil {
		for i := 6; i < buflen; i++ {
			b[i] = byte(now >> uint(8*(i-6)))
		}
	}
	b[6] = (b[6] & 0x0F) | 0x70
	b[8] = (b[8] & 0x3F) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// mintTraceparent generates a fresh W3C traceparent (version-00).
func mintTraceparent() string {
	hex := func(n int) string {
		buf := make([]byte, n)
		if _, err := rand.Read(buf); err != nil {
			now := time.Now().UnixNano()
			for i := range buf {
				buf[i] = byte(now >> uint(8*(i%8)))
			}
		}
		const hexDigits = "0123456789abcdef"
		out := make([]byte, len(buf)*2)
		for i, v := range buf {
			out[i*2] = hexDigits[v>>4]
			out[i*2+1] = hexDigits[v&0x0F]
		}
		return string(out)
	}
	return "00-" + hex(16) + "-" + hex(8) + "-01"
}
