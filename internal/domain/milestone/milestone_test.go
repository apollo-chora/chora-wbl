package milestone_test

import (
	"errors"
	"testing"

	"github.com/apollo-chora/chora-wbl/internal/domain/milestone"
)

// RED — milestone creation validates required fields.
func TestNewMilestone_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		tenantID  string
		placement string
		title     string
		wantErr   error
	}{
		{name: "missing tenant", tenantID: "", placement: "p-1", title: "Week 1 review", wantErr: milestone.ErrInvalidArgument},
		{name: "missing placement", tenantID: "t-1", placement: "", title: "Week 1", wantErr: milestone.ErrInvalidArgument},
		{name: "missing title", tenantID: "t-1", placement: "p-1", title: "", wantErr: milestone.ErrInvalidArgument},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := milestone.NewMilestone(tc.tenantID, tc.placement, tc.title, "summary", []string{"competency-go"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// RED — successful creation: PENDING status, IDs.
func TestNewMilestone_PendingByDefault(t *testing.T) {
	t.Parallel()
	m, err := milestone.NewMilestone("t-1", "place-1", "Week 4 Review", "Frontend competency", []string{"angular", "tablet-design"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if m.Status != milestone.StatusPending {
		t.Errorf("expected pending, got %q", m.Status)
	}
	if len(m.Competencies) != 2 {
		t.Errorf("competencies not copied")
	}
}

// RED — sign-off requires both employer + student; PENDING -> EMPLOYER_SIGNED -> SIGNED_OFF.
func TestMilestone_DualSignOff(t *testing.T) {
	t.Parallel()
	m, _ := milestone.NewMilestone("t-1", "place-1", "Week 4", "summary", []string{"go"})
	// Cannot complete in pending.
	if err := m.MarkComplete("Done!"); err != nil {
		t.Fatalf("mark complete: %v", err)
	}
	if m.Status != milestone.StatusInReview {
		t.Errorf("after complete should be IN_REVIEW, got %q", m.Status)
	}
	// Employer signs first.
	if err := m.SignOffEmployer("gcid-emp", "looks good"); err != nil {
		t.Fatalf("employer signoff: %v", err)
	}
	if m.Status != milestone.StatusEmployerSigned {
		t.Errorf("expected EMPLOYER_SIGNED, got %q", m.Status)
	}
	// Cannot double-sign by employer.
	if err := m.SignOffEmployer("gcid-emp", ""); !errors.Is(err, milestone.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
	// Student signs.
	if err := m.SignOffStudent("gcid-stu", "agree"); err != nil {
		t.Fatalf("student signoff: %v", err)
	}
	if m.Status != milestone.StatusSignedOff {
		t.Errorf("expected SIGNED_OFF, got %q", m.Status)
	}
	if m.SignedOffAt == nil {
		t.Errorf("SignedOffAt must be stamped")
	}
}

// RED — student cannot sign without employer sign-off first.
func TestMilestone_StudentBeforeEmployer(t *testing.T) {
	t.Parallel()
	m, _ := milestone.NewMilestone("t-1", "place-1", "X", "y", nil)
	_ = m.MarkComplete("note")
	// Student tries to sign first.
	if err := m.SignOffStudent("gcid-stu", "ok"); !errors.Is(err, milestone.ErrInvalidTransition) {
		t.Errorf("student sign-off before employer should fail, got %v", err)
	}
}

// RED — state machine guard clauses: complete twice and sign-offs with an
// empty gcid must both fail.
func TestMilestone_TransitionGuards(t *testing.T) {
	t.Parallel()
	m, _ := milestone.NewMilestone("t-1", "place-1", "X", "y", nil)
	if err := m.MarkComplete("done"); err != nil {
		t.Fatalf("mark complete: %v", err)
	}
	if err := m.MarkComplete("again"); !errors.Is(err, milestone.ErrInvalidTransition) {
		t.Errorf("mark complete from in_review should fail, got %v", err)
	}
	// Sign-offs require a non-empty gcid.
	if err := m.SignOffEmployer("", "note"); !errors.Is(err, milestone.ErrInvalidArgument) {
		t.Errorf("employer sign-off with empty gcid should fail, got %v", err)
	}
	if err := m.SignOffEmployer("gcid-emp", "ok"); err != nil {
		t.Fatalf("employer sign-off: %v", err)
	}
	if err := m.SignOffStudent("", "note"); !errors.Is(err, milestone.ErrInvalidArgument) {
		t.Errorf("student sign-off with empty gcid should fail, got %v", err)
	}
}
