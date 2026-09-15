# Subscription producer integration

## Implemented boundary

The subscription scheduler now executes a short-lived `--job` process. That
process acquires and acknowledges the unified operation before service work;
the daemon is never recorded as the owner of a refresh. Automatic admission
honors the persistent automation pause. Scheduler failures propagate to the
job result instead of being swallowed by the iteration loop.

The subscription CGI common layer and `broray-subscriptions` CLI use the same
job lifecycle. The CGI's zero process exit on an HTTP business error no longer
records a completed operation. The new `owner-check` coordinator action checks
the full current executor identity and fence. Service mutation checks use a
builtin read of `/proc/self/stat`: ash's inherited `$$` in a subshell cannot
impersonate the parent executor.

Fetch (180 seconds) and parse/staging (300 seconds) execute under the native
supervisor in a private preparation directory. The helper receives paths, not
subscription URLs or credentials as its own command arguments. The owning
process validates the returned metadata and confirms the helper tree is gone
before entering the protected server synchronization phase. Cancellation leaves
the catalog unchanged. Unconfirmed children retain the fence and preparation.

The old per-subscription and server-sync PID locks are no longer created or
reclaimed. Existing locks are preserved for compatibility recovery. Durable
subscription writes use private same-directory temporary files and the native
fsync/rename replacement. The global fence excludes these writers throughout.

List and summary no longer perform stale recovery. If a persisted `running`
subscription points to a terminal managed operation, GET projects an error
without changing the file. Explicit, admitted recovery only rewrites metadata
linked to a confirmed terminal operation and leaves unknown legacy locks alone.

## Evidence and scope

The original GET regression deleted an unknown lock; the pre-fix FAIL and exact
source are in `docs/evidence/subscription-get-reproducer-20260915/` (workspace).
An initial cancellation harness failure was diagnosed as unreaped zombie
children adopted by the Python test subreaper. The harness now performs its
init-like reaping duty using waitpid on its own adopted children; production
absence checks were not weakened. The diagnostic is preserved separately.

Linux checks execute the actual parser, coordinator, supervisor and subscription
service with a deterministic local curl fixture. Physical checks execute those
same production components with ARM64 binaries on the test router, in isolated
directories with no persistent Xray service and no external subscription URL.
The first physical fixture stopped at library import because it omitted proxy
configuration. It admitted no job, was archived and retired; the corrected
fixture supplies private test settings without changing router configuration.

The finalized checkpoint records the exact passing counts and hashes. These
checks do not constitute full installed-application or VPN acceptance.

## Remaining release work

- Subscription active-server synchronization/rollback and protected commit crash
  recovery still require dedicated testing and improvements. Existing domain
  rollback failure paths must not be presented as verified recovery.
- Automatic cleanup of preparation left after executor SIGKILL, history pruning
  while subscription metadata still refers to an old operation, and legacy
  subscription state migration need coverage.
- The separate old `broray subscription-add/subscription-update` format is not
  converted by this stage. Other CLI/CGI producers, server checks, auto-switch,
  Xray maintenance and route/updater recovery still require coherent rollout.
- Full install/upgrade/reinstall, legacy blocked delivery, journal persistence,
  VPN continuity, reboot/crash and endurance gates remain open.
