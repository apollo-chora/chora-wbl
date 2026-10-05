// Package httpapi is the HTTP/JSON adapter for chora-wbl.
//
// Hexagonal layout: this layer depends on the domain packages
// (internship / apprenticeship / employer / milestone / evidence) and the
// in-memory store (inmem). The domain layer NEVER imports anything from
// this package.
//
// Endpoints (CHO-23 — ~12 routes):
//
//	GET    /healthz   /readyz   /health                 (liveness/readiness)
//	POST   /api/internships                             (post listing)
//	GET    /api/internships                             (list tenant listings)
//	POST   /api/internships/{id}/applications           (apply)
//	POST   /api/applications/{id}/accept                (accept application)
//	POST   /api/applications/{id}/reject                (reject application)
//	POST   /api/apprenticeships                         (create program)
//	GET    /api/apprenticeships                         (list programs)
//	POST   /api/employers                               (create employer)
//	GET    /api/employers/{id}/roster                   (roster view)
//	POST   /api/milestones                              (create milestone)
//	POST   /api/milestones/{id}/complete                (mark complete)
//	POST   /api/milestones/{id}/sign-off-employer       (employer sign-off)
//	POST   /api/milestones/{id}/sign-off-student        (student sign-off)
//	POST   /api/evidence                                (submit evidence)
//	POST   /api/evidence/{id}/approve                   (approve evidence)
//	POST   /api/evidence/{id}/reject                    (reject evidence)
//
// Middleware extracts X-Tenant-Id + gcid from request headers and rejects
// requests missing them on protected endpoints. Health endpoints bypass.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/apollo-chora/chora-wbl/internal/adapter/inmem"
	"github.com/apollo-chora/chora-wbl/internal/domain/apprenticeship"
	"github.com/apollo-chora/chora-wbl/internal/domain/employer"
	"github.com/apollo-chora/chora-wbl/internal/domain/evidence"
	"github.com/apollo-chora/chora-wbl/internal/domain/internship"
	"github.com/apollo-chora/chora-wbl/internal/domain/milestone"
	"github.com/apollo-chora/chora-wbl/internal/events"
	"github.com/apollo-chora/chora-wbl/internal/observability"
)

// Deps wires the in-memory store + event bus into the HTTP adapter.
type Deps struct {
	Store *inmem.Store
	Bus   *events.Bus
}

// NewServer builds the routed http.Handler.
func NewServer(deps Deps) http.Handler {
	mux := http.NewServeMux()

	// Health.
	mux.HandleFunc("/healthz", logging(healthHandler))
	mux.HandleFunc("/healthz/", logging(healthHandler))
	mux.HandleFunc("/health", logging(healthHandler))
	mux.HandleFunc("/readyz", logging(readyHandler))
	mux.HandleFunc("/readyz/", logging(readyHandler))

	// Internships.
	mux.HandleFunc("/api/internships", logging(actorRequired(internshipsHandler(deps))))
	mux.HandleFunc("/api/internships/", logging(actorRequired(internshipSubHandler(deps))))

	// Applications (state machine).
	mux.HandleFunc("/api/applications/", logging(actorRequired(applicationSubHandler(deps))))

	// Apprenticeships.
	mux.HandleFunc("/api/apprenticeships", logging(actorRequired(apprenticeshipsHandler(deps))))

	// Employers + roster.
	mux.HandleFunc("/api/employers", logging(actorRequired(employersHandler(deps))))
	mux.HandleFunc("/api/employers/", logging(actorRequired(employerSubHandler(deps))))

	// Milestones.
	mux.HandleFunc("/api/milestones", logging(actorRequired(milestonesHandler(deps))))
	mux.HandleFunc("/api/milestones/", logging(actorRequired(milestoneSubHandler(deps))))

	// Evidence.
	mux.HandleFunc("/api/evidence", logging(actorRequired(evidenceHandler(deps))))
	mux.HandleFunc("/api/evidence/", logging(actorRequired(evidenceSubHandler(deps))))

	return mux
}

// -- middleware ----------------------------------------------------------

func logging(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		tc := observability.FromRequest(r)
		w.Header().Set("traceparent", tc.TraceparentHeader())
		sw := &statusWriter{ResponseWriter: w}
		next(sw, r)
		observability.LogRequest(
			tc,
			r.Header.Get("X-Tenant-Id"),
			r.Header.Get("gcid"),
			r.Method,
			r.URL.Path,
			sw.status,
			time.Since(start),
		)
	}
}

