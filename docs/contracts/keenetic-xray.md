# Contract: KEENETIC (managed ProxyN + KeenDNS WebUI access) and XRAY

Source: `/home/claude/broadmin/broray` @ f505928 (BROray 3.2.0-r12), read-only.
Paths below are relative to `runtime/app/` unless they start with `runtime/`.
On the router the app is installed at `/opt/broray` (`web-new/` → `/opt/broray/web-new/`, `lib/` → `/opt/broray/lib/`).

Conventions used in this file
- "Envelope" = `{success:true,data:<D>,error:null}` from `broray_api_success` (`web-new/api/auth-common.sh:96-111`) or `{success:false,data:null,error:{code,message,details}}` from `broray_api_error` (`auth-common.sh:25-57`; `details` is `null` when empty). Error responses carry `Status:` header; success responses carry none (HTTP 200) unless stated.
- Every JSON response carries the headers of `broray_api_print_json_headers` (`auth-common.sh:7-14`): `Content-Type: application/json; charset=utf-8`, `Cache-Control: no-store, no-cache, must-revalidate`, `Pragma: no-cache`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`.
- Exception: the hard-coded `AUTH_MODULE_UNAVAILABLE` fallback (`api/keenetic/status.cgi:13-20` and siblings) emits only `Status` + `Content-Type`, and its `error` object has no `details` key.
- Auth: `broray_api_require_session` (`auth-common.sh:72-95`) – cookie `BRORAY_SESSION`; failure → 401 `AUTH_REQUIRED` "Требуется авторизация."; missing `/opt/broray/lib/web-auth.sh` → 500 `SESSION_BACKEND_UNAVAILABLE` "Модуль проверки сессии недоступен.".
- Method check: `broray_api_require_method` (`auth-common.sh:59-70`) → 405 `METHOD_NOT_ALLOWED`, message "Для этой операции требуется метод <M>.", details "Получен метод <actual>.". Method check runs BEFORE the session check in every CGI here.
- CSRF: none of the CGIs in this area check `Origin`/`X-BROray-*` headers, and `lib/web-auth.sh` has no such check (grep: no matches). Only the session cookie is required. Whether lighttpd adds anything: UNCONFIRMED (no Origin rule found in `runtime/state-seed/config/lighttpd.conf`).
- Request bodies: the JS always sends `body: {}` (JSON) for POSTs except Xray install. No CGI in this area reads the body except `install.cgi`. A Go implementation must accept and ignore any body on those POSTs.
- JS client: `BROrayUI.apiRequest` (`assets/js/common.js:4-60`) throws when `!response.ok`, when the body is not a JSON object, or when it is an array. Thrown error: `.message` = `payload.error.message` (if string, non-empty) else "Ошибка запроса (HTTP n)." / "Некорректный ответ сервера (HTTP n).", `.status`, `.payload`, `.code` = `payload.error.code`, `.details`.

---

## 1. Endpoint table

| # | URL | Method | Lock | Async | JS caller |
|---|-----|--------|------|-------|-----------|
| K1 | `/api/keenetic/status.cgi[?force=1]` | GET | none | no (30 s file cache) | `keenetic.js:617-638` `loadStatus` |
| K2 | `/api/keenetic/check-upstream.cgi` | POST | global op `keenetic:check-upstream` | no | `keenetic.js:751-758` via `runAction` |
| K3 | `/api/keenetic/create.cgi` | POST | `keenetic:create` | no (blocking, can take ~30 s+) | `keenetic.js:760-767` |
| K4 | `/api/keenetic/repair.cgi` | POST | `keenetic:repair` | no | `keenetic.js:769-776` |
| K5 | `/api/keenetic/sync-description.cgi` | POST | `keenetic:sync-description` | no | `keenetic.js:778-785` |
| K6 | `/api/keenetic/delete.cgi` | POST | `keenetic:delete` | no | `keenetic.js:787-814` (after confirm dialog) |
| K7 | `/api/keenetic/web-access-status.cgi` | GET | none | no | `keenetic.js:640-674` `loadWebAccessStatus` |
| K8 | `/api/keenetic/web-access-enable.cgi` | POST | `keenetic:web-access-enable` | no | `keenetic.js:816-825` via `runWebAccessAction` |
| K9 | `/api/keenetic/web-access-disable.cgi` | POST | `keenetic:web-access-disable` | no | `keenetic.js:827-853` (after confirm) |
| X1 | `/api/xray/status.cgi` | GET | none | no (15 s cache keyed by PID) | `xray.js:471-483` `loadStatus` |
| X2 | `/api/xray/start.cgi` | POST | `xray:start` | no (blocks up to 180 s) | `xray.js:727-732` |
| X3 | `/api/xray/stop.cgi` | POST | `xray:stop` | no (≤12 s wait) | `xray.js:733-744` |
| X4 | `/api/xray/restart.cgi` | POST | `xray:restart` | no (≤180 s) | `xray.js:745-756` |
| X5 | `/api/xray/check-config.cgi` | POST | none | no | `xray.js:539-563` `checkConfig` |
| X6 | `/api/xray/update-check.cgi` | POST | none | no (GitHub, up to ~20 s per request, many requests) | `xray.js:565-588` `checkUpdate` |
| X7 | `/api/xray/diagnostics.cgi` | GET | none | no | `xray.js:590-635` `runDiagnostics` |
| X8 | `/api/xray/install.cgi` | POST (JSON body) | background job `xray:install` | **yes**, HTTP 202 | `xray.js:322-335` `installRelease` (both "install selected" and "reinstall" buttons) |
| X9 | `/api/xray/operation-status.cgi` | GET | none | poll target | `xray.js:645-715` `pollOperation` |
| X10 | `/api/xray/update.cgi` | POST | job `xray:update` | yes, 202 | **not used by JS** |
| X11 | `/api/xray/reinstall.cgi` | POST | job `xray:reinstall` | yes, 202 | **not used by JS** (the "reinstall" button posts to install.cgi, `xray.js:761-763`) |

Also used on both pages (documented by the auth area): `GET /api/session.cgi` (`keenetic.js:969-984`, `xray.js:771`, xray keep-alive every 60 000 ms `xray.js:784-788`).

---

## 2. KEENETIC page

### 2.0 Common machinery

**Interface name selection** (`lib/interface-owner.sh:199-221` `broray_interface_selected_name`, evaluated when `lib/keenetic-page.sh` is sourced, `keenetic-page.sh:11-15`):
1. `$BRORAY_INTERFACE` if set and valid;
2. else `broray_interface_owner_name` = `interfaceName` from `/opt/broray/config/interface.json` **only if** the receipt fully validates (incl. live running/startup equality, see 2.0.3) (`interface-owner.sh:1301-1309`);
3. else `.managedInterface` of `/opt/broray/routes/config.json` if it matches `Proxy[0-9]+` (`interface-owner.sh:189-197`);
4. else `Proxy0` (`BRORAY_INTERFACE_FALLBACK`, `interface-owner.sh:10`).
Name grammar: `Proxy` + one or more digits (`interface-owner.sh:75-90`).

**Library load precondition** (`interface-owner.sh:54-73`): `BRORAY_PROXY_HOST` = first `.inbounds[]|select(.protocol=="socks")|.listen` from `/opt/broray/config/config.json`, `BRORAY_PROXY_PORT` = its `.port` (default 2080). If the host is not a dotted IPv4 or port not 1..65535, the owner library aborts loading. Consequences: `broray_interface_selected_name` is undefined → interface name is `""` → every action returns rc 1 (`keenetic-page.sh:314-320`) → 500 `KEENETIC_ACTION_FAILED` with empty details; status fails (interface-core cannot load, `keenetic-page.sh:422-430` / `interface-core.sh:11`) → 500 `KEENETIC_STATUS_FAILED`.

**NDM CLI**: all Keenetic commands run as `/opt/broray/bin/broray-system-ndmc -c "<command>"` which unsets `LD_LIBRARY_PATH LD_PRELOAD` and execs `/bin/ndmc` (`bin/broray-system-ndmc:1-5`; selection `interface-owner.sh:15-16`, `web-publish.sh:8`).

**Global operation lock** used by K2–K6, K8, K9 (`api/keenetic/action-common.sh:31-45`, `web-access-action-common.sh:41-69`):
- `/opt/broray/lib/routes-api-operation.sh` unreadable → 500 `GLOBAL_LOCK_UNAVAILABLE` "Общий координатор операций недоступен."
- `broray_routes_api_lock_acquire "keenetic:<action>" keenetic` (`lib/routes-api-operation.sh:132-172`): returns 2 if `/opt/var/lib/broray-updater/request.lock` or `/tmp/broray-global-operation.lock` exists, or the updater operation pointed by `/opt/var/lib/broray/last-operation` has `state.json .running==true`, or a pending routes action disallows it; otherwise calls `broray_ops_begin system "keenetic:<action>" keenetic USER protected` (`lib/operation-client.sh:135-168`, returns 2 on any structured `{ok:false}` rejection from the coordinator).
- rc 2 → 409 `OPERATION_BUSY` "Другая конфликтующая операция BROray уже выполняется."; other non-zero → 500 `GLOBAL_LOCK_FAILED` "Не удалось установить общую блокировку BROray."
- Lock is finished `completed` inside `broray_api_success`, `failed` inside `broray_api_error` (`auth-common.sh:16-23,31,99`). If finishing fails on success → 503 `OPERATION_FINALIZATION_FAILED` "Результат операции сохранён, но её завершение ещё не подтверждено." (`auth-common.sh:99-101`).
- The lock is taken BEFORE the backend library check, so a missing backend still consumes/fails an operation record.

#### 2.0.1 Status model – `broray_keenetic_status_json` (`lib/keenetic-page.sh:629-955`)

**Expected side** (`broray_keenetic_read_expected_json`, `keenetic-page.sh:29-109`):
- Runs `/opt/broray/bin/broray xray status` (→ `xray-control.sh:120-130` → `lib/xray.sh` status `.data`, see X1) and `/opt/broray/bin/broray current` (→ `lib/server.sh:103-` active server JSON). Invalid/failed output → `{}`.
- `listen = .socks.address // .socks.listen // ""`, `port = .socks.port // 0`, `socks_active = .socks.active // false` (from xray status: address from `settings.json .xray.socksAddress // .listenAddress // .socksAddress` default `127.0.0.1`, port `.xray.socksPort // .socksPort` default `2080`, `xray.sh:207-248`).
- `server_name = .name // .id`, whitespace collapsed, trimmed, chars `" \ ; | & \` $ < >` removed (`keenetic-page.sh:48-50`).
- `description = "BROray - " + server_name` (ASCII hyphen), cleared if it fails `broray_interface_owner_description_valid` (≤128 chars, prefix `BROray` or `BROray - `, none of `" ' \ ; \` $ | & < >`, single line; `interface-owner.sh:92-106`).
- `upstream = listen + ":" + port` when listen non-empty and port>0.
- `ready = listen!="" && port>0 && server_name!="" && description!=""`.
- Output: `{ready, interfaceName, description, protocol:"socks5", upstream, socks:{listen,port,active}, activeServer:{name}}`.
- NOTE: expected host/port come from **settings.json via xray status**, while the mutation layer uses the **config.json socks inbound** (`interface-owner.sh:54-66`). They can disagree.

