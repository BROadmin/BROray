# Legacy 3.1.0 compatibility candidate — not released

This separate compatibility tool is not part of `runtime/` and is not installed.
Its only supported source is the exact 3.1.0-r09c02 archive, SHA-256
`635e905d0fb31aff84cd92026dfed21ad204ea45c8d0a5562fecb248178b32e8`.
The original user incident's cause is still **ROOT_CAUSE_NOT_PROVEN**.

## Contract

The native parent holds verified idle producers and admission processes through
`ptrace`, preserving persistent Xray. It refuses busy/unknown executors and a
launch beneath BROray's own web/daemon process. It checks full process identity,
not a PID or command-name match alone. Native utilities outside `/opt`, including
`ip`, and temporary Xray probes are not treated as a preserved runtime.

The trusted two-phase callback checks all 299 archived source files, seven
platform files, source/init links, protected updater/Xray state, and business
data. It only writes private evidence and the new version's durable automation
pause. The old 3.1.0 does not understand that pause file: stopped automation stays
stopped during this boot; the 3.1.1 update must preserve the pause. A reboot before
updating can start old automation again. Recovery does not change saved settings.

The native parent alone archives exact dead service PID projections and renames
the unchanged five-file global fence. All held services are detached on exit or
guard death. A callback cannot retire the fence or keep writing after reporting
success. A failure after stopping a daemon can leave that daemon stopped and the
global fence present; no success is returned for this case.

Supported lock actions are explicitly enumerated in `preflight.sh`. Protected
installation/activation actions remain blocked for their own recovery. Route
progress remains unchanged; users use the existing resume/restore controls.
This tool does not perform business rollback or create a new route engine.

Concurrent privileged administration is outside the closed set of BROray
producers. Run through a standalone verified Keenetic Web CLI/SSH bootstrap,
not from BROray CGI. Inputs and callbacks are trusted root-private bundle code,
not a public API accepting arbitrary paths or commands.

## Evidence

`legacy-recovery-policy-linux-20260916-03`: 23 native-process tests and 10
archived-source policy tests PASS. These use real Linux ptrace/processes but
fake daemon bodies and fake Xray; they do not prove physical VPN continuity or
full legacy application recovery. ARM64/x86_64 builds are reproducible.

Preserved preceding failures reproduce: empty legacy bundle, a live web broker
ancestor, a callback descendant, a native `ip` writer outside `/opt`, and a
temporary Xray incorrectly classified as persistent. Test-harness discovery
collision is separately recorded and fixed.

## Open acceptance

- Signed bundle/bootstrap delivery, verified before execution.
- Full archived application scenario and ARM64 ptrace/fsync on the router.
- Physical blocked 3.1.0 → recovery → ordinary signed 3.1.1 update.
- Real VPN continuity, interrupted finalization, disk failures and reboot.

No stable publication or release readiness follows from this checkpoint.