func actorRequired(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("X-Tenant-Id")) == "" {
			writeErr(w, http.StatusBadRequest, "X-Tenant-Id required")
			return
		}
		if strings.TrimSpace(r.Header.Get("gcid")) == "" {
			writeErr(w, http.StatusBadRequest, "gcid required")
			return
		}
		next(w, r)
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(p []byte) (int, error) {
	if sw.status == 0 {
		sw.status = http.StatusOK
	}
	return sw.ResponseWriter.Write(p)
}

// -- health --------------------------------------------------------------

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func readyHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// -- internships ---------------------------------------------------------

type internshipCreateBody struct {
	EmployerID    string   `json:"employer_id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	DurationWeeks int      `json:"duration_weeks"`
	Skills        []string `json:"skills"`
}

func internshipsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("X-Tenant-Id")
		gcid := r.Header.Get("gcid")
		switch r.Method {
		case http.MethodPost:
			var body internshipCreateBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			l, err := internship.NewListing(tenantID, body.EmployerID, body.Title, body.Description, body.DurationWeeks, body.Skills)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			deps.Store.Internships.Save(l)
			env := events.NewEnvelope(r.Header.Get("traceparent"), "", tenantID, gcid)
			deps.Bus.Publish("chora.wbl.internship.listing_created.v1", env, map[string]interface{}{
				"listing_id":  l.ID,
				"employer_id": l.EmployerID,
			})
			writeJSON(w, http.StatusCreated, l)
		case http.MethodGet:
			out := deps.Store.Internships.ListByTenant(tenantID)
			writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func internshipSubHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Sub-paths: /api/internships/{id}/applications  (POST)
		path := strings.TrimPrefix(r.URL.Path, "/api/internships/")
		parts := strings.Split(path, "/")
		if len(parts) != 2 || parts[1] != "applications" {
			writeErr(w, http.StatusNotFound, "unknown sub-path")
			return
		}
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		tenantID := r.Header.Get("X-Tenant-Id")
		gcid := r.Header.Get("gcid")
		listingID := parts[0]

		// Verify listing exists + is open.
		listing, ok := deps.Store.Internships.Get(tenantID, listingID)
		if !ok {
			writeErr(w, http.StatusNotFound, "listing not found")
			return
		}
		if listing.Status != internship.StatusOpen {
			writeErr(w, http.StatusConflict, "listing not open")
			return
		}

		var body struct {
			Motivation string `json:"motivation"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		app, err := internship.NewApplication(tenantID, listingID, gcid, body.Motivation)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		deps.Store.Applications.Save(app)
		env := events.NewEnvelope(r.Header.Get("traceparent"), "", tenantID, gcid)
		deps.Bus.Publish("chora.wbl.internship.application_submitted.v1", env, map[string]interface{}{
			"application_id": app.ID,
			"listing_id":     app.ListingID,
		})
		writeJSON(w, http.StatusCreated, app)
	}
}

func applicationSubHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Sub-paths:
		//   /api/applications/{id}/accept (POST)
		//   /api/applications/{id}/reject (POST)
		path := strings.TrimPrefix(r.URL.Path, "/api/applications/")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			writeErr(w, http.StatusNotFound, "unknown sub-path")
			return
		}
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		tenantID := r.Header.Get("X-Tenant-Id")
		gcid := r.Header.Get("gcid")
		appID := parts[0]
		action := parts[1]

		app, ok := deps.Store.Applications.Get(tenantID, appID)
		if !ok {
			writeErr(w, http.StatusNotFound, "application not found")
			return
		}

		switch action {
		case "accept":
			if err := app.Accept(); err != nil {
				if errors.Is(err, internship.ErrInvalidTransition) {
					writeErr(w, http.StatusConflict, err.Error())
					return
				}
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			deps.Store.Applications.Save(app)
			env := events.NewEnvelope(r.Header.Get("traceparent"), "", tenantID, gcid)
			deps.Bus.Publish("chora.wbl.internship.application_accepted.v1", env, map[string]interface{}{
				"application_id": app.ID,
				"listing_id":     app.ListingID,
				"applicant_gcid": app.GCID,
			})
			writeJSON(w, http.StatusOK, app)
		case "reject":
			var body struct {
				Reason string `json:"reason"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			if err := app.Reject(body.Reason); err != nil {
				if errors.Is(err, internship.ErrInvalidTransition) {
					writeErr(w, http.StatusConflict, err.Error())
					return
				}
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			deps.Store.Applications.Save(app)
			env := events.NewEnvelope(r.Header.Get("traceparent"), "", tenantID, gcid)
			deps.Bus.Publish("chora.wbl.internship.application_rejected.v1", env, map[string]interface{}{
				"application_id": app.ID,
				"reason":         body.Reason,
			})
			writeJSON(w, http.StatusOK, app)
		default:
			writeErr(w, http.StatusNotFound, "unknown action")
		}
	}
}

// -- apprenticeships -----------------------------------------------------

type apprenticeshipCreateBody struct {
	EmployerID     string   `json:"employer_id"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	DurationMonths int      `json:"duration_months"`
	FocusAreas     []string `json:"focus_areas"`
}

func apprenticeshipsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("X-Tenant-Id")
		gcid := r.Header.Get("gcid")
		switch r.Method {
		case http.MethodPost:
			var body apprenticeshipCreateBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			p, err := apprenticeship.NewProgram(tenantID, body.EmployerID, body.Title, body.Description, body.DurationMonths, body.FocusAreas)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			deps.Store.Apprenticeships.Save(p)
			env := events.NewEnvelope(r.Header.Get("traceparent"), "", tenantID, gcid)
			deps.Bus.Publish("chora.wbl.apprenticeship.program_created.v1", env, map[string]interface{}{
				"program_id":  p.ID,
				"employer_id": p.EmployerID,
			})
			writeJSON(w, http.StatusCreated, p)
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"items": deps.Store.Apprenticeships.ListByTenant(tenantID),
			})
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// -- employers + roster --------------------------------------------------

type employerCreateBody struct {
	Name         string `json:"name"`
	ContactEmail string `json:"contact_email"`
	Sector       string `json:"sector"`
}

func employersHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("X-Tenant-Id")
		gcid := r.Header.Get("gcid")
		switch r.Method {
		case http.MethodPost:
			var body employerCreateBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			e, err := employer.NewEmployer(tenantID, body.Name, body.ContactEmail, body.Sector)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			deps.Store.Employers.Save(e)
			env := events.NewEnvelope(r.Header.Get("traceparent"), "", tenantID, gcid)
			deps.Bus.Publish("chora.wbl.employer.created.v1", env, map[string]interface{}{
				"employer_id": e.ID,
			})
			writeJSON(w, http.StatusCreated, e)
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"items": deps.Store.Employers.ListByTenant(tenantID),
			})
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func employerSubHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Sub-paths: /api/employers/{id}/roster  (GET)
		path := strings.TrimPrefix(r.URL.Path, "/api/employers/")
		parts := strings.Split(path, "/")
		if len(parts) != 2 || parts[1] != "roster" {
			writeErr(w, http.StatusNotFound, "unknown sub-path")
			return
		}
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		tenantID := r.Header.Get("X-Tenant-Id")
		empID := parts[0]
		// Validate employer exists for this tenant.
		if _, ok := deps.Store.Employers.Get(tenantID, empID); !ok {
			writeErr(w, http.StatusNotFound, "employer not found")
			return
		}
		entries := deps.Store.Roster.List(tenantID, empID)
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": entries})
	}
}

// -- milestones ----------------------------------------------------------

