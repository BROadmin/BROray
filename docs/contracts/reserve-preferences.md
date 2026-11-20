# Reserve roster preferences

`GET /api/servers/reserve-settings.cgi` returns `schemaVersion`, `enabled`,
`primaryServerId`, `backupServerIds`, `configured` and a content `revision`.
An absent settings file means OFF with an empty selection; GET creates no
file. `configured` means that the selection contains a primary and at least
one backup, not that those servers have passed runtime readiness checks.

`POST` accepts `expectedRevision` and either or both roster fields. It uses
the existing operations header and same-origin guard, a 4 KiB request limit,
strict JSON decoding and the global operation fence. Each partial write
preserves the other selection. A stale revision or busy fence returns 409;
duplicate/case-aliased/unknown keys, null values, invalid IDs, duplicate
backups, primary/backup overlap, more than two backups or unavailable server
records are refused. The primary and backup selections can be made in either
order while the feature is off. `enabled` is not writable through this API.

Preferences are stored in `config/system/server-reserve.json`, schema 1.
The file is read without following directory or file links and is limited to
4 KiB. Saves use durable atomic replacement and readback under the existing
global fence. Invalid or ambiguous existing files are preserved. An uncertain
write reports `CONFIG_SAVE_UNCONFIRMED`; clients must read back rather than
retry automatically. A fresh service instance reads the same saved selection.

This endpoint does not change ordinary autoswitch/quality settings, apply
Xray configuration, rename an interface or restart VPN. Changing the roster
while the stored reserve mode is enabled is refused with `RESERVE_MODE_ACTIVE`
until the queued lifecycle operation is integrated.

## Backup compatibility

The servers backup part includes an optional `reserve` field only when a
preferences file exists. Inactive rosters are validated against the final
staged server set and written through the existing backup transaction.
Staging never edits live settings. Older backups without this field preserve
the existing selection; empty/null legacy sections retain their prior
semantics, including whitespace around JSON null.

The new field rejects ambiguous aliases/duplicates without imposing its
small file limit on an entire legacy server backup. Current or imported
enabled reserve mode is explicitly blocked at restore preparation until
the verified lifecycle/restore integration is implemented. Export remains
read-only. This temporary implementation gate must be resolved or explicitly
accounted for before reserve mode is exposed as ready.

## Remaining integration

Enabling/disabling, active roster changes and restoring an enabled mode need
the existing queued operation and ownership/readback/rollback checks. The
ordinary autoswitch must be excluded while reserve owns switching, without
discarding the user's ordinary settings. The UI, scheduler, restart recovery
and physical Keenetic tests remain separate gates. API tests use the local
CGI harness and Linux ARM64 VM; they do not prove physical behavior.
