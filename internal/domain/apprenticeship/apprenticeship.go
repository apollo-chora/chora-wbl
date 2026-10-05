// Package apprenticeship is the pure-domain core for the Apprenticeship
// aggregate of the Work-Based Learning service.
//
// Apprenticeships differ from internships by being long-term programs (months
// to years) with structured competency progression. The aggregate captures
// the program definition; per-learner placements are tracked via Milestones.
package apprenticeship

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/apollo-chora/chora-wbl/internal/domain/internship"
)

// MinDurationMonths enforces "longer-term" semantics — anything < 2 months
// should be modelled as an internship, not an apprenticeship.
const MinDurationMonths = 2

// Errors.
var (
	// ErrInvalidArgument — guard failure.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrInvalidTransition — illegal state transition.
	ErrInvalidTransition = errors.New("invalid transition")
)

// Status — Program lifecycle.
type Status string

const (
	// StatusOpen — accepting applicants.
	StatusOpen Status = "open"
	// StatusClosed — closed.
	StatusClosed Status = "closed"
)

// Program is the aggregate root for an apprenticeship program.
type Program struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	EmployerID     string    `json:"employer_id"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	DurationMonths int       `json:"duration_months"`
	FocusAreas     []string  `json:"focus_areas"`
	Status         Status    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// NewProgram constructs a Program.
func NewProgram(tenantID, employerID, title, desc string, durationMonths int, focus []string) (*Program, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrInvalidArgument)
	}
	if strings.TrimSpace(employerID) == "" {
		return nil, fmt.Errorf("%w: employer_id required", ErrInvalidArgument)
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("%w: title required", ErrInvalidArgument)
	}
	if durationMonths < MinDurationMonths {
		return nil, fmt.Errorf("%w: duration_months must be >= %d (use Internship for shorter)", ErrInvalidArgument, MinDurationMonths)
	}
	now := time.Now().UTC()
	return &Program{
		ID:             internship.NewUUIDv7(),
		TenantID:       tenantID,
		EmployerID:     employerID,
		Title:          title,
		Description:    desc,
		DurationMonths: durationMonths,
		FocusAreas:     append([]string(nil), focus...),
		Status:         StatusOpen,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// Close transitions Open -> Closed.
func (p *Program) Close() error {
	if p.Status == StatusClosed {
		return fmt.Errorf("%w: already closed", ErrInvalidTransition)
	}
	p.Status = StatusClosed
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// ProgramRepo is an in-memory tenant-scoped store.
type ProgramRepo struct {
	mu sync.RWMutex
	by map[string]*Program
}

// NewProgramRepo constructs.
func NewProgramRepo() *ProgramRepo { return &ProgramRepo{by: make(map[string]*Program)} }

// Save upserts.
func (r *ProgramRepo) Save(p *Program) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.by[p.ID] = p
}

// Get tenant-scoped.
func (r *ProgramRepo) Get(tenantID, id string) (*Program, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.by[id]
	if !ok || p.TenantID != tenantID {
		return nil, false
	}
	return p, true
}

// ListByTenant returns programs for a tenant.
func (r *ProgramRepo) ListByTenant(tenantID string) []*Program {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Program, 0)
	for _, p := range r.by {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out
}
