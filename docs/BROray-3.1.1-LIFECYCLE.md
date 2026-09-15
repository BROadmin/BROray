# Lifecycle audit and local implementation decisions

2026-09-15. Exact runtime baseline: local commit `6951446`, application archive `635e905d0fb31aff84cd92026dfed21ad204ea45c8d0a5562fecb248178b32e8`.

## Entry points and ownership

| Entry | Guard/state owner | Nested work | Completion and recovery boundary |
| --- | --- | --- | --- |
| servers/*.cgi mutation | servers/common → routes-api-operation | child shell → server-service → probe or runtime activation | API success/error and EXIT release; probe differs from persistent Xray |
| subscriptions/*.cgi mutation | subscriptions/common → routes-api-operation | subscription-service per-subscription lock, staging and server merge | child failure/normal return; publication/merge is protected |
| S28 subscription scheduler | five-file system lock; daemon PID + separate start file | scheduler_once → subscription updates | EXIT trap and end-of-cycle; old stale reclaimer rejects system scope |
| S27 auto-switch | five-file system lock + local auto-switch lock | quality refresh → server checks, then activation | HUP/INT/TERM and normal cycle release; no universal EXIT trap |
| routes CGI | routes-api-operation + per-bundle progress | route CLI, network/router helpers | explicit stop produces resumable progress; absent PID does not remove logical reservation |
| DoT, custom routes, Keenetic writes | same routes coordinator | target-specific configuration commits | classify as protected until domain-specific cancellation is wired |
| Xray web install/update/reinstall | routes coordinator, then PID handoff to background worker | version/config validation, runtime replacement | preserve rollback and live Xray distinction; generic background stop cannot kill this worker |
| updater/reinstall | updater request.lock and durable operation store | compact slot transaction + platform handoff | global fence is read-only admission; explicit existing transaction recovery retained |
| legacy OPKG/broray-system | owner-identity.tsv, native OPKG lock-backed control mutex | backup, transaction, rollback | continue using existing contract; not a background cancellation target |
| status/report | read snapshots only | no probe or router refresh | registry failure is unknown, never an empty active list |

The exact global-lock search inventory is saved in the parent workspace. Every shared-fence creator is either the routes coordinator, the two scheduler binaries, Xray worker handoff, or the protected legacy system code. Per-subscription locks, probe children, route progress, local auto-switch lock, service singleton PID files, and updater request.lock remain additional lifecycle objects. Existing cleanup paths must not be reused blindly.

## Important findings beyond the initial defect

1. The old routes reclaimer uses unsuccessful `kill -0` as proof of death. Synthetic tests reproduce that behaviour for a denied liveness check. New classification requires a consistent proc identity and distinguishes unavailable from absent.
2. Subscription `list` performs stale-state writes. It must not independently clean a managed operation while the global coordinator is repairing it.
3. The old S27 stop and probe cleanup use numeric PID signalling. A new recovery button must not call those code paths as an identity-safe cancellation API.
4. The existing package transaction control mutex uses a native OPKG lock. It is appropriate for its transaction but too coupled to use for every heartbeat or journal record.
5. A second mkdir mutex would itself have an owner-publication/crash window. Read/hash/unlink cannot safely arbitrate two recovery attempts and a new writer.

## Chosen local implementation boundary

Keep the existing operation store and the updater-visible global fence pathname. Serialize short state changes with a small bundled native guard using kernel advisory record locking. Its lock is released by the kernel on process death; a remaining regular lock file does not mean busy. The guard is a short-lived executable, not a daemon, and does not use the external `flock` command or OPKG lock. This is a narrowly scoped addition required by the crash/race contract, not a rewrite of scheduler.

The Linux contract is documented in [fcntl_locking(2)](https://man7.org/linux/man-pages/man2/fcntl_locking.2.html): locks survive exec and are released on process termination. All application producers must enter the guard for acquire/release/recovery. Updater keeps its pre/post admission checks and never removes the global fence. No background process may inherit a protected transaction's authority by only reusing a PID or environment variable.

Owner publication consists of one schema-versioned JSON record, plus a compatibility PID projection for the existing updater. Mutable heartbeat and cancellation are separate records so they cannot overwrite owner identity or a terminal result. A caller cannot set arbitrary paths, PIDs, shell commands or signals through HTTP.

Prototype revision 2 closes the mkdir/owner publication gap for new-format fences: prepare all six fence files in the operation directory, fsync files and ancestor directories, then use the non-replacing `symlink(2)` syscall to publish the global name in one step. The old updater's exact classifier returns `unsafe-object` for this present symlink and does not mutate it; its exclusion remains effective. Existing old writers using mkdir also cannot take the occupied name. Recovery accepts only the exact registry-owned target and matching immutable owner record; foreign symlinks are rejected. New-format dangling/invalid targets remain ambiguous. Legacy five-file directories still require a separate migration procedure. Power-loss durability on the target filesystem is a physical gate.

Publication and job-start acknowledgement are separate problems. The prototype has not yet completed the begin/ack/retry protocol, so it stays disconnected from long-lived schedulers. A failed response from begin must be retried/adopted with the same private launch nonce and exact owner identity before any job work starts. See the explicit integration gaps in `PRE-ROUTER-STATUS.md`.

Signal escalation remains disabled unless the target is a managed helper with a supported safe process handle. Classification and repeated proc reads do not by themselves authorize `kill(PID)`. Prefer cooperative cancellation and bounded isolated helpers. Domain commits, updater, Xray replacement and resumable route operations remain protected.

## Required pre-router deliverables

- Exact baseline and failing-case reproducer: prepared, five local cases PASS.
- Kernel guard source and cross builds with explicit architecture metadata; target capability check required before enablement.
- Owner schema/classifier and serialized acquire/recovery with hostile-state/race tests.
- Cooperative cancellation integration and supported helper escalation; unsupported modes remain explicit.
- Sanitized structured events, bounded history, authenticated report and UI.
- Reproducible local preparation bundle, manifest and read-only router preflight; no uploader or automatic router target in local test entry points.

The originating user's process failure is still `ROOT_CAUSE_NOT_PROVEN`. Physical boot semantics, networking, target binaries and delivery through the already-blocked 3.1.0 UI remain untested until the router is released.
