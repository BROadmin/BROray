# Server and Xray job scope — 2026-09-16

The protected-route policy exposed three older callers that admitted server
and Xray jobs with `scope=routes`. This made their cooperative preparation
protected and rejected native helper registration. The actual worker, CLI
dispatch and WebUI launch now use `system`. The global conflict fence and the
protected commit boundary remain in force.

## Reproduction before production edits

- `system-scope-baseline-20260916`: all three actual admission paths recorded
  `routes/protected` instead of `system/cooperative` (three expected failures).
- `server-scope-baseline-20260916` and `xray-scope-baseline-20260916`: helper
  preparation exited 74 before the probe/download could start.

## Validation

- `system-scope-linux-20260916`: the three admission, 18 server lifecycle,
  eight Xray lifecycle, three handoff and five activation checks passed. The
  five activation cases repeat cases in the server suite. Its subsequent
  automatic-cycle test exceeded the old 40-second readiness budget in the
  offline emulator; this failed run is preserved.
- `system-scope-linux-20260916-02`: eight automatic-cycle checks and ten
  deliberate lost/malformed-response checks passed. Only the automatic-cycle
  test readiness budget changed to 90 seconds, with diagnostic state on timeout;
  production operation deadlines and automatic-cycle code did not change.
- Combined: **50 distinct Linux checks**. Environments use actual Linux
  processes/coordinator and private business/transport fixtures.
- `physical-server-scope-20260916`: **9 ARM checks** passed, including real
  probe supervision, cancellation, catalog import, paused producer and API error.
- `physical-xray-scope-20260916-03`: **7 ARM checks** passed, including binary
  replacement, rollback, failed rollback preservation, cancellation and WebUI
  executor handoff. Native test binaries/HTTP responses are synthetic; the
  installed Xray is not used as an installation target.
- Two earlier ARM Xray runs stopped on the real free-space checks. Their
  evidence is retained. Input binaries were moved to private RAM, and obsolete
  r01 installation inputs were archived before retirement. Real installer
  output/backup and all production free-space checks remained on `/opt`.
- Successful namespaces were archived and retired; installed Xray's full
  identity was unchanged. This checkpoint does not install or publish a release.
