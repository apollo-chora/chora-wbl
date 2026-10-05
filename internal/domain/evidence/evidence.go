// Package evidence is the pure-domain core for the Evidence aggregate of
// the Work-Based Learning service.
//
// Evidence is a learner-submitted artifact, reflection, or report tied to
// a milestone. Each submission goes through a SUBMITTED -> APPROVED |
// REJECTED state machine driven by the supervisor (employer-side gcid).
package evidence

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/apollo-chora/chora-wbl/internal/domain/internship"
)

// MaxBodyLen caps the textual body. Larger artifacts go via Attachments
// (GCS URI / opaque references) rather than inline body.
const MaxBodyLen = 16 * 1024

// Errors.
var (
	// ErrInvalidArgument — guard failure.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrInvalidTransition — illegal state-machine transition.
	ErrInvalidTransition = errors.New("invalid transition")
)

// Kind enumerates evidence types.
type Kind string

const (
	// KindArtifact — a deliverable / work output.
	KindArtifact Kind = "artifact"
	// KindReflection — a written reflection.
	KindReflection Kind = "reflection"
	// KindReport — a formal report.
	KindReport Kind = "report"
)

// Status enumerates Evidence lifecycle.
type Status string

const (
	// StatusSubmitted — initial.
	StatusSubmitted Status = "submitted"
	// StatusApproved — supervisor approved.
	StatusApproved Status = "approved"
	// StatusRejected — supervisor rejected.
	StatusRejected Status = "rejected"
)

// Reviewer captures a supervisor's review action.
type Reviewer struct {
	GCID       string    `json:"gcid"`
	Note       string    `json:"note,omitempty"`
	ReviewedAt time.Time `json:"reviewed_at"`
}

// Evidence is the aggregate root.
type Evidence struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	GCID        string    `json:"gcid"`
	MilestoneID string    `json:"milestone_id"`
	Kind        Kind      `json:"kind"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Attachments []string  `json:"attachments,omitempty"`
	Status      Status    `json:"status"`
	Review      *Reviewer `json:"review,omitempty"`
	SubmittedAt time.Time `json:"submitted_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewEvidence constructs.
func NewEvidence(tenantID, gcid, milestoneID string, kind Kind, title, body string, attachments []string) (*Evidence, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrInvalidArgument)
	}
	if strings.TrimSpace(gcid) == "" {
		return nil, fmt.Errorf("%w: gcid required", ErrInvalidArgument)
	}
	if strings.TrimSpace(milestoneID) == "" {
		return nil, fmt.Errorf("%w: milestone_id required", ErrInvalidArgument)
	}
	if !validKind(kind) {
		return nil, fmt.Errorf("%w: unknown kind %q", ErrInvalidArgument, kind)
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("%w: body required", ErrInvalidArgument)
	}
	if len(body) > MaxBodyLen {
		return nil, fmt.Errorf("%w: body exceeds %d", ErrInvalidArgument, MaxBodyLen)
	}
	now := time.Now().UTC()
	return &Evidence{
		ID:          internship.NewUUIDv7(),
		TenantID:    tenantID,
		GCID:        gcid,
		MilestoneID: milestoneID,
		Kind:        kind,
		Title:       title,
		Body:        body,
		Attachments: append([]string(nil), attachments...),
		Status:      StatusSubmitted,
		SubmittedAt: now,
		UpdatedAt:   now,
	}, nil
}

func validKind(k Kind) bool {
	switch k {
	case KindArtifact, KindReflection, KindReport:
		return true
	default:
		return false
	}
}

// Approve transitions submitted -> approved.
func (e *Evidence) Approve(reviewerGCID, note string) error {
	if e.Status != StatusSubmitted {
		return fmt.Errorf("%w: cannot approve from %q", ErrInvalidTransition, e.Status)
	}
	now := time.Now().UTC()
	e.Status = StatusApproved
	e.Review = &Reviewer{GCID: reviewerGCID, Note: note, ReviewedAt: now}
	e.UpdatedAt = now
	return nil
}

// Reject transitions submitted -> rejected.
func (e *Evidence) Reject(reviewerGCID, note string) error {
	if e.Status != StatusSubmitted {
		return fmt.Errorf("%w: cannot reject from %q", ErrInvalidTransition, e.Status)
	}
	now := time.Now().UTC()
	e.Status = StatusRejected
	e.Review = &Reviewer{GCID: reviewerGCID, Note: note, ReviewedAt: now}
	e.UpdatedAt = now
	return nil
}

// Repo is an in-memory tenant-scoped store.
type Repo struct {
	mu sync.RWMutex
	by map[string]*Evidence
}

// NewRepo constructs.
func NewRepo() *Repo { return &Repo{by: make(map[string]*Evidence)} }

// Save upserts.
func (r *Repo) Save(e *Evidence) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.by[e.ID] = e
}

// Get tenant-scoped.
func (r *Repo) Get(tenantID, id string) (*Evidence, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.by[id]
	if !ok || e.TenantID != tenantID {
		return nil, false
	}
	return e, true
}

// ListByMilestone returns evidence scoped to (tenantID, milestoneID).
func (r *Repo) ListByMilestone(tenantID, milestoneID string) []*Evidence {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Evidence, 0)
	for _, e := range r.by {
		if e.TenantID == tenantID && e.MilestoneID == milestoneID {
			out = append(out, e)
		}
	}
	return out
}
