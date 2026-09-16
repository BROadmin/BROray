# Route NDMC lifecycle, 2026-09-16

Baselines `route-ndmc-baseline-20260916` and `route-cache-baseline-20260916`
reproduce deletion of an unknown directory by the original reader/cache code.
Source review also found five command wrappers sending signals using saved
shell PIDs, including a separate export watchdog. Shell job reaping cannot
reserve those numeric identities.

All wrappers now call one synchronous native runner, preserving their existing
command authorization and timeout budgets. A persistent flock lane serializes
reads and writes. An old directory, a symlink or unknown guard content is
preserved and rejected. The native runner owns an unreaped group leader and
uses subreaper adoption to drain detached descendants before releasing the
lane. It never signals a PID loaded from a file. There is no nested ptrace;
the existing protected-route tracer still supervises the whole job. A stuck
uninterruptible child keeps the lane held. Cache publication uses a separate
persistent native guard instead of deleting PID directories.

Evidence:

- Two repeat builds for ARM64 and x86_64 match; static ELF architecture checked.
- `route-ndmc-final-linux-20260916`: 10 PASS, including exit propagation,
  timeout, detached descendants, serialization, unknown content, cache refresh,
  authorization and nesting under the actual protected route tracer.
- `physical-route-ndmc-20260916-04`: 6 PASS with actual Keenetic read commands,
  actual cache refresh, unchanged running config, native detached child drain
  and the protected route entry. Installed Xray identity unchanged; namespace
  archived and retired. No route writes were issued.
- Early ARM harness failures are preserved: unsuitable injected Entware loader
  environment and absent `setsid` CLI. Final harness uses the router's normal
  environment and the existing native session fixture. Attempt 2 never started
  because the failed prefix had not yet been retired.

This checkpoint does not represent installed-candidate acceptance.
