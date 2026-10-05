package inmem_test

import (
	"testing"

	"github.com/apollo-chora/chora-wbl/internal/adapter/inmem"
)

// RED — NewStore composes all per-aggregate repos as live instances.
func TestNewStore_ComposesRepos(t *testing.T) {
	t.Parallel()
	s := inmem.NewStore()
	if s.Internships == nil {
		t.Error("Internships repo is nil")
	}
	if s.Applications == nil {
		t.Error("Applications repo is nil")
	}
	if s.Apprenticeships == nil {
		t.Error("Apprenticeships repo is nil")
	}
	if s.Employers == nil {
		t.Error("Employers repo is nil")
	}
	if s.Roster == nil {
		t.Error("Roster is nil")
	}
	if s.Milestones == nil {
		t.Error("Milestones repo is nil")
	}
	if s.Evidence == nil {
		t.Error("Evidence repo is nil")
	}
}
