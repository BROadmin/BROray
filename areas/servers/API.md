# areas/servers — exported API (for areas/subscriptions and others)

Import: `github.com/BROadmin/broray/areas/servers`. Every path is the 3.x
layout mapped by `compat.Deps` (`/opt/broray/servers/<id>.json` →
`d.Path("servers", id+".json")`, quality → `d.RunPath("server-quality", id+".json")`).

## Records

* `type Record = map[string]any` — a 3.x server record (schemaVersion 2),
  decoded JSON (numbers `json.Number`/`int64`). Field names and value types are
  exactly 3.x (`lib/server-import.sh`), so files are interchangeable.
* `ParseLink(link, id string, source map[string]any) (Record, error)` —
  `broray_server_import_dispatch` without the file write. Supports `vless://`,
  `vmess://` (modern `uuid@host` and legacy base64 JSON), `trojan://`, `ss://`
  (SIP002, percent form, legacy, 2022 keys), `hysteria2://` / `hy2://`, all
  stream extensions (header, ech, pcs, vcn, pqv, fm, mKCP, xhttp extra,
  ML-KEM encryption, REALITY ML-DSA). Parsing happens first, then the id is
  validated, then the record is built, extended and validated
  (`broray_server_validate`). `source == nil` → `{"type":"manual"}`; for a
  subscription pass the full source object you want stored
  (`{"type":"subscription","subscriptionId":…,"nodeIndex":…,"importKey":…}`).
  Errors are `*ParseError`; `Lines` are the 3.x stderr lines
  (`"Ошибка: …"`), `Text()` joins them (empty = 3.x silent failure; the
  preview then says "Конфигурация не прошла проверку.").
* `Validate(rec Record, label string) error` — `broray_server_validate`
  (stream fields + validate_model + structure). `label` is the path shown in
  "сервер имеет неправильную структуру: <label>".
* `CanonicalKey(rec) (string, error)` — 64-hex import key
  (`broray_server_subscription_import_key`; name/id/uri/source excluded).
  Use it for duplicate detection and `source.importKey`.
* `ContinuityKey(rec) (string, error)` — endpoint identity
  (`broray_server_subscription_continuity_key`).
* `MaskRecord(rec) Record` — `broray_server_mask_json` (for servers.cgi lists).
* `ValidID(id) bool`, `DetectProtocol(uri) (string, error)`,
  `VlessEncryptionValid(s) bool`.

## Files

* `ServersDir(d)`, `RecordPath(d, id)`, `QualityPath(d, id)`, `ActiveFile(d)`.
* `ReadRecord(d, id) (Record, error)` (`ErrNotFound` if missing; symlinks refused).
* `RecordExists(d, id) bool`, `RecordIDs(d) ([]string, error)` (glob order,
  dotfiles skipped), `ListRecords(d) ([]Record, error)`.
* `WriteRecord(d, rec) error` — validates, then writes `servers/<id>.json`
  atomically (0600).
* `DeleteRecord(d, id) error` — removes the record and its quality file
  (missing files are fine).
* `ActiveID(d) string` — first line of `config/active-server` ("" = none).
* `NewManualID(d) (string, error)` — `manual-<YYYYmmddHHMMSS>-<8 hex>`.

## Xray

* `GenerateConfig(ctx, d, rec) ([]byte, error)` — the persistent Xray config
  (`broray_generate_server_config`) with the current settings
  (`config/system/settings.json`, falling back to the daemon config).
* `TestConfig(ctx, d, cfg []byte) error` — `xray run -test -c` on the bytes.
* `XrayConfig(rec, GenSettings, label)` / `Outbound(rec)` — pure generator.
* Re-applying the active server after its record changed:
  `d.Svc.ReapplyActiveServer(ctx, reason)` (registered by this area). It
  regenerates the config of `ActiveID(d)`, runs `xray -test`, applies it via
  `d.Svc.ApplyXrayConfig` and resyncs the Keenetic interface. **The caller
  must already hold the global fence (`d.Lock`)**; it does not take it.
  Error codes: `ACTIVE_SERVER_MISSING`, `SERVER_NOT_FOUND`,
  `SERVER_CONFIG_INVALID`, `XRAY_CONFIG_REJECTED`, `XRAY_APPLY_FAILED`.
  On failure the subscription service restores the old record and calls it
  again (3.x `broray_server_import` transaction).
