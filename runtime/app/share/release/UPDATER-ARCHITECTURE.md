# BROray updater v5 architecture

The OPKG package installs a small persistent platform and one immutable compact
application slot. Xray and user state live outside application slots.

The WebUI submits update or reinstall requests to the persistent daemon. A
request receives an operation ID before it is admitted. The request lock, common
operation fence and resumable-route gate remain held for the full mutation, not
only for enqueue. Background server switching and subscription refresh use the
same admission boundary.

For an update, the daemon downloads and verifies the channel, archive, full
manifest and candidate identity, builds a staging slot, captures service state,
then atomically changes `/opt/broray/current`. Health checks cover the active
release identity, OPKG registration, WebUI backend, Xray configuration and
previously running services. Candidate defaults are committed only after that
health gate.

Reinstall uses the same transaction but targets the active candidate. It does
not require the channel to report `updateAvailable=false`. Rollback health also
does not depend on channel availability.

Failures before the switch remove staging. Failures after the switch restore the
previous slot and candidate-default journal. A rollback failure is durable and
fenced as recovery-required; the daemon stops rather than replaying an ambiguous
request. Startup recovery can retire only bounded, provably abandoned request
residue and preserves an unrelated terminal operation pointer.

Direct OPKG upgrade, forced reinstall and removal are outside this application
transaction. Removal is accepted only with a fresh WebUI authorization marker
bound to a non-empty operation ID.
