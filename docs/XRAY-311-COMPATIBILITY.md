# Xray compatibility for BROray 3.1.1

All seven exact ARM64 archives in the live catalog were downloaded from the
catalog's official XTLS/Xray-core asset URLs and verified against their hashes.
The physical Keenetic test used the user's active VLESS/XHTTP/REALITY profile,
copied to a private RAM namespace with only logging and the inbound loopback
port changed. Subscription credentials and runtime logs remain private.

| Xray | Config | Start and restart | External HTTPS |
| --- | --- | --- | --- |
| 26.9.9 | PASS | PASS | 3/3 |
| 26.9.8 | PASS | PASS | 3/3 |
| 26.7.28 | PASS | PASS | 3/3 |
| 26.7.11 | PASS | PASS | 3/3 |
| 26.6.27 | PASS | PASS | 3/3 |
| 26.3.27 | PASS | PASS | 3/3 |
| 26.2.6 | PASS | Start passed; restart not reached | 0/2 |

Evidence: `docs/evidence/live-xray-matrix-r10-20260916/result.json` outside the
implementation repository. The installed candidate was r10. The early-ack
fix in r11 changes coordinator admission, not the tested Xray configuration
generator or runtime configuration. The registry states this provenance.

The verdict is scoped to this profile and ARM64 router. It does not assert that
every Xray configuration fails on 26.2.6 or that other router architectures were
tested. It agrees with the earlier 3.1.0 profile-specific verdict, but is a fresh
physical run, not a renamed old result.

The installed Xray identity and all subscription/server/configuration files were
preserved. Direct external HTTPS through SOCKS was used, without SSH transport.
Each passing core was started twice and its directly owned processes reaped.
Actual core replacement is separate: the manager correctly refused the first
attempt because /opt lacked space for its safe atomic replacement reserve.
The RAM matrix does not certify the installation transaction.

An earlier RAM attempt used an obsolete loopback fixture target after the user
activated a real subscription; it is invalid for compatibility conclusions.
Only the fresh live-profile matrix above supplies these results.
