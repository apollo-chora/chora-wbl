-- chora-wbl: initial schema
--
-- Per the locked architecture, WBL is a sub-context of Content Delivery and
-- lives in the chora_delivery DB at M12. This migration is the placeholder
-- skeleton; the M12 hand-off will move these tables into chora_delivery.{schema}
-- with RLS policies + PgBouncer connection pool.
--
-- Cross-DB queries are FORBIDDEN per .claude/rules/ddd-enforcement.md —
-- inter-domain communication is via event-bus (NATS JetStream) events only.

-- Internship listings.
CREATE TABLE IF NOT EXISTS wbl_internship_listing (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL,
    employer_id     UUID NOT NULL,
    title           TEXT NOT NULL,
    description     TEXT,
    duration_weeks  INT NOT NULL CHECK (duration_weeks > 0),
    skills          TEXT[] DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'open',
    posted_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_wbl_internship_tenant ON wbl_internship_listing (tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_wbl_internship_employer ON wbl_internship_listing (employer_id) WHERE deleted_at IS NULL;

-- Applications.
CREATE TABLE IF NOT EXISTS wbl_internship_application (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL,
    listing_id      UUID NOT NULL,
    gcid            UUID NOT NULL,
    motivation      TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'submitted',
    reject_reason   TEXT,
    submitted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wbl_application_listing ON wbl_internship_application (listing_id);
CREATE INDEX IF NOT EXISTS idx_wbl_application_gcid ON wbl_internship_application (gcid);

-- Apprenticeship programs.
CREATE TABLE IF NOT EXISTS wbl_apprenticeship_program (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL,
    employer_id     UUID NOT NULL,
    title           TEXT NOT NULL,
    description     TEXT,
    duration_months INT NOT NULL CHECK (duration_months >= 2),
    focus_areas     TEXT[] DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'open',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wbl_apprenticeship_tenant ON wbl_apprenticeship_program (tenant_id);

-- Employers.
CREATE TABLE IF NOT EXISTS wbl_employer (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL,
    name            TEXT NOT NULL,
    contact_email   TEXT,
    sector          TEXT,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_wbl_employer_tenant ON wbl_employer (tenant_id) WHERE deleted_at IS NULL;

-- Roster.
CREATE TABLE IF NOT EXISTS wbl_employer_roster (
    tenant_id       UUID NOT NULL,
    employer_id     UUID NOT NULL,
    gcid            UUID NOT NULL,
    placement_kind  TEXT NOT NULL,
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, employer_id, gcid, placement_kind)
);

-- Milestones.
CREATE TABLE IF NOT EXISTS wbl_milestone (
    id                  UUID PRIMARY KEY,
    tenant_id           UUID NOT NULL,
    placement_id        TEXT NOT NULL,
    title               TEXT NOT NULL,
    summary             TEXT,
    competencies        TEXT[] DEFAULT '{}',
    status              TEXT NOT NULL DEFAULT 'pending',
    employer_sign_gcid  UUID,
    employer_sign_note  TEXT,
    employer_signed_at  TIMESTAMPTZ,
    student_sign_gcid   UUID,
    student_sign_note   TEXT,
    student_signed_at   TIMESTAMPTZ,
    complete_note       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    signed_off_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_wbl_milestone_placement ON wbl_milestone (placement_id);

-- Evidence.
CREATE TABLE IF NOT EXISTS wbl_evidence (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL,
    gcid            UUID NOT NULL,
    milestone_id    UUID NOT NULL,
    kind            TEXT NOT NULL,
    title           TEXT,
    body            TEXT NOT NULL,
    attachments     TEXT[] DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'submitted',
    review_gcid     UUID,
    review_note     TEXT,
    reviewed_at     TIMESTAMPTZ,
    submitted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wbl_evidence_milestone ON wbl_evidence (milestone_id);
CREATE INDEX IF NOT EXISTS idx_wbl_evidence_gcid ON wbl_evidence (gcid);
