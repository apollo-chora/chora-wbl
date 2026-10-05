// Package inmem composes the in-memory aggregate stores for chora-wbl.
//
// All five domain aggregates own a tenant-scoped repo; this composite is
// what the HTTP adapter holds as its single store dependency. M12 swaps
// these for a Postgres adapter package targeting chora_delivery DB
// (WBL is a sub-context of Content Delivery per the locked architecture).
package inmem

import (
	"github.com/apollo-chora/chora-wbl/internal/domain/apprenticeship"
	"github.com/apollo-chora/chora-wbl/internal/domain/employer"
	"github.com/apollo-chora/chora-wbl/internal/domain/evidence"
	"github.com/apollo-chora/chora-wbl/internal/domain/internship"
	"github.com/apollo-chora/chora-wbl/internal/domain/milestone"
)

// Store bundles per-aggregate repos.
type Store struct {
	Internships     *internship.ListingRepo
	Applications    *internship.ApplicationRepo
	Apprenticeships *apprenticeship.ProgramRepo
	Employers       *employer.Repo
	Roster          *employer.Roster
	Milestones      *milestone.Repo
	Evidence        *evidence.Repo
}

// NewStore returns a Store with empty repos.
func NewStore() *Store {
	return &Store{
		Internships:     internship.NewListingRepo(),
		Applications:    internship.NewApplicationRepo(),
		Apprenticeships: apprenticeship.NewProgramRepo(),
		Employers:       employer.NewRepo(),
		Roster:          employer.NewRoster(),
		Milestones:      milestone.NewRepo(),
		Evidence:        evidence.NewRepo(),
	}
}
