# Emergency recovery: pause, stop, verify

Source baseline: `364b5feff019c6c42de1057c614fd4425e3797f1`.

The existing `recover` command only called `ops_recover_global` and
`ops_recover_orphans`. It did not pause producers or request cancellation.
The existing button hid every refusal behind the same generic message.

Before production changes, `recovery-flow-baseline-20260916` reproduced a
successful stale-fence retirement even when persisting the required automation
pause was impossible. This is a synthetic-owner Linux test, not a router test.

Use the existing stop-background transaction first, under the same short
coordinator guard. Failure to persist the pause must prevent recovery. Request
cancellation only through the existing policy; routes and protected phases
continue. Then use existing full owner and descendant checks before retiring a
fence. Never wait while holding the coordinator guard. The existing UI may retry
the transaction a bounded number of times while cancellable work drains.

Return structured preservation reasons and HTTP 409 for confirmed recovery
refusals. Keep automation paused after success. An updater fence also prevents
reporting successful recovery, even if there is no background global fence.

This stage does not resolve five-file legacy locks, protected domain commits or
route resource locks. It must preserve them. The separate compatibility adapter,
existing route resume admission, physical full application acceptance and final
release remain required.

## Validation

- Final runtime: 12 focused recovery cases, 12 authenticated CGI cases,
  8 real Linux supervisor integration cases and 9 route policy cases: PASS.
- Preceding revision: all 57 general coordinator cases passed. The combined
  runner then failed before route tests because a second suite attempted to
  create an existing `/opt/broray` test symlink. The setup now validates and
  reuses that link; the separate nine-case route run passed. The failed runner
  remains preserved. The final production change after the 57-case run only
  validates the updater pointer before reading it; focused FIFO/state tests
  and the physical run cover that change.
- Final physical ARM prefix: 9 PASS, native coordinator/supervisor and actual
  process identities. The first seven-case prefix run also passed. Both
  namespaces were archived and retired; neither installed an application.
- Production page in Chromium with mocked HTTP: 7 outcome cases PASS,
  screenshots at 1440 and 360 pixels; mobile feedback does not overflow.
- Source archive integrity, shell/JS syntax and diff checks: PASS.

Evidence directories in the workspace: `recovery-flow-linux-20260916`,
`recovery-flow-linux-20260916-02`, `recovery-flow-route-linux-20260916`,
`physical-recovery-flow-20260916-02`, `recovery-ui-20260916`.

The installed r07 and stable release have not been replaced. This is a component
checkpoint, not final release acceptance.
