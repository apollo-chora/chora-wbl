package internship_test

import (
	"errors"
	"testing"

	"github.com/apollo-chora/chora-wbl/internal/domain/internship"
)

// RED — internship aggregate construction validates required fields.
func TestNewListing_RequiresFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		tenantID   string
		employerID string
		title      string
		duration   int
		wantErr    error
	}{
		{name: "missing tenant", tenantID: "", employerID: "emp-1", title: "Backend Internship", duration: 12, wantErr: internship.ErrInvalidArgument},
		{name: "missing employer", tenantID: "t-1", employerID: "", title: "Backend Internship", duration: 12, wantErr: internship.ErrInvalidArgument},
		{name: "missing title", tenantID: "t-1", employerID: "emp-1", title: "", duration: 12, wantErr: internship.ErrInvalidArgument},
		{name: "non-positive duration", tenantID: "t-1", employerID: "emp-1", title: "ok", duration: 0, wantErr: internship.ErrInvalidArgument},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := internship.NewListing(tc.tenantID, tc.employerID, tc.title, "desc", tc.duration, []string{"go"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// RED — successful construction stamps a UUIDv7 id and OPEN status.
func TestNewListing_StampsIDAndOpenStatus(t *testing.T) {
	t.Parallel()
	l, err := internship.NewListing("t-1", "emp-1", "Backend Intern", "desc", 12, []string{"go"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if l.ID == "" {
		t.Errorf("expected ID stamped")
	}
	if l.Status != internship.StatusOpen {
		t.Errorf("expected status open, got %q", l.Status)
	}
	if l.PostedAt.IsZero() {
		t.Errorf("expected PostedAt set")
	}
	if len(l.Skills) != 1 || l.Skills[0] != "go" {
		t.Errorf("skills not copied")
	}
}

// RED — Application lifecycle: submit -> Accept -> only allowed once.
func TestApplication_Lifecycle(t *testing.T) {
	t.Parallel()
	app, err := internship.NewApplication("t-1", "lst-1", "gcid-1", "Strong motivation")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if app.Status != internship.AppStatusSubmitted {
		t.Errorf("expected submitted, got %q", app.Status)
	}
	if err := app.Accept(); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if app.Status != internship.AppStatusAccepted {
		t.Errorf("expected accepted, got %q", app.Status)
	}
	// Cannot accept twice.
	if err := app.Accept(); !errors.Is(err, internship.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
	// Cannot reject after accept.
	if err := app.Reject("not enough fit"); !errors.Is(err, internship.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// RED — Application reject succeeds and stores reason.
func TestApplication_Reject(t *testing.T) {
	t.Parallel()
	app, err := internship.NewApplication("t-1", "lst-1", "gcid-1", "motivation")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if err := app.Reject("incomplete profile"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if app.Status != internship.AppStatusRejected {
		t.Errorf("expected rejected")
	}
	if app.RejectReason == "" {
		t.Errorf("expected reject reason recorded")
	}
}

// RED — Application requires non-empty motivation.
func TestApplication_RequiresMotivation(t *testing.T) {
	t.Parallel()
	if _, err := internship.NewApplication("t-1", "lst-1", "gcid-1", ""); !errors.Is(err, internship.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

// RED — Repository in-memory ops.
func TestListingRepo_SaveAndFind(t *testing.T) {
	t.Parallel()
	r := internship.NewListingRepo()
	l, _ := internship.NewListing("t-1", "emp-1", "title", "desc", 8, nil)
	r.Save(l)
	got, ok := r.Get("t-1", l.ID)
	if !ok || got.ID != l.ID {
		t.Errorf("expected hit on tenant-scoped get, got ok=%v got=%+v", ok, got)
	}
	// Cross-tenant guard.
	if _, ok := r.Get("t-OTHER", l.ID); ok {
		t.Errorf("cross-tenant access must be denied")
	}
	// List tenant.
	listed := r.ListByTenant("t-1")
	if len(listed) != 1 {
		t.Errorf("expected 1 listing, got %d", len(listed))
	}
}

// RED — Listing.Close transitions Open -> Closed and is non-idempotent for
// invalid transitions.
func TestListing_Close(t *testing.T) {
	t.Parallel()
	l, _ := internship.NewListing("t-1", "emp-1", "X", "y", 8, nil)
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if l.Status != internship.StatusClosed {
		t.Errorf("expected closed")
	}
	if err := l.Close(); err == nil {
		t.Errorf("re-close should error")
	}
}

// RED — Listing.SoftDelete is idempotent.
func TestListing_SoftDelete(t *testing.T) {
	t.Parallel()
	l, _ := internship.NewListing("t-1", "emp-1", "X", "y", 8, nil)
	l.SoftDelete()
	if l.DeletedAt == nil {
		t.Errorf("expected DeletedAt stamped")
	}
	prev := *l.DeletedAt
	l.SoftDelete()
	if !l.DeletedAt.Equal(prev) {
		t.Errorf("soft delete must be idempotent")
	}
}

// RED — Listing constructor: max desc length.
func TestListing_DescTooLong(t *testing.T) {
	t.Parallel()
	big := make([]byte, internship.MaxDescLen+1)
	for i := range big {
		big[i] = 'x'
	}
	if _, err := internship.NewListing("t-1", "emp-1", "X", string(big), 8, nil); err == nil {
		t.Errorf("expected ErrInvalidArgument on oversize desc")
	}
}

// RED — Application Get + cross-tenant guard.
func TestApplicationRepo_Get(t *testing.T) {
	t.Parallel()
	r := internship.NewApplicationRepo()
	a, _ := internship.NewApplication("t-1", "lst-1", "g", "m")
	r.Save(a)
	if _, ok := r.Get("t-1", a.ID); !ok {
		t.Errorf("expected hit")
	}
	if _, ok := r.Get("t-OTHER", a.ID); ok {
		t.Errorf("cross-tenant must miss")
	}
	if _, ok := r.Get("t-1", "missing"); ok {
		t.Errorf("expected miss")
	}
}

// RED — NewApplication missing listingID.
func TestApplication_RequiresListing(t *testing.T) {
	t.Parallel()
	if _, err := internship.NewApplication("t-1", "", "g", "m"); err == nil {
		t.Errorf("expected error on missing listing")
	}
	if _, err := internship.NewApplication("t-1", "lst-1", "", "m"); err == nil {
		t.Errorf("expected error on missing gcid")
	}
	if _, err := internship.NewApplication("", "lst-1", "g", "m"); err == nil {
		t.Errorf("expected error on missing tenant")
	}
}
