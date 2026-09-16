# Remaining route admission work — source analysis

This records the next bounded step after the full legacy recovery cycle. It
does not change runtime behavior or claim a passing route recovery test.

## Observed paths

- `routes-web-action.sh` and `routes/preflight.cgi` acquire the old global
  five-file fence before invoking the existing export/delete/resume flow.
- `routes-api-operation.sh` now refuses pre-existing generations, but release
  still uses the old PID/scope check. These calls are not coordinator jobs.
- Direct `broray-routes` also lacks coordinator admission. Its eight resource
  consumers have generation tokens and full owners, but do not bind a retained
  resource lock to a coordinator job or supervised descendant registry.
- `ops_pending_domain` blocks every running or resumable route progress record.
  It needs a narrowly defined exception for the same bundle's existing resume
  preflight and resume, while unrelated work stays blocked.
- `ops_recover_global` preserves protected commits even after proving the owner
  and registered helpers gone. Route-specific admission must preserve the saved
  route progress and use the existing repair controls.
- `ops_supervisor_register` currently refuses all protected/route work. The
  native helper already tracks descendants with ptrace and drains them after
  timeout or owner death. User route cancellation must remain prohibited if a
  separate protected registration contract is introduced.
- `routes-operation-progress.sh` derives interrupted display state using the
  old PID/resource projection. It must not become authority for lock removal.

## Implementation boundary

Use the existing coordinator and descendant supervisor to establish ownership
for the whole route command, including direct CLI entry. Bind the route resource
generation to that job before any route write. Recover only after matching that
binding and proving its executor and descendants absent. Keep unknown legacy
resource locks blocked with an explanation. Do not add a route rollback engine,
new route UI actions, automatic continuation, or user cancellation.

Before runtime edits, reproduce admission failure for an interrupted same-bundle
operation, successful existing restore/resume under an owned lease, refusal for
a different bundle or a live descendant, and CLI/WebUI contention. A local test
is not physical Keenetic route/VPN acceptance.