**Actual side** (`broray_keenetic_read_actual_json`, `keenetic-page.sh:378-601`; sources `lib/interface-core.sh`):
- `exists` = `interface <name>` line present in `show running-config` (`interface-owner.sh:990-1002`, `1543-1554`).
- If exists: fields read from `show interface <name>` via `broray_interface_value` (`interface-core.sh:226-260`): `type, description, link, connected, state, mtu, via, local-endpoint-address, remote-endpoint-address, admin-only`. A field counts only if a line beginning (after leading spaces) with `<field>:` exists; value = text after `:` trimmed. Each value read re-runs `show interface` (10 ndmc calls). ndmc rc≠0 or any stderr → field empty.
- `protocol` = 3rd token of the first `proxy protocol X` line of the running-config block, lowercased; `upstream` = `"$3:$4"` of first `proxy upstream H P` line (`keenetic-page.sh:489-531`). Block = lines from exact `interface <name>` to first `!` (`interface-owner.sh:975-988`).
- `link` true iff raw ∈ {up,yes,true,on}; `connected` same set; `adminOnly` true iff ∈ {yes,true,on}; `mtu` number or `null`.
- Output: `{exists, interfaceName, description, type, protocol, upstream, link:bool, connected:bool, state:"<raw>", mtu:int|null, via, localAddress, remoteAddress, adminOnly:bool, raw:{link,connected,state}}`.

**Comparison** (`keenetic-page.sh:607-627,650-850`):
- Description normalisation (server side): tabs/newlines→space; NBSP→space; the dash characters U+2010, U+2011, U+2012, U+2013, U+2014, U+2015, U+2212, U+FE58, U+FE63, U+FF0D → `-`; any `\s*-\s*` → ` - `; collapse spaces; trim. `checks.description = normalized(actual)==normalized(expected)` (note: true when both are empty).
- `protocolMatch = expected.ready && exists && actual.protocol==expected.protocol`
- `upstreamMatch = expected.ready && exists && actual.upstream==expected.upstream` (exact string compare `"H:P"`)
- `linkOk = exists && actual.link`; `connectedOk = exists && actual.connected`; `stateOk = exists && actual.state=="up"`
- `upstreamReachable = expected.socks.active` (Xray SOCKS port in LISTEN per `netstat -lnt`, and Xray running). **There is no separate TCP probe**: "check upstream" just recomputes status.
- `matchesExpected = ready && exists && protocolMatch && upstreamMatch && descriptionMatch`
- `healthy = exists && upstreamReachable && linkOk && connectedOk && stateOk`
- `$displayName = expected.description // "BROray"` (jq `//`: an empty string is NOT replaced, so displayName is `""` when no active server → problem texts contain a double space, e.g. "Интерфейс  не создан.").
- `problems` (array, then jq `unique` ⇒ sorted lexicographically, deduplicated), in generation order:
  1. `!ready` → "Не получены параметры SOCKS Xray или имя активного сервера."
  2. `!exists` → "Интерфейс <D> не создан."
  3. exists && !protocolMatch → "Протокол <D> не совпадает с ожидаемым."
  4. exists && !upstreamMatch → "Адрес Xray в <D> настроен неправильно."
  5. exists && !descriptionMatch → "Описание <D> не совпадает с активным сервером."
  6. !upstreamReachable → "Прокси Xray недоступен."
  7. exists && !linkOk → "Связь интерфейса <D> не установлена."
  8. exists && !connectedOk → "Интерфейс <D> не подключён."
  9. exists && !stateOk → "Интерфейс <D> не работает."
- **Ownership overlay** (`keenetic-page.sh:852-941`):
  - `ownershipConfirmed = exists && broray_interface_owner_valid(name)` (full receipt check, 2.0.3).
  - `ownershipReceiptPresent` = `config/interface.json` is a regular non-symlink file with `(schemaVersion 1 or 2) && owner=="BROray" && interfaceName==name`.
  - `checks.ownership = exists ? ownershipConfirmed : true`.
  - If exists && !ownershipConfirmed: `healthy=false`, `matchesExpected=false`, problem "Интерфейс <D> не подтверждён как принадлежащий BROray." added (then `unique`).
  - `interfaceType="managed-socks-proxy"`, `interfaceDisplayName=$displayName`.
  - `health` (schema of `common.js:333-372`): `severity` = "error" if !expectedReady or !exists or !ownershipConfirmed or !healthy; "warning" if !matchesExpected; else "ok". `availability:"available"`, `operational:healthy`, `consistent: matchesExpected && (!exists || ownershipConfirmed)`, `actionRequired: severity!="ok"`, `freshness:{state:"fresh",checkedAt:updatedAt}`, `lastOperation:null`, `reasons` (`unique_by(.code)`, i.e. sorted by code) from codes `KEENETIC_EXPECTED_UNAVAILABLE` ("Не получены параметры SOCKS Xray или активного сервера."), `PROXY0_MISSING`, `PROXY0_OWNERSHIP_UNCONFIRMED`, `XRAY_SOCKS_UNAVAILABLE` ("Прокси Xray недоступен."), `PROXY0_CONFIGURATION_MISMATCH` ("Настройки <D> отличаются от ожидаемых."), `PROXY0_DOWN` ("Интерфейс <D> не находится в рабочем состоянии."), each `{code,message,details:null}`; `facts:{interfaceName, exists, ownershipConfirmed, ownershipReceiptPresent, socksActive, link, connected, state, matchesExpected}`.
- `updatedAt` = `date '+%Y-%m-%dT%H:%M:%S%z'` (e.g. `2026-11-02T10:15:30+0300`, offset without colon) (`keenetic-page.sh:19-22`).
- Result is atomically written to `/opt/broray/run/keenetic-status.json` (tmp + mv, `keenetic-page.sh:943-952`). That file is also read by `lib/server-service.sh:87-101,389-401` (home dashboard – other area).

**Cache** (`keenetic-page.sh:978-1006`): non-forced status returns the file verbatim if it is a non-empty, non-symlink, valid-JSON file with mtime age < 30 s (`BRORAY_KEENETIC_STATUS_CACHE_SECONDS`). The JS always uses `force=1` (`keenetic.js:746,978`), so the cache only serves other callers.

#### 2.0.2 Ownership receipt `/opt/broray/config/interface.json` (schema 2, written by `broray_interface_owner_write`, `interface-owner.sh:1366-1435`, mode 0600, atomic mv)
```json
{"schemaVersion":2,"contract":"r14c38-proxy-owned-interface/1","owner":"BROray",
 "interfaceName":"Proxy0","protocol":"socks5","upstream":{"host":"192.168.1.1","port":2080},
 "description":"BROray - Server","selectionMode":"created|repaired|adopted-existing|description-synced|legacy-schema1-migrated|allocated",
 "writeProtocolSha256":"dc8501856b27260738828e9959e10512496410ff44adfceaa8564056bed1ddfa",
 "runningBlockSha256":"<64hex>","startupBlockSha256":"<64hex>","updatedAt":"2026-11-02T10:15:30+0300"}
```
`writeProtocolSha256` is the constant `BRORAY_KEENETIC_WRITE_POLICY_SHA256` from `lib/keenetic-write-policy.sh:24`; any policy mismatch invalidates receipts (`interface-owner.sh:1226-1235`). Accepted contracts: r14c34…r14c38 (`interface-owner.sh:1227-1231`). Schema 1 receipts (legacy) are accepted only if the live blocks match the legacy shape (`interface-owner.sh:1240-1299`) and are migrated on repair.

The receipt is **not deleted** by delete (`interface-manage.sh:420-461`); after delete it becomes invalid (live block absent) but `ownershipReceiptPresent` stays true. JS comment `keenetic.js:329-330` relies on this ("A retained receipt is history").

#### 2.0.3 Exact running/startup block comparison (`broray_interface_owner_record_valid`, `interface-owner.sh:1197-1238`)
Receipt valid iff: `interfaceName==selected`, `owner=="BROray"`, `protocol=="socks5"`, `upstream.host/port == BRORAY_PROXY_HOST/PORT` (current config.json socks inbound), for schema 2: valid description, `runningBlockSha256==startupBlockSha256`, allowed contract, policy SHA equal, and `broray_interface_source_exact` for both `running` (`show running-config`) and `startup` (`more startup-config`) (`interface-owner.sh:558-594,774-803`):
- Block extraction (`interface-owner.sh:596-633`): exactly one exact line `interface <name>`, terminated by exactly one `!`, no nested `interface` line; rc 1 = absent, rc 2 = malformed.
- Exact signature (`interface-owner.sh:674-729`), block lines (without the `!`):
  ```
  interface ProxyN
      description "BROray - X"        (line 2; or `    description BROray`; UTF-8 bytes ≥0x80 may appear as \xHH and are decoded, interface-owner.sh:121-165)
      security-level public
      ip global <1..65534>            (optional, only as line 4)
      proxy protocol socks5
      proxy upstream <host> <port>
      up                              OR   proxy connect via <token>  +  up
  ```
  `<token>` = `[A-Za-z][A-Za-z0-9._/-]*`, 1..64 chars, no `..` or `//`. Total 6 or 7 lines (+1 if `ip global`), exactly one `up`.
- Declaration count of `interface <name>` in the snapshot must be 1.
- Block SHA = sha256 of block with `ip global` and `proxy connect via` lines removed (`interface-owner.sh:731-772`); must equal the receipt SHA.
- Any ndmc rc≠0 or non-empty stderr on `show running-config` / `more startup-config` → capture fails → invalid.

`broray_interface_owner_valid` = record valid (+ legacy migratable check for schema 1) (`interface-owner.sh:1327-1339`).

#### 2.0.4 Adoption of an existing ProxyN (`broray_interface_select_safe`, `interface-owner.sh:1480-1525`; called by create/repair via `broray_interface_select_for_action`, `interface-manage.sh:149-160`)
1. If a fully valid receipt names an interface: absent in running-config → reuse that name; present and owner_valid → reuse.
2. Else `preferred` = routes `managedInterface`; if its running block passes the *loose* signature (`interface-owner.sh:1341-1356`: block contains `proxy protocol socks5`, `proxy upstream <HOST> <PORT>`, and a line matching `^\s*description\s+"?BROray(\s|—|-|"|$)`) → `adopted-existing`.
3. Else `broray_interface_first_matching` – exactly one `interface ProxyN` in running-config passing the loose signature (`interface-owner.sh:1577-1599`) → `adopted-existing`.
4. Else `broray_interface_first_free` – first `Proxy0..Proxy255` with no `interface ProxyN` line in running-config (`interface-owner.sh:1455-1478`) → `allocated-free`.
For `adopted-existing`, `broray_interface_owner_write(name,"adopted-existing")` is executed immediately (requires the exact signature with the **current expected description** on both running and startup and equal SHAs); failure → stderr `BRORAY_PROXY_ERROR:PROXY_SAFE_SELECTION_FAILED:…` → action fails 500.
`broray_interface_sync_route_policy` (`interface-owner.sh:371-499`) then rewrites `routes/config.json .managedInterface`, `targetInterface` in `routes/manifests/*.json` and `routes/catalog/*/{routes,version}.json`, and – if the name changed – resets `routes/installed/routes.json`, `routes/installed/bundles/*.json`, `routes/state/*.json`, deletes catalog export artefacts, and always deletes `run/routes-router-config-cache.json`.

