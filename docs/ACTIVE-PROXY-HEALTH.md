# Active VPN health after the r12 field report

Status: local source correction; publication held by the user. WebUI testing
is reserved for the user and is not performed in this stage.

## Cause and reproduced failures

`broray-connection-monitor` pings the current server address. Its `available`
means that an address exists; `up` means that ICMP replies were received.
Neither value establishes whether the active VPN can carry traffic.

The released r12 consumer uses `available`. An earlier unpublished local fix
preferred `up`; that fix is withdrawn. Before this correction, two tests
reproduced both mistakes: a working proxy with blocked ICMP was treated as
failed, while a failed proxy with working ICMP was treated as healthy.
Evidence: workspace `docs/evidence/active-proxy-baseline-20260916/`.

## Current behavior

- Verify the active server, runtime executable, command line and process start
  identity. Read the exact running configuration's single local SOCKS listener.
  A nondefault LAN address or port is supported. Wildcard listeners use local
  loopback; remote addresses, ambiguity and unsupported authentication produce
  unknown, not a failed VPN measurement.
- Probe the **existing** listener with HTTPS through `socks5h`. The first
  successful HTTP 204 from Cloudflare or Google confirms connectivity. If both
  requests fail, count one failed sample. Each request has a 3-second connect
  timeout and a 6-second total timeout; the owned helper is bounded to 25 seconds.
- Ignore curl's user config and proxy bypass environment. DNS goes through
  SOCKS; there is no direct retry, redirect following or disabled TLS validation.
- Bind the measurement to the server, process identity and configuration hash
  before and after the request. Changed/incomplete context does not count as a
  completed failure. A new runtime/configuration starts a fresh failure sequence.
- At the configured failure threshold, use the existing real candidate checks.
  They launch temporary Xray instances and therefore cannot replace the check
  of the currently running instance. Recheck the active VPN before switching;
  recovery cancels the switch. Keep cooldown and candidate quality selection.
- Cancellation drains the supervised probe processes and preserves the
  persistent Xray. Manual-off and stopped-runtime behavior remain unchanged.
  Protected activation and operation publication guards are retained.

State schema 3 gains `activeHealth` and `lastProxyContext`; the obsolete
`lastMonitorFingerprint` is null. A successful switch clears the previous
server's health result. ICMP metrics remain diagnostic/quality information.

## Verification scope

Passed: 10 active-proxy scenarios, 11 automatic-job integration methods and
10 updater regression methods (including 868 routes on a nondefault LAN).
The activation method was repeated after clearing obsolete health on a switch;
it is not counted twice. Evidence directories in workspace `docs/evidence/`:
`active-proxy-v3-20260916/`, `active-proxy-integration-20260916/`, and
`active-proxy-final-activation-20260916/`.

Linux suites exercise the real coordinator, supervision, production auto-switch,
publication and configuration generation with deterministic transport and a
native process-identity fixture. They do not simulate a physical VPN provider.

The read-only ARM check on the test Keenetic confirms a real successful request
through the installed SOCKS listener, a failed request through a closed local
port despite `NO_PROXY=*`, rejection of a foreign endpoint, and unchanged
runtime identity, configuration and router routes. This standalone transport
check does not exercise coordinator cancellation or real failover.
Evidence: workspace `docs/evidence/active-proxy-router-20260916/`.

The completed-test inventory and exact source/build hashes are recorded in the
new preparation checkpoint. Failed preliminary runs remain preserved: schema 4
was correctly refused by the publication guard; the source retains schema 3.
A cancellation fixture with unsafe file mode was fixed without weakening the
production guard.

## Release boundaries

- No publication, GitHub/site updates or Stable promotion in this stage.
- New physical installation/update/rollback and actual live-node failover remain
  separate release gates. Existing r12 evidence is not acceptance of new bytes.
- Scheduled quality refresh still shares the daemon cycle and can delay the
  next active measurement. Threshold 3 is not a promise of exactly 45 seconds.
- Failure of both probe targets can also reflect a wider network outage; it
  does not identify the provider as the root cause.
- Candidate `minimumRating` still applies. An ICMP-filtered candidate can be
  rated `acceptable` despite working HTTPS, and excluded by a `good` minimum.
- Manual VPN route policy is a separate proposal and is not implemented here.