type milestoneCreateBody struct {
	PlacementID  string   `json:"placement_id"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Competencies []string `json:"competencies"`
}

func milestonesHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("X-Tenant-Id")
		gcid := r.Header.Get("gcid")
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "only POST supported")
			return
		}
		var body milestoneCreateBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		m, err := milestone.NewMilestone(tenantID, body.PlacementID, body.Title, body.Summary, body.Competencies)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		deps.Store.Milestones.Save(m)
		env := events.NewEnvelope(r.Header.Get("traceparent"), "", tenantID, gcid)
		deps.Bus.Publish("chora.wbl.milestone.created.v1", env, map[string]interface{}{
			"milestone_id": m.ID,
			"placement_id": m.PlacementID,
		})
		writeJSON(w, http.StatusCreated, m)
	}
}

func milestoneSubHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Sub-paths:
		//   /api/milestones/{id}/complete            (POST)
		//   /api/milestones/{id}/sign-off-employer   (POST)
		//   /api/milestones/{id}/sign-off-student    (POST)
		path := strings.TrimPrefix(r.URL.Path, "/api/milestones/")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			writeErr(w, http.StatusNotFound, "unknown sub-path")
			return
		}
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "only POST supported")
			return
		}
		tenantID := r.Header.Get("X-Tenant-Id")
		gcid := r.Header.Get("gcid")
		mID := parts[0]
		action := parts[1]

		m, ok := deps.Store.Milestones.Get(tenantID, mID)
		if !ok {
			writeErr(w, http.StatusNotFound, "milestone not found")
			return
		}
		var body struct {
			Note string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
			// Empty body OK; only error on actual decode failure.
			if err.Error() != "EOF" {
				writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
		}

		var topic string
		switch action {
		case "complete":
			if err := m.MarkComplete(body.Note); err != nil {
				if errors.Is(err, milestone.ErrInvalidTransition) {
					writeErr(w, http.StatusConflict, err.Error())
					return
				}
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			topic = "chora.wbl.milestone.completed.v1"
		case "sign-off-employer":
			if err := m.SignOffEmployer(gcid, body.Note); err != nil {
				if errors.Is(err, milestone.ErrInvalidTransition) {
					writeErr(w, http.StatusConflict, err.Error())
					return
				}
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			topic = "chora.wbl.milestone.employer_signed.v1"
		case "sign-off-student":
			if err := m.SignOffStudent(gcid, body.Note); err != nil {
				if errors.Is(err, milestone.ErrInvalidTransition) {
					writeErr(w, http.StatusConflict, err.Error())
					return
				}
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			topic = "chora.wbl.milestone.signed_off.v1"
		default:
			writeErr(w, http.StatusNotFound, "unknown action")
			return
		}
		deps.Store.Milestones.Save(m)
		env := events.NewEnvelope(r.Header.Get("traceparent"), "", tenantID, gcid)
		deps.Bus.Publish(topic, env, map[string]interface{}{
			"milestone_id": m.ID,
			"placement_id": m.PlacementID,
			"status":       string(m.Status),
		})
		writeJSON(w, http.StatusOK, m)
	}
}

// -- evidence ------------------------------------------------------------

type evidenceCreateBody struct {
	MilestoneID string   `json:"milestone_id"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Attachments []string `json:"attachments"`
}

func evidenceHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("X-Tenant-Id")
		gcid := r.Header.Get("gcid")
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "only POST supported")
			return
		}
		var body evidenceCreateBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		e, err := evidence.NewEvidence(tenantID, gcid, body.MilestoneID, evidence.Kind(body.Kind), body.Title, body.Body, body.Attachments)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		deps.Store.Evidence.Save(e)
		env := events.NewEnvelope(r.Header.Get("traceparent"), "", tenantID, gcid)
		deps.Bus.Publish("chora.wbl.evidence.submitted.v1", env, map[string]interface{}{
			"evidence_id":  e.ID,
			"milestone_id": e.MilestoneID,
			"kind":         string(e.Kind),
		})
		writeJSON(w, http.StatusCreated, e)
	}
}

func evidenceSubHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Sub-paths:
		//   /api/evidence/{id}/approve  (POST)
		//   /api/evidence/{id}/reject   (POST)
		path := strings.TrimPrefix(r.URL.Path, "/api/evidence/")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			writeErr(w, http.StatusNotFound, "unknown sub-path")
			return
		}
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "only POST supported")
			return
		}
		tenantID := r.Header.Get("X-Tenant-Id")
		gcid := r.Header.Get("gcid")
		evID := parts[0]
		action := parts[1]

		e, ok := deps.Store.Evidence.Get(tenantID, evID)
		if !ok {
			writeErr(w, http.StatusNotFound, "evidence not found")
			return
		}
		var body struct {
			Note string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		var topic string
		switch action {
		case "approve":
			if err := e.Approve(gcid, body.Note); err != nil {
				if errors.Is(err, evidence.ErrInvalidTransition) {
					writeErr(w, http.StatusConflict, err.Error())
					return
				}
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			topic = "chora.wbl.evidence.approved.v1"
		case "reject":
			if err := e.Reject(gcid, body.Note); err != nil {
				if errors.Is(err, evidence.ErrInvalidTransition) {
					writeErr(w, http.StatusConflict, err.Error())
					return
				}
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			topic = "chora.wbl.evidence.rejected.v1"
		default:
			writeErr(w, http.StatusNotFound, "unknown action")
			return
		}
		deps.Store.Evidence.Save(e)
		env := events.NewEnvelope(r.Header.Get("traceparent"), "", tenantID, gcid)
		deps.Bus.Publish(topic, env, map[string]interface{}{
			"evidence_id":  e.ID,
			"milestone_id": e.MilestoneID,
			"status":       string(e.Status),
		})
		writeJSON(w, http.StatusOK, e)
	}
}

// -- helpers -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