Write gate for every ProxyN mutation: `broray_interface_require_write_policy` (`interface-owner.sh:515-544`) – policy rc 2/3/4/5 → stderr `BRORAY_PROXY_ERROR:PROXY_PHYSICAL_WRITE_PROTOCOL_REQUIRED|PROXY_WRITE_POLICY_INVALID|PROXY_WRITE_PATH_UNKNOWN|PROXY_SERIALIZATION_DISCOVERY_REQUIRED`. In this build the policy is `staged/enabled=true` (`keenetic-write-policy.sh:20-28`) so it passes.

#### 2.0.5 Action dispatcher `broray_keenetic_run_action` (`keenetic-page.sh:309-372`)
- Interface name must be `Proxy<digits>` else rc 1.
- `create|repair`: requires `expected.ready` else stderr "Невозможно настроить <name> без SOCKS Xray и активного сервера." rc 2 → **409** `KEENETIC_EXPECTED_STATE_UNAVAILABLE`. Then `ash /opt/broray/lib/interface.sh <create|repair>` (stdout+stderr captured; on rc≠0 echoed to stderr and returned), then `interface.sh sync-name`, `sleep 1`, full status.
- `delete`: `interface.sh delete`, `sleep 1`, status.
- `sync-description`: `interface.sh sync-name`, `sleep 1`, status.
- `check-upstream|refresh`: status only.
- Success output = full status JSON (identical shape to K1).

### K1 `GET /api/keenetic/status.cgi`
- File: `api/keenetic/status.cgi:1-50`. Query: `force=1` (matched as an exact `&force=1&` token in `&$QUERY_STRING&`) selects uncached status; anything else → cached (30 s).
- Errors: 405; 401; 500 `KEENETIC_BACKEND_UNAVAILABLE` "Backend-модуль Keenetic недоступен." (`/opt/broray/lib/keenetic-page.sh` unreadable); 500 `KEENETIC_STATUS_FAILED` "Не удалось получить состояние управляемого ProxyN." details = last 30 stderr lines.
- Side effects: ndmc reads (`show running-config` ×≥2, `show interface <n>` ×10, `more startup-config` + `show running-config` again for ownership), `broray xray status` (spawns `xray run -test`), `broray current`; writes `/opt/broray/run/keenetic-status.json`; temp files under `/opt/broray/tmp/`.
- Success `data` example (illustrative, built from the jq programs):
```json
{"exists":true,"healthy":true,"matchesExpected":true,"interfaceName":"Proxy0",
 "description":"BROray - Netherlands","protocol":"socks5","upstream":"192.168.1.1:2080",
 "link":true,"connected":true,"state":"up","updatedAt":"2026-11-02T10:15:30+0300",
 "expectedReady":true,"upstreamReachable":true,
 "expected":{"ready":true,"interfaceName":"Proxy0","description":"BROray - Netherlands","protocol":"socks5",
   "upstream":"192.168.1.1:2080","socks":{"listen":"192.168.1.1","port":2080,"active":true},
   "activeServer":{"name":"Netherlands"}},
 "actual":{"exists":true,"interfaceName":"Proxy0","description":"BROray - Netherlands","type":"Proxy",
   "protocol":"socks5","upstream":"192.168.1.1:2080","link":true,"connected":true,"state":"up","mtu":1500,
   "via":"","localAddress":"","remoteAddress":"","adminOnly":false,"raw":{"link":"up","connected":"yes","state":"up"}},
 "checks":{"exists":true,"protocol":true,"upstream":true,"description":true,"upstreamReachable":true,
   "link":true,"connected":true,"state":true,"ownership":true},
 "technical":{"type":"Proxy","mtu":1500,"via":"","localAddress":"","remoteAddress":"","adminOnly":false},
 "problems":[],"ownershipConfirmed":true,"ownershipReceiptPresent":true,
 "interfaceType":"managed-socks-proxy","interfaceDisplayName":"BROray - Netherlands",
 "health":{"schemaVersion":1,"module":"keenetic","availability":"available","severity":"ok","operational":true,
   "consistent":true,"actionRequired":false,"freshness":{"state":"fresh","checkedAt":"2026-11-02T10:15:30+0300"},
   "reasons":[],"facts":{"interfaceName":"Proxy0","exists":true,"ownershipConfirmed":true,"ownershipReceiptPresent":true,
   "socksActive":true,"link":true,"connected":true,"state":"up","matchesExpected":true},"lastOperation":null}}
```
- Fields the JS reads (`keenetic.js`): `expectedReady` (215,322,358,397), `exists` (321,365), `healthy` (333,372,379), `checks.*` incl. `ownership` (210,281,303), `actual.description/protocol/upstream` (211,326,353,419,423), `expected.description/protocol/upstream` (212,327,341,354,418,422), `interfaceDisplayName` (340), `description` (342), `upstreamReachable` (404), `protocol` (408), `upstream` (409), `link` (410), `connected` (411), `state` (412), `technical.type/mtu/via/localAddress/remoteAddress` (426-430), `updatedAt` (431), `problems` (300). Not read: `matchesExpected`, `health`, `ownershipConfirmed`, `ownershipReceiptPresent`, `interfaceName`, `interfaceType` (JS recomputes match itself).

### K2–K6 Keenetic actions (POST)
- Files: `api/keenetic/{check-upstream,create,repair,sync-description,delete}.cgi` (2-line wrappers) → `api/keenetic/action-common.sh:1-95`. Body ignored.
- Order: method (405) → session (401) → global lock (409/500) → backend lib (500 `KEENETIC_BACKEND_UNAVAILABLE`) → action.
- Success: envelope with the full status JSON (same as K1). JS `runAction` (`keenetic.js:723-742`) calls `renderStatus(data)` and toasts the success message.
- Errors from the action (`action-common.sh:60-95`):
  - rc 0 but stdout not JSON → 500 `KEENETIC_INVALID_RESPONSE` "Backend Keenetic вернул некорректный ответ."
  - rc 2 → **409** `KEENETIC_EXPECTED_STATE_UNAVAILABLE` "Операция недоступна без параметров SOCKS Xray и активного сервера."
  - other → 500 `KEENETIC_ACTION_FAILED` "Не удалось выполнить операцию с управляемым ProxyN."
  - `details` = last 30 lines of (stderr then stdout). Details contain machine lines such as `BRORAY_PROXY_ERROR:<CODE>:<text>`, `BRORAY_PROXY_STAGE_ERROR:stage=<s> rc=<n>`, `BRORAY_PROXY_REPAIR_ROLLBACK=PASS failedStage=<s>`, `ОШИБКА: интерфейс ProxyN уже существует` / `Используйте repair`, `ОШИБКА: интерфейс ProxyN занят и не принадлежит BROray`. The JS never shows `details` (only `error.message`, `keenetic.js:153-179`).
- JS success toasts: check-upstream "Проверка подключения к Xray завершена."; create "Интерфейс <name> создан."; repair "Интерфейс <name> восстановлен."; sync "Описание <name> синхронизировано."; delete "Интерфейс <name> удалён." (name = `currentInterfaceName()`, i.e. display description).

**Exact Keenetic commands per action**

`check-upstream`: read-only, identical to K1 forced status (`keenetic-page.sh:340-342`).

`create` (`interface-manage.sh:224-260`, `162-222`):
1. selection/adoption (2.0.4). If the selected interface exists: owner-valid → "ОШИБКА: интерфейс X уже существует / Используйте repair"; else "…занят и не принадлежит BROray / Чужой прокси-интерфейс оставлен без изменений"; rc 1.
2. Commands (plain `broray_interface_ndmc`, no stage evidence): `interface ProxyN` → (verify exists) → `interface ProxyN proxy protocol socks5` → `interface ProxyN proxy upstream <H> <P>` → `interface ProxyN proxy connect` → `interface ProxyN description "BROray - X"` (or `interface ProxyN description BROray` when no server) → `interface ProxyN security-level public` → `interface ProxyN up` → `sleep 5` → `system configuration save` → verify exists → `broray_interface_wait_present` (up to 10 attempts, 1 s apart: running exact, startup exact, runtime ready) (`interface-manage.sh:56-81`). Runtime ready = `show interface` has single `type: Proxy`, `link: up`, `description: <expected>`, `connected: yes`, `state: up` (`interface-core.sh:262-296`).
   Convergence failure → stderr `BRORAY_PROXY_ERROR:PROXY_CREATE_CONVERGENCE_TIMEOUT:…`.
3. On any failure with the interface present → rollback `no interface ProxyN`, `sleep 5`, `system configuration save`.
4. Loose signature check, `owner_write(name,"created")`, route policy sync.
5. Then (page layer) `interface.sh sync-name` and status.
Description text is derived from `/opt/broray/config/active-server` → `/opt/broray/servers/<id>.json .name // .id` sanitised (`lib/interface-sync.sh:10-50`); `BROray` alone when there is no active server.

`repair` (`interface-manage.sh:390-418`, `330-388`):
- Not existing → same as create sequence (no "exists" refusal), then owner_write `repaired`.
- Existing → `require_owned` (else `BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:…` rc 1); schema-1 receipt → legacy migration: `interface ProxyN description "…"`, `interface ProxyN proxy connect`, `interface ProxyN up`, sleep 5, `system configuration save`, exact wait; rollback `interface ProxyN description null` or `interface ProxyN no description` + save (`interface-manage.sh:290-328`).
- If receipt host/port == current and description == expected and runtime ready → "Исправление не требуется" (no writes).
- Else staged commands with evidence: `interface ProxyN proxy upstream <H> <P>` (if host/port changed), `interface ProxyN description …` (if changed), then `sleep 5` + `system configuration save` (only if something changed), wait_present incl. baseline SHA (the rest of running/startup config unchanged apart from the proxy block – `broray_interface_scope_baseline_sha256`, `interface-owner.sh:832-854`).
- Failure → rollback `broray_interface_restore_owned` (`interface-manage.sh:262-288`): `interface ProxyN` (if absent), protocol, upstream(old), `proxy connect`, description(old), `security-level public`, `up`, sleep 5, save, wait; stderr `BRORAY_PROXY_REPAIR_ROLLBACK=PASS failedStage=…` or `BRORAY_PROXY_ERROR:PROXY_RECOVERY_REQUIRED:…`.
- Success → owner_write `repaired`, route sync, delete `run/interface-last-failure.json`.

`sync-description` (`interface-sync.sh:195-255`): needs exact owner, or (receipt exists and differs only by description – `broray_interface_description_drift_admissible`, `interface-sync.sh:151-193`); else `BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:…`. If old == expected → "Синхронизация не требуется". Else `interface ProxyN description "…"` → `sleep 5` → `system configuration save` → exact wait (10×1 s); rollback to old description + save. Success → owner_write `description-synced`.

`delete` (`interface-manage.sh:440-461,420-438`): no valid owner → if no receipt file and name absent in both running and startup → "Интерфейс X уже отсутствует; delete mutation не выполнялась" (success, rc 0); else `BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:полный R14C01 ownership receipt отсутствует` rc 1 (this is the case after a previous delete: receipt retained but invalid → **500**). Valid owner → `no interface ProxyN` → `sleep 5` → `system configuration save` → verify absent in running-config. Receipt file is left in place.

