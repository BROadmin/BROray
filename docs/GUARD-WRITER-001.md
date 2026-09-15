# Forked native writer exclusion

The v3 guard used process-owned POSIX record locks. A real Linux regression
forked a native publisher, killed its parent with self-SIGKILL, then admitted a
second coordinator while the first publisher still held its inherited file
descriptor. The original FAIL and exact source/build inputs are preserved in
`docs/evidence/guard-writer-reproducer-20260915/` in the project workspace.

Guard v4 uses the Linux `flock` syscall. Fork and exec share the descriptor-owned
lock until the last close. The persistent lock file, pathname/inode checks,
private-file checks, bounded acquisition and exit codes remain in force. This
does not depend on an installed `flock` command or the newer OFD-lock kernel ABI.

**Upgrade constraint:** v3 POSIX locks and v4 flock locks do not interoperate.
Switch unpublished guard generations only with all old coordinators quiescent.
The tested installation used a new isolated namespace; a full application has
not yet been installed or released with either guard.

Validation: 25 actual Linux checks PASS (15 guard, 2 orphan publishers, 8
supervisor/coordinator integration). Five isolated physical checks PASS on
Keenetic Peak KN-2710, kernel 4.9-ndm-5, aarch64, including both publication
modes and repeated helper registration/drain. Test namespaces were archived
and retired. Both architectures build twice with identical bytes.

This closes the forked-writer audit from LAUNCH-001. Producer integration and
full installation, migration, transaction/VPN continuity and endurance
acceptance remain release gates.

Kernel contracts: [flock](https://man7.org/linux/man-pages/man2/flock.2.html),
[record locks and OFD locks](https://man7.org/linux/man-pages/man2/F_OFD_SETLK.2const.html).
