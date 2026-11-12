# Pinned Clash server field accounting

`field_dispositions.json` accounts for all **949 field declarations across 62 schemas and 24 registered server types** in `clashschema/registry.go`, including nested structures, source types, validation boundaries and conditional runtime disposition. These are declaration totals, not 949 independent parameters or 24 executable backends. Regenerate with `generate_dispositions.ps1`. The source pin is Mihomo `0455d0d28c4526937380505153e65a843d8612e8`; the target pin is Xray `676eaf66f8f05d4d558ee7a74b4736623d3feeb9` (v26.11.9).

The execution boundary is `ConvertNode`. `clashschema.Parse` normalizes declared types and isolates `KnownFields`. Unknown extensions are retained only after the known projection validates. `capabilityProjection` removes explicitly classified fields solely for validation; its projection is never returned as an executable profile. An ordinary conversion error remains an error. Records with a capability retain all structured source fields and are revalidated before storage, backup and attempted execution.

| Scope | Disposition |
| --- | --- |
| Ordinary VLESS/VMess/Trojan/SS/HTTP/SOCKS/HY2 fields | Direct mapping through the shared Xray importer and server validation where implemented |
| Socket TFO/MPTCP and strict IP family; TLS pin/name; HTTP headers; gRPC UA; HTTP camouflage; XHTTP extras; supported mKCP fields | Direct conditional mappings; accepted JSON alone is not live interoperability evidence |
| Explicit `udp: false` | Full private source retained as `local-policy`; no UDP-capable projection is returned |
| Nonempty `interface-name` / nonzero `routing-mark` | Full private source retained as `local-control`; empty/zero controls remain convertible |
| Shadowsocks cipher/password | Exact pinned 39-name registry and SS2022 key semantics validate before any unavailable fallback; seven cipher names map including XChaCha, 32 other source ciphers lack a core equivalent |
| Static ECH; compatible WS early data; XHTTP download; HY2 QUIC bandwidth/windows/BBR, obfuscation and hopping; explicit mux=false v2ray-plugin WS | Conditional source-derived mappings through existing target implementations; nested context still disables the entire original node when unavailable |
| Family preference, dialer references, dynamic ECH lookup, WS arbitrary early-data header/path suffix/fast open, gRPC pool controls, remaining HY2 tuning, unimplemented known fields | Typed private preservation, `conversion-unimplemented` |
| `skip-cert-verify: true` | `core-removed`: pinned Xray 26.11.9 returns a removed-feature error for AllowInsecure |
| Packet-address framing, sing-mux, unsupported protocol wrappers, H2 transport, removed mKCP tuning | Typed private preservation, `core-unsupported`; no invented target fields |
| Inline ordinary TLS mTLS | PEM/key agreement checked; `core-unsupported` based on pinned-core client-certificate failure |
| Source-local certificate/key/state/planet references | Preserved privately; no file reads, writes, processes or network side effects; referenced content remains unresolved |
| MASQUE | Basic H3 WARP uses the native target with P-256 DER keys, addresses, source-default SNI/URI and endpoint. MTU/stack/resolver/other transport or congestion contexts remain explicit unavailable |
| AnyTLS, EasyTier, gost-relay, Hysteria 1, Mieru, OpenVPN, ShadowQUIC, Snell, SSH, SSR, Sudoku, Tailscale, TrustTunnel, TUIC, ZeroTier | Source endpoint/credential/selected adapter semantics validated; typed complete source retained with `core-unsupported` |
| Unknown root or nested extension | Known fields validate first, then `unknown-extension`; unregistered protocol names reject |

The seven added mapping subsets have the following concrete boundaries. The import/generation, private backup, masking and identity contracts are exercised by `TestClashEquivalent357*`; they do not certify a live server's interoperability.

| Source subset | Emitted Xray setting and boundary |
| --- | --- |
| SS `xchacha20-ietf-poly1305` | `settings.servers[].method = xchacha20-poly1305`; same AEAD, canonical target spelling |
| Enabled static `ech-opts.config` | `tlsSettings.echConfigList`; dynamic lookup and ECH with incompatible security remain unavailable |
| Ordinary `xhttp-opts.download-settings` | `xhttpSettings.extra.downloadSettings` with explicit destination and source-defined inheritance of HTTP/TLS options; `stream-one` with a download rejects |
| HY2 bandwidth, receive windows, BBR profile, salamander/gecko, ports/hop interval | `finalmask.quicParams` and UDP masks; source Mbps/byte units and hop defaults are applied. Empty SNI becomes the source server name |
| Basic MASQUE H3 WARP | Native `masqueSettings.warp` with DER keys/addresses plus URI/SNI defaults; MTU, stack, resolver, other transport and congestion controls remain outside this subset |
| WS early data in `Sec-WebSocket-Protocol` | Path `ed` query consumed by the core; arbitrary headers, path-suffix early data and fast-open are unavailable |
| `v2ray-plugin`, `mode=websocket`, explicit `mux=false` | Ordinary WS transport with TLS/HTTP fields; omitted/enabled mux and upgrade/fast-open modes remain unavailable |

Validation is reported separately from disposition. `declared-shape-only` is not full backend acceptance: library-private Mieru traffic-pattern payloads, ZeroTier identity/world binary content, Sudoku custom table layouts, TLSMirror programs, Restls scripts and source-local file contents remain within explicitly unavailable source contexts. Static ECH is different: its declared shape and Base64 encoding are validated and its bytes are mapped; complete ECH wire-format and peer compatibility are left to the TLS runtime. The importer does not claim offline certification of arbitrary ECH payloads.

Additional source checks: [Snell v6 PSK bounds](https://github.com/MetaCubeX/mihomo/blob/0455d0d28c4526937380505153e65a843d8612e8/transport/snell/v6.go), [Sudoku config validation](https://github.com/MetaCubeX/mihomo/blob/0455d0d28c4526937380505153e65a843d8612e8/transport/sudoku/config.go), [ShadowQUIC versions](https://github.com/MetaCubeX/mihomo/blob/0455d0d28c4526937380505153e65a843d8612e8/transport/shadowquic/quic_version.go). Snell version 5 is accepted as the source's version-4 compatibility alias.

Shadowsocks source: the pinned Mihomo `go.mod` selects [sing-shadowsocks2 v0.2.8](https://github.com/MetaCubeX/sing-shadowsocks2/tree/v0.2.8). Its [exact-name registry](https://github.com/MetaCubeX/sing-shadowsocks2/blob/v0.2.8/cipher/method_registry.go), [AEAD methods](https://github.com/MetaCubeX/sing-shadowsocks2/blob/v0.2.8/shadowaead/method.go), [stream methods](https://github.com/MetaCubeX/sing-shadowsocks2/blob/v0.2.8/shadowstream/method.go) and [2022 methods](https://github.com/MetaCubeX/sing-shadowsocks2/blob/v0.2.8/shadowaead_2022/method.go) distinguish invalid names from valid source methods that the pinned Xray core cannot execute. The regression fixture `bad-cipher` is invalid, not an alias.