Stage evidence: staged commands write `/opt/broray/run/interface-last-command.json` (`{schemaVersion:1,contract:"r14c01-proxy-ndmc-stage-evidence/1",stage,command,rc,stdout,stderr,observedAt}`, `interface-core.sh:16-36`); failures copy it to `/opt/broray/run/interface-last-failure.json` (`interface-manage.sh:19-29`). A staged command fails if rc≠0 **or stderr non-empty** (`interface-core.sh:173-179`). Allowed staged grammar: `interface-core.sh:67-137`.

Timing: a successful create ≈ 7 ndmc writes + 5 s settle + save + convergence polls + 1 s + sync-name + full status (several seconds to tens of seconds). The JS has no timeout of its own (fetch default).

### K7 `GET /api/keenetic/web-access-status.cgi`
- File: `api/keenetic/web-access-status.cgi:1-45` → `broray_web_publish_status_json` (`lib/web-publish.sh:225-379`). Read-only (never adopts/repairs).
- Errors: 405, 401, 500 `WEB_ACCESS_BACKEND_UNAVAILABLE` "Модуль управления адресом WebUI недоступен.", 500 `WEB_ACCESS_STATUS_FAILED` "Не удалось получить состояние адреса WebUI через KeenDNS." (also when output is not a JSON object). Note: `web-publish.sh` itself aborts loading if `lib/keenetic-write-policy.sh` is missing (stderr `BRORAY_WEB_PUBLISH_ERROR:library:WRITE_POLICY_LIBRARY_UNAVAILABLE:…`, `web-publish.sh:17-24`).
- Inputs: LAN IP = first line of `/opt/broray/run/lan-ip` (dotted IPv4) (`web-publish.sh:123-132`; written by `lib/network.sh:135-137` / `lib/package-setup.sh:436`); port constant 8080 (`BRORAY_WEB_PUBLISH_PORT`); object name constant `broray`.
- ndmc reads: `show ndns` (fields `name`, `domain`, `access` parsed as `key: value`, `web-publish.sh:176-199`); `show running-config` (failure → whole status fails rc 1).
- `address = "https://broray.<name>.<domain>/"` iff `name` is a DNS label (1–63, `[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?`) and `domain` is a valid ≥2-label domain (`web-publish.sh:201-220,249-259`). `localAddress = "http://<lan>:8080/"`.
- Live block: lines from `ip http proxy broray` (after trimming) to `!` (`web-publish.sh:397-420`). Owned exact (`web-publish.sh:422-494,562-575`): exactly one parent, one `!` (last), exactly one `upstream http <host> <port>` matching the receipt, optional lines `domain ndns`, `ssl redirect`, `security-level public` each ≤1 and matching the receipt's recorded presence profile, no other non-empty lines.
- Receipt `/opt/broray/config/web-publish.json` (`web-publish.sh:496-526,594-637`): `{schemaVersion:3, owner:"BROray", name:"broray", writePolicySha256:<policy sha>, serializationProfile:"dynamic-bounded-v1", upstream:{scheme:"http",host:<lan>,port:8080}, domain:"ndns", sslRedirect:true, securityLevel:"public", serialization:{domain,sslRedirect,securityLevel: "present"|"omitted"}, updatedAt}`.
- Recovery marker `/opt/broray/config/web-publish-recovery-required.json` (`{schemaVersion:1,state:"recovery-required",path:"web-publish",failedStage,writePolicySha256,updatedAt}`, `web-publish.sh:750-767`).
- State machine (`web-publish.sh:288-331`):
  | condition (first match) | state | canEnable | canDisable | consistent | reason.code / message |
  |---|---|---|---|---|---|
  | recovery marker exists | `recovery` | false | false | false | `WEB_ACCESS_RECOVERY_REQUIRED` "Предыдущая операция с адресом WebUI требует восстановления." |
  | live block owned-exact | `enabled` | true only if !consistent && lan && ndns && policy | true | lan valid && receipt host==lan && port==8080 | !consistent → `WEB_ACCESS_LAN_CHANGED` "LAN-адрес роутера изменился; адрес WebUI можно восстановить."; else !ndns → `KEENDNS_ADDRESS_UNAVAILABLE` "Публикация включена, но KeenDNS не вернул корректное доменное имя."; else `WEB_ACCESS_ENABLED` "Адрес WebUI через KeenDNS включён и принадлежит BROray." |
  | no live block and no receipt file | `disabled` | lan && ndns && policy | false | false | `WEB_ACCESS_DISABLED` "Адрес WebUI через KeenDNS выключен." / `WEB_ACCESS_LAN_UNAVAILABLE` "Не удалось определить локальный адрес WebUI." / `KEENDNS_ADDRESS_UNAVAILABLE` "KeenDNS не вернул корректное доменное имя." / `WEB_ACCESS_POLICY_UNAVAILABLE` "Управление адресом WebUI недоступно для текущего профиля записи." (checked in that order) |
  | otherwise | `conflict` | false | false | false | `WEB_ACCESS_OWNERSHIP_CONFLICT` "Обнаружен чужой, изменённый или неполный объект broray; автоматическое изменение запрещено." |
- Success `data`:
```json
{"schemaVersion":1,"state":"enabled","enabled":true,"consistent":true,"canEnable":false,"canDisable":true,
 "address":"https://broray.myrouter.keenetic.pro/","localAddress":"http://192.168.1.1:8080/",
 "keenDns":{"available":true,"name":"myrouter","domain":"keenetic.pro","access":"cloud"},
 "ownership":{"liveBlockPresent":true,"receiptPresent":true,"receiptValid":true,"liveBlockOwnedExact":true},
 "recoveryRequired":false,"reason":{"code":"WEB_ACCESS_ENABLED","message":"Адрес WebUI через KeenDNS включён и принадлежит BROray."}}
```
  Empty strings are emitted as `null` for `address`, `localAddress`, `keenDns.name/domain/access`.
- JS reads (`keenetic.js:474-523`): `reason.message` (fallback "Состояние адреса WebUI получено."), `enabled`, `address`, `localAddress`, `keenDns.access` (mapped `cloud`→"Через облако", `direct`→"Прямой доступ", `auto`→"Автоматически", other → raw value or "—", case-insensitive), `state` (`enabled` + `consistent===true` → badge "Включён"/success, `enabled` otherwise → "Требуется восстановление"/warning, `disabled` → "Выключен", `conflict` → "Конфликт"/error, `recovery` → "Требуется восстановление"/error, anything else → "Недоступен"/warning), `consistent`, `canEnable`, `canDisable`. Enable button label "Восстановить адрес" when enabled else "Включить". Open/Copy buttons shown only when `enabled && address`.
- On error the JS sets badge "Ошибка", shows the message in the description, hides all four web-access buttons (`keenetic.js:659-670`).

### K8 `POST /api/keenetic/web-access-enable.cgi`, K9 `POST /api/keenetic/web-access-disable.cgi`
- Files: wrappers → `api/keenetic/web-access-action-common.sh:1-121`. Order: method, session, action check (400 `WEB_ACCESS_ACTION_INVALID` "Неизвестная операция с адресом WebUI." – unreachable via the wrappers), global lock `keenetic:web-access-<enable|disable>` (same 409/500 as above), backend (500 `WEB_ACCESS_BACKEND_UNAVAILABLE`).
- Success: runs `broray_web_publish_ensure` / `_delete`, then recomputes `broray_web_publish_status_json`; success `data` = K7 shape. If the action succeeded but status fails → 500 `WEB_ACCESS_STATUS_FAILED` "Операция выполнена, но итоговое состояние адреса WebUI не подтверждено.".
- Failure mapping (`web-access-action-common.sh:99-121`): takes the **last** stderr line `BRORAY_WEB_PUBLISH_ERROR:<stage>:<CODE>:<message>`; `error.code=<CODE>`, `error.message=<message>` (everything after the 3rd colon). HTTP 409 if CODE matches `*OWNERSHIP*`, `*RECEIPT*`, `*RECOVERY_REQUIRED`, `*DEVELOPMENT_WRITE_DISABLED`, `*WRITE_POLICY_INVALID`, `*WRITE_PATH_INVALID`, `*PHYSICAL_SERIALIZATION_REQUIRED`; else 500. No line → 500 `WEB_ACCESS_ACTION_FAILED` "Не удалось изменить адрес WebUI через KeenDNS.". `details` = last 30 stderr lines. Because the last line wins, a failed rollback yields 500 `WEB_PUBLISH_ROLLBACK_FAILED` "Byte-exact rollback не подтверждён; установлен recovery marker." (rollback line printed after the primary error, `web-publish.sh:828-834`). Note `WEB_PUBLISH_DELETE_AUTHORITY_REFUSED` maps to **500** (does not match the 409 patterns).
- Full error code list from `web-publish.sh`: `WEB_PUBLISH_LAN_IP_INVALID`, `WEB_PUBLISH_NAME_INVALID`, `WEB_PUBLISH_PORT_INVALID`, `WEB_PUBLISH_RECOVERY_REQUIRED`, `WEB_PUBLISH_STORAGE_UNAVAILABLE`, `WEB_PUBLISH_RUNNING_CONFIG_UNAVAILABLE`, `WEB_PUBLISH_RECEIPT_INVALID`, `WEB_PUBLISH_RECEIPT_BACKUP_FAILED`, `WEB_PUBLISH_OWNERSHIP_MISMATCH`, `WEB_PUBLISH_RECEIPT_LIVE_MISMATCH`, `WEB_PUBLISH_NDNS_PREREQUISITE_FAILED`, `WEB_PUBLISH_APPLY_COMMAND_FAILED`, `WEB_PUBLISH_VERIFY_TIMEOUT`, `WEB_PUBLISH_SAVE_CONVERGENCE_FAILED`, `WEB_PUBLISH_RECEIPT_COMMIT_FAILED`, `WEB_PUBLISH_FINAL_VERIFY_FAILED`, `WEB_PUBLISH_DELETE_AUTHORITY_REFUSED`, `WEB_PUBLISH_DELETE_COMMAND_FAILED`, `WEB_PUBLISH_RECEIPT_DELETE_FAILED`, `WEB_PUBLISH_ROLLBACK_FAILED`, `WEB_PUBLISH_COMMAND_NOT_AUTHORIZED`, policy codes `WEB_PUBLISH_DEVELOPMENT_WRITE_DISABLED|WRITE_POLICY_INVALID|WRITE_PATH_INVALID|PHYSICAL_SERIALIZATION_REQUIRED|WRITE_POLICY_FAILED`. Messages are verbatim strings in `web-publish.sh:45-49,855-1073`.
- **Enable** (`broray_web_publish_ensure`, `web-publish.sh:851-990`):
  1. lan from `run/lan-ip`; recovery marker → refuse; `show running-config`.
  2. If a live block or receipt exists → policy check. Receipt present must be valid (else `RECEIPT_INVALID`); backed up to `/opt/broray/tmp/web-publish-owner-before.<pid>.json`.
  3. Live block present: must be owned-exact vs receipt (else `OWNERSHIP_MISMATCH`). If receipt host==lan, port==8080 and block exact → success "KeenDNS HTTP Proxy BROray уже настроен и подтверждён." (no writes). Else initial_state=owned (LAN migration).
  4. Live block absent: receipt present → `RECEIPT_LIVE_MISMATCH` (no adoption). Else initial_state=absent.
  5. Prerequisites: no recovery marker, policy, `show ndns` returns rc 0 and non-empty.
  6. Commands, in order: `ip http proxy broray`, `ip http proxy broray upstream http <lan> 8080`, `ip http proxy broray domain ndns`, `ip http proxy broray ssl redirect`, `ip http proxy broray security-level public` (`web-publish.sh:655-673`).
  7. Wait exact in running-config (8 attempts, 1 s); `system configuration save`; wait exact again; write receipt (profile observed live); final verify.
  8. Any failure → rollback: from absent: `no ip http proxy broray`, wait absent, save, wait absent, restore receipt; from owned: re-apply the 5 commands with old host/port, wait, save, wait, restore receipt. Rollback failure → recovery marker written.
