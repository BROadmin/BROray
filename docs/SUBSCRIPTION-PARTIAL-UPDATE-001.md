# Partial subscription update — stage 08

Target: current commit 725c53f3ae2077e9f1abb8db0129554abc59c320 plus prepared stages 01–07.
No router or working checkout is changed by preparation. This is ready code, not a release.

## Policy
The owning subscription update chooses retain-unmatched when its validated preparation
reports rejected > 0. A rejected row has no trustworthy identity and is not a deletion.
The mode is an internal fifth sync argument, not a provider directive or public API setting.
Four-argument internal callers retain the historical replace default. Unknown modes fail.

Valid parsed nodes are added/updated using the existing importKey and stable-ID rules.
All previous nodes of THIS subscription whose keys are absent from the accepted set are
copied byte-for-byte into a private merged set, before any live catalog writes.
The original prepared directory is not modified. Existing validation, owner admission,
backup, active-server handling and commit/rollback paths remain in control.

A missing active node survives a partial response without changing its ID or reconnecting it.
An active node that WAS parsed still follows the existing config-validation/apply rules.
Manual nodes and other subscriptions are outside the retention set. Enabled/disabled target
placement follows the existing subscription setting. Retained file contents are unchanged;
no fresh observation timestamps are fabricated for old nodes.

The union is bounded by BRORAY_SUB_MAX_NODES (default and maximum 500). Exceeding the bound
fails with PARTIAL_UPDATE_LIMIT before live writes; it does not evict old nodes to make room.
Ambiguous old keys, unsafe retained files or filename/ID collisions fail with
SERVER_SYNC_CONFLICT, also before live writes. The existing restoration path handles
subsequent commit errors; this patch does not claim to redesign crash/power-loss recovery.

## Result fields
Existing received/parsed/accepted/rejected and added/updated/unchanged retain their meanings.
retained counts unmatched old nodes kept separately, not nodes parsed from the new response.
catalogTotal (sync: total) and subscription.serversReceived count the resulting owned set.
removed is zero in retain-unmatched mode. deletionPolicy records the internal mode.
WebUI shows a separate retained count plus a warning that those parameters may be outdated.
The result status remains partial; a saved node is NOT represented as freshly validated.

## Deliberate limits
Any rejected node, including a duplicate, activates conservative retention. The existing
importer does not yet distinguish every harmless duplicate from an incomplete parse.
No guess maps a broken line to an old node by name or line number.
A response with zero accepted nodes remains an error and leaves the prior catalog untouched.
A subsequent fully parsed response (zero rejections) uses ordinary reconciliation again;
missing inactive nodes may be removed, while the existing active-node conflict protection stays.
A truncated-but-valid response with no rejected rows cannot be identified by this policy.
There is no automatic expiration/removal of retained nodes during repeated partial replies.

## Tests and integration
Direct sync tests execute actual shell/filesystem code but replace owner and live Xray hooks.
Protected tests execute real Linux owner/guard/supervisor, parser, synchronization and commit;
only HTTP is synthetic, and the guest has no network device. Pure UI tests execute the
actual result-rendering functions, not the full page/browser shell.
No provider, physical router, real tunnel or complete multi-page WebUI acceptance is claimed.
The primary CUMULATIVE-CURRENT patch applies all stages 01–08 once to clean pinned HEAD.
The standalone stage08 patch requires exact stage07 cumulative bytes first.
The production updater, server IDs/schema, and existing async/cancel/progress code are preserved.
