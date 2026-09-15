# BROray 3.0.0-r15c16

This immutable candidate is the clean metadata-package bootstrap for compatible
Keenetic `aarch64` routers with Entware architecture `aarch64-3.10`. It is not an overlay
for candidate-05, candidate-11 or R13C10.

## Supported lifecycle

- clean installation is performed by the immutable SHA-256-bound candidate OPKG feed;
- the router helper installs the exact size/SHA-256-pinned IPK path and permits
  failed-bootstrap cleanup only for byte-exact candidate registration and a
  fresh installer-owned operation marker;
- before feed mutation and `opkg install`, the router helper accepts either no
  raw BROray status stanza or one exact physical `install prefer,user
  not-installed` tombstone for `broray` `3.0.0-r14` `aarch64-3.10`, with unique
  Package/Version/Architecture/Status fields; it additionally requires empty
  `opkg status broray` stdout/stderr and absent info, payload and recovery
  authorization; package hooks never classify OPKG registration from inside
  the active OPKG transaction;
- application update and same-version application reinstall are performed only
  by the persistent updater v5 exposed through BROray WebUI;
- direct OPKG upgrade, forced reinstall and command-line removal fail closed;
- normal and full removal are WebUI-authorized OPKG transactions;
- normal removal preserves one protected, checksummed user-data archive for the
  next clean installation; full removal deletes that archive;
- an interrupted updater operation is either resumed, rolled back, or fenced in
  a recovery-required state. Ambiguous rollback never loops automatically.

## Keenetic and Entware invariants

- the target reports exactly one nonempty diagnostic `hw_id`, Keenetic
  architecture `aarch64`, and OPKG architecture `aarch64-3.10`; `hw_id` is
  evidence only and never a model allowlist;
- the LAN address is selected only from the unique intersection of an exact
  configured `security-level private` interface and live RFC1918 bindings;
  protected/public interfaces, interface names and first-address ordering are
  not selectors, and every failure preserves a stable diagnostic code and
  aggregate counts before temporary cleanup;
- a free or already-owned `ProxyN` is selected using an exact, complete,
  receipt-bound configuration block; foreign interfaces are not modified;
- the HTTP proxy object is exact, scoped, receipt-bound and rollback-safe;
- the local lighttpd instance has a private executable identity and does not
  rely on Entware's basename-wide `S80lighttpd` process matching;
- all mutating WebUI and background operations use the common operation fence,
  updater fence and resumable-route gate;
- public KeenDNS access is accepted only with the application session boundary;
  the local session endpoint must return HTTP 401 before installation commits.

## Artifact gates

- every tar member is a safe regular file, directory or relative symlink;
- application and platform manifests are complete and byte-exact;
- the OPKG member set, modes, control stanza, feed metadata and architecture are
  verified from the final bytes;
- all target scripts pass BusyBox ash syntax validation;
- three independent builds are byte-identical;
- three full validation runs pass against the final server archive;
- server publication is immutable and the channel pointer is replaced
  atomically only after all object checks succeed.

## DoT catalog and physical evidence

- the BROray DoT catalog contains exactly eight verified endpoints and does
  not contain `yandex-secondary` or `77.88.8.1`;
- every installed DoT record is classified against the exact
  `address`/effective-port/`sni` catalog identity; a catalog match never grants
  BROray ownership or delete authority;
- active target scripts must remain compatible with Entware `jq 1.8.1-2`
  without Oniguruma and therefore must not invoke jq regex functions;
- the SHA-256-bound physical read-only transcript proves a stable empty DoT
  record set on the observed router and no persistent mutation. It does not
  certify any write path, installation, or stable promotion.

Publication remains fail-closed until separate physical Keenetic CLI evidence
certifies all four write paths: Proxy interface, HTTP Proxy, DoT, and routes.
Read-only discovery cannot satisfy that gate.

The first `probe` build carries enabled, reversible write behavior in the exact
target publication bytes. Enabled probe behavior is not a certification claim:
the probe server installer refuses publication before any server-filesystem
mutation. A deterministic physical-write protocol binds the exact IPK, app and
active write-source hashes. Physical transcripts certify that exact protocol
and those exact bytes. No target byte may be changed after physical PASS. Only
a subsequent `publication` carrier reproducing the same target publication
bytes and carrying the matching external four-path receipt may pass validation;
its server installer rechecks the receipt and protocol SHA-256 against the
actual publication manifest, router installer, IPK, app archive and index.

Physical router promotion remains a separate gate: direct LAN WebUI, KeenDNS
TLS/session enforcement, ProxyN SOCKS traffic, reboot, safe unmount and
interaction with Entware `S80lighttpd` must be verified on compatible physical
Keenetic aarch64 hardware.