- **Disable** (`broray_web_publish_delete`, `web-publish.sh:992-1075`): recovery marker → refuse. No live block: receipt present → `RECEIPT_LIVE_MISMATCH`; else success (no-op). Live block: policy; must be owned-exact (else `DELETE_AUTHORITY_REFUSED`); backup receipt; prerequisites (`show ndns`); `no ip http proxy broray`; wait absent (8×1 s); `system configuration save`; wait absent; delete receipt. Failures roll back by re-applying the old block.
- JS success toasts: enable "Адрес WebUI восстановлен." if `state.webAccess.enabled` before the call, else "Адрес WebUI через KeenDNS включён."; disable "Адрес WebUI через KeenDNS выключен." (`keenetic.js:816-853`).

### Startup reconciliation of ProxyN (not an endpoint; must be reproduced by the daemon)
- `runtime/init/S24broray` (the deployed init; `runtime/root/opt/etc/init.d/S24broray` is an older variant): Entware `rc.func` service with `PROCS=xray`, `ARGS="run -c /opt/broray/config/config.json"`, `PREARGS="env XRAY_LOCATION_ASSET=/opt/broray/bin"` (`S24broray:3-6`), `PRECMD` = `lib/lighttpd-guard.sh maintain` (`:7-15`), `POSTCMD` = `broray_reconnect_owned`: if `config/interface.json` has owner BROray, schema 1|2, `interfaceName` `Proxy\d+` → `ndmc -c "interface <n> proxy connect"` then `ndmc -c "interface <n> up"` (`:17-47,142`). `pidof`/`killall` are overridden to the identity-checked Xray process (`:155-172`, `lib/xray-process.sh`). After `rc.func`: start/restart/status/check/reconfigure fail (exit 1) unless the verified Xray PID exists; stop/kill fail if it still exists (`:183-186`). `rc.func` itself is not in the repo: its exact behaviour is UNCONFIRMED (standard Entware).
- On `start`: if an updater daemon owns `/opt/var/lib/broray-updater/request.lock` with operation `update-*|reinstall-*` → stop reconcile sidecar, run `interface.sh sync-name` + `check` synchronously, write `/opt/broray/run/interface-reconcile.status` (`state=success|error`, `attempt=1`, `method=sync-name-update`) (`:124-140,190-191`); elif `BRORAY_STARTUP_RECONCILE_SKIP=1` or global lock dir exists → stop reconcile; else start sidecar `broray-service interface-reconcile start` (only if an owned interface name is found, `lib/startup-sidecars.sh:12-16`). Then start `home-snapshot` sidecar and `/opt/etc/init.d/S28broray-subscriptions start`. `stop|kill|restart` first stop reconcile and home-snapshot sidecars (`:175-179`).
- `bin/broray-service <name> start` (`bin/broray-service:13-38`) launches the daemon (`>> logs/<name>.log`) and waits up to 180 s (uptime-based) for `.complete and .ready`; exit 75 on timeout.
- `bin/broray-interface-reconcile` (`:1-68`): up to 10 attempts; each sleeps 3 s, begins op `system keenetic:reconcile '' SYSTEM_RECOVERY cooperative` (failure → status `deferred`, next attempt), tick `checking`, `interface.sh ownership-check` (no pending operation/quarantine/reservation files and owner valid; 30 s helper timeout) → failure: status `error … ownership-check`, exit 1 (75 if rc 75). Tick `committing`; supervised (`broray-ops-supervisor --protected-interface`, 120 s) `lib/interface-reconcile-worker.sh`: `check && sync-name && check` → 0; else `repair && check` → 0; else `ownership-check` → 10; else 75. Results: 0 → `success … reconcile`, finish completed; 10 → `error … configuration-confirmed`, finish failed; else `error … recovery-required`, exit 75. After 10 deferrals → `deferred 10 admission`, exit 75. Status file format `state=…\nattempt=…\nmethod=…\n`. Log `/opt/broray/run/interface-reconcile.log` (and service log `/opt/broray/logs/interface-reconcile.log`).

---

## 3. XRAY page

### 3.0 Two status implementations (important)
- `lib/xray.sh` (`broray_xray_status_json`, `xray.sh:318-576`) is used by X1, by `broray xray status` (`xray-control.sh:120-130`, `.data` only) and therefore by K1 and X7.
- Binary path (`xray.sh:26-57`): `settings.json .xray.binaryPath // .xrayBinary // .paths.xray`, else first existing of `/opt/bin/xray`, `/opt/broray/runtime/xray`, `/opt/broray/xray/xray`, else `/opt/bin/xray`. Config path (`xray.sh:65-98`): `settings .xray.configPath // .xrayConfig // .paths.xrayConfig`, else first existing of `/opt/broray/run/config.json`, `/opt/broray/run/xray-config.json`, `/opt/broray/config/xray/config.json`, `/opt/broray/config/config.json`, `/opt/etc/xray/config.json`. Settings file `/opt/broray/config/system/settings.json`.
- The lifecycle/CLI side (`xray-control.sh:3-6`) and install use fixed `/opt/broray/runtime/xray` and `/opt/broray/config/config.json`, asset dir `/opt/broray/bin`. If `/opt/bin/xray` exists, status and lifecycle observe different binaries (and the JS `installable()` check `latestStatus.version === catalogCurrentVersion` fails).

### X1 `GET /api/xray/status.cgi`
- File `api/xray/status.cgi:1-32`. Errors: 405, 401, 500 `XRAY_BACKEND_UNAVAILABLE` "Backend-модуль Xray недоступен." (`/opt/broray/lib/xray.sh` unreadable). The script prints headers and then the **pre-enveloped** output of `broray_xray_status_json_cached` directly (no `broray_api_success`). If that function fails the body is empty with HTTP 200 → JS error "Некорректный ответ сервера (HTTP 200)." (`common.js:35-45`).
- Cache (`xray.sh:578-626`): `/opt/broray/run/xray-status-cache.json` (0600) reused when it is a valid `{success:true,data:{…}}`, age < 15 s (mtime), and its `.data.pid` equals the current verified PID (string compare, `""` when stopped).
- Computation:
  - `installed` = binary is a regular file; `binarySizeBytes` (`wc -c`), `binarySha256`.
  - `executable` = `-x`; `version` = first token of line 1 of `<bin> version` matching `^[vV]?[0-9]+(\.[0-9A-Za-z_-]+)+$` without leading v, else 2nd field (`xray.sh:108-131`); `architecture` = `linux/<arch>` from the version output, else mapped `uname -m` (aarch64→arm64, armv7l→arm, mipsel→mipsle, mips, x86_64→amd64, i?86→386) (`xray.sh:133-173`); `deviceArchitecture` = raw `uname -m`.
  - `running`/`pid`: verified runtime PID (`lib/xray-process.sh:4-55`): candidates `pidof xray`; `/proc/<pid>/exe` must equal `readlink -f` of the binary; cmdline argv[1]=="run", exactly one `-c|-config|--config` (or `=` form) equal to the config path, no `-test`/`-confdir`, ≥4 args, not zombie, start-time stable.
  - `configExists`, `configSizeBytes`, `configSha256`; `configValid` = `<bin> run -test -config <config>` rc 0 (note: no `XRAY_LOCATION_ASSET` here – contrary to the CLI validate); on failure `configCheckOutput` = last 20 lines.
  - `socksAddress`/`socksPort` from settings (defaults `127.0.0.1`/`2080`); `socksActive` = running && `netstat -lnt` has a LISTEN row with local address `addr:port`, `0.0.0.0:port`, `:::port` or `[::]:port` (`xray.sh:250-283`).
  - `storageFreeKb` = 4th column of `df -k /opt`.
  - `lastError` = first applicable of "Бинарный файл Xray не найден." / "Не удалось определить версию Xray." / "Бинарный файл Xray не является исполняемым." / "Конфигурация Xray не прошла проверку." / "Активная конфигурация Xray не найдена.".
  - `health` (`xray.sh:424-472`, `lib/status-contract.sh:35-`): reasons (in order) `XRAY_BINARY_MISSING` "Бинарный файл Xray не найден.", `XRAY_NOT_EXECUTABLE` "Бинарный файл Xray нельзя запустить.", `XRAY_CONFIG_MISSING` "Активная конфигурация Xray не найдена.", `XRAY_CONFIG_INVALID` "Конфигурация Xray не прошла проверку.", `XRAY_PROCESS_STOPPED` "Процесс Xray остановлен.", `XRAY_SOCKS_INACTIVE` "Локальный SOCKS-интерфейс не слушает настроенный адрес и порт."; severity `error` if !installed||!executable||!configExists||!configValid, `warning` if !running||!socksActive, else `ok`; operational/consistent true and actionRequired false only when all six are true, otherwise `operational:false, consistent:configValid, actionRequired:true`; facts `{processRunning,configValid,socksActive,socksAddress,socksPort}`; freshness fresh/updatedAt.
- Response (whole body):
```json
{"success":true,"data":{"installed":true,"executable":true,"running":true,"pid":2345,
 "version":"25.10.15","architecture":"arm64","deviceArchitecture":"aarch64",
 "binaryPath":"/opt/broray/runtime/xray","binarySizeBytes":30212345,"binarySha256":"<64hex>",
 "configPath":"/opt/broray/config/config.json","configExists":true,"configSizeBytes":4321,"configSha256":"<64hex>",
 "configValid":true,"configCheckOutput":null,"socksAddress":"192.168.1.1","socksPort":2080,"socksActive":true,
 "socks":{"address":"192.168.1.1","listen":"192.168.1.1","port":2080,"active":true},
 "health":{"schemaVersion":1,"module":"xray","availability":"available","severity":"ok","operational":true,"consistent":true,
   "actionRequired":false,"freshness":{"state":"fresh","checkedAt":"2026-11-02T10:15:30+0300"},"reasons":[],
   "facts":{"processRunning":true,"configValid":true,"socksActive":true,"socksAddress":"192.168.1.1","socksPort":2080},"lastOperation":null},
 "storagePath":"/opt","storageFreeKb":812345,"updateAvailable":false,"lastError":null,"updatedAt":"2026-11-02T10:15:30+0300"},
 "error":null}
```
  `pid`, `version`, `architecture`, `deviceArchitecture`, `binarySha256`, `configSha256`, `configCheckOutput`, `lastError` are `null` when empty. `updateAvailable` is always `false`.
- JS reads (`xray.js:423-469`): `running`, `socksActive` (or `socks.active`), `health.severity`, `health.reasons[0].message`, `configValid`, `version`, `pid`, `socksAddress`, `socksPort`, `architecture`, `deviceArchitecture`, `storageFreeKb` (shown as KB/MB, `formatStorage`), `configPath`, `configSizeBytes`, `configSha256`. Severity → badge: ok "Работает" (`status-badge-success`), warning "Требуется внимание", busy "Выполняется", error "Требуется исправление" (`status-badge-danger`), else "Не проверено". Fallback severity when `health` missing: ok iff running && configValid && socksActive, else error.

