# User field issues 1–8 — local work, publication held

No browser/WebUI acceptance, router installation, signing, publication, GitHub
or project-site update is authorized by this checkpoint. The user requested
local changes only and will perform WebUI acceptance separately.

## Local changes

1. The ProxyN parser accepts one numeric `ip global` line in the observed
   Keenetic position. The priority must be a canonical integer from 1 to
   65534. Unknown/duplicate/misplaced fields and changed SOCKS endpoints remain
   rejected. Internet priority is user-owned connection policy; like dynamic
   `proxy connect via`, it is preserved in configuration and excluded from the
   BROray endpoint fingerprint. No `no ip global` command is issued.
2. Startup reconciliation performs supervised read-only ownership preflight
   before entering the protected mutation phase. A preflight refusal finishes
   the job without retaining a global fence. Mutations execute inside a bounded
   supervisor. A failed mutation releases its fence only after ownership is
   confirmed again; unresolved state stays protected. Explicit recovery checks
   domain markers and complete child absence, then performs a bounded read-only
   ownership check. Old unsupervised jobs require proof of a previous boot.
3. Active proxy HTTPS health replaces ICMP/address availability as the failover
   decision source (previous local checkpoint, not newly released).
4. Updater endpoint validation uses actual configured/local LAN addresses
   (previous local checkpoint; deployment must refresh the persistent updater
   platform before route capture).
5. Hysteria2 authority parsing accepts `:443/?...` (previous local checkpoint).
6. Subscription web requests hand work to a coordinator-owned background
   executor. Parsing publishes numeric progress, and fetch/parse deadlines have
   different error codes. Supervisor SIGCHLD wakeups remove the per-event polling
   sleep. This addresses reproduced latency; the customer's original subscription
   timeout has not been independently reproduced with their exact environment.
7. Subscription cards offer cooperative cancellation through the existing
   operations endpoint. The protected commit phase remains non-cancellable.
   The backend retains the fence until children have drained.
8. Requests have a bounded response wait, persistent form error feedback,
   background phase/progress feedback, and resumed polling after transient
   failures. A lost HTTP response is reported as uncertain, not as proof that
   the server-side operation stopped. POST requests are never auto-retried.

## Baseline evidence

- `docs/evidence/interface-ip-global-baseline-20260917/result.json`: real parser
  against synthetic blocks. The block without `ip global` reproduces the exact
  customer ownership hash; adding only `ip global 42129` fails the signature and
  changes the hash.
- `docs/evidence/field-followup-baseline-20260917/`: failed interface preflight
  retains a fence; 100 short parser-like subprocesses take 4.833 seconds.
- `docs/evidence/interface-priority-fixed-20260917/`: four test methods pass,
  including strict negative cases. These are local Linux fixture tests.

## Local verification evidence

- `field-final-20260917`: 19 passing checks for asynchronous import, early
  cancellation on either side of executor transfer, cancellation during parse,
  network failures, real parser publication, read-only status projection,
  existing executor handoff and strict interface priority parsing.
- `field-regression-20260917`: the startup-sidecars suite passes all 13 methods.
  The next suite used an incorrect previous-boot fixture (boot ID at the record
  root instead of inside `owner`); production correctly refused that identity.
  That fixture was corrected and is exercised in `field-backend-20260917`.
- Successful reconciliation took 26.937 seconds in QEMU. The former 25-second
  test wait was insufficient; only that test wait was raised to 60 seconds.
  Production timeouts were not extended.
- `field-backend-20260917`: eight field/recovery methods pass, including the
  corrected old-boot identity, refusal of unresolved transactions and same-boot
  unsupervised owners. The following subscription suite exposed a missing
  progress directory on the first direct update. Preparation now creates it
  itself instead of depending on the web launcher. The unchanged full-import
  scenario, strengthened to assert the directory is initially absent, passes
  in `field-complete-20260917`.
- `field-complete-20260917`: all 35 checks pass (12 subscription lifecycle,
  14 native supervisor, 8 coordinator/supervisor integration, 1 subscription
  backend admission). This guest contains the final production sources.
- The supervisor's 100-process local benchmark improves from 4.833 seconds to
  1.318–1.793 seconds. This is emulator evidence, not a router performance claim.
- ARM64 and Linux x86-64 supervisor builds are reproducible byte-for-byte.
  Shell/CGI, JSON and JavaScript syntax checks pass; no browser was driven.

All paths above are under workspace `docs/evidence/`. Earlier failed runs are
preserved and do not count as acceptance. IO-APIC boot failure was addressed
only in the disposable QEMU harness (`noapic`); it did not run product tests.

## Acceptance status

LOCAL SOURCE VERIFIED, PUBLICATION HELD. The selected passing suites total
75 test executions (19 + 13 + 8 + 35). Repeated preliminary runs are not counted.
The only production change after the 19/13/8 groups was creation of the missing
subscription progress directory; the final 35-test guest covers that change.

Items 3–5 retain the prior local verification documented in
`ACTIVE-PROXY-HEALTH.md` and `SUBSCRIPTION-HYSTERIA2-URI.md`.
There was no physical router or browser test in this checkpoint. The original
customer subscription timeout still requires confirmation in their environment.
The existing r13c02 archive does not contain these new source changes. No new
release archive was built or published; this is not a release-ready claim.
