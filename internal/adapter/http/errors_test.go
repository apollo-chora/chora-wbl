package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "github.com/apollo-chora/chora-wbl/internal/adapter/http"
	"github.com/apollo-chora/chora-wbl/internal/adapter/inmem"
	"github.com/apollo-chora/chora-wbl/internal/events"
)

// perform issues a request against srv and returns the recorder. Mirrors the
// setHeaders helper in handlers_test.go (same package) inline for brevity.
func perform(srv http.Handler, method, path, body, tenant, gcid string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	setHeaders(req, tenant, gcid)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// newTestServerWithStore is newTestServer plus the backing store (the helper
// in handlers_test.go only exposes handler + bus) — needed to close listings
// or drive repos mid-flow.
func newTestServerWithStore(t *testing.T) (http.Handler, *events.Bus, *inmem.Store) {
	t.Helper()
	bus := events.NewBus()
	store := inmem.NewStore()
	return httpapi.NewServer(httpapi.Deps{Store: store, Bus: bus}), bus, store
}

// -- shared flow helpers -----------------------------------------------------

func createListing(t *testing.T, srv http.Handler, tenant, gcid string) string {
	t.Helper()
	w := perform(srv, http.MethodPost, "/api/internships",
		`{"employer_id":"emp-1","title":"X","description":"y","duration_weeks":8}`, tenant, gcid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create listing: %d %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode listing: %v", err)
	}
	id, _ := out["id"].(string)
	if id == "" {
		t.Fatal("listing id missing")
	}
	return id
}

func createApplication(t *testing.T, srv http.Handler, listingID, tenant, gcid string) string {
	t.Helper()
	w := perform(srv, http.MethodPost, "/api/internships/"+listingID+"/applications",
		`{"motivation":"strong fit"}`, tenant, gcid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create application: %d %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode application: %v", err)
	}
	id, _ := out["id"].(string)
	if id == "" {
		t.Fatal("application id missing")
	}
	return id
}

func createMilestone(t *testing.T, srv http.Handler, tenant, gcid string) string {
	t.Helper()
	w := perform(srv, http.MethodPost, "/api/milestones",
		`{"placement_id":"place-1","title":"Week 4","summary":"sm","competencies":["go"]}`, tenant, gcid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create milestone: %d %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode milestone: %v", err)
	}
	id, _ := out["id"].(string)
	if id == "" {
		t.Fatal("milestone id missing")
	}
	return id
}

func createEvidence(t *testing.T, srv http.Handler, tenant, gcid string) string {
	t.Helper()
	w := perform(srv, http.MethodPost, "/api/evidence",
		`{"milestone_id":"m-1","kind":"reflection","title":"W4","body":"learned a lot","attachments":[]}`, tenant, gcid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create evidence: %d %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	id, _ := out["id"].(string)
	if id == "" {
		t.Fatal("evidence id missing")
	}
	return id
}

// -- actor middleware --------------------------------------------------------

// RED — gcid is required even when the tenant header is present.
func TestActorRequired_RejectsMissingGcid(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/internships", `{}`, "t-1", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// -- internships -------------------------------------------------------------

// RED — POST /api/internships rejects malformed JSON.
func TestCreateInternship_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/internships", `{`, "t-1", "g-1")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — POST /api/internships surfaces domain validation (duration_weeks <= 0).
func TestCreateInternship_ValidationError(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	body := `{"employer_id":"emp-1","title":"X","description":"y","duration_weeks":0}`
	w := perform(srv, http.MethodPost, "/api/internships", body, "t-1", "g-1")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — unsupported verb on /api/internships (only POST/GET).
func TestInternships_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPut, "/api/internships", "", "t-1", "g-1")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// RED — apply sub-path requires exactly {id}/applications.
func TestInternshipApply_UnknownSubPath(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/internships/some-id", `{}`, "t-1", "g-1")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// RED — applying requires POST.
func TestInternshipApply_WrongMethod(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodGet, "/api/internships/some-id/applications", "", "t-1", "g-1")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// RED — applying to an unknown listing.
func TestInternshipApply_ListingNotFound(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/internships/nope/applications", `{"motivation":"x"}`, "t-1", "g-1")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// RED — closed listings reject applications.
func TestInternshipApply_ListingNotOpen(t *testing.T) {
	t.Parallel()
	srv, _, store := newTestServerWithStore(t)
	id := createListing(t, srv, "t-1", "g-poster")
	// Close the listing via the domain repo (no close endpoint exists).
	l, ok := store.Internships.Get("t-1", id)
	if !ok {
		t.Fatal("listing not found in store")
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close listing: %v", err)
	}
	w := perform(srv, http.MethodPost, "/api/internships/"+id+"/applications", `{"motivation":"x"}`, "t-1", "g-1")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — malformed application body.
func TestInternshipApply_InvalidBody(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createListing(t, srv, "t-1", "g-poster")
	w := perform(srv, http.MethodPost, "/api/internships/"+id+"/applications", `{`, "t-1", "g-applicant")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — empty motivation fails domain validation.
func TestInternshipApply_EmptyMotivation(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createListing(t, srv, "t-1", "g-poster")
	w := perform(srv, http.MethodPost, "/api/internships/"+id+"/applications", `{"motivation":""}`, "t-1", "g-applicant")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// -- applications (state machine) --------------------------------------------

// RED — accept sub-path requires {id}/action.
func TestApplicationSub_UnknownSubPath(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/applications/some-id", `{}`, "t-1", "g-1")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// RED — accept/reject require POST.
func TestApplicationSub_WrongMethod(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodGet, "/api/applications/some-id/accept", "", "t-1", "g-1")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// RED — acting on an unknown application.
func TestApplicationSub_NotFound(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/applications/nope/accept", `{}`, "t-1", "g-1")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// RED — a second accept is an invalid transition -> 409.
func TestApplication_AcceptTwice(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createListing(t, srv, "t-1", "g-poster")
	appID := createApplication(t, srv, id, "t-1", "g-applicant")
	if got := perform(srv, http.MethodPost, "/api/applications/"+appID+"/accept", "", "t-1", "g-employer"); got.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", got.Code, got.Body.String())
	}
	w := perform(srv, http.MethodPost, "/api/applications/"+appID+"/accept", "", "t-1", "g-employer")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 on second accept, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — reject with malformed JSON.
func TestApplication_RejectInvalidBody(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createListing(t, srv, "t-1", "g-poster")
	appID := createApplication(t, srv, id, "t-1", "g-applicant")
	w := perform(srv, http.MethodPost, "/api/applications/"+appID+"/reject", `{"reason":`, "t-1", "g-employer")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — unknown action on an existing application.
func TestApplication_UnknownAction(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createListing(t, srv, "t-1", "g-poster")
	appID := createApplication(t, srv, id, "t-1", "g-applicant")
	w := perform(srv, http.MethodPost, "/api/applications/"+appID+"/archive", `{}`, "t-1", "g-employer")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// -- apprenticeships ---------------------------------------------------------

// RED — POST /api/apprenticeships rejects malformed JSON.
func TestApprenticeships_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/apprenticeships", `{`, "t-1", "g-1")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// RED — apprenticeships must be >= 2 months.
func TestApprenticeships_ValidationError(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	body := `{"employer_id":"emp-1","title":"X","description":"y","duration_months":1}`
	w := perform(srv, http.MethodPost, "/api/apprenticeships", body, "t-1", "g-1")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — GET /api/apprenticeships lists tenant programs.
func TestApprenticeships_List(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	body := `{"employer_id":"emp-1","title":"SE App","description":"long","duration_months":12,"focus_areas":["backend"]}`
	if got := perform(srv, http.MethodPost, "/api/apprenticeships", body, "t-1", "g-1"); got.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", got.Code, got.Body.String())
	}
	w := perform(srv, http.MethodGet, "/api/apprenticeships", "", "t-1", "g-1")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Errorf("expected 1 program, got %d", len(out.Items))
	}
}

// RED — unsupported verb on /api/apprenticeships.
func TestApprenticeships_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodDelete, "/api/apprenticeships", "", "t-1", "g-1")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// -- employers + roster ------------------------------------------------------

// RED — POST /api/employers rejects malformed JSON.
func TestEmployers_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/employers", `{`, "t-1", "g-1")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// RED — employer name is required.
func TestEmployers_ValidationError(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	body := `{"name":"","contact_email":"info@acme.test","sector":"tech"}`
	w := perform(srv, http.MethodPost, "/api/employers", body, "t-1", "g-1")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — GET /api/employers lists tenant employers.
func TestEmployers_List(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	body := `{"name":"Acme","contact_email":"info@acme.test","sector":"tech"}`
	if got := perform(srv, http.MethodPost, "/api/employers", body, "t-1", "g-1"); got.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", got.Code, got.Body.String())
	}
	w := perform(srv, http.MethodGet, "/api/employers", "", "t-1", "g-1")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Errorf("expected 1 employer, got %d", len(out.Items))
	}
}

// RED — unsupported verb on /api/employers.
func TestEmployers_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPatch, "/api/employers", "", "t-1", "g-1")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// RED — roster requires exactly {id}/roster.
func TestEmployerRoster_UnknownSubPath(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodGet, "/api/employers/some-id/foo", "", "t-1", "g-1")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// RED — roster view is GET-only.
func TestEmployerRoster_WrongMethod(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/employers/some-id/roster", `{}`, "t-1", "g-1")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// RED — roster for an unknown tenant-scoped employer.
func TestEmployerRoster_NotFound(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodGet, "/api/employers/nope/roster", "", "t-1", "g-1")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// -- milestones --------------------------------------------------------------

// RED — GET /api/milestones is unsupported (POST only).
func TestMilestones_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodGet, "/api/milestones", "", "t-1", "g-1")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// RED — POST /api/milestones rejects malformed JSON.
func TestMilestones_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/milestones", `{`, "t-1", "g-1")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// RED — placement_id is required.
func TestMilestones_ValidationError(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	body := `{"placement_id":"","title":"X","summary":"y","competencies":[]}`
	w := perform(srv, http.MethodPost, "/api/milestones", body, "t-1", "g-1")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — milestone sub-path requires {id}/action.
func TestMilestoneSub_UnknownSubPath(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/milestones/some-id", `{}`, "t-1", "g-1")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// RED — milestone actions require POST.
func TestMilestoneSub_WrongMethod(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodGet, "/api/milestones/some-id/complete", "", "t-1", "g-1")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// RED — acting on an unknown milestone.
func TestMilestoneSub_NotFound(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/milestones/nope/complete", `{}`, "t-1", "g-1")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// RED — malformed action body (non-EOF decode failure) -> 400.
func TestMilestoneSub_InvalidBody(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createMilestone(t, srv, "t-1", "g-trainer")
	w := perform(srv, http.MethodPost, "/api/milestones/"+id+"/complete", `{`, "t-1", "g-student")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — an empty body is accepted (EOF is not a decode failure).
func TestMilestoneSub_EmptyBodyAccepted(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createMilestone(t, srv, "t-1", "g-trainer")
	w := perform(srv, http.MethodPost, "/api/milestones/"+id+"/complete", "", "t-1", "g-student")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with empty body, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — complete twice is an invalid transition -> 409.
func TestMilestoneSub_CompleteTwice(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createMilestone(t, srv, "t-1", "g-trainer")
	if got := perform(srv, http.MethodPost, "/api/milestones/"+id+"/complete", `{"note":"done"}`, "t-1", "g-student"); got.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", got.Code, got.Body.String())
	}
	w := perform(srv, http.MethodPost, "/api/milestones/"+id+"/complete", `{"note":"again"}`, "t-1", "g-student")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 on second complete, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — employer sign-off before the milestone is in review -> 409.
func TestMilestoneSub_SignOffBeforeInReview(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createMilestone(t, srv, "t-1", "g-trainer")
	w := perform(srv, http.MethodPost, "/api/milestones/"+id+"/sign-off-employer", `{"note":"early"}`, "t-1", "g-employer")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — student cannot sign before the employer -> 409.
func TestMilestoneSub_StudentSignOffWrongOrder(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createMilestone(t, srv, "t-1", "g-trainer")
	if got := perform(srv, http.MethodPost, "/api/milestones/"+id+"/complete", `{"note":"done"}`, "t-1", "g-student"); got.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", got.Code, got.Body.String())
	}
	w := perform(srv, http.MethodPost, "/api/milestones/"+id+"/sign-off-student", `{"note":"me"}`, "t-1", "g-student")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — unknown milestone action.
func TestMilestoneSub_UnknownAction(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createMilestone(t, srv, "t-1", "g-trainer")
	w := perform(srv, http.MethodPost, "/api/milestones/"+id+"/archive", `{}`, "t-1", "g-trainer")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// -- evidence ----------------------------------------------------------------

// RED — GET /api/evidence is unsupported (POST only).
func TestEvidence_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodGet, "/api/evidence", "", "t-1", "g-1")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// RED — POST /api/evidence rejects malformed JSON.
func TestEvidence_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/evidence", `{`, "t-1", "g-1")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// RED — unknown evidence kind fails domain validation.
func TestEvidence_ValidationError(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	body := `{"milestone_id":"m-1","kind":"bogus","title":"X","body":"y","attachments":[]}`
	w := perform(srv, http.MethodPost, "/api/evidence", body, "t-1", "g-1")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — evidence sub-path requires {id}/action.
func TestEvidenceSub_UnknownSubPath(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/evidence/some-id", `{}`, "t-1", "g-1")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// RED — evidence actions require POST.
func TestEvidenceSub_WrongMethod(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodGet, "/api/evidence/some-id/approve", "", "t-1", "g-1")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// RED — acting on unknown evidence.
func TestEvidenceSub_NotFound(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := perform(srv, http.MethodPost, "/api/evidence/nope/approve", `{}`, "t-1", "g-1")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// RED — malformed review note (non-EOF) -> 400.
func TestEvidenceSub_InvalidBody(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createEvidence(t, srv, "t-1", "g-student")
	w := perform(srv, http.MethodPost, "/api/evidence/"+id+"/approve", `{`, "t-1", "g-supervisor")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — approving twice is an invalid transition -> 409.
func TestEvidenceSub_ApproveTwice(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createEvidence(t, srv, "t-1", "g-student")
	if got := perform(srv, http.MethodPost, "/api/evidence/"+id+"/approve", `{"note":"good"}`, "t-1", "g-supervisor"); got.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", got.Code, got.Body.String())
	}
	w := perform(srv, http.MethodPost, "/api/evidence/"+id+"/approve", `{"note":"again"}`, "t-1", "g-supervisor")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 on second approve, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — reject is a valid transition from submitted + emits the event.
func TestEvidenceSub_RejectOk(t *testing.T) {
	t.Parallel()
	srv, bus := newTestServer(t)
	id := createEvidence(t, srv, "t-1", "g-student")
	w := perform(srv, http.MethodPost, "/api/evidence/"+id+"/reject", `{"note":"incomplete"}`, "t-1", "g-supervisor")
	if w.Code != http.StatusOK {
		t.Fatalf("reject: %d %s", w.Code, w.Body.String())
	}
	got := bus.Drain()
	topics := map[string]int{}
	for _, e := range got {
		topics[e.Topic]++
	}
	if topics["chora.wbl.evidence.rejected.v1"] == 0 {
		t.Errorf("missing rejected event; got %+v", topics)
	}
}

// RED — rejecting twice is an invalid transition -> 409.
func TestEvidenceSub_RejectTwice(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createEvidence(t, srv, "t-1", "g-student")
	if got := perform(srv, http.MethodPost, "/api/evidence/"+id+"/reject", `{"note":"nope"}`, "t-1", "g-supervisor"); got.Code != http.StatusOK {
		t.Fatalf("reject: %d %s", got.Code, got.Body.String())
	}
	w := perform(srv, http.MethodPost, "/api/evidence/"+id+"/reject", `{"note":"again"}`, "t-1", "g-supervisor")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 on second reject, got %d body=%s", w.Code, w.Body.String())
	}
}

// RED — unknown evidence action.
func TestEvidenceSub_UnknownAction(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	id := createEvidence(t, srv, "t-1", "g-student")
	w := perform(srv, http.MethodPost, "/api/evidence/"+id+"/shred", `{}`, "t-1", "g-supervisor")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
