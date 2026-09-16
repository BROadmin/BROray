# Route operations: user cancellation disabled

The user narrowed BROray 3.1.1 on 16 September 2026: route operations should not be interrupted through BROray controls. This replaces the earlier requirement for cancellable route preparation.

## Behavior

- The coordinator enforces protected mode for route scope and route actions throughout their lifetime. Individual cancel, bulk stop and cancellable helper registration cannot bypass this through an older cooperative record. A dead protected owner still requires domain recovery.
- Legacy route stop API returns authenticated HTTP 409 / ROUTES_STOP_NOT_SUPPORTED. CLI stop and the progress library refuse without writing operation state. Old stop markers do not interrupt protected work.
- Route views hide and disable stop controls, expose canStop=false and explain the restriction in Russian. General background stop continues to cancel supported operations and pause automatic launches.
- Existing error, rollback and resume paths remain. Pending route state still prevents a conflicting writer.

## Verification

- Reproducible baseline: previous code accepted route cancellation through coordinator, CLI and progress library. Baseline-02 corrected the first fixture's missing guest /opt/broray symlink.
- 9 route policy Linux tests passed. The subsequent general suite found a stale diagnostic expectation left from before the five-service inventory. Only that test assertion changed; diagnostic production code did not.
- Final general regression: 57 coordinator, 7 public projection, 12 CGI and 8 actual Linux supervisor integration tests passed. Together with the 9 unchanged route tests: 93 Linux tests.
- 8 physical ARM tests passed in a private prefix using real process identity. The first harness correctly hit DOMAIN_OPERATION_BUSY because it retained its own resumable fixture; the second explicitly verifies that fence before completing the fixture and checking subscriptions. Failed and successful attempts are preserved separately; both namespaces were retired after inspection.
- Source syntax, baseline integrity and git diff checks passed. Physical closure confirms installed r07, automation paused and no global fence or remaining test namespaces.

Evidence is sealed in the workspace checkpoint BROray-3.1.1-ROUTE-CANCEL-POLICY-001. This change is not installed. These tests do not claim full route job integration, actual Keenetic route application, protected domain recovery, VPN continuity or release readiness.
