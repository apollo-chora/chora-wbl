package apprenticeship_test

import (
	"errors"
	"testing"

	"github.com/apollo-chora/chora-wbl/internal/domain/apprenticeship"
)

// RED — apprenticeship requires multi-month duration (longer-term per brief).
func TestNewProgram_RequiresValidDuration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		months     int
		wantErr    error
		wantStatus apprenticeship.Status
	}{
		{name: "zero months", months: 0, wantErr: apprenticeship.ErrInvalidArgument},
		{name: "1 month — too short", months: 1, wantErr: apprenticeship.ErrInvalidArgument},
		{name: "valid 6 months", months: 6, wantStatus: apprenticeship.StatusOpen},
		{name: "valid 24 months", months: 24, wantStatus: apprenticeship.StatusOpen},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := apprenticeship.NewProgram("t-1", "emp-1", "Software Engineer Apprenticeship", "desc", tc.months, []string{"backend"})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if p.Status != tc.wantStatus {
				t.Errorf("expected status %q, got %q", tc.wantStatus, p.Status)
			}
			if p.ID == "" {
				t.Errorf("expected ID stamped")
			}
		})
	}
}

// RED — Program close transitions to closed.
func TestProgram_Close(t *testing.T) {
	t.Parallel()
	p, _ := apprenticeship.NewProgram("t-1", "emp-1", "X", "y", 12, nil)
	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if p.Status != apprenticeship.StatusClosed {
		t.Errorf("expected closed, got %q", p.Status)
	}
	if err := p.Close(); !errors.Is(err, apprenticeship.ErrInvalidTransition) {
		t.Errorf("re-close should be invalid, got %v", err)
	}
}

// RED — repository scoped lookup.
func TestProgramRepo_TenantScopedLookup(t *testing.T) {
	t.Parallel()
	r := apprenticeship.NewProgramRepo()
	p, _ := apprenticeship.NewProgram("t-1", "emp-1", "X", "y", 12, nil)
	r.Save(p)
	if _, ok := r.Get("t-1", p.ID); !ok {
		t.Errorf("expected hit")
	}
	if _, ok := r.Get("t-OTHER", p.ID); ok {
		t.Errorf("cross-tenant must miss")
	}
	if got := r.ListByTenant("t-1"); len(got) != 1 {
		t.Errorf("expected 1, got %d", len(got))
	}
}

// RED — NewProgram guard clauses: tenant / employer / title are required.
func TestNewProgram_RequiredFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		tenantID string
		employer string
		title    string
	}{
		{name: "missing tenant", tenantID: "", employer: "emp-1", title: "X"},
		{name: "missing employer", tenantID: "t-1", employer: "", title: "X"},
		{name: "missing title", tenantID: "t-1", employer: "emp-1", title: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := apprenticeship.NewProgram(tc.tenantID, tc.employer, tc.title, "desc", 12, nil)
			if !errors.Is(err, apprenticeship.ErrInvalidArgument) {
				t.Fatalf("expected %v, got %v", apprenticeship.ErrInvalidArgument, err)
			}
		})
	}
}
