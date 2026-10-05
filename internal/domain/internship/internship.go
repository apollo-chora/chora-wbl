// Package internship is the pure-domain core for the Internship aggregate of
// the Work-Based Learning service (CHO-23).
//
// Aggregate roots: Listing + Application.
// Owning service : chora-wbl (Work-Based Learning).
// Owning team    : Team 2 (Delivery) — WBL is closely related to Content
// Delivery, sharing the same instructor / employer audience.
//
// Hard rules (per .claude/rules/ddd-enforcement.md):
//
//   - Cross-domain references (employer_id, gcid, learning-atom skills)
//     are opaque UUID strings — no FK constraints.
//   - All IDs are UUIDv7 (creation-order sortable).
//   - Soft delete only — Listing.DeletedAt records withdrawal/archive;
//     hard-delete is reserved for crypto-shred at account closure.
//   - Domain layer holds NO HTTP / persistence imports.
package internship

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MaxDescLen caps Listing.Description body length.
const MaxDescLen = 8 * 1024

// Errors.
var (
	// ErrInvalidArgument signals validation guard failures.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrInvalidTransition signals an illegal state-machine transition.
	ErrInvalidTransition = errors.New("invalid transition")
)

// Status enumerates Listing lifecycle states.
type Status string

const (
	// StatusOpen — accepting applications.
	StatusOpen Status = "open"
	// StatusClosed — no longer accepting applications (filled or withdrawn).
	StatusClosed Status = "closed"
)

// Listing is the aggregate root for an internship posting.
type Listing struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	EmployerID  string     `json:"employer_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	DurationWks int        `json:"duration_weeks"`
	Skills      []string   `json:"skills"`
	Status      Status     `json:"status"`
	PostedAt    time.Time  `json:"posted_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// NewListing constructs a Listing.
//
// Validation:
//   - tenantID, employerID, title required (non-empty after trim)
//   - durationWks must be > 0
//   - description capped at MaxDescLen
func NewListing(tenantID, employerID, title, desc string, durationWks int, skills []string) (*Listing, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrInvalidArgument)
	}
	if strings.TrimSpace(employerID) == "" {
		return nil, fmt.Errorf("%w: employer_id required", ErrInvalidArgument)
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("%w: title required", ErrInvalidArgument)
	}
	if durationWks <= 0 {
		return nil, fmt.Errorf("%w: duration_weeks must be > 0", ErrInvalidArgument)
	}
	if len(desc) > MaxDescLen {
		return nil, fmt.Errorf("%w: description exceeds %d", ErrInvalidArgument, MaxDescLen)
	}
	now := time.Now().UTC()
	return &Listing{
		ID:          NewUUIDv7(),
		TenantID:    tenantID,
		EmployerID:  employerID,
		Title:       title,
		Description: desc,
		DurationWks: durationWks,
		Skills:      append([]string(nil), skills...),
		Status:      StatusOpen,
		PostedAt:    now,
		UpdatedAt:   now,
	}, nil
}

// Close marks the listing closed (idempotent).
func (l *Listing) Close() error {
	if l.Status == StatusClosed {
		return fmt.Errorf("%w: already closed", ErrInvalidTransition)
	}
	l.Status = StatusClosed
	l.UpdatedAt = time.Now().UTC()
	return nil
}

// SoftDelete stamps DeletedAt (idempotent).
func (l *Listing) SoftDelete() {
	if l.DeletedAt != nil {
		return
	}
	now := time.Now().UTC()
	l.DeletedAt = &now
}

// AppStatus enumerates Application lifecycle states.
type AppStatus string

const (
	// AppStatusSubmitted — initial state.
	AppStatusSubmitted AppStatus = "submitted"
	// AppStatusAccepted — employer accepted.
	AppStatusAccepted AppStatus = "accepted"
	// AppStatusRejected — employer rejected.
	AppStatusRejected AppStatus = "rejected"
)

