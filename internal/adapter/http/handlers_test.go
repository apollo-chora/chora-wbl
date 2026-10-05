package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "github.com/apollo-chora/chora-wbl/internal/adapter/http"
	"github.com/apollo-chora/chora-wbl/internal/adapter/inmem"
	"github.com/apollo-chora/chora-wbl/internal/events"
)

func newTestServer(t *testing.T) (http.Handler, *events.Bus) {
	t.Helper()
	bus := events.NewBus()
	store := inmem.NewStore()
	return httpapi.NewServer(httpapi.Deps{Store: store, Bus: bus}), bus
}

func setHeaders(req *http.Request, tenant, gcid string) {
	if tenant != "" {
		req.Header.Set("X-Tenant-Id", tenant)
	}
	if gcid != "" {
		req.Header.Set("gcid", gcid)
	}
}

// RED — health.
func TestHealth(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	for _, p := range []string{"/healthz", "/readyz", "/health"} {
		req := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("path %s: expected 200, got %d", p, w.Code)
		}
	}
}

// RED — actor middleware: missing tenant returns 400.
func TestMiddleware_RejectsMissingHeaders(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/internships", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400 missing-headers, got %d", w.Code)
	}
}

// RED — POST /api/internships creates listing + emits event.
func TestCreateInternship(t *testing.T) {
	t.Parallel()
	srv, bus := newTestServer(t)
	body := `{"employer_id":"emp-1","title":"Backend Intern","description":"Build APIs","duration_weeks":12,"skills":["go"]}`
	req := httptest.NewRequest("POST", "/api/internships", strings.NewReader(body))
	setHeaders(req, "t-1", "gcid-poster")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["id"] == nil {
		t.Errorf("expected id in response")
	}
	got := bus.Drain()
	if len(got) != 1 || got[0].Topic != "chora.wbl.internship.listing_created.v1" {
		t.Errorf("expected listing_created event, got %+v", got)
	}
}

// RED — GET /api/internships lists tenant-scoped listings.
func TestListInternships(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	// Create one.
	body := `{"employer_id":"emp-1","title":"X","description":"y","duration_weeks":8}`
	req := httptest.NewRequest("POST", "/api/internships", strings.NewReader(body))
	setHeaders(req, "t-1", "gcid-1")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	// List.
	req2 := httptest.NewRequest("GET", "/api/internships", nil)
	setHeaders(req2, "t-1", "gcid-1")
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("list: %d", w2.Code)
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(out.Items))
	}
}

// RED — POST /api/internships/{id}/applications: apply -> accept -> reject lifecycle.
func TestApplyAcceptReject(t *testing.T) {
	t.Parallel()
	srv, bus := newTestServer(t)
	// Create listing.
	body := `{"employer_id":"emp-1","title":"X","description":"y","duration_weeks":8}`
	req := httptest.NewRequest("POST", "/api/internships", strings.NewReader(body))
	setHeaders(req, "t-1", "gcid-poster")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var listing map[string]any
	json.NewDecoder(w.Body).Decode(&listing)
	listingID := listing["id"].(string)

	// Apply.
	appBody := `{"motivation":"Strong fit"}`
	req2 := httptest.NewRequest("POST", "/api/internships/"+listingID+"/applications", strings.NewReader(appBody))
	setHeaders(req2, "t-1", "gcid-applicant")
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != 201 {
		t.Fatalf("apply: %d %s", w2.Code, w2.Body.String())
	}
	var app map[string]any
	json.NewDecoder(w2.Body).Decode(&app)
	appID := app["id"].(string)

	// Accept.
	req3 := httptest.NewRequest("POST", "/api/applications/"+appID+"/accept", nil)
	setHeaders(req3, "t-1", "gcid-employer")
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)
	if w3.Code != 200 {
		t.Fatalf("accept: %d %s", w3.Code, w3.Body.String())
	}

	// Reject (after accept) should fail 409 invalid transition.
	rejBody := `{"reason":"changed mind"}`
	req4 := httptest.NewRequest("POST", "/api/applications/"+appID+"/reject", bytes.NewReader([]byte(rejBody)))
	setHeaders(req4, "t-1", "gcid-employer")
	w4 := httptest.NewRecorder()
	srv.ServeHTTP(w4, req4)
	if w4.Code != 409 {
		t.Errorf("expected 409 conflict on reject-after-accept, got %d", w4.Code)
	}

	got := bus.Drain()
	// At least 3 events: listing_created, application_submitted, application_accepted.
	if len(got) < 3 {
		t.Errorf("expected >=3 events, got %d: %+v", len(got), got)
	}
}

