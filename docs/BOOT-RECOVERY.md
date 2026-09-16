# Recovery when services start after reboot

Baseline: `boot-recovery-baseline-20260916` reproduces a cooperative global
fence surviving initialization after a changed kernel boot ID. A subsequent
manual operation could recover it, but paused automation left it visible.

Initialization now uses the existing guarded recovery path only when the full
owner identity proves a different boot. It preserves automation pause and does
not signal processes. Same-boot, missing/ambiguous identity, updater fences and
protected commits stay fenced for their normal explicit recovery paths.

`test_boot_recovery.py` covers all five cases. Existing initialization, recovery
and coordinator suites are rerun together in
`boot-recovery-final-linux-20260916`. Those tests use a synthetic proc tree;
physical reboot acceptance is recorded separately for the installed candidate.