### X2/X3/X4 `POST /api/xray/{start,stop,restart}.cgi`
- Files `api/xray/start.cgi:1-102` (stop/restart identical except action names/codes). Body ignored. Order: method, session, global lock `xray:<action>`/bundle `xray` (409 `OPERATION_BUSY`, 500 `GLOBAL_LOCK_FAILED`; no `GLOBAL_LOCK_UNAVAILABLE` check here), 500 `BRORAY_CLI_UNAVAILABLE` "CLI BROray недоступен." if `/opt/broray/bin/broray` not executable.
- Runs `/opt/broray/bin/broray xray <start|stop|restart>` (`lib/xray-control.sh:165-218`):
  - start: `broray xray validate` must pass → `/opt/etc/init.d/S24broray start` → wait until verified PID exists (deadline 180 s by `/proc/uptime`, 1 s poll).
  - stop: `S24broray stop` → wait until not running (12 × 1 s).
  - restart: validate → `S24broray restart` → wait running (180 s).
  - stdout `{success, action, running, pid}` (`xray-control.sh:132-163`).
- Errors: stdout not JSON → 500 `XRAY_<ACTION>_FAILED` "Не удалось запустить Xray." / "Не удалось остановить Xray." / "Не удалось перезапустить Xray."; rc≠0 or `.success!=true` → 500 same code, message "Xray не был запущен." / "Xray не был остановлен." / "Xray не был перезапущен.", details = `.message // .output` + stderr (last 30 lines).
- Success `data`: `{"success":true,"action":"start","running":true,"pid":2345,"completedAt":"2026-11-02T07:15:30Z"}` (UTC `strftime`).
- JS: toast `successMessage` ("Xray запущен." / "Xray остановлен." / "Xray перезапущен."), then `loadStatus(false)`; does not read `data`. Stop and restart require a confirm dialog (`xray.js:733-756`).

### X5 `POST /api/xray/check-config.cgi`
- File `api/xray/check-config.cgi:1-86`. No lock. Runs `broray xray validate` (`xray-control.sh:79-118`: `XRAY_LOCATION_ASSET=/opt/broray/bin /opt/broray/runtime/xray run -test -c /opt/broray/config/config.json`; outputs `{success, config, output}`; output "Исполняемый файл Xray не найден." / "Файл конфигурации Xray не найден." when missing).
- Errors: 500 `BRORAY_CLI_UNAVAILABLE`; 500 `XRAY_CONFIG_CHECK_FAILED` "Не удалось получить результат проверки конфигурации." (non-JSON); **422** `XRAY_CONFIG_INVALID` "Конфигурация Xray не прошла проверку." details = `.output // .message`.
- Success `data`: `{"valid":true,"output":"Xray 25.10.15 …\nConfiguration OK.","configPath":"/opt/broray/config/config.json","checkedAt":"2026-11-02T07:15:30Z"}`.
- JS (`xray.js:539-563`): reads `valid`, `output`. Because invalid config returns 422, the JS `data.valid=false` branch is never reached: an invalid config produces only an error toast with the message above and leaves `config-validity`/output unchanged.

### X6 `POST /api/xray/update-check.cgi`
- File `api/xray/update-check.cgi:1-62`. No lock, body ignored. Runs `broray xray update-check` → `broray_xray_update_check` (`lib/xray-releases.sh:196-217`) in a private `mktemp -d /tmp/broray-xray-catalog.XXXXXX` (removed on exit).
- **Catalog fetch** (`xray-releases.sh:148-194`):
  - Requires `uname -m` ∈ {aarch64, arm64} (context architecture `arm64`), else failure.
  - Context = `share/release/manifest.json {candidateId,releaseId,version}` + `architecture` + `sourceSha256` of `lib/server-config-generator.sh`, `lib/xray.sh`, `lib/parser-vless.sh` (`xray-releases.sh:91-112`).
  - Compatibility registry `share/xray-compatibility.json` (80 records in this build) used only if its sha256 equals the `app/share/xray-compatibility.json` line of `/opt/broray/current/SHA256SUMS`; else `[]` (`xray-releases.sh:114-125`).
  - GitHub: `GET https://api.github.com/repos/XTLS/Xray-core/releases?per_page=20&page=N`, N=1..10 (`curl -fsSL --proto =https --tlsv1.2 --connect-timeout 8 --max-time 20 --max-filesize 8388608`, headers `Accept: application/vnd.github+json`, `User-Agent: BROray-Xray/<manifest version>`; `xray-releases.sh:80-89`). Stop when ≥2 stable and ≥5 pre-releases collected, or a page has <20 items (`complete=true`); after 10 pages `complete=false`.
  - Normalisation (`lib/xray-releases.jq:9-21`): non-draft, boolean `prerelease`, `valid_tag` (`v` + `Y.M.D[-suffix]`, Y ≤4 digits, M/D ≤2 digits, chars `[0-9A-Za-z.-]`, ≤80), exactly one uploaded asset `Xray-linux-arm64-v8a.zip` (size>0, numeric id) and one `Xray-linux-arm64-v8a.zip.dgst`, both with the canonical `https://github.com/XTLS/Xray-core/releases/download/<tag>/<name>` URL, string `published_at`.
  - Selection: newest 2 stable + newest 5 pre-releases (by `published_at` desc, unique by tag); plus `v<installed>` and the latest-tested compatible tag from the registry if missing (fetched via `releases/tags/<tag>`); for any asset without a `sha256:` API digest the `.dgst` file is downloaded (`--max-filesize 16384`) and the `SHA2-256=`/`sha2-256 ` line parsed (`xray-update.sh:417-430`).
  - Compatibility per release (`xray-releases.jq:55-65`): registry records matching context, tag and archive sha; sort by `[status=="incompatible", candidateId==context.candidateId, testedAt]` and take the last (→ an `incompatible` record wins over a `compatible` one). Result = the record + `label` ("Совместима с BROray"/"Частично совместима с BROray"), or `{status:"untested",label:"Совместимость не проверялась"}`.
- **Latest/compare** (`xray-update.sh:114-325`):
  - `currentVersion` = `sed -n '1s/^Xray \([^ ]*\).*/\1/p'` of `/opt/broray/runtime/xray version` (empty → `{success:false,error:"Не удалось определить установленную версию Xray."}`).
  - Latest = first entry of the (desc-sorted) catalog with `prerelease==false` and the asset (`xray-update.sh:64-112`); none → "Не найден официальный архив Xray для Linux ARM64.". Fetch failure → "Не удалось получить список официальных релизов Xray.".
  - Version key (`xray-update.sh:22-57`): split on `.`, first three parts, non-numeric → 0, `printf %04d%02d%02d`. `updateAvailable = latestKey > currentKey`; `installedNewer = currentKey > latestKey`.
  - Storage: `storage.freeBytes = df -k /opt ×1024`; `storage.requiredBytes = max(current binary size, latest asset size) + 1 048 576`; `storage.ok = free ≥ required`, `shortfallBytes`. `temporaryStorage.freeBytes = df -k /tmp ×1024`; `requiredBytes = current size + 1 048 576`; `reinstallAllowed = tmpFree ≥ required && storage.ok`.
  - `message`: "Недостаточно свободного места в /opt для безопасной установки Xray." / "Недостаточно временной памяти для безопасной установки Xray." / "Доступна новая версия Xray." / "Установленная версия новее последнего стабильного релиза." / "Установлена актуальная версия Xray.".
- Success `data`:
```json
{"success":true,"currentVersion":"25.10.15","latestVersion":"25.12.8","latestTag":"v25.12.8","channel":"stable",
 "prerelease":false,"publishedAt":"2025-12-08T10:00:00Z","updateAvailable":true,"installedNewer":false,
 "asset":{"name":"Xray-linux-arm64-v8a.zip","size":12345678,"url":"https://github.com/XTLS/Xray-core/releases/download/v25.12.8/Xray-linux-arm64-v8a.zip"},
 "digest":{"name":"Xray-linux-arm64-v8a.zip.dgst","size":1024,"url":"https://github.com/.../Xray-linux-arm64-v8a.zip.dgst"},
 "archiveSha256":"<64hex>","compatibility":{"status":"untested","label":"Совместимость не проверялась"},
 "storage":{"freeBytes":831841280,"requiredBytes":31260921,"shortfallBytes":0,"ok":true},
 "temporaryStorage":{"freeBytes":120000000,"requiredBytes":31260921,"shortfallBytes":0,"reinstallAllowed":true},
 "message":"Доступна новая версия Xray.",
 "context":{"candidateId":"3.1.0-r09c02","releaseId":"3.1.0-r09","version":"3.1.0","architecture":"arm64",
   "sourceSha256":{"lib/server-config-generator.sh":"…","lib/xray.sh":"…","lib/parser-vless.sh":"…"}},
 "catalogComplete":true,
 "releases":[{"tagName":"v25.12.8","version":"25.12.8","prerelease":false,"publishedAt":"…","installed":false,"available":true,
   "archiveSha256":"<64hex>","asset":{"id":123,"name":"Xray-linux-arm64-v8a.zip","size":12345678,"url":"…"},
   "digest":{"name":"Xray-linux-arm64-v8a.zip.dgst","url":"…"},
   "compatibility":{"candidateId":"…","architecture":"arm64","xrayTag":"v25.12.8","archiveSha256":"…","testedAt":"…","status":"compatible","evidence":"…","label":"Совместима с BROray"}}]}
```
  `releases` (`xray-releases.jq:66-72`) is sorted by `published_at` desc. If no row is `installed`, a synthetic row `{tagName:"v<current>",version:<current>,installed:true,available:false,prerelease:null,compatibility:{status:"untested",label:"Не проверялась на совместимость с BROray"}}` is appended (`xray-releases.sh:213-216`).
- Errors: stdout not JSON → **502** `XRAY_UPDATE_CHECK_FAILED` "Не удалось проверить официальный релиз Xray." (details = stderr+stdout tail); rc≠0 or `.success!=true` → **502** `XRAY_UPDATE_CHECK_FAILED` "Проверка обновления Xray завершилась ошибкой." details = `.error // .message`.
- JS (`xray.js:182-223,289-320`): reads `updateAvailable`, `installedNewer`, `latestVersion`, `channel` ("pre-release" → "Предварительный" else "Стабильный"), `temporaryStorage.{reinstallAllowed,requiredBytes,freeBytes,shortfallBytes}`, `storage.{ok,freeBytes,requiredBytes,shortfallBytes}`, `message`, `currentVersion`, `releases[]` (`tagName`, `version`, `installed`, `prerelease`, `available`, `archiveSha256`, `asset.size`, `compatibility.status|testedAt|limitations`), `context`, `catalogComplete`. Note `update-message` is written twice; the second write wins: "Список получен частично: показаны найденные официальные версии." if `catalogComplete===false`, else "Список обновлён. Совместимость указана для текущей сборки BROray" (`xray.js:219-221`) – storage/`message` texts are never visible.
- On error the JS clears the catalog and shows "Не удалось подтвердить список версий. Установка недоступна до повторной проверки. " + message (`xray.js:577-582`).

