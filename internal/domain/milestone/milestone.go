// Package milestone is the pure-domain core for the Milestone aggregate
// of the Work-Based Learning service.
//
// A Milestone is a competency checkpoint in a placement that requires dual
// sign-off (employer + student). Per the WBL UX brief §6/§8, each milestone
// captures the supervisor + learner attestation independently before being
// considered complete.
package milestone

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/apollo-chora/chora-wbl/internal/domain/internship"
)

// Errors.
var (
	// ErrInvalidArgument — guard failure.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrInvalidTransition — illegal state transition.
	ErrInvalidTransition = errors.New("invalid transition")
)

// Status — Milestone state machine.
//
// Pending → InReview → EmployerSigned → SignedOff
type Status string

const (
	// StatusPending — created but no work logged yet.
	StatusPending Status = "pending"
	// StatusInReview — learner marked complete; awaiting sign-off.
	StatusInReview Status = "in_review"
	// StatusEmployerSigned — employer endorsed; awaiting student.
	StatusEmployerSigned Status = "employer_signed"
	// StatusSignedOff — fully signed off (both parties).
	StatusSignedOff Status = "signed_off"
)

// SignOff records a single attestation.
type SignOff struct {
	GCID     string    `json:"gcid"`
	Note     string    `json:"note,omitempty"`
	SignedAt time.Time `json:"signed_at"`
}

// Milestone is the aggregate root.
type Milestone struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	PlacementID  string     `json:"placement_id"`
	Title        string     `json:"title"`
	Summary      string     `json:"summary"`
	Competencies []string   `json:"competencies"`
	Status       Status     `json:"status"`
	EmployerSign *SignOff   `json:"employer_sign,omitempty"`
	StudentSign  *SignOff   `json:"student_sign,omitempty"`
	CompleteNote string     `json:"complete_note,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	SignedOffAt  *time.Time `json:"signed_off_at,omitempty"`
}

// NewMilestone constructs.
func NewMilestone(tenantID, placementID, title, summary string, competencies []string) (*Milestone, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrInvalidArgument)
	}
	if strings.TrimSpace(placementID) == "" {
		return nil, fmt.Errorf("%w: placement_id required", ErrInvalidArgument)
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("%w: title required", ErrInvalidArgument)
	}
	now := time.Now().UTC()
	return &Milestone{
		ID:           internship.NewUUIDv7(),
		TenantID:     tenantID,
		PlacementID:  placementID,
		Title:        title,
		Summary:      summary,
		Competencies: append([]string(nil), competencies...),
		Status:       StatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// MarkComplete moves Pending -> InReview. The note is optional learner
// commentary attached at completion time.
func (m *Milestone) MarkComplete(note string) error {
	if m.Status != StatusPending {
		return fmt.Errorf("%w: cannot mark complete from %q", ErrInvalidTransition, m.Status)
	}
	m.Status = StatusInReview
	m.CompleteNote = note
	m.UpdatedAt = time.Now().UTC()
	return nil
}

// SignOffEmployer transitions InReview -> EmployerSigned.
//
// Only valid from InReview — a milestone marked complete must wait for
// employer sign-off before student sign-off (per dual-sign-off semantics).
func (m *Milestone) SignOffEmployer(gcid, note string) error {
	if m.Status != StatusInReview {
		return fmt.Errorf("%w: employer sign-off only valid in in_review (got %q)", ErrInvalidTransition, m.Status)
	}
	if strings.TrimSpace(gcid) == "" {
		return fmt.Errorf("%w: gcid required", ErrInvalidArgument)
	}
	now := time.Now().UTC()
	m.EmployerSign = &SignOff{GCID: gcid, Note: note, SignedAt: now}
	m.Status = StatusEmployerSigned
	m.UpdatedAt = now
	return nil
}

// SignOffStudent transitions EmployerSigned -> SignedOff.
//
// Student cannot sign before employer — enforces the dual-attestation
// invariant.
func (m *Milestone) SignOffStudent(gcid, note string) error {
	if m.Status != StatusEmployerSigned {
		return fmt.Errorf("%w: student sign-off requires employer_signed (got %q)", ErrInvalidTransition, m.Status)
	}
	if strings.TrimSpace(gcid) == "" {
		return fmt.Errorf("%w: gcid required", ErrInvalidArgument)
	}
	now := time.Now().UTC()
	m.StudentSign = &SignOff{GCID: gcid, Note: note, SignedAt: now}
	m.Status = StatusSignedOff
	m.SignedOffAt = &now
	m.UpdatedAt = now
	return nil
}

// Repo is an in-memory tenant-scoped store.
type Repo struct {
	mu sync.RWMutex
	by map[string]*Milestone
}

// NewRepo constructs.
func NewRepo() *Repo { return &Repo{by: make(map[string]*Milestone)} }

// Save upserts.
func (r *Repo) Save(m *Milestone) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.by[m.ID] = m
}

// Get tenant-scoped.
func (r *Repo) Get(tenantID, id string) (*Milestone, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.by[id]
	if !ok || m.TenantID != tenantID {
		return nil, false
	}
	return m, true
}

// ListByPlacement returns milestones for a placement.
func (r *Repo) ListByPlacement(tenantID, placementID string) []*Milestone {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Milestone, 0)
	for _, m := range r.by {
		if m.TenantID == tenantID && m.PlacementID == placementID {
			out = append(out, m)
		}
	}
	return out
}
