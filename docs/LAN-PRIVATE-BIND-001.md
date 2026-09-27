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
Clean installation with multiple segments uses the explicit `install` selection
mode. It lists admitted IPv4 addresses and Keenetic interface names, accepts a
number through the controlling `/dev/tty`, and offers 0 to cancel. It never reads
the installation script's stdin. Empty or invalid input does not select a default.
The selected address is revalidated against fresh configured/live snapshots before
it is returned. With no controlling terminal, installation explains the supported
`BRORAY_LAN_IP_OVERRIDE` setting and stops before the clean bootstrap's mutation
boundary. Ordinary transport/WebUI service discovery never opens a dialog.

Existing `configure_local_address` persists the choice in `settings.listenAddress`;
initial SOCKS and WebUI use that setting. Update/reinstall preservation is unchanged.
The separate WebUI-only pin continues to take precedence for WebUI when configured.

The clean bootstrap has an earlier pre-mutation check outside the application
archive. `scripts/prepare-clean-lan-selection.py` integrates selection into that
exact verified postinst block and exports the selected address for later setup.
Use it before generating the IPK/bootstrap hashes and signatures. An application
archive alone does not update a previously published immutable installer. The tool
requires the input SHA-256, refuses unknown/already-modified boundaries, writes a
new output file only and does not install, sign or publish anything.

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
UI-TOAST-01 was the next separate historic stage; no notification code changed there.

## LAN-INSTALL-01 continuation (2026-09-27)

The first-install gap above is now addressed in working source. Evidence resides
in `artifacts/BROray-3.2.0-lan-selection-20260927` in the enclosing project.
Tests cover actual ash/PTY selection, clean postinst pre-mutation integration,
cancel/EOF/headless installation, explicit invalid addresses, changed interface
roles during selection and preservation of existing multi-LAN behavior.
On KN-2710, the exact new network library was exercised only in an isolated RAM
fixture using actual Entware utilities and an SSH terminal. Its `sort -o` behavior
required plain input/output redirection; that regression is retained. No clean
installation or live LAN configuration change was performed in this stage.