### X7 `GET /api/xray/diagnostics.cgi`
- File `api/xray/diagnostics.cgi:1-349`. Errors: 405, 401, 500 `XRAY_BACKEND_UNAVAILABLE` "Backend Xray недоступен." (CLI not executable or `lib/xray.sh` unreadable). Otherwise always HTTP 200 with a hand-printed envelope.
- Inputs: `broray xray status` (→ `status`, `{}` if invalid), `broray xray validate` (→ `validation`), fixed `BINARY=/opt/broray/runtime/xray`, `CONFIG=/opt/broray/config/config.json`, `df -k /opt`, `df -k /tmp`, first existing of `/opt/broray/run/xray-reinstall-test.json`, `/opt/broray/run/xray-lifecycle-test.json` (success if `.success==true`).
- `checks` (fixed order, `diagnostics.cgi:144-299`): `binary-exists` (ok/error, "Бинарный файл Xray", "Файл найден."/"Файл отсутствует."), `binary-executable` (ok/error, "Права запуска", "Бинарный файл исполняемый."/"Нет права исполнения."), `process-running` (ok/warning, "Процесс Xray", "Процесс работает."/"Процесс остановлен."), `config-exists` (ok/error, "Конфигурация", "Файл конфигурации найден."/"Файл конфигурации отсутствует."), `config-valid` (ok/error, "Проверка конфигурации", "Configuration OK."/"Конфигурация не прошла проверку."), `socks-active` (ok/warning, "SOCKS", "Локальный SOCKS-порт активен."/"SOCKS-порт не подтверждён."), `sha256` (ok/warning, "SHA-256", "Контрольная сумма вычислена."/"Контрольная сумма недоступна."), `opt-storage` (≥45000 KB ok, ≥20000 warning, else error; "Свободное место /opt", "<n> КБ"), `tmp-storage` (≥130000 ok, ≥90000 warning, else error; "Временное хранилище /tmp", "<n> КБ"), `real-device-test` (ok/warning, "Испытание на Keenetic", file path / "Успешный результат испытания не найден.").
- `data`: `{checkedAt:"<local %Y-%m-%dT%H:%M:%S%z>", status:<object>, validation:<object>, binarySha256:<hex|null>, checks:[{id,status,title,details}], summary:{ok,warning,error}}`.
- JS reads `checks[].status|title|id|details`, `summary.ok|warning|error` (`xray.js:590-635`); CSS class `diagnostic-<status>`.

### X8 `POST /api/xray/install.cgi` (async)
- File `api/xray/install.cgi:1-7` → `lib/xray-web-operation.sh` → `lib/xray-web-launch.sh:1-55` (mode `install`). `update.cgi`/`reinstall.cgi` are identical with mode `update`/`reinstall` and no body.
- Request body (JSON, `Content-Length` required, 1..4096 bytes, read raw via `dd`, `lib/web-request-body.sh:5-34`), validated by `broray_xray_install_request_valid` (`xray-releases.sh:219-227`): object whose keys (minus optional `allowPartial`) are exactly `allowDowngrade, allowPrerelease, allowUntested, archiveSha256, currentVersion, tag`; `tag` valid_tag; `currentVersion` string; `archiveSha256` 64 lowercase hex; the three `allow*` booleans; `allowPartial` boolean if present. Any failure (incl. missing/oversized body) → **400** `XRAY_INSTALL_REQUEST_INVALID` "Некорректный запрос выбора версии Xray. Обновите список версий.".
  JS body (`xray.js:324-326`): `{tag, currentVersion: catalogCurrentVersion, archiveSha256, allowUntested: status=="untested", allowPrerelease: prerelease===true, allowDowngrade: isDowngrade(row), allowPartial: status=="incompatible"}`.
- Launch sequence:
  1. 400 `XRAY_OPERATION_MODE_INVALID` "Неизвестный режим обслуживания Xray." (bad mode; unreachable).
  2. `/opt/broray/run/xray-web-operation.pid` exists → **409** `XRAY_LEGACY_OWNER_UNCONFIRMED` "Владелец предыдущей операции Xray не подтверждён. Откройте диагностику операций.".
  3. `broray_job_begin system "xray:<mode>" xray USER cooperative` (`lib/operation-job.sh:17-22`): rc 2 → **409** `OPERATION_BUSY`; other → **503** `OPERATION_UNAVAILABLE` "Не удалось подготовить операцию Xray.".
  4. `mkdir -m 700 /opt/broray/run/xray-operations/<opId>`; write `operation-id` → failure **500** `XRAY_OPERATION_STATE_FAILED` "Не удалось подготовить каталог операции Xray." / "Не удалось записать идентификатор операции Xray.".
  5. (install) body → `<work>/request.json`, validate (400 above).
  6. nonce (16 random bytes hex) – failure **503** `RANDOM_UNAVAILABLE` "Не удалось подготовить передачу операции.".
  7. Write pointer `/opt/broray/run/xray-web-operation.json` = `{backgroundOperationId, operation:<mode>, operationRunning:true, running:true, success:null, result:null, error:null}` via `broray-ops-guard --replace-file` (0600) – failure **503** `XRAY_OPERATION_STATE_FAILED` "Не удалось сохранить начало операции Xray.".
  8. Spawn `ash lib/xray-web-worker.sh <work> <mode> <nonce>` in background, stdout+stderr → `<work>/worker.log`; `broray_ops_handoff_to <pid> <nonce>` – failure **503** `XRAY_HANDOFF_UNCONFIRMED` "Передача операции Xray ещё не подтверждена. Проверьте её состояние в журнале." (operation left unresolved).
  9. Response: `Status: 202 Accepted`, envelope data `{"accepted":true,"operation":"install","operationId":"op-…","pid":null}`.
  Any error before handoff finishes the job as failed (`auth-common.sh:16-23`).
- JS: toast "Установка выбранной версии запущена.", keeps controls locked, shows `operation-section` with `operationCopy(mode).runningTitle/runningMessage`, and starts polling X9 (`xray.js:513-523`). Confirm dialog before sending (`xray.js:330-333`).

**Worker** (`lib/xray-web-worker.sh:1-45`) – validates work dir/operation id, accepts handoff, `broray_xray_install_dispatch <mode> <work>/request.json > <work>/output.json`, exit trap `broray_xray_job_exit` (`lib/xray-install-job.sh:4-14`): abort cleanup → `broray_xray_web_finish(rc)` → `broray_job_exit(rc)`.

**Install phases** (`broray_xray_update_install`, `xray-install-job.sh:16-70`) – coordinator phase names via `broray_job_checkpoint` (`operation-job.sh:96-106`, enum in `lib/operation-public.jq:42`):
1. Preconditions: binary/config regular files; no `/opt/broray/runtime/xray.new` or `xray.broray-*-backup` (→ rc 75); no `/opt/broray/update/xray.lock`.
2. checkpoint **`fetching`**; RAM work dir `mktemp -d /tmp/broray-operations/xray-job-<opId>-XXXXXX` (RAM root must be tmpfs/ramfs/rootfs, 0700, owned by uid; `xray-update.sh:387-403`).
3. Preparation helper (supervised, timeout 600 s) `lib/xray-install-prepare.sh` (`:1-327`):
   - `install`: `broray_xray_selected_check` (`xray-releases.sh:229-251`): current version must equal `request.currentVersion` ("Установленная версия изменилась. Обновите список и повторите выбор."); arm64 ("Архитектура устройства не поддерживается этим пакетом BROray."); re-resolve `releases/tags/<tag>` ("Не удалось подтвердить выбранный официальный релиз Xray."); archive sha must equal request ("Контрольная сумма выбранного релиза изменилась. Повторите проверку версий."); consent checks: incompatible needs `allowPartial` ("Подтвердите установку частично совместимой версии Xray после ознакомления с её ограничениями."), untested needs `allowUntested` ("Подтвердите установку версии, не проверявшейся на совместимость с BROray."), prerelease needs `allowPrerelease` ("Подтвердите установку предварительного релиза."), downgrade (key compare) needs `allowDowngrade` ("Подтвердите понижение версии Xray.").
   - `update`/`reinstall` (legacy, `xray-releases.sh:254-268`): full catalog check, synthesize a request for `latestTag` (update) or the installed row (reinstall) with all `allow*` = false → therefore fails for untested/incompatible/prerelease targets. update additionally requires `updateAvailable` ("Новая версия Xray отсутствует."); reinstall refuses `installedNewer` ("Автоматическое понижение версии запрещено.") and current≠target ("Для доступной новой версии нужно использовать update.").
   - Space: RAM free ≥ current size + 1 MiB; /opt free ≥ max(current, asset size) + 1 MiB (messages with byte counts, `xray-install-prepare.sh:139-157`).
   - Download archive (`curl -fL --connect-timeout 15 --max-time 180`, no `-s` ⇒ progress meter on stderr) and `.dgst` (`--max-time 60`); verify size, official SHA2-256, equality with confirmed `archiveSha256`; `unzip -p <zip> xray`; size >10 000 000; `version` equals target; `XRAY_LOCATION_ASSET=/opt/broray/bin <candidate> run -test -c /opt/broray/config/config.json` (failure → `{success:false,error:"Новая версия Xray несовместима с действующей конфигурацией BROray.",details:<last 20 lines>}`); record old sha; write `prepared.json {currentVersion,targetVersion,oldSha256,candidateSha256,candidateSize}`.
4. Verify prepared metadata and hashes; record whether Xray was running; checkpoint **`switching`**.
5. Commit (`broray_xray_update_commit`, `xray-update.sh:513-708`): /opt free ≥ candidate + 8 MiB ("На /opt недостаточно места для атомарной замены Xray."); if running: `broray xray stop` semantics (`S24broray stop`, ≤12 s); `mv xray → xray.broray-<opId>-backup`; copy candidate → `xray.new`, chmod 755, sha check, `mv xray.new → xray`; version check; config test; if it was running: `S24broray start` and wait for verified PID **and** SOCKS LISTEN (≤180 s, `xray-releases.sh:270-285`). Any failure → restore backup (stop, copy back, sha check, restart if previously running) with JSON `{success:false,rolledBack,rollbackSuccess,error:"<reason> Предыдущий Xray восстановлен."|"<reason> Восстановление не подтверждено. Резервный бинарник и блокировка сохранены."}` (rc 1 or 75).
   Success stdout: `{"success":true,"installed":true,"mode":"install","previousVersion":"25.10.15","version":"25.12.8","sha256":"<64hex>","running":true,"message":"Новая версия Xray безопасно установлена."}` (reinstall message: "Текущая версия Xray безопасно переустановлена.").
6. Finish (`xray-web-worker.sh:116-137`): pointer replaced by `{backgroundOperationId, operation, success: rc==0 && output.success==true, running:false, operationRunning:false, pid:null, workerPid:null, result:<output.json object or {}>, error: null | (rc 130 → "Операция Xray отменена.") | (output.error // "Операция Xray завершилась с ошибкой. Откройте диагностику."), completedAt:"<UTC Z>"}`.
- Cancellation: no cancel control on this page; a coordinator cancel (operations area) makes checkpoints return 130 → "Операция Xray отменена.".
- Files: `/opt/broray/run/xray-web-operation.json` (pointer), `/opt/broray/run/xray-operations/<opId>/{operation-id,request.json,worker.log,output.json}` (never pruned by any code found – UNCONFIRMED whether the coordinator prunes them), RAM `/tmp/broray-operations/xray-job-<opId>-*/` (removed on exit), `/opt/broray/runtime/xray{,.new,.broray-<opId>-backup}`.