// RED — reject path on a fresh application succeeds.
func TestApply_Reject(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	body := `{"employer_id":"emp-1","title":"X","description":"y","duration_weeks":8}`
	req := httptest.NewRequest("POST", "/api/internships", strings.NewReader(body))
	setHeaders(req, "t-1", "gcid-poster")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var listing map[string]any
	json.NewDecoder(w.Body).Decode(&listing)
	listingID := listing["id"].(string)

	req2 := httptest.NewRequest("POST", "/api/internships/"+listingID+"/applications", strings.NewReader(`{"motivation":"strong"}`))
	setHeaders(req2, "t-1", "gcid-applicant")
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	var app map[string]any
	json.NewDecoder(w2.Body).Decode(&app)
	appID := app["id"].(string)

	// Reject with reason.
	req3 := httptest.NewRequest("POST", "/api/applications/"+appID+"/reject", strings.NewReader(`{"reason":"incomplete"}`))
	setHeaders(req3, "t-1", "gcid-employer")
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)
	if w3.Code != 200 {
		t.Fatalf("reject: %d %s", w3.Code, w3.Body.String())
	}
}

// RED — POST /api/milestones creates milestone.
func TestCreateMilestone(t *testing.T) {
	t.Parallel()
	srv, bus := newTestServer(t)
	body := `{"placement_id":"place-1","title":"Week 4","summary":"All going well","competencies":["go"]}`
	req := httptest.NewRequest("POST", "/api/milestones", strings.NewReader(body))
	setHeaders(req, "t-1", "gcid-trainer")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	got := bus.Drain()
	if len(got) != 1 || got[0].Topic != "chora.wbl.milestone.created.v1" {
		t.Errorf("expected milestone.created event, got %+v", got)
	}
}

// RED — milestone dual sign-off via API.
func TestMilestone_DualSignOffViaAPI(t *testing.T) {
	t.Parallel()
	srv, bus := newTestServer(t)
	// Create.
	body := `{"placement_id":"place-1","title":"Week 4","summary":"sm","competencies":["go"]}`
	req := httptest.NewRequest("POST", "/api/milestones", strings.NewReader(body))
	setHeaders(req, "t-1", "gcid-trainer")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var ms map[string]any
	json.NewDecoder(w.Body).Decode(&ms)
	id := ms["id"].(string)

	// Mark complete.
	complete := httptest.NewRequest("POST", "/api/milestones/"+id+"/complete", strings.NewReader(`{"note":"done"}`))
	setHeaders(complete, "t-1", "gcid-stu")
	wc := httptest.NewRecorder()
	srv.ServeHTTP(wc, complete)
	if wc.Code != 200 {
		t.Fatalf("complete: %d %s", wc.Code, wc.Body.String())
	}

	// Employer sign-off.
	emp := httptest.NewRequest("POST", "/api/milestones/"+id+"/sign-off-employer", strings.NewReader(`{"note":"good"}`))
	setHeaders(emp, "t-1", "gcid-employer")
	we := httptest.NewRecorder()
	srv.ServeHTTP(we, emp)
	if we.Code != 200 {
		t.Fatalf("employer sign-off: %d %s", we.Code, we.Body.String())
	}

	// Student sign-off.
	stu := httptest.NewRequest("POST", "/api/milestones/"+id+"/sign-off-student", strings.NewReader(`{"note":"agree"}`))
	setHeaders(stu, "t-1", "gcid-stu")
	ws := httptest.NewRecorder()
	srv.ServeHTTP(ws, stu)
	if ws.Code != 200 {
		t.Fatalf("student sign-off: %d %s", ws.Code, ws.Body.String())
	}

	got := bus.Drain()
	// Expect: created, complete, employer_signed, signed_off (+ any list events)
	topics := map[string]int{}
	for _, e := range got {
		topics[e.Topic]++
	}
	for _, want := range []string{
		"chora.wbl.milestone.created.v1",
		"chora.wbl.milestone.completed.v1",
		"chora.wbl.milestone.signed_off.v1",
	} {
		if topics[want] == 0 {
			t.Errorf("missing event %s; got %+v", want, topics)
		}
	}
}

