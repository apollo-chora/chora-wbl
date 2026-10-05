package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// White-box test (internal package): statusWriter.Write defaults the captured
// status to 200 when a handler writes without calling WriteHeader first. All
// production handlers reach Write via writeJSON (WriteHeader first), so this
// branch is exercised directly to keep it honest.
func TestStatusWriter_WriteDefaultsStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	n, err := sw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes written, got %d", n)
	}
	if sw.status != http.StatusOK {
		t.Errorf("expected default status %d, got %d", http.StatusOK, sw.status)
	}
	if rec.Body.String() != "hello" {
		t.Errorf("body not forwarded to underlying writer: %q", rec.Body.String())
	}
}

// WriteHeader forwards the code and latches it on the statusWriter.
func TestStatusWriter_WriteHeaderLatchesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	sw.WriteHeader(http.StatusCreated)
	if sw.status != http.StatusCreated {
		t.Errorf("expected latched status %d, got %d", http.StatusCreated, sw.status)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("underlying writer code = %d, want %d", rec.Code, http.StatusCreated)
	}
}
