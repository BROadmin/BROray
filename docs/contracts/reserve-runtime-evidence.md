# Active reserve runtime evidence

The servers area prepares a read-only plan under the existing global operation
fence. It reads the configured roster and a bounded regular server record,
generates the existing profile config, applies the existing routing compiler,
then attaches the reserved diagnostic input. The plan retains the exact output
hash, raw record hash, settings/defaults identity and roster revision. Its node
fingerprint is the generated profile config before the diagnostic addition.
Preparation does not apply a config, change the active pointer, test a candidate
core, enable reserve mode, or modify the interface.

Binding accepts only `d.App.Xray`, its wanted live child and the exact active
pointer/config from that plan. It reuses the existing real procfs, executable,
command-line, diagnostic routing and owned listener verification before and
after capture. A matching open port or the legacy process discovery fallback
cannot supply this evidence. A binding is private process-local state; it is
not restored from disk and is not itself proof of successful traffic.

Automatic samples require current enabled preferences, a held fence, an
unpaused coordinator and no core maintenance. Record, settings, roster, active
pointer, config, core inode and supervisor identity are checked again around
the measurement. Reads refuse symlinks and nonregular files and have explicit
size limits. The production path calls `reserve.Probe` with the fixed HTTPS
controls, real certificate validation and its existing timeout behavior.
Unknown ownership, stale/future outcomes or a changed generation during a
measurement do not set `Sample.Verified` and carry no policy identity.

There is one separately proven failure case: the previously bound owned child
has been reaped unexpectedly, the same supervisor still wants the same
lifecycle, and all other authority checks remain unchanged. This counts as
unhealthy even if a request completed successfully just before that exit.
Manual Stop/Restart, replaced core, changed settings or missing unowned sockets
cannot produce that failure proof.

The policy identity is an ephemeral random token for the desired lifecycle and
plan. A newly proved automatic child generation can keep that identity when
the same supervisor, lifecycle intent and complete plan remain unchanged. Old
child observations remain invalid. An explicit lifecycle change or a changed
plan creates a new identity. The controller must discard all bindings and
readiness after daemon restart and revalidate switch intents before mutation.
The stable policy identity alone is insufficient for commit: retain the child
binding associated with the failing sample and compare its generation at the
protected apply boundary. If the child changed, obtain a fresh active probe
before deciding or applying. Pending transaction evidence must retain that
exact runtime/configuration authority as well as the policy token.

Tests use the real Linux ARM64 supervisor and pinned official Xray. Production
probing of an unavailable upstream demonstrates verified unhealthy evidence;
healthy outcome mapping uses an injected result with independently checked real
ownership, while actual successful TLS/204 traffic is covered by the reserve
module tests. Test-only unexpected exits use a pidfd followed by renewed owned
process/listener verification before signaling; production signal paths are
unchanged. VM results are not physical router evidence.

Pending integration: queued enable/disable and switch transaction, durable
pending reconciliation, isolated backup evidence, manual lifecycle and restore,
owned interface naming, API/UI status, and physical failure/recovery tests. This
adapter does not expose the enabled setting or schedule itself.