// RED — POST /api/evidence + approve.
func TestSubmitAndApproveEvidence(t *testing.T) {
	t.Parallel()
	srv, bus := newTestServer(t)
	body := `{"milestone_id":"m-1","kind":"reflection","title":"W4","body":"learned a lot","attachments":[]}`
	req := httptest.NewRequest("POST", "/api/evidence", strings.NewReader(body))
	setHeaders(req, "t-1", "gcid-stu")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	var ev map[string]any
	json.NewDecoder(w.Body).Decode(&ev)
	id := ev["id"].(string)

	// Approve.
	app := httptest.NewRequest("POST", "/api/evidence/"+id+"/approve", strings.NewReader(`{"note":"good work"}`))
	setHeaders(app, "t-1", "gcid-supervisor")
	wa := httptest.NewRecorder()
	srv.ServeHTTP(wa, app)
	if wa.Code != 200 {
		t.Fatalf("approve: %d %s", wa.Code, wa.Body.String())
	}

	got := bus.Drain()
	topics := map[string]int{}
	for _, e := range got {
		topics[e.Topic]++
	}
	if topics["chora.wbl.evidence.submitted.v1"] == 0 {
		t.Errorf("missing submitted event")
	}
	if topics["chora.wbl.evidence.approved.v1"] == 0 {
		t.Errorf("missing approved event")
	}
}

// RED — GET /api/employers/{id}/roster.
func TestEmployerRoster(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	// Create employer.
	empBody := `{"name":"Acme","contact_email":"info@acme.test","sector":"tech"}`
	req := httptest.NewRequest("POST", "/api/employers", strings.NewReader(empBody))
	setHeaders(req, "t-1", "gcid-admin")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create employer: %d %s", w.Code, w.Body.String())
	}
	var emp map[string]any
	json.NewDecoder(w.Body).Decode(&emp)
	empID := emp["id"].(string)

	// Roster initially empty.
	rstr := httptest.NewRequest("GET", "/api/employers/"+empID+"/roster", nil)
	setHeaders(rstr, "t-1", "gcid-admin")
	wr := httptest.NewRecorder()
	srv.ServeHTTP(wr, rstr)
	if wr.Code != 200 {
		t.Fatalf("roster GET: %d %s", wr.Code, wr.Body.String())
	}
	var rs struct {
		Items []map[string]any `json:"items"`
	}
	json.NewDecoder(wr.Body).Decode(&rs)
	if len(rs.Items) != 0 {
		t.Errorf("expected empty roster, got %d", len(rs.Items))
	}
}

// RED — Apprenticeship CRUD.
func TestApprenticeshipCreate(t *testing.T) {
	t.Parallel()
	srv, bus := newTestServer(t)
	body := `{"employer_id":"emp-1","title":"SE Apprenticeship","description":"long-term","duration_months":12,"focus_areas":["backend"]}`
	req := httptest.NewRequest("POST", "/api/apprenticeships", strings.NewReader(body))
	setHeaders(req, "t-1", "gcid-admin")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	got := bus.Drain()
	if len(got) != 1 || got[0].Topic != "chora.wbl.apprenticeship.program_created.v1" {
		t.Errorf("expected program_created event, got %+v", got)
	}
}
