// Package employer is the pure-domain core for the Employer aggregate
// of the Work-Based Learning service.
//
// An Employer is a tenant-scoped industry partner profile that owns the
// upstream linkage for internship + apprenticeship listings. The Roster
// tracks active learner placements per employer (cross-aggregate view —
// an in-memory analogue of the chora_wbl.employer_roster table).
package employer

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/apollo-chora/chora-wbl/internal/domain/internship"
)

// ErrInvalidArgument — guard failure.
var ErrInvalidArgument = errors.New("invalid argument")

// Status — Employer lifecycle (active / inactive via soft delete).
type Status string

const (
	// StatusActive — visible + can post listings.
	StatusActive Status = "active"
	// StatusInactive — soft-deleted; existing placements preserved per
	// edge-case "industry partner deactivated" in the brief.
	StatusInactive Status = "inactive"
)

// Employer is the aggregate root.
type Employer struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	Name         string     `json:"name"`
	ContactEmail string     `json:"contact_email"`
	Sector       string     `json:"sector"`
	Status       Status     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

// NewEmployer constructs an Employer.
func NewEmployer(tenantID, name, contactEmail, sector string) (*Employer, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrInvalidArgument)
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("%w: name required", ErrInvalidArgument)
	}
	return &Employer{
		ID:           internship.NewUUIDv7(),
		TenantID:     tenantID,
		Name:         name,
		ContactEmail: contactEmail,
		Sector:       sector,
		Status:       StatusActive,
		CreatedAt:    time.Now().UTC(),
	}, nil
}

// Deactivate is the idempotent soft-delete operation per
// .claude/rules/ddd-enforcement.md §5.
func (e *Employer) Deactivate() {
	if e.DeletedAt != nil {
		return
	}
	now := time.Now().UTC()
	e.DeletedAt = &now
	e.Status = StatusInactive
}

// Repo is an in-memory tenant-scoped store.
type Repo struct {
	mu sync.RWMutex
	by map[string]*Employer
}

// NewRepo constructs.
func NewRepo() *Repo { return &Repo{by: make(map[string]*Employer)} }

// Save upserts.
func (r *Repo) Save(e *Employer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.by[e.ID] = e
}

// Get tenant-scoped.
func (r *Repo) Get(tenantID, id string) (*Employer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.by[id]
	if !ok || e.TenantID != tenantID {
		return nil, false
	}
	return e, true
}

// ListByTenant returns active employers for a tenant.
func (r *Repo) ListByTenant(tenantID string) []*Employer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Employer, 0)
	for _, e := range r.by {
		if e.TenantID == tenantID && e.DeletedAt == nil {
			out = append(out, e)
		}
	}
	return out
}

// RosterEntry is a single learner-placement linked to an employer.
type RosterEntry struct {
	GCID         string    `json:"gcid"`
	PlacementKnd string    `json:"placement_kind"` // intern / apprentice
	JoinedAt     time.Time `json:"joined_at"`
}

// Roster is the in-memory cross-aggregate view: tenant → employer → entries.
//
// The view is intentionally simple — production replaces it with a
// materialised table populated from internship.Application accept events
// + apprenticeship.Program enrolment events.
type Roster struct {
	mu      sync.RWMutex
	entries map[string]map[string][]RosterEntry // tenantID -> employerID -> entries
}

// NewRoster constructs an empty roster.
func NewRoster() *Roster {
	return &Roster{entries: make(map[string]map[string][]RosterEntry)}
}

// Add inserts a roster entry. Idempotent on (gcid, placement_kind) within an
// employer (re-adding the same gcid as the same kind is a no-op).
func (r *Roster) Add(tenantID, employerID, gcid, kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[tenantID]; !ok {
		r.entries[tenantID] = make(map[string][]RosterEntry)
	}
	for _, e := range r.entries[tenantID][employerID] {
		if e.GCID == gcid && e.PlacementKnd == kind {
			return
		}
	}
	r.entries[tenantID][employerID] = append(r.entries[tenantID][employerID], RosterEntry{
		GCID:         gcid,
		PlacementKnd: kind,
		JoinedAt:     time.Now().UTC(),
	})
}

// List returns the roster entries scoped to (tenantID, employerID).
func (r *Roster) List(tenantID, employerID string) []RosterEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.entries[tenantID]; !ok {
		return nil
	}
	src := r.entries[tenantID][employerID]
	out := make([]RosterEntry, len(src))
	copy(out, src)
	return out
}
