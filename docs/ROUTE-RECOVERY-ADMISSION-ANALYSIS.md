# Remaining route admission work — source analysis

The full legacy recovery cycle is complete (LEGACY-FULL-CYCLE-004). This
records the remaining route integration; it is not physical route acceptance.

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

## Reproduced admission and entry failures, 16 September

`route-admission-baseline-20260916`: six isolated native-guard cases, two
expected failures. A paused same-bundle route record prevents its own
`preflight:resume`/`resume`. The same unconditional rejection also masks a live
global owner. Different-bundle, running progress, unknown resource generation
and updater transaction refusals already hold.

The first narrow change passes the action and bundle to pending-domain
admission. Only a valid non-running resumable record for that same bundle can
be passed by the existing resume/preflight pair, and only with no route
resource generation present. Global ownership, updater admission and protected
recovery are still checked. This policy does not itself migrate CGI/CLI entry.

`route-entry-baseline-20260916`: all three new requirements fail with actual
Linux processes and the original CLI, replacing business functions with
harmless private-file fixtures. Direct CLI entry bypasses a foreign global
fence, has no coordinator owner and allows a detached descendant to write
after returning. These are independent of the original user's unknown crash.

## Next integration contract

- Reuse the native descendant supervisor with a distinct protected-route
  registration. User cancellation files must not interrupt this mode; internal
  failure/timeouts still retain recoverable evidence.
- A CLI wrapper owns one coordinator job. Recursive plan/export CLI calls
  reuse it only after proving actual membership in its traced process tree.
  Inherited environment variables alone do not grant permission.
- Web route entry must cover the whole mutation, including CGI state writes.
  Replaying a validated CGI request must preserve its input body and perform
  authentication before admission. A child must not acquire a second global
  fence. Do not migrate only the inner CLI and leave CGI writers untracked.
- Bind resource generations to that exact operation/supervisor. Recovery
  requires executor and descendant absence and the matching generation; old
  or ambiguous route records remain preserved with a reason.
- Preserve existing progress, transaction backups and rollback markers. The
  route plan already refuses unregistered matching routes as external; do not
  silently adopt those routes while clearing an operation fence.
- Audit the shared ndmc reader lane and its PID-based timeout paths before
  physical route acceptance. A new global job alone does not fix these paths.
