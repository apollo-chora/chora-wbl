package employer_test

import (
	"errors"
	"testing"

	"github.com/apollo-chora/chora-wbl/internal/domain/employer"
)

// RED — employer creation requires name + tenant linkage + canonical fields.
func TestNewEmployer_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		tenantID string
		empName  string
		wantErr  error
	}{
		{name: "missing tenant", tenantID: "", empName: "Acme Corp", wantErr: employer.ErrInvalidArgument},
		{name: "missing name", tenantID: "t-1", empName: "", wantErr: employer.ErrInvalidArgument},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := employer.NewEmployer(tc.tenantID, tc.empName, "info@acme.test", "tech")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// RED — happy path: ID stamped, ACTIVE status.
func TestNewEmployer_ActiveDefault(t *testing.T) {
	t.Parallel()
	e, err := employer.NewEmployer("t-1", "Acme Corp", "info@acme.test", "tech")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if e.ID == "" {
		t.Errorf("expected ID stamped")
	}
	if e.Status != employer.StatusActive {
		t.Errorf("expected active, got %q", e.Status)
	}
}

// RED — soft delete (deactivate) per ddd-enforcement.
func TestEmployer_Deactivate(t *testing.T) {
	t.Parallel()
	e, _ := employer.NewEmployer("t-1", "Acme", "info@acme.test", "tech")
	e.Deactivate()
	if e.Status != employer.StatusInactive {
		t.Errorf("expected inactive after deactivate")
	}
	if e.DeletedAt == nil {
		t.Errorf("expected DeletedAt stamped (soft delete)")
	}
	// Idempotent.
	prev := *e.DeletedAt
	e.Deactivate()
	if !e.DeletedAt.Equal(prev) {
		t.Errorf("deactivate should be idempotent")
	}
}

// RED — repo lookup.
func TestEmployerRepo_SaveAndGet(t *testing.T) {
	t.Parallel()
	r := employer.NewRepo()
	e, _ := employer.NewEmployer("t-1", "Acme", "info@acme.test", "tech")
	r.Save(e)
	got, ok := r.Get("t-1", e.ID)
	if !ok || got.ID != e.ID {
		t.Errorf("expected hit")
	}
	if _, ok := r.Get("t-OTHER", e.ID); ok {
		t.Errorf("cross-tenant must miss")
	}
	if got := r.ListByTenant("t-1"); len(got) != 1 {
		t.Errorf("expected 1, got %d", len(got))
	}
}

// RED — roster: list active learners assigned to an employer (cross-aggregate
// view served by domain helper, not by FK).
func TestEmployerRoster_AddAndList(t *testing.T) {
	t.Parallel()
	r := employer.NewRoster()
	r.Add("t-1", "emp-1", "gcid-A", "intern")
	r.Add("t-1", "emp-1", "gcid-B", "apprentice")
	r.Add("t-1", "emp-2", "gcid-C", "intern")
	got := r.List("t-1", "emp-1")
	if len(got) != 2 {
		t.Errorf("expected 2 roster entries, got %d", len(got))
	}
	// Tenant scope.
	if got := r.List("t-OTHER", "emp-1"); len(got) != 0 {
		t.Errorf("cross-tenant must be empty")
	}
}

// RED — roster Add is idempotent on (gcid, placement kind) within an employer.
func TestEmployerRoster_AddIdempotent(t *testing.T) {
	t.Parallel()
	r := employer.NewRoster()
	r.Add("t-1", "emp-1", "gcid-A", "intern")
	// Re-adding the same learner as the same kind is a no-op.
	r.Add("t-1", "emp-1", "gcid-A", "intern")
	got := r.List("t-1", "emp-1")
	if len(got) != 1 {
		t.Errorf("duplicate (gcid, kind) add must be a no-op; got %d entries", len(got))
	}
}
