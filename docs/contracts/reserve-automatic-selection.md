# Reserve ownership and ordinary automatic selection

Reserve preferences absent or explicitly disabled preserve ordinary automatic
selection. This guard does not alter the saved ordinary auto-switch policy.

When reserve mode is enabled, the ordinary scheduler does not enqueue active
health or provider-withdrawal replacement work. Independently configured quality
refresh remains available. A manual check of the active server uses an isolated
quality probe; it cannot update ordinary failure counters or enqueue ordinary
failover. Actual reserve health belongs to the reserve evidence adapter.

Already queued ordinary health, failover and replacement stages recheck mode
ownership before doing work. Active health probes also recheck after their slow
measurement, before publishing ordinary counters or preparing a failover result.
Protected activation stages check again while holding the existing global
operation fence. Invalid, ambiguous or unreadable reserve preferences do not
authorize ordinary automatic selection. The strict bounded no-follow settings
reader is reused; no unverified shell utility is introduced.

The existing coordinator executes operations serially. Future reserve enable,
disable and roster changes must use that same coordinator and global apply
fence. They must invalidate prior policy evidence and reconcile pending state;
these checks alone do not implement those transactions or allow direct HTTP
mutation of the enabled flag. Manual activation/disconnection, active backup
restore, startup reconciliation and interface description remain integration
work. The reserve feature is not exposed as enabled by this stage.

Tests exercise the real coordinator with fake network/apply services, including
enabled and invalid ownership at all seven old selection stages, OFF behavior,
manual and scheduled quality, mode change during a probe, and both fully
prepared failover and withdrawal replacement losing authority before apply.
Linux ARM64 VM results are not physical Keenetic failover evidence.
