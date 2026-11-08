# Protocol Import Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox syntax for tracking. Implementation is inline; one fresh final review.

**Goal:** Поддержать согласованные протоколы и параметры без потери настроек.
**Architecture:** Отдельные парсеры/валидаторы новых протоколов, общие проверяемые stream extensions; прежние записи и штатная синхронизация остаются основой. Приватные структуры маскируются целиком.
**Tech Stack:** Go 1.27.1/vendor, JavaScript без новых зависимостей, официальный Xray 26.10.31, offline arm64 QEMU.
**Spec:** `docs/superpowers/specs/2026-11-08-protocol-import-expansion.md`

## Global Constraints

- Локальная рабочая ветка с сохранением подготовленного AWG; нет публикации/изменения роутеров.
- Нет второго ядра, исполнения чужого полного Xray JSON, shell-команд из INI.
- Секреты не выводить; неизвестные значения не включать в ошибки.
- Каждый новый путь проходит RED/GREEN. Финальный smoke использует настоящий Xray.
- Git commit пока недоступен (нет имени/email); сохранять квитанции и исходники, не повторять неизменную ошибку и не подставлять чужого автора.

## Review Focus

- IPv6/percent-encoded credentials и неоднозначные query aliases → Task 1.
- INI с PostUp/AWG полями, неверные ключи/AllowedIPs → Task 2.
- JSON с полем вне схемы и ошибочной частичной подпиской → Tasks 1–3.
- Заголовки с секретами/CRLF и конфликт Host, значения 0/false → Task 3.
- Браузерный preflight не допускает backend-формат или теряет предупреждения → Task 4.

### Task 1: SOCKS5 и HTTP/HTTPS

Files: `areas/servers/proxy.go`, `protocol_expansion_test.go`, `build.go`, `validate.go`, `xrayconfig.go`, `key.go`, `store.go`; `areas/subscriptions/xrayjson_proxy.go`, `xrayjson.go`, `protocol_expansion_test.go`.
Interfaces: `parseProxy(link,id string,source map[string]any) (Record,*ParseError)`; `validateProxy(Record) *ParseError`. Record protocol socks/http, username/password, network raw, security none/tls. Outbound maps users to user/pass. JSON converter produces validated URI.
- [x] RED: URI credentials/IPv6/TLS/default ports, unknown/conflicting fields, masks, backup, JSON→record→outbound.
- [x] GREEN: implement parser/validator/dispatcher/generator/identity; converter flat+nested forms.
- [x] Run affected tests in offline VM; preserve first output.

### Task 2: WireGuard и Hysteria2 JSON

Files: `areas/servers/wireguard.go`, `protocol_expansion_test.go`; `areas/subscriptions/xrayjson_extra.go`, `extract.go`, `protocol_expansion_test.go`; dispatcher, validation, generator, masking/key hooks.
Interfaces: `parseWireGuard(text,id string,source map[string]any) (Record,*ParseError)`; `validateWireGuard(Record) *ParseError`; record wireguard map holds Xray settings, endpoint summary from first peer. JSON WG converted to one INI item; no splitting multiline item. Hysteria JSON converted through supported hy2 URI with explicit stream extensions.
- [x] RED: INI keys/peers/IPv6/MTU/reserved/DNS, command/AWG rejection; WG/HY2 JSON fields and roundtrip.
- [x] GREEN: bounded INI model, masking, validation, generated outbounds, subscription extract/conversion.
- [x] Run protocol/subscription tests; do not claim live traffic before Task 5.

### Task 3: Transport parameters and no silent losses

Files: `areas/servers/stream_options.go`, `stream_options_test.go`, `parse.go`, `build.go`, `validate.go`, `xrayconfig.go`, `key.go`; `areas/subscriptions/xrayjson.go`, `protocol_expansion_test.go`.
Interfaces: `extendedStreamOptions` map in record; validated merge into generated stream settings, keys included in canonical identity and masked if sensitive. URI typed JSON options and native JSON options share validator. URI query audit records metadata warnings or rejects unsupported keys.
- [x] RED: WS/upgrade headers+heartbeat, gRPC false/zero values, sockopt whitelist; CRLF/Host conflicts; XHTTP/TLS/REALITY/finalmask preservation; unknown fields cannot silently disappear.
- [x] GREEN: bounded strict schemas, shared option validation, generators and JSON conversion; keep existing fixtures compatible or document intentional changed rejections.
- [x] Run complete servers/subscriptions regression packages once after all touched paths are green.

### Task 4: WebUI and diagnostics

Files: `webui/static/servers.html`, `assets/js/servers-import-preflight.js`, `servers.js`; `tests/webui-availability/protocols.test.cjs`.
- [x] RED: all schemes and multiline INI reach preview/save, unsupported warnings visible, secrets absent from rendered public output.
- [x] GREEN: input framing and help, capabilities (HTTP TCP-only, AWG parse-only), safe warning display.
- [x] Node UI+availability regression tests.

### Task 5: Real core, review, report

Files: `areas/servers/udp_xray_test.go` or separate `protocol_xray_test.go`, local offline VM harness/evidence, `docs/PROTOCOL-SUPPORT.md`, report.
- [x] SHA-verify existing official Xray and derive hermetic fixture runner; no network adapter.
- [x] Test generated configs and TCP traffic for SOCKS/HTTP/HTTPS/WG/HY2; UDP for applicable protocols; honest limitations for unavailable scenario.
- [x] Full project compile, targeted final regression, one fresh code review, fixes with RED/GREEN.
- [x] Update matrix/report/ledger with exact results, unsupported fields and pending deployment. Do not publish.