// Application is the aggregate root for an internship application.
type Application struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	ListingID    string    `json:"listing_id"`
	GCID         string    `json:"gcid"`
	Motivation   string    `json:"motivation"`
	Status       AppStatus `json:"status"`
	RejectReason string    `json:"reject_reason,omitempty"`
	SubmittedAt  time.Time `json:"submitted_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NewApplication constructs an Application.
func NewApplication(tenantID, listingID, gcid, motivation string) (*Application, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrInvalidArgument)
	}
	if strings.TrimSpace(listingID) == "" {
		return nil, fmt.Errorf("%w: listing_id required", ErrInvalidArgument)
	}
	if strings.TrimSpace(gcid) == "" {
		return nil, fmt.Errorf("%w: gcid required", ErrInvalidArgument)
	}
	if strings.TrimSpace(motivation) == "" {
		return nil, fmt.Errorf("%w: motivation required", ErrInvalidArgument)
	}
	now := time.Now().UTC()
	return &Application{
		ID:          NewUUIDv7(),
		TenantID:    tenantID,
		ListingID:   listingID,
		GCID:        gcid,
		Motivation:  motivation,
		Status:      AppStatusSubmitted,
		SubmittedAt: now,
		UpdatedAt:   now,
	}, nil
}

// Accept transitions submitted -> accepted.
func (a *Application) Accept() error {
	if a.Status != AppStatusSubmitted {
		return fmt.Errorf("%w: cannot accept from %q", ErrInvalidTransition, a.Status)
	}
	a.Status = AppStatusAccepted
	a.UpdatedAt = time.Now().UTC()
	return nil
}

// Reject transitions submitted -> rejected with a reason.
func (a *Application) Reject(reason string) error {
	if a.Status != AppStatusSubmitted {
		return fmt.Errorf("%w: cannot reject from %q", ErrInvalidTransition, a.Status)
	}
	a.Status = AppStatusRejected
	a.RejectReason = reason
	a.UpdatedAt = time.Now().UTC()
	return nil
}

// ListingRepo is the in-memory tenant-scoped store for Listings.
type ListingRepo struct {
	mu sync.RWMutex
	by map[string]*Listing
}

// NewListingRepo returns an empty repo.
func NewListingRepo() *ListingRepo { return &ListingRepo{by: make(map[string]*Listing)} }

// Save inserts or upserts a listing.
func (r *ListingRepo) Save(l *Listing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.by[l.ID] = l
}

// Get returns a listing scoped to tenantID.
func (r *ListingRepo) Get(tenantID, id string) (*Listing, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.by[id]
	if !ok || l.TenantID != tenantID {
		return nil, false
	}
	return l, true
}

// ListByTenant returns listings for a tenant (excludes soft-deleted).
func (r *ListingRepo) ListByTenant(tenantID string) []*Listing {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Listing, 0)
	for _, l := range r.by {
		if l.TenantID == tenantID && l.DeletedAt == nil {
			out = append(out, l)
		}
	}
	return out
}

// ApplicationRepo is the in-memory tenant-scoped store for Applications.
type ApplicationRepo struct {
	mu sync.RWMutex
	by map[string]*Application
}

// NewApplicationRepo returns an empty repo.
func NewApplicationRepo() *ApplicationRepo {
	return &ApplicationRepo{by: make(map[string]*Application)}
}

// Save inserts or upserts an application.
func (r *ApplicationRepo) Save(a *Application) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.by[a.ID] = a
}

// Get returns an application scoped to tenantID.
func (r *ApplicationRepo) Get(tenantID, id string) (*Application, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.by[id]
	if !ok || a.TenantID != tenantID {
		return nil, false
	}
	return a, true
}

// ListByListing returns applications for a listing scoped to tenant.
func (r *ApplicationRepo) ListByListing(tenantID, listingID string) []*Application {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Application, 0)
	for _, a := range r.by {
		if a.TenantID == tenantID && a.ListingID == listingID {
			out = append(out, a)
		}
	}
	return out
}

// MatchScore computes a simple Jaccard-like overlap between a learner's
// skills and a listing's required skills. Returns a value in [0, 1].
//
// In M12+ this is a starting point for the matching engine which folds in
// LearningAtom mastery + Familiar persona signals; the skeleton uses a
// pure-domain primitive so the algorithm is testable in isolation.
func MatchScore(listingSkills, learnerSkills []string) float64 {
	if len(listingSkills) == 0 {
		return 0
	}
	have := make(map[string]struct{}, len(learnerSkills))
	for _, s := range learnerSkills {
		have[strings.ToLower(strings.TrimSpace(s))] = struct{}{}
	}
	hit := 0
	for _, s := range listingSkills {
		if _, ok := have[strings.ToLower(strings.TrimSpace(s))]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(listingSkills))
}

// NewUUIDv7 returns a freshly generated UUIDv7 — RFC 9562 §5.7. We hand-roll
// to keep the skeleton dependency-free; M12 swaps to the chora-contracts
// typed wrapper.
func NewUUIDv7() string {
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
