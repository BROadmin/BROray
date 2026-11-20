# Optional VPN reserve: decision policy

`modules/reserve` is a deterministic, no-I/O reducer. It is not yet connected
to the server queue, API, UI, scheduler, persistent settings or router.
Its tests prove policy behavior in a Linux ARM64 VM, not router recovery.

`Advance(state, event, now, policy)` returns an independent state snapshot
and zero or one switch intent. The zero state is disabled. The roster has
one primary and one or two distinct backups, each bound to the SHA-256
fingerprint of its effective server configuration. No DIRECT intent exists.

## Timing and evidence

The approved defaults are active probes every 15 seconds, three consecutive
failed rounds, 12 seconds per control URL; primary recovery probes every
60 seconds, three consecutive successful rounds and a 10-minute return
guard. Emergency failover ignores that guard. Unknown last-switch history
starts a conservative guard at verified configuration/enable time.

Probe start times enforce cadence; overlapping or duplicate rounds are not
counted. Success or an unknown active verdict resets the failure count.
Failed or unknown primary recovery resets its success count. A long gap
(more than twice the interval plus two URL timeouts) breaks a series.

Internal freshness limits are 30 seconds for accepting a completed sample
and 120 seconds for backup readiness. These are initial engineering limits,
not a measured recovery-time promise. Emergency decisions additionally
require the active failure evidence to remain within the 30-second limit.
The adapter enforces actual per-URL deadlines; this reducer performs no probes.

Samples require the current server fingerprint and verified evidence.
Active samples also require the current runtime identity. Future, excessively
old, malformed or pre-configuration samples cannot authorize a switch.
Configuration, enable changes and confirmed lifecycle completion clear proof
and establish a new earliest probe-start boundary.

## Switch lifecycle

An intent includes a monotonic token, source runtime identity, destination
fingerprint, reason and issue time. Issuing it does not change the active
server. One unresolved intent blocks further intents and roster changes.
Only a verified matching completion can update active state; an ambiguous,
wrong-token, wrong-destination or temporally impossible result remains pending.
Verified rollback to the original server clears pending without changing the
last successful switch time. Disable retains the roster and connection, clears
probe history and suppresses new intents; an in-flight operation still needs
verified reconciliation, including after disabling.

## Required integration contract

- Serialize events and actual lifecycle operations under the existing queue
  and maintenance fences. Revalidate enabled state, pending token, source
  runtime, destination fingerprint and fresh readiness before any mutation.
- `Sample.Verified` is an adapter assertion, not a proof produced by this
  module. The adapter must establish process/listener ownership and actual
  proxied test traffic without an unintended direct route. A successful TCP
  connect or ping alone is insufficient.
- Confirm activation and rollback using actual configuration, process,
  listener and proxied traffic evidence. Uncertainty must not become success
  or release an unresolved operation for another switch.
- Treat process restart and restored state as a reconciliation boundary.
  Persisted counters/readiness alone must never authorize actions. Revalidate
  settings and runtime, clear old samples, and reconcile outstanding intents.
- Keep the working backup active during primary probes. Update only the
  owned interface description to `BROray - mix-auto`; preserve ProxyN identity.
  Disabling restores the description for the current server without forcing
  a switch or restart.

The existing ordinary autoswitch is unchanged. Verified probe adapters,
persistence/API/UI, interface naming, measured latency and physical Keenetic
tests remain required before the optional feature can be declared ready.

## Linux process/listener evidence

`CaptureProcess` records the real procfs process's boot ID, start ticks and
executable device/inode, checking the expected executable path. `VerifyListener`
holds the PID directory, requires that identity to remain unchanged, and
associates the endpoint's listening socket inode with an FD of that process.
It repeats the identity/socket checks before returning. Network-table reads
are capped at 2 MiB per family and FD enumeration at 4096 entries; exceeding
those bounds refuses evidence rather than accepting partial observations.

Exact IPv4/IPv6 and same-family wildcard bindings are supported. Multiple
matching sockets are ambiguous even if owned by one process. An IPv6 wildcard
on an IPv4 endpoint's port is conservatively refused because procfs alone
does not establish its `IPV6_V6ONLY` setting. Native 32-bit address word order
is used instead of assuming little endian. Missing procfs data, changed
identity, no owned listener or a closed socket never means readiness.

This is a read-only snapshot, not a lock on future process behavior. It
performs no signals, cleanup or configuration mutation. It must be used with
the existing lifecycle fence and before/after actual proxied traffic checks;
the adapter must separately bind the effective configuration and route.
The helper does not set `Sample.Verified` by itself. Tests use real local
listeners and a child fixture inside the offline Linux ARM64 VM; they do not
establish physical Keenetic capability or full VPN readiness.

## Diagnostic routing in the active process