### X9 `GET /api/xray/operation-status.cgi`
- File `api/xray/operation-status.cgi:1-11` → `broray_xray_web_status` (`lib/xray-web-status.sh:3-39`).
- Algorithm: read pointer (object) → `id = backgroundOperationId` if it passes `operation_id` (`lib/operation-public.jq:10-`). If id: `broray_ops_call status` snapshot (must have `.complete==true`), `job = first(.operations[] | select(.operationId==id)) // null`; `running = job.running` (null → incomplete); if job absent and pointer has `running==false` and boolean `success` → running false; else running null, incomplete. `logTail` = last 4096 bytes of `run/xray-operations/<id>/worker.log`. No id but `run/xray-web-operation.pid` exists → incomplete.
- Output: `{operation: pointer.operation if install|update|reinstall else "reinstall", operationId: pointer.backgroundOperationId // null, operationRunning: <bool|null>, complete: <bool>, pid: null, backgroundOperation: <job|null>, result: <pointer, or if job.running==false while pointer.running==true: pointer + {running:false, operationRunning:false, success:false, error: job.state=="aborted" ? "Операция Xray отменена." : "Операция Xray прервана. Откройте диагностику."}>, logTail: <string|null>}`.
- Errors: function failure → **503** `XRAY_STATUS_UNAVAILABLE` "Не удалось прочитать состояние операции Xray."; `complete!=true` → **503** `XRAY_OWNER_UNCONFIRMED` "Владелец операции Xray не подтверждён. Откройте диагностику операций.".
- Example while running:
```json
{"operation":"install","operationId":"op-20261102101530-1234-abcd","operationRunning":true,"complete":true,"pid":null,
 "backgroundOperation":{"operationId":"op-…","running":true,"phase":"fetching","state":"running"},
 "result":{"backgroundOperationId":"op-…","operation":"install","operationRunning":true,"running":true,"success":null,"result":null,"error":null},
 "logTail":"  % Total    % Received …"}
```
  (`backgroundOperation` is the coordinator's row verbatim – its shape belongs to the operations contract; this example's fields are illustrative.)
- JS polling (`xray.js:645-715`): on page load `pollOperation(false)`; after a 202 `pollOperation(true)`. If `operationRunning` truthy → show section, lock controls, show `logTail` in `#operation-log`, re-poll after **2000 ms**. Else: show `logTail` if any; if `result` and notify → title "Операция завершена"/"Операция завершилась ошибкой", message `result.message || copy.successMessage` or `result.error || "Не удалось завершить операцию."`, on success `markUpdateInstalled(result.result || result)` (reads `.version`, `.message`), clear catalog, toast; if no `result` or not notify → hide section. Then unlock, reload status, and (notify && result) re-run `checkUpdate()`. Any request error → show section with the error message, unlock, **stop polling** (so a transient 503 ends tracking).

### X10/X11 `POST /api/xray/update.cgi`, `/api/xray/reinstall.cgi`
Same as X8 without a body (`api/xray/update.cgi:1-13`, `reinstall.cgi:1-13`), modes `update`/`reinstall` (legacy non-interactive policy above). Not referenced by any JS.

### 3.1 Logs and rotation
- Xray runtime logs: generated config sets `log.access=/opt/broray/logs/access.log`, `log.error=/opt/broray/logs/error.log`, `loglevel = settings.logLevel // "warning"` (`lib/server-config-generator.sh:30-32,409-412`).
- Sidecar logs `/opt/broray/logs/{server-auto-switch,connection-monitor,home-snapshotd-service,interface-reconcile,subscription-scheduler}.log` (`lib/service-lifecycle.sh:16-34`); others rotated: `lighttpd-error.log`, `subscriptions.log`, `update-last.log`, `home-snapshots.log`.
- Per-operation logs: `/opt/broray/run/xray-operations/<id>/worker.log` (exposed as `logTail`), `/opt/broray/run/interface-reconcile.log`, ProxyN evidence `/opt/broray/run/interface-last-command.json`, `/opt/broray/run/interface-last-failure.json`. `bin/broray-xray-start` (legacy hysteria route helper, not referenced by S24) logs via `logger -t BROray`.
- Rotation `bin/broray-log-maintenance` (`:1-148`), run every 300 s by `bin/broray-connection-monitor` (`:17-24,132-142`, `BRORAY_MONITOR_MAINTENANCE_INTERVAL`) and by package setup (`lib/package-setup.sh:464-471`). Serialised by `broray-ops-guard <logs>/maintenance.guard` (re-execs itself with `--locked`; the `--once` arg callers pass is ignored).
  - Limits: access.log 1 048 576 B; error.log and lighttpd-error.log 262 144 B; all other listed logs 262 144 B (env overrides `BRORAY_ACCESS_LOG_MAX_BYTES`, `BRORAY_ERROR_LOG_MAX_BYTES`, `BRORAY_RUNTIME_LOG_MAX_BYTES`); keep `BRORAY_LOG_ROTATIONS`=2.
  - `rotate_file` (`:28-68`): only regular, single-link, non-symlink files larger than the limit; shift `.1.gz→.2.gz…`; the live file's last `max_bytes` bytes are gzipped to `.1.gz` (tail only); then the live file is truncated in place (`: >file`, chmod 600) – writers keep their fd.
  - Archives with numeric suffix > keep (≤4 digits) are deleted for the 11 known names (`:108-117`).
  - Then `broray_ops_call history-prune`, and deletes operation dirs under `/opt/var/lib/broray/operations` beyond the newest 20 (by name, `sort -r`), skipping the current pointer, dirs with `owner.json`, background/running/platform-preflight state, or platform/fence evidence (`:119-146`).

---

## 4. Endpoints built dynamically in JS
- `keenetic.js:624` `"/api/keenetic/status.cgi" + (force ? "?force=1" : "")` – JS always passes `force=true`.
- `keenetic.js:682` `"/api/keenetic/web-access-" + action + ".cgi"`, action ∈ {`enable`, `disable`}.
- `keenetic.js:729` `"/api/keenetic/" + action + ".cgi"`, action ∈ {`check-upstream`, `create`, `repair`, `sync-description`, `delete`}.
- `xray.js:514` `"/api/xray/" + endpoint`, endpoint ∈ {`start.cgi`, `stop.cgi`, `restart.cgi`, `install.cgi`}.

## 5. JS dependencies on backend wording / values / shapes
- `keenetic.js:202-207` normalises descriptions with `/^BROray\s*[‐-―−-]\s*/ → "BROray — "` and recomputes `checks.description` client-side when `expectedReady===true` (overriding the server value), `keenetic.js:209-224`. Buttons (`keenetic.js:318-336`): create shown iff `!exists && expectedReady`; repair shown iff `exists && !(healthy && all three effective checks)`; sync shown iff `exists && expectedReady && normalized descriptions differ`; delete shown iff `exists`.
- `keenetic.js:303-307` drops every problem string matching `/описан/i` when the effective description check is true – the problem texts must keep the word "Описание"/"описан…" exactly where the server emits description problems.
- `keenetic.js:62-131` maps `link` (`true|"up"|"yes"|"Да"` → "Установлена", false-likes → "Не установлена"), `connected` (→ "Подключено"/"Нет подключения"), `state` (`"up"|"running"` → "Работает", `"down"|"stopped"` → "Остановлен", otherwise raw), protocol `socks5` (any case) → "SOCKS5". `technical.mtu` falsy → "—".
- `keenetic.js:189-200` renders `updatedAt` with `new Date(value).toLocaleString("ru-RU")`, falling back to the raw string if unparsable – the `+0300` (no colon) format must be preserved for identical output (parsing of that format is browser-dependent: UNCONFIRMED).
- `keenetic.js:338-344` title uses `interfaceDisplayName || expected.description || description || "BROray"` (the ProxyN name itself is never shown).
- Web access: `state` enum `enabled|disabled|conflict|recovery`, `keenDns.access` values `cloud|direct|auto`, flags `canEnable/canDisable/enabled/consistent` (`keenetic.js:474-523`).
- `keenetic.js:168-179`, `xray.js:58-65`: redirect to login on HTTP 401 (and on codes `AUTH_REQUIRED`/`SESSION_REQUIRED` in keenetic.js).
- Xray: `compatibility.status` ∈ {`compatible`,`untested`,`incompatible`} (others treated as untested), `compatibility.limitations` (array of strings, optional), `compatibility.testedAt` compared as strings to choose the "last compatible" row (always visible even if prerelease) (`xray.js:229-232,254-259,292-298`); `archiveSha256` must match `/^[a-f0-9]{64}$/`; `available===true`; `prerelease===true/false/null`; `channel==="pre-release"`; version strings parsed by `/[.-]/` for downgrade detection (`xray.js:246-252`: `a*10000+b*100+c`).
- `installable(row)` (`xray.js:234-244`) requires `latestStatus.version === catalogCurrentVersion` (X1 `version` vs X6 `currentVersion`) and space: `temporaryStorage.freeBytes ≥ temporaryStorage.requiredBytes` and `storage.freeBytes ≥ max(temporaryStorage.requiredBytes, asset.size + 1048576)`.
- Operation poll relies on `operationRunning`, `operation`, `result.operation`, `result.success`, `result.message`, `result.error`, `result.result.version|message`, `logTail` (`xray.js:645-715`). `operation` falls back to "reinstall" copy texts.
- Diagnostics CSS class `diagnostic-<check.status>` with status ∈ {ok, warning, error} (`xray.js:615`).
- All HTML ids used by the JS are static in `keenetic.html` / `xray.html` (listed in `keenetic.js:11-56`; xray ids e.g. `xray.html:115-362`); the backend generates no HTML. `operation-log.js` only wraps `#operation-log` in a `<details>`.

## 6. Riskiest behaviours (for the Go port)
1. ProxyN ownership: exact running+startup block grammar, SHA canonicalisation (strip `ip global`/`proxy connect via`), policy-SHA-bound receipt, hex-escaped UTF-8 descriptions, and loose-signature adoption that writes a receipt during selection (2.0.2–2.0.4).
2. Keenetic mutations: exact command sequences, 5 s settle before every save, "stderr non-empty = failure" rule, convergence polling (10×1 s / 8×1 s) and byte-exact rollbacks incl. recovery markers; delete keeps the receipt so a second delete returns 500.
3. Two divergent Xray views (status picks `/opt/bin/xray` and run/ config candidates first; lifecycle/install use `/opt/broray/runtime/xray`) feeding both the Keenetic "expected upstream" (settings.json socks) and the mutation layer (config.json socks inbound).
4. Xray install job: 202 + handoff, pointer file + coordinator snapshot merge, synthesized failure when the job ended without finishing the pointer, 503 on any incomplete snapshot (stops JS polling), consent flags and re-verification of tag/sha/version, atomic binary swap with rollback and SOCKS readiness wait.
5. GitHub catalog construction (paging/stop rule, 2 stable + 5 pre, extra installed/compatible tags, `.dgst` fallback, registry SHA gating against `current/SHA256SUMS`, incompatible-wins sort) and the JS-visible quirks (422 for invalid config, overwritten update message, status.cgi un-wrapped output).
