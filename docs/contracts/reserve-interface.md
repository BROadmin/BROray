# Reserve interface integration

The servers area supplies an optional description override through the existing
Services boundary. Valid enabled reserve preferences select `BROray - mix-auto`;
missing or disabled preferences keep ordinary active-server naming and its
`BROray` fallback. Invalid reserve preferences cannot authorize a rename.
Keenetic expected status and description synchronization use the same provider.
The active server name remains separate from the interface description.

Existing repair and sync code already changes only description/save when the
owned endpoint and UDP configuration are unchanged. It is preserved byte for
byte. Tests cover enable, active-server change and disable without changing
ProxyN, upstream, UDP, foreign interfaces or Xray config, plus the fallback when
the active name is unavailable. The initial claim that this path reconnected
the interface was disproved by its baseline test and full source trace.

`Services.VerifyOwnedInterface` is read-only and refuses an unavailable owner
service. The Keenetic implementation requires the caller's existing fence,
current schema-2 receipt authority, fresh matching running/startup blocks,
the expected description and SOCKS endpoint, an active local SOCKS service and
connected/up router runtime. It repeats receipt, expected state and live runtime
checks before returning the technical interface name. It does not use the
cached status as commit evidence or adopt a foreign interface. Missing server
display metadata does not prevent verification of the approved `BROray`
fallback; active profile/process evidence remains the separate runtime adapter's
responsibility. This check alone does not prove Internet access.

Disabling reserve must retain the current VPN without restarting it. Its exact
generated diagnostic input may therefore remain until a later normal config
apply. Ordinary health ignores only that input after validating the whole
diagnostic addition through the existing reconstruction/equality check. A tag
alone is insufficient; changed route/auth/UDP settings and arbitrary multiple
SOCKS inputs remain unsupported. The diagnostic input can never become the
ordinary health target when no main SOCKS input exists. Config bytes are never
rewritten by this recognition step. Ordinary configs without the reserved last
SOCKS tag avoid the additional strict parse.

These hooks do not enable reserve mode, execute a server switch or establish a
transaction journal. Queued enable/disable/switch, pending recovery, backup
probes, UI and physical router testing remain integration work. Router tests in
this stage use the existing local fake transport; no physical PASS is implied.
