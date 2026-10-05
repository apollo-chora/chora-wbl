package evidence_test

import (
	"errors"
	"testing"

	"github.com/apollo-chora/chora-wbl/internal/domain/evidence"
)

// RED — evidence submission validates required fields.
func TestNewEvidence_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		tenantID    string
		gcid        string
		milestoneID string
		kind        evidence.Kind
		body        string
		wantErr     error
	}{
		{name: "missing tenant", gcid: "g", milestoneID: "m-1", kind: evidence.KindArtifact, body: "x", wantErr: evidence.ErrInvalidArgument},
		{name: "missing gcid", tenantID: "t-1", milestoneID: "m-1", kind: evidence.KindArtifact, body: "x", wantErr: evidence.ErrInvalidArgument},
		{name: "missing milestone", tenantID: "t-1", gcid: "g", kind: evidence.KindArtifact, body: "x", wantErr: evidence.ErrInvalidArgument},
		{name: "empty body", tenantID: "t-1", gcid: "g", milestoneID: "m-1", kind: evidence.KindReflection, body: "", wantErr: evidence.ErrInvalidArgument},
		{name: "unknown kind", tenantID: "t-1", gcid: "g", milestoneID: "m-1", kind: evidence.Kind("bogus"), body: "x", wantErr: evidence.ErrInvalidArgument},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := evidence.NewEvidence(tc.tenantID, tc.gcid, tc.milestoneID, tc.kind, "title", tc.body, nil)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// RED — successful creation: SUBMITTED status; UUIDv7 ID.
func TestNewEvidence_StampsSubmitted(t *testing.T) {
	t.Parallel()
	e, err := evidence.NewEvidence("t-1", "gcid-1", "m-1", evidence.KindReflection, "Week 4", "I learned a lot", []string{"https://gs/a/x.png"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if e.Status != evidence.StatusSubmitted {
		t.Errorf("expected SUBMITTED, got %q", e.Status)
	}
	if e.ID == "" {
		t.Errorf("expected ID stamped")
	}
}

// RED — body length capped.
func TestEvidence_BodyTooLong(t *testing.T) {
	t.Parallel()
	big := make([]byte, evidence.MaxBodyLen+1)
	for i := range big {
		big[i] = 'x'
	}
	if _, err := evidence.NewEvidence("t-1", "g", "m-1", evidence.KindReflection, "x", string(big), nil); !errors.Is(err, evidence.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument on oversize, got %v", err)
	}
}

// RED — approve transitions SUBMITTED -> APPROVED; rejection -> REJECTED.
func TestEvidence_ApproveAndReject(t *testing.T) {
	t.Parallel()
	e, _ := evidence.NewEvidence("t-1", "g", "m-1", evidence.KindArtifact, "x", "body", nil)
	if err := e.Approve("supervisor-gcid", "great work"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if e.Status != evidence.StatusApproved {
		t.Errorf("expected APPROVED, got %q", e.Status)
	}
	// Cannot re-approve.
	if err := e.Approve("supervisor-gcid", ""); !errors.Is(err, evidence.ErrInvalidTransition) {
		t.Errorf("re-approve should fail")
	}
	// Cannot reject after approve.
	if err := e.Reject("supervisor-gcid", "no"); !errors.Is(err, evidence.ErrInvalidTransition) {
		t.Errorf("reject after approve should fail")
	}

	// Independent: reject path.
	r, _ := evidence.NewEvidence("t-1", "g", "m-1", evidence.KindArtifact, "x", "body", nil)
	if err := r.Reject("supervisor-gcid", "incomplete"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if r.Status != evidence.StatusRejected {
		t.Errorf("expected REJECTED, got %q", r.Status)
	}
}

// RED — repo: get + cross-tenant guard.
func TestEvidenceRepo_Get(t *testing.T) {
	t.Parallel()
	r := evidence.NewRepo()
	e, _ := evidence.NewEvidence("t-1", "g", "m-1", evidence.KindArtifact, "x", "y", nil)
	r.Save(e)
	if _, ok := r.Get("t-1", e.ID); !ok {
		t.Errorf("expected hit")
	}
	if _, ok := r.Get("t-OTHER", e.ID); ok {
		t.Errorf("cross-tenant must miss")
	}
	if _, ok := r.Get("t-1", "missing"); ok {
		t.Errorf("missing must miss")
	}
}

// RED — repo: list by milestone, scoped to tenant.
func TestEvidenceRepo_ListByMilestone(t *testing.T) {
	t.Parallel()
	repo := evidence.NewRepo()
	a, _ := evidence.NewEvidence("t-1", "g1", "m-1", evidence.KindArtifact, "x", "y", nil)
	b, _ := evidence.NewEvidence("t-1", "g1", "m-1", evidence.KindReflection, "x", "y", nil)
	c, _ := evidence.NewEvidence("t-1", "g2", "m-2", evidence.KindArtifact, "x", "y", nil)
	repo.Save(a)
	repo.Save(b)
	repo.Save(c)
	got := repo.ListByMilestone("t-1", "m-1")
	if len(got) != 2 {
		t.Errorf("expected 2, got %d", len(got))
	}
	if got := repo.ListByMilestone("t-OTHER", "m-1"); len(got) != 0 {
		t.Errorf("cross-tenant must be empty")
	}
}