`AttachHealthInbound` is a pure transformation of an already generated and
routed configuration. It appends a loopback-only SOCKS listener in the same
Xray process. The first two routing rules apply only to this diagnostic tag:
TCP port 443 for `cp.cloudflare.com` and `www.gstatic.com` goes to the existing
`proxy` outbound; all other diagnostic traffic goes to a dedicated blackhole.
The usual inbound, its rules, outbound order, credentials and custom masks
are preserved. The existing strict JSON decoder rejects duplicate keys and
retains integer precision. Occupied diagnostic tags, port collisions, an
unsupported/direct proxy or ambiguous input refuse transformation.

This avoids mistaking a user DIRECT rule for a successful VPN check without
changing the user's routing. Backup generation already uses the existing
raw `XrayConfig`, which excludes user routing; it needs no second generator.

The transformation is not wired into live configuration generation yet.
Integration must allocate/revalidate the port under the lifecycle fence,
apply only for explicitly enabled reserve mode, validate the result with the
installed core, and confirm process/config/listener ownership. Disabling must
keep the current VPN running; any retained owned diagnostic input must be
excluded from ordinary health endpoint discovery until the next normal config
apply removes it. API, scheduler and settings must reconcile this lifecycle.

The real-core test requires `BRORAY_XRAY_BIN` with SHA-256
`c2d1e66a130556d32089adcf38202fcb6f9027e41fb456cc2cda9a5fabdc093a`.
It uses local echo traffic through a SOCKS upstream in the offline VM for
both address families, including a separate normal DIRECT path and blocked
diagnostic destinations. That test proves route selection. HTTPS validation
is covered separately below; record/lifecycle integration and physical
recovery remain gates.

## Owned HTTPS evidence

`Probe` now performs the synchronous traffic check for an already running
owned diagnostic input. Its target contains the captured process identity,
absolute configuration path, expected SHA-256 and loopback endpoint. Before
and after the requests it checks bounded, no-follow configuration reads,
the exact generated diagnostic route, process command line (`run -c` with
that path), and the process/listener proof. The resulting identity binds
those observations. The caller must hold the lifecycle fence and separately
bind the server-record revision; a matching on-disk file alone does not
prove which configuration was loaded outside that lifecycle contract.

Only the fixed HTTPS control URLs are requested through SOCKS with remote
DNS. Production uses system certificate roots and TLS 1.2 or newer. Each
URL has a 12-second timeout; redirects are not followed and only HTTP 204
with a successful body read counts as healthy. Owned runtime with failed
control requests yields `unhealthy`; cancellation or unavailable/changed
configuration, ownership or route evidence yields `unknown` without an
identity. Error codes expose no profile or raw core/transport secrets.

The adapter does not start/stop Xray, persist state or set `Sample.Verified`.
Integration still needs to map the validated identity into the current
record and serialize it against queue operations. An absent process/listener
is unknown here: classifying a confirmed active-process outage requires the
supervisor's current ownership/generation and maintenance state, not a guess
from a failed TCP connection.

Offline real-Xray tests now include actual HTTPS/204 for IPv4 and IPv6 using
an ephemeral local certificate authority, rejected certificates, non-204
responses, redirects, cancellation, wrong configuration/process identity,
route/input tampering, symlinks, and a configuration change during a request.
The private CA is supplied only to an unexported test helper; the public
probe rejects it. These are VM results, not physical-router readiness.

## Confirmed supervisor exit

`xray.Process.RuntimeSnapshot` supplies a cheap atomic observation without
filesystem, network or version-command I/O. It records wanted/running state,
current and last launched PID, successful-launch generation, explicit
lifecycle intent version and confirmation that the exact owned child was
reaped. An unexported owner token binds it to that supervisor instance and
is lost on serialization. Persisted observations are therefore not proof
after a daemon restart.

`after.ConfirmsUnexpectedExit(before)` requires the same owner, generation
and intent: the previous instance was running and wanted, the same child is
now confirmed reaped, no child is running and the supervisor still wants it
running. A deliberate Stop or a new explicit Start attempt invalidates the
old intent. An idempotent Start of an already wanted live child does not.
Automatic restart preserves the intent but advances generation on successful
launch, so old-child evidence cannot describe the new process. Exhausted
observation counters saturate and suppress proof while start/stop continues.

The existing WNOWAIT/reap ordering, guarded signals and inherited child lock
remain unchanged. A snapshot never authorizes sending signals or deleting
ownership files. Reserve integration still must bind this evidence to the
current active record/configuration and queue/maintenance state. It cannot
turn an arbitrary missing listener into a verified failure, and does not by
itself trigger failover. Tests cover actual owned normal/signaled exits,
manual stop/failed restart, automatic generation changes, serialization and
counter exhaustion inside the Linux ARM64 VM.
