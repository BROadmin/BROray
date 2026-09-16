# Uninstall component integration, 2026-09-16

Uninstall already owns the legacy system fence. Its route-delete and server
deactivate CLI calls started a second background job, rejected by that fence.
Rollback route export had the same conflict. Reproduced in the actual lifecycle
dispatcher with private business fixtures and the real full self identity:
`system-components-baseline-20260916-02` and traced `-03`. The first attempt
failed earlier because the fixture omitted its runtime capability path; it is
not used as regression evidence.

An internal dispatcher now verifies the exact uninstall action, operation ID
and live self identity, acquires the existing native OPKG control mutex, then
calls the original domain functions in separate subshells. The mutex's FIFO
descriptor remains inherited until synchronous descendants finish. Public
CLIs retain normal job admission; there is no environment bypass token. The
server deactivation commit body is shared with the protected public worker.

Rollback uses the exact restored config and active-server snapshot and restores
the previous Xray running state, without launching a new activation job or
regenerating the saved config. Persistent Xray starts outside the short OPKG
mutex to avoid inheriting its FIFO into the daemon.

`physical-system-components-20260916`: 4 PASS with real native Entware OPKG
mutex and actual resource lease. Route/service business actions were harmless
fixtures; the installed Xray identity and router routes were unchanged. The
empty private FIFO was never a package payload. All namespaces retired after
archival. Linux dispatcher and server-worker regression is recorded separately.

`system-components-linux-20260916`: 23 PASS (5 dispatcher tests, 18 production
server-worker regression tests). Full installed uninstall/rollback acceptance
remains a separate stage.
