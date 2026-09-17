# LAN-01 — stable private WebUI binding

Base: 725c53f3ae2077e9f1abb8db0129554abc59c320 plus exact stages 01–10.
This repairs the reported restart failure after adding a second private segment.
It is not a multi-listener redesign, a network selector UI or a SOCKS address change.
It never binds 0.0.0.0 or changes firewall rules.

## Address admission
Read-only `ndmc -c 'show running-config'` and `ip -4 addr show` snapshots must succeed.
Private RFC1918 addresses are intersected with exact live IPv4 entries. Duplicate
configured addresses (including public/protected use), duplicate live addresses,
ambiguous security-level or invalid addresses are excluded. Interface names,
SSH_CONNECTION, model numbers and output order are not preference selectors.
Only static IPv4 addresses in the existing command grammar are handled.
The snapshots establish configured security classification and unique live address;
they do not establish a separate Linux/Keenetic interface-name mapping.
Read commands have an 8-second timeout plus 2-second grace and an output file limit.
Full configuration output is temporary and never printed into diagnostics.

## Explicit setting
Optional string `webuiLanAddress` in config/system/settings.json pins WebUI only.
Absent or empty means automatic; a non-string or inadmissible nonempty value fails.
BRORAY_LAN_IP_OVERRIDE stays supported but now also requires private classification.
Different simultaneous explicit choices produce EXPLICIT_PIN_CONFLICT.
No rc.unslung edit is made. An existing user workaround is not removed automatically.
A non-private override formerly passed the old check; refusing it is deliberate.

## Automatic preference
WebUI: explicit choice, validated persistent lighttpd server.bind, validated
settings.listenAddress, then sole admitted candidate. Transport does not read
a WebUI-specific pin or infer its address from the lighttpd configuration.

Hints are always revalidated. An expired automatic hint may fall back only to a
single remaining admitted address. With multiple candidates and no valid choice,
LAN_SELECTION_REQUIRED is retained: no arbitrary first IP or wildcard listener.
Clean installation with multiple segments and no selected transport address still
requires explicit configuration; an automatic first-install network wizard is NOT
implemented. This limitation is separate from the reported previously-working case.

## Lifecycle and preservation
No changes to settings.listenAddress, Xray config, server data or ProxyN are made by
WebUI startup. The existing settings file is the persistent container; this stage
only reads the optional field and does not add an uncoordinated settings writer.
Existing updater preservation of that file is reused; no new root/backup contract.
S25 detects without overwriting run/lan-ip first. A confirmed running instance with
a different chosen address requires a normal restart and is not silently rebound.
The lighttpd configuration is written to a private candidate, syntax-checked and
renamed only after success. The runtime address is written atomically; existing
unsafe symlinks are rejected. Stop and process identity functions stay unchanged.
Setup WebUI probes/publication and historical transaction HTTP postchecks use the
WebUI address rather than the SOCKS address. Existing initial Xray setup stays intact.

## Delivery and limits
Use the cumulative patch on the pinned clean commit; the incremental patch requires
exact stage10 output. No runtime deployment, installation, reboot or complete updater
is performed in this package. Tests use synthetic ndmc/ip/daemon/HTTP fixtures.
Physical multiple-private restart, real lighttpd, native authentication/KeenDNS,
updater/reinstall retention, IP renumbering and role changes need candidate acceptance.
UI-TOAST-01 is the next separate stage; no notification code is changed here.
