-- Seed the note:read, note:create, and note:manage permission rows and
-- backfill them onto existing built-in roles. The catalog reconciler
-- upserts new permission rows on startup, but it does NOT re-assert seed
-- permissions onto already-seeded ADMIN/FIELD_TECH roles (additive mode,
-- see reconcile.go). Without this migration, deployments upgraded from a
-- release prior to this one would never grant the new note keys, so the
-- NoteService endpoints would silently deny.
--
-- SUPER_ADMIN is reconciled in full mode at boot and converges on its
-- own. Unlike the pool/activity seeds, FIELD_TECH IS backfilled here
-- (read + create only): the notepad is an org-shared surface every team
-- member is expected to read and post to.

INSERT INTO permission (key, description) VALUES
    ('note:read',   'View the shared team notepad.'),
    ('note:create', 'Add notes to the shared team notepad and edit or delete your own notes.'),
    ('note:manage', 'Delete any note on the shared team notepad.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- Scoped to builtin_key so operator-created custom roles aren't touched.
-- ON CONFLICT makes this safe to replay against orgs that already hold
-- any of the keys.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key IN ('note:read', 'note:create', 'note:manage')
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'FIELD_TECH'
  AND r.deleted_at IS NULL
  AND p.key IN ('note:read', 'note:create')
ON CONFLICT (role_id, permission_id) DO NOTHING;
