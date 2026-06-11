-- The shared team notepad: one org-wide feed of notes every member can
-- read and post to. Rows are soft-deleted; authorship (user_id) drives
-- the author-only edit/delete rule enforced in the domain layer.
CREATE TABLE note (
    id         BIGSERIAL PRIMARY KEY,
    org_id     BIGINT NOT NULL,
    user_id    BIGINT NOT NULL,
    content    TEXT   NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ NULL,

    CONSTRAINT fk_note_organization FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    -- Users are soft-deleted, never hard-deleted; RESTRICT keeps a
    -- deactivated author's notes attributable.
    CONSTRAINT fk_note_user FOREIGN KEY (user_id)
        REFERENCES "user"(id) ON DELETE RESTRICT
);

-- Matches the keyset predicate (org_id, created_at, id) used by ListNotes.
CREATE INDEX idx_note_org_feed
    ON note (org_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE TRIGGER update_note_updated_at
    BEFORE UPDATE ON note
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
