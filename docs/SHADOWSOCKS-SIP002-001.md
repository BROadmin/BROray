# Shadowsocks URI compatibility — stage 06

Base: b8aab47470d8a0cdee39be374c349723aac176e0. Runtime change: parser-shadowsocks.sh only.

## Supported inputs
SIP002 plaintext percent-encoded method:password, Base64/Base64URL userinfo with
valid optional padding, legacy whole-authority Base64, and optional endpoint slash.
Plain credentials and fragment are percent-decoded once; decoded Base64 credentials
are not decoded a second time. A literal plus remains plus, and backslashes never
become printf escapes. UTF-8 is checked byte-for-byte before shell assignment.
Hostnames, IPv4 and bracketed IPv6 (including IPv4-tail forms) are validated locally.
No DNS lookup or connection is made. Numeric ports are normalized to decimal.
Existing eight cipher names remain unchanged. No new proxy engine is introduced.

2022 keys must decode to 16 bytes (AES-128) or 32 bytes (AES-256/ChaCha20).
Colon-separated identity keys are checked individually and retained for AES-2022.
A key encoded without padding or with URL-safe characters is normalized to the same
binary key in standard padded Base64. Ordinary passwords are never normalized.
AEAD-2022 encoded userinfo and legacy envelopes remain compatibility extensions:
SIP002 prescribes plaintext percent-encoded userinfo for AEAD-2022; this importer
also preserves earlier BROray input forms when valid. It does not emit share links.

## Safety and boundaries
SIP003 plugins are not implemented: all nonempty plugin values, encoded key names,
repeated plugin parameters with a nonempty value, and bare plugin flags are rejected.
An empty plugin= is accepted as no plugin for compatibility. Other query parameters
are ignored according to SIP002; arbitrary Xray transports are not configured here.
Invalid Base64, invalid percent encoding, control bytes and invalid UTF-8 are rejected.
Errors never include the supplied URI, password, key or unknown cipher string.

Temporary decoded data lives in an exclusive mktemp directory with umask 077.
Cleanup runs on normal exit, error and handled termination signals; SIGKILL/power loss
can leave a private temporary directory and is not claimed to be recoverable here.
Neither shared util.sh nor server schema, ID calculation, generator, updater or
protected catalog commit logic changes. No actual key entropy can be proven by this parser.
Malformed addresses/ports formerly accepted are now rejected, not silently repaired.
An unescaped + in a display name now stays +; spaces should be encoded as %20.
URI length is bounded at 16384 shell characters; decoded text at 8192 Unicode characters.

## Verification sources
- https://shadowsocks.org/doc/sip002.html (URI grammar, percent encoding, plugin query)
- https://shadowsocks.org/doc/sip022.html (AEAD-2022 PSK size and representation)
- https://shadowsocks.org/doc/sip023.html (colon-separated identity PSKs)
- XTLS/Xray-core v26.9.9, infra/conf/shadowsocks.go, blob 18451ab5c978f9eb478bf8b75a819ac071e53ef1
- XTLS/Xray-core v26.9.9, proxy/shadowsocks_2022/outbound.go, blob 5d1b9c9fb8429b5ca832e78902de3c8797d53aad

Run tests only in a disposable Linux environment, not on a production router.
Use BRORAY_TEST_ROOT for the checkout; tests use temporary directories and synthetic data.
BRORAY_TEST_XRAY points to a verified binary for configuration checks, not connectivity.
HTTP/HWID/metadata, SIP008 JSON, new ciphers, SIP003 and provider interoperability remain
separate tasks. Test receipts document coverage and do not assert release readiness.
