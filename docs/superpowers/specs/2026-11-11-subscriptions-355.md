# BROray 3.5.5: subscription import

Authorized: fix the three reported subscription URLs, add Clash import, build one candidate for aarch64/armv7/mipsel/mips, and verify all three on the test router. Publication is not part of this request.

1. Xray Int32Range empty string means 0..0, including URI and JSON XMUX. Other invalid types, negative and overflowing values remain rejected.
2. Valid endpoint user level (VLESS/VMess/native XDRIVE: uint32; Trojan/Shadowsocks: uint8, matching Xray26.11.9) is normalized to BROray's local policy. Explicit import warnings explain that foreign policy/DNS/routing are not applied. Ordinary Xray JSON and native XDRIVE must agree. Credentials and transport options must survive.
3. Clash/Mihomo YAML imports individual supported servers, not DNS/rules/proxy-groups or remote providers. Strict bounded YAML decoding, duplicate-key rejection, bounded structure and safe field-only errors are mandatory. Known unsupported protocols/options are rejected individually with reasons. AmneziaWG extensions must never silently become ordinary WireGuard. Supported mappings and limits must be documented and tested.
4. A fully failed node import must retain received/parsed/accepted/rejected and sanitized reasons; existing saved servers must remain intact. Fetch errors with no parsed payload retain zero counts.
5. The three live endpoints are fetched/parsed on the router after signed candidate installation. Preserve user data, unrelated configuration and baseline connection state. No claims of traffic support from import-only checks.

No credentials, private subscription bodies/URLs, server addresses or private signing key in artifacts. No reboot, opkg, deletion, foreign objects or public release. Preserve previous failures as evidence. Only affected tests and candidate packaging/native checks are required; unchanged prior suites are inherited with byte binding.
