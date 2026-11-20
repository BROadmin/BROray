# Catalog schedule settings

The existing `catalog-schedule.cgi` GET returns `settingsValid` and a revision
from the same validated settings snapshot. Unknown/malformed stored settings
are displayed as unconfirmed and cannot be overwritten through this endpoint.
Missing settings retain the safe defaults. Reads/writes retain the platform's
no-follow file handling; changes hold the existing global operation fence.

Legacy POST bodies with intervalHours, autoApply and optional maxRemovalPercent
remain accepted. New clients send exactly one of those fields plus
expectedRevision. Stale snapshots return ROUTES_SCHEDULE_CHANGED. Duplicate,
unknown, null and incorrectly typed fields are refused. Changes do not start a
router mutation or replace the other saved settings.

Each control saves independently. A dirty neighbouring control survives status
refreshes and other setting saves. If another client changed that same field,
the draft is preserved and the user must re-enter a value after the conflict.
Lost replies cause readback, never a repeated write. Unconfirmed readback locks
settings until valid status arrives. Older polls and manual-check responses
cannot overwrite a later preference save. The existing manual check and
scheduled update/transaction/ownership behavior remain in place.

Evidence is local Linux ARM64, actual-controller Node tests and the extracted
shipped widget with a loopback fake API. This does not replace whole-page,
real-backend browser or physical Keenetic acceptance.
