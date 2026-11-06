# SERVERS area contract (BROray 3.2.0-r12 → Go daemon 3.5.0)

Source: `/home/claude/broadmin/broray` @ f505928. All paths below are relative to
`runtime/app/` unless they start with `runtime/` or `/opt`. On the router
`runtime/app` is installed as `/opt/broray` (`BRORAY_ROOT`), so
`lib/x.sh` = `/opt/broray/lib/x.sh`, `web-new/api/...` = `/opt/broray/web-new/api/...`.

Page: `web-new/servers.html`. Scripts loaded in this order (servers.html:321-331):
`common.js`, `icons.js`, `app-shell.js`, `theme.js`, `dialogs.js`,
`servers-completion.js`, `servers.js`, `servers-import-preflight.js`,
`servers-check-all.js`, `servers-auto-switch.js`, `servers-quality-refresh.js`.
No other page calls `/api/servers/*` (grep of web-new: only common.js:87 names
`/api/servers/check.cgi`).

---------------------------------------------------------------------------

## 0. Common conventions for this area

### 0.1 Envelope (all `/api/servers/*.cgi`)

Success (auth-common.sh:96-111): HTTP 200 (no explicit `Status:` line), headers
auth-common.sh:7-14 (`Content-Type: application/json; charset=utf-8`,
`Cache-Control: no-store, no-cache, must-revalidate`, `Pragma: no-cache`,
`X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: no-referrer`), body pretty-printed by jq:

```json
{ "success": true, "data": <payload>, "error": null }
```

Error (auth-common.sh:25-57): `Status: <code text>`, same headers,

```json
{ "success": false, "data": null,
  "error": { "code": "<CODE>", "message": "<ru text>", "details": "<string>|null" } }
```

`broray_api_success` first releases a held operation lock; if that fails it
emits `503 OPERATION_FINALIZATION_FAILED` "Результат операции сохранён, но её
завершение ещё не подтверждено." (auth-common.sh:99-101). Only relevant to the
two save CGIs that hold a routes-api lock.

**Exception:** `POST /api/servers/check.cgi` with header `X-BROray-Queue: 1`
(the only way the JS calls it) is served by the *operations* API and returns a
raw, non-enveloped object (see §2.7).

### 0.2 Auth / CSRF

* Every endpoint calls `broray_api_require_session` (cookie `BRORAY_SESSION`)
  → `401 AUTH_REQUIRED` "Требуется авторизация." (auth-common.sh:72-95);
  missing `/opt/broray/lib/web-auth.sh` → `500 SESSION_BACKEND_UNAVAILABLE`
  "Модуль проверки сессии недоступен.".
* Method check → `405 METHOD_NOT_ALLOWED` "Для этой операции требуется метод
  <M>." details "Получен метод <actual>." (auth-common.sh:59-70). Method is
  checked **before** session in every servers CGI.
* No CSRF header / Origin check on any `/api/servers/*.cgi` except the queued
  `check.cgi` path (operations/common.sh:12-27: `X-BROray-Request: operations`,
  origin check, `Content-Type: application/json[; charset=utf-8]`, body ≤4096).
* JS 401 handling: `servers.js handleError` (servers.js:890-894),
  preflight (servers-import-preflight.js:1213-1215, 788), check-all
  (servers-check-all.js:338-344 dead code), auto-switch/quality-refresh
  `loadStatus` (servers-auto-switch.js:342-345, servers-quality-refresh.js:183-186)
  → `BROrayUI.redirectToLogin()` = `location.replace("/")` (common.js:329-331).

### 0.3 Body reader for the servers wrapper (`api/servers/common.sh`)

`broray_servers_api_read_body_to_file` (common.sh:29-46), limit 65536 bytes,
reader lib/web-request-body.sh:5-34:

| condition | HTTP | code | message |
|---|---|---|---|
| CONTENT_LENGTH empty/non-digit | 411 | CONTENT_LENGTH_REQUIRED | Для запроса требуется корректный Content-Length. |
| CONTENT_LENGTH = 0 | 400 | REQUEST_BODY_REQUIRED | Тело запроса отсутствует. |
| > 65536 | 413 | REQUEST_TOO_LARGE | Тело запроса превышает 64 КБ. |
| dd failure | 400 | REQUEST_BODY_READ_FAILED | Не удалось прочитать тело запроса. |
| short read | 400 | REQUEST_BODY_INCOMPLETE | Тело запроса получено не полностью. |
| not a JSON object | 400 | REQUEST_JSON_INVALID | Тело запроса должно быть корректным JSON-объектом. |
| lib missing | 500 | REQUEST_BODY_LIBRARY_UNAVAILABLE | Обработчик тела запроса недоступен. |

Field extraction: `jq -r '.[$field] // empty'` (common.sh:48-57) — i.e.
`null`/`false`/missing → empty string; numbers are stringified.

### 0.4 Execution wrapper `broray_servers_api_run` (common.sh:59-118)

* Loads lib/server-service.sh + lib/server-import.sh; failure →
  `500 SERVER_LIBRARY_UNAVAILABLE` "Служба серверов недоступна.".
* For job actions (import/check/activate/deactivate/delete/
  quality-batch-complete, set by `broray_servers_api_lock`, common.sh:20-27)
  the function runs inside `lib/server-job-worker.sh <action> <fn> args…`
  which registers an Operation-Manager job `servers:<action>` scope `servers`
  source `USER` (`SERVER_CHECK_AUTO` for `check … scheduled`, `AUTO_SWITCH` for
  `check … auto-switch`), cancelability `cooperative`
  (server-job-worker.sh:18-24). Only the allowed action:function pairs are
  accepted (server-job-worker.sh:9-14, else exit 64).
* stdout → data. If stdout is **not** valid JSON the data becomes
  `{"message": "<raw stdout>"}` (common.sh:78-85).
* Exit-code mapping (common.sh:106-117):

| rc | HTTP | code | message |
|---|---|---|---|
| 0 | 200 | — | success envelope |
| 76 | 409 | OPERATION_BUSY | Другая конфликтующая операция BROray уже выполняется. |
| 130 | 409 | OPERATION_CANCELLED | Операция остановлена. Прежняя оценка сервера сохранена. |
| 124 | 504 | SERVER_CHECK_TIMEOUT | Время проверки истекло. Прежняя оценка сервера сохранена. |
| 75 | 503 | OPERATION_UNRESOLVED | Завершение операции ещё не подтверждено. Блокировка сохранена. |
| other ≠0 | 400 | SERVER_OPERATION_FAILED | Операция с сервером завершилась ошибкой. `details` = full stderr of the child (falls back to stdout). Typical stderr line from `broray_die`: `Ошибка: <text>` (util.sh:7-10). |

`broray_job_begin` returning 2 (busy) → worker exit 76 (server-job-worker.sh:24).
The JS shows `error.message + " " + error.details` (servers.js:900-913), so the
`details` text (e.g. `Ошибка: сервер X не найден`) is user-visible.

---------------------------------------------------------------------------

## 1. Endpoint table

| # | URL | Method | Called from JS | Async model |
|---|---|---|---|---|
| 1 | /api/servers/summary.cgi | GET | servers.js:705, servers-auto-switch.js:325; intercepted by servers-completion.js:674-707 | sync read |
| 2 | /api/servers/details.cgi?id=<id> | GET | servers.js:573 (string concat) | sync read |
| 3 | /api/servers/import.cgi (`preview:true`) | POST | servers-import-preflight.js:774-776 | sync, no state change |
| 4 | /api/servers/import.cgi (commit) | POST | servers-import-preflight.js:1284-1290 (also servers.js:873, unreachable — §4.3) | sync job |
| 5 | /api/servers/activate.cgi | POST | servers.js:770-775 | sync job |
| 6 | /api/servers/deactivate.cgi | POST | servers.js:798-803 | sync job |
| 7 | /api/servers/delete.cgi | POST | servers.js:826-831 | sync job |
| 8 | /api/servers/check.cgi (queued, `X-BROray-Queue: 1`) | POST | common.js:117 via `followQueued` (servers.js:727,751; servers-check-all.js:365) | **queued**; poll `/api/operations/status.cgi` |
| 8b | /api/servers/check.cgi (direct, no queue header) | POST | not used by JS (dead `checkNext` servers-check-all.js:297-361 would call it without the header) | sync job |
| 9 | /api/servers/quality-batch-complete.cgi | POST | **not called** (const only, servers-check-all.js:10-11) | sync job |
| 10 | /api/servers/auto-switch-status.cgi | GET | servers-auto-switch.js:329 | sync read |
| 11 | /api/servers/auto-switch-save.cgi | POST | servers-auto-switch.js:386-389 | sync |
| 12 | /api/servers/quality-refresh-status.cgi | GET | servers-quality-refresh.js:178 | sync read |
| 13 | /api/servers/quality-refresh-save.cgi | POST | servers-quality-refresh.js:209 | sync |
| ext | /api/operations/status.cgi | POST (lookup) / GET (poll) | common.js:105, 153 | operations area; fields used listed in §2.8 |
| ext | /api/session.cgi | GET | servers.js:965-980 | auth area; `session.user` used |

All JS requests go through `BROrayUI.apiRequest` (common.js:4-60):
`credentials:"same-origin"`, `cache:"no-store"`, object bodies are JSON-encoded
with `Content-Type: application/json`. Non-2xx or non-object JSON → thrown
Error with `.status`, `.payload`, `.code` (= `payload.error.code` | string
`payload.error` | `"HTTP_RESPONSE_INVALID"`), `.details`, message =
`payload.error.message` or "Ошибка запроса (HTTP n)." / "Некорректный ответ
сервера (HTTP n).".

---------------------------------------------------------------------------

## 2. Endpoints

### 2.1 GET /api/servers/summary.cgi

summary.cgi:1-13. Method GET, session. No params. Sets
`BRORAY_SERVER_SKIP_KEENETIC_REFRESH=true` so the Keenetic status is **only
read from file**, never refreshed (summary.cgi:6-13, server-service.sh:219-221).
Runs `broray_server_summary` (server-service.sh:216-539) **without** a job
(not in the lock list).

Side effects: `mkdir -p /opt/broray/run/server-quality /opt/broray/tmp`;
temp files `/opt/broray/tmp/server-summary-*.$$.json[l]` (deleted). No
process start. Runs `pidof xray`, `netstat -lnt` (SOCKS listener).
`broray_active_proxy_context` reads xray pid / runtime identity / config hash
(active-proxy-health.sh:4-16).

Files read:
* `/opt/broray/config/active-server` (first line = active id)
* `/opt/broray/servers/*.json` (glob order = list order; see §5.10)
* `/opt/broray/run/server-quality/<id>.json` (mtime → freshness)
* `/opt/broray/config/system/server-auto-switch.json`
* `/opt/broray/config/system/settings.json` (socks address/port; lib/xray.sh:207-248)
* `/opt/broray/run/keenetic-status.json` (+ mtime)
* `/opt/broray/run/server-auto-switch-state.json` (`.activeHealth`)

Response `data` (server-service.sh:497-536):

| field | type | notes |
|---|---|---|
| total | number | count of server files |
| available | number | servers with `quality.status=="available"` |
| unavailable | number | `quality.status=="unavailable"` |
| connectionState | "connected"\|"disabled"\|"degraded"\|"error" | §5.6 |
| xrayRunning | bool | `pidof xray` |
| socksActive | bool | listener on settings socks addr:port while xray running |
| activeConnection | {method:"current-socks-https", available:bool, up:bool, freshness:"fresh"\|"unknown"} | |
| keenetic | {healthy:bool, consistent:bool, freshness:"fresh"\|"stale"\|"expired"\|"unknown"} | from keenetic-status.json `.healthy`, `.matchesExpected`; 60 s/300 s |
| activeServer | server row \| null | the row from `servers` whose id = active id |
| servers | array of server rows | |
| autoSwitch | object | entire auto-switch config file + `selectionRuleLabel`; fallback `{"enabled":false,"selectionRule":"manual","selectionRuleLabel":"Ручной выбор"}` |
| qualityPolicy | {method:"https-proxy+icmp", throughputMeasured:false, listLoadProbes:false, automaticProbe:"on-active-failure"} | constant |
| health | status-contract object (module "servers") | §5.6 |
| updatedAt | string `%Y-%m-%dT%H:%M:%S%z` (e.g. `2026-11-02T14:05:01+0300`) | |

`selectionRuleLabel` (server-service.sh:343-348): best-quality→"Лучшее
качество", lowest-ping→"Минимальная задержка", preferred→"Предпочитаемый
сервер", else "Ручной выбор".

Server row (server-service.sh:269-305):

| field | source |
|---|---|
| id | `.id` (must equal filename stem, else whole request fails) |
| name | `.name // .id` |
| address | `.address` |
| port | `.port` |
| protocol | `.protocol` (vless/vmess/trojan/shadowsocks/hysteria2) |
| transport | `.network // .transport.type // "unknown"` (raw/ws/grpc/httpupgrade/xhttp/kcp/hysteria) |
| security | `.security // "none"` |
| sourceType | `.source.type // "manual"` |
| subscriptionId | `.source.subscriptionId // null` |
| nodeIndex | `.source.nodeIndex // null` |
| active | bool |
| quality | defaults `{status:"unknown",ping:null,jitter:null,disconnects:0,successfulChecks:0,failedChecks:0,lastCheckedAt:null,lastSuccessAt:null,durationMs:null,rating:"unknown",error:null,measurementMethod:"https-proxy+icmp",measurementSource:"unknown"}` **+ quality file contents (unparseable file → `{}`)** + `{freshness, ageSeconds, stale, measurementMethod:(q.measurementMethod//"https-proxy+icmp"), measurementSource:(q.measurementSource//"unknown")}` |

`freshness`/`ageSeconds` are computed from the **mtime** of the quality file
(status-contract.sh:130-170), not from `lastCheckedAt`; no file →
`freshness:"unknown"`, `ageSeconds:null`, `stale:true`. Quality-file extra
fields (`serverFingerprint`, `activeHealth`) pass through.

Example:

```json
{"success":true,"data":{
 "total":2,"available":1,"unavailable":0,"connectionState":"connected",
 "xrayRunning":true,"socksActive":true,
 "activeConnection":{"method":"current-socks-https","available":true,"up":true,"freshness":"fresh"},
 "keenetic":{"healthy":true,"consistent":true,"freshness":"fresh"},
 "activeServer":{"id":"manual-20261101120000-1a2b3c4d","name":"NL REALITY","address":"nl.example.net","port":443,
   "protocol":"vless","transport":"raw","security":"reality","sourceType":"manual","subscriptionId":null,"nodeIndex":null,"active":true,
   "quality":{"status":"available","ping":42,"jitter":5,"disconnects":0,"successfulChecks":7,"failedChecks":1,
     "lastCheckedAt":"2026-11-02T13:50:10+0300","lastSuccessAt":"2026-11-02T13:50:10+0300","durationMs":4000,
     "rating":"excellent","error":null,"measurementMethod":"https-proxy+icmp","measurementSource":"manual",
     "serverFingerprint":"<64hex>","freshness":"fresh","ageSeconds":891,"stale":false}},
 "servers":[ <the row above>, {"id":"subscription-sub1-0002", "...":"...","quality":{"status":"unknown","ping":null,"jitter":null,"disconnects":0,"successfulChecks":0,"failedChecks":0,"lastCheckedAt":null,"lastSuccessAt":null,"durationMs":null,"rating":"unknown","error":null,"measurementMethod":"https-proxy+icmp","measurementSource":"unknown","freshness":"unknown","ageSeconds":null,"stale":true}} ],
 "autoSwitch":{"schemaVersion":3,"enabled":false,"failureThreshold":3,"cooldownMinutes":10,"minimumRating":"acceptable",
   "selectionRule":"best-quality","preferredServerId":null,"qualityRefreshEnabled":false,"qualityRefreshIntervalMinutes":60,
   "updatedAt":null,"selectionRuleLabel":"Лучшее качество"},
 "qualityPolicy":{"method":"https-proxy+icmp","throughputMeasured":false,"listLoadProbes":false,"automaticProbe":"on-active-failure"},
 "health":{"schemaVersion":1,"module":"servers","availability":"available","severity":"warning","operational":true,"consistent":true,
   "actionRequired":true,"freshness":{"state":"fresh","checkedAt":"2026-11-02T14:05:01+0300"},
   "reasons":[{"code":"QUALITY_INCOMPLETE","message":"Не для всех серверов выполнена проверка качества.","details":null}],
   "facts":{"connectionState":"connected","activeServerPresent":true,"xrayOperational":true,"socksActive":true,
     "connection":{"available":true,"up":true,"freshness":"fresh"},"proxy0":{"healthy":true,"consistent":true,"freshness":"fresh"},
     "quality":{"fresh":1,"stale":0,"expired":0,"unknown":1}},"lastOperation":null},
 "updatedAt":"2026-11-02T14:05:01+0300"},"error":null}
```

Fields the JS reads:
* `servers.js renderSummary` (servers.js:176-390): `total`, `available`,
  `unavailable` (fallback "0"), `autoSwitch.enabled`, `autoSwitch.selectionRuleLabel`,
  `autoSwitch.selectionRule` (only `=== "manual"`), `health.severity`,
  `connectionState`, `xrayRunning`, `socksActive`, `activeServer.{id,name,address,port,protocol,transport,security,quality.{rating,status,freshness,ping,jitter,lastCheckedAt,measurementSource}}`,
  `servers[]`.
* `renderServerCard` (servers.js:428-541): `id,name,address,port,protocol,transport,security,active,quality.{status,freshness,ping,jitter,lastCheckedAt,measurementSource}`.
* `servers-auto-switch.js fillServers` (servers-auto-switch.js:213-234): `servers[].id`, `.name`.
* `servers-check-all.js extractServerIds` (servers-check-all.js:133-244): dead code (not used by `runCheckAll`).
* `servers-completion.js handleSummary` (servers-completion.js:620-660):
  recursively collects every key matching
  `/^(lastCheckedAt|checkedAt|lastCheckAt|lastCheck|last_checked_at)$/i` in the
  whole envelope (depth ≤10) and shows the newest as "Последняя проверка: …".
  Because `health.freshness.checkedAt` is always "now" (= `updatedAt`), the
  label effectively shows the summary generation time — must be preserved.
* JS failure: `handleError` toast; manual refresh toast "Список серверов обновлён." (servers.js:710-712).

Errors: 405/401; any `broray_die` (invalid filename id, id/filename mismatch:
jq `error("server id does not match filename")` → "не удалось сформировать
данные сервера <id>") → 400 SERVER_OPERATION_FAILED.

### 2.2 GET /api/servers/details.cgi?id=<id>

details.cgi:1-18. Id extraction: `sed -n 's/^.*id=\([^&]*\).*$/\1/p'` on
QUERY_STRING (greedy: last `id=` occurrence wins, also matches e.g. `xid=`),
then only `%2D`→`-`, `%2E`→`.`, `%5F`→`_` are decoded. Not a job.

`broray_server_details` (server-service.sh:541-575):
* `broray_server_validate_id` — empty → die "не указан идентификатор сервера";
  chars outside `[a-zA-Z0-9._-]` → die "идентификатор сервера содержит
  недопустимые символы" (server.sh:10-22).
* missing file → die "сервер <id> не найден".
* Output = server record with secrets removed (`broray_server_mask_json`,
  server-service.sh:126-157): deletes `.uri .uuid .password .auth .privateKey
  .connection.{uuid,password,auth,privateKey} .hysteria.obfsPassword`;
  `.reality.publicKey` → first 6 chars + "…" + last 4 if length >12, else
  "••••••". Everything else (incl. `method`, `tls.*`, `reality.shortId`,
  `reality.mldsa65Verify`, `transport.kcpSeed`, `source.importKey`) is returned.
* plus `active: bool`, `quality: <broray_server_get_quality>` (server-service.sh:159-209):
  file contents + `{freshness, ageSeconds, stale}`, defaulted
  `measurementMethod`/`measurementSource`; no/invalid file → the default object
  (same as summary defaults plus `freshness:"unknown",ageSeconds:null,stale:true`).
  Note: an invalid quality JSON here yields defaults (summary: `{}`+defaults —
  equivalent).

JS reads (`renderDetails`, servers.js:607-699): `id, address, port, protocol,
network || transport.type, security, source.type, source.subscriptionId,
source.nodeIndex, reality.serverName || tls.serverName, reality.publicKey,
reality.shortId, xhttp.path || transport.path, grpc.serviceName ||
transport.serviceName, quality.lastCheckedAt, quality.measurementSource,
quality.measurementMethod (=== "https-proxy+icmp" → "HTTPS через Xray + ICMP
ping"), quality.successfulChecks, quality.failedChecks, quality.error` (shown
in `.server-detail-error`). Cached per id in `state.details` (servers.js:558-604);
cache cleared after activate/deactivate/import/delete/check.

### 2.3 POST /api/servers/import.cgi — preview

import.cgi:1-70. Body (≤64 KB): `{"uri": string, "preview": true}`.
* `uri` empty/missing → `400 URI_REQUIRED` "Не указана конфигурация сервера." (import.cgi:20-24).
* `preview` must be JSON `true` (`jq -e '.preview == true'`).
* Note: `broray_servers_api_lock import` runs before (only sets a variable);
  preview itself is **not** a job and takes no lock.

Processing (import.cgi:28-69): private dir `mktemp -d /tmp/broray-import-preview.XXXXXX`
(failure → `500 PREVIEW_UNAVAILABLE` "Не удалось подготовить проверку.");
in a subshell: `BRORAY_SERVERS=<dir>/servers`, `BRORAY_TMP=<dir>/tmp`, run
`broray_server_import_dispatch "$uri" manual '' 0` (full parse + save +
validation incl. `subscription-xray-json.jq validate_model`), require exactly
one `*.json` produced, compute `canonical = broray_server_subscription_import_key(file)`
(§5.9), then scan live `/opt/broray/servers/*.json` (symlink → fail) for an
equal key; first match → duplicate name `.name // .id`.

Success data:

```json
{"protocol":"vless","name":"NL REALITY","host":"nl.example.net","port":443,
 "transport":"raw","security":"reality",
 "canonical":"3f1c…(64 lowercase hex)",
 "duplicate":null}
```

`transport` = record `.network`; `duplicate` = null or
`"Такая конфигурация уже сохранена: <name>."`.

Failure: `400 SERVER_PREVIEW_FAILED`, message = last 6 lines of the subshell
stderr (e.g. `Ошибка: VLESS REALITY не поддерживает этот транспорт`), or
"Конфигурация не прошла проверку." if empty; details null (import.cgi:64-67).
The preview dir is always removed (trap).

JS (`previewItems`, servers-import-preflight.js:770-793): POSTs each line
sequentially; requires `data.canonical` to match `/^[0-9a-f]{64}$/` else item
error "Роутер не подтвердил разбор конфигурации."; copies **all** data keys
onto the item; marks in-paste duplicates by `canonical` ("Эта конфигурация
повторяется во вставленном списке."); per-item error text =
`error.payload.error.message` (servers-import-preflight.js:488-509). 401 aborts
the batch. Displayed fields: `name, host, port, protocol, transport, security,
duplicate` (servers-import-preflight.js:862-1051).

### 2.4 POST /api/servers/import.cgi — commit

Body `{"uri": string}` (any `preview` other than `true` → commit). Empty →
`400 URI_REQUIRED`. Runs worker `import` → `broray_server_import_job "$uri"
manual "" 0` (server-job-worker.sh:43, server-import-job.sh:2-33):

1. job checkpoint `parsing`; `mktemp -d /opt/broray/tmp/server-import-<opId>-XXXXXX`, writes `uri`, `operation-id`.
2. Supervised helper (timeout 30 s) `lib/server-import-prepare.sh` → runs
   `broray_server_import_dispatch` with `BRORAY_SERVERS=<dir>/servers`
   (server-import-prepare.sh:1-14). Helper rc 75 → OPERATION_UNRESOLVED.
3. Exactly one produced record; id valid; `.id==$id and .source.type=="manual"`;
   target `/opt/broray/servers/<id>.json` must not exist.
4. checkpoint `committing`; copy to `/opt/broray/servers/.import-XXXXXX`,
   chmod 600, atomic replace via `bin/broray-ops-guard --replace-file`.

Success data: `{"imported": true, "id": "manual-20261102140501-1a2b3c4d"}`.
**No duplicate check on commit** (duplicates are only blocked by the preview
step in the browser). JS ignores the data; per-item state imported/failed
(servers-import-preflight.js:1246-1311); failure text = `error.payload.error.message`
(i.e. "Операция с сервером завершилась ошибкой." — details are not shown by the
preflight UI). After all items: toast "Сервер импортирован." /
"Импортировано серверов: N.", clicks `#refresh-servers`, after 700 ms clicks
`#cancel-import` (servers-import-preflight.js:1339-1372). On first failure the
batch stops: "Импорт остановлен. Успешно: k из N." (servers-import-preflight.js:1373-1411).
Delay 160 ms between items (servers-import-preflight.js:1296).

Server id generation (server-import.sh:12-63): manual →
`manual-<date +%Y%m%d%H%M%S>-<8 lowercase hex from /dev/urandom>` (32 attempts
until unused); subscription → `subscription-<subId>-<%04d index>`.

### 2.5 POST /api/servers/activate.cgi

Body `{"id": string}` (≤64 KB). Worker `activate` → `broray_server_activate`
(server-service.sh:894-936):
1. validate id / exists (die "сервер <id> не найден").
2. checkpoint `checking`; dir `/opt/broray/tmp/server-activate-<opId>-XXXXXX`;
   supervised helper 30 s `lib/server-activate-prepare.sh` → generate config
   (§4.4) into `<dir>/config.json`, `xray run -test -c` (env
   `XRAY_LOCATION_ASSET=/opt/broray/bin`), failure → die "Xray отклонил
   конфигурацию сервера".
3. checkpoint `committing` (protected); `broray_xray_apply_prepared_server`
   (server-xray-manager.sh:64-154): backup `/opt/broray/config/config.json` →
   `/opt/broray/backup/config.server.<YYYYmmdd-HHMMSS>.json`, active-server →
   `/opt/broray/backup/active-server.<ts>`; move new config into
   `/opt/broray/config/config.json`; write active id to
   `/opt/broray/config/active-server`; `/opt/etc/init.d/S24broray restart`;
   on restart failure restore config/active and restart again, die
   "не удалось применить сервер"; then `broray_interface_sync_description`
   (Keenetic interface description, warning only).
4. Force-refresh Keenetic status (`broray_keenetic_status_json`), then print summary.

**Response data:** stdout contains human text ("Активирован сервер:\nНазвание:
…", server-xray-manager.sh:148-153, plus init-script output) followed by the
summary JSON, so stdout is not valid JSON and the wrapper returns
`{"message": "<entire stdout>"}` (common.sh:78-85). JS ignores data: toast
"Сервер активирован.", clears details cache, reloads summary (servers.js:776-782).
Confirm dialog text: "Подключить сервер" / "Активировать «name»?" (servers.js:758-768).

### 2.6 POST /api/servers/deactivate.cgi

deactivate.cgi:1-6. **No body is read** (JS sends `{}`). Worker `deactivate`
→ `broray_server_deactivate` (server-service.sh:998-1059): checkpoint
`committing` (protected); if xray running `S24broray stop` (die "не удалось
остановить Xray"); wait up to 10×1 s (die "процесс Xray не остановился");
remove host route from `/opt/broray/run/xray-server-route` (`ip route del
<ip>/32 dev <if>`); **set auto-switch `enabled=false`** (+`updatedAt`) in
`server-auto-switch.json`, or create
`{schemaVersion:1,enabled:false,failureThreshold:3,cooldownMinutes:10,minimumRating:"acceptable",selectionRule:"manual",preferredServerId:null,updatedAt}`
if missing/invalid (server-service.sh:938-977); delete
`/opt/broray/config/active-server` and `/opt/broray/run/interface-status.json`.

Printed JSON: `{"deactivated":true,"previousActiveServerId":<id|null>,"connectionState":"disabled","autoSwitch":{"enabled":false},"updatedAt":"…"}`.
UNCONFIRMED: Entware `rc.func` stop prints status text to stdout, which would
turn the data into `{"message": …}` as in 2.5. JS ignores data: toast
"Серверы отключены." (servers.js:804-810). Confirm: danger dialog "Отключить
серверы" (servers.js:786-796).

### 2.7 POST /api/servers/check.cgi (queued; used by JS)

check.cgi:2-6: if `HTTP_X_BRORAY_QUEUE == "1"` →
`broray_operations_api POST servers-check` (web-new/api/operations/common.sh:5-117).

Request (common.js:65-77, 86-130): headers `X-BROray-Request: operations`,
`X-BROray-Origin: <location.origin>`, `X-BROray-Queue: 1`,
`Content-Type: application/json`; body exactly `{"id": <serverId|"all">, "nonce": <32 lowercase hex>}`;
30 s abort timer.

Validation (operations/common.sh):
| check | HTTP | code / message |
|---|---|---|
| any QUERY_STRING | 400 | INVALID_REQUEST "Параметры запроса не поддерживаются." |
| `X-BROray-Request != operations` | 403 | CSRF_REJECTED "Обновите страницу и повторите действие." |
| origin lib missing | 503 | ORIGIN_VALIDATION_UNAVAILABLE "Проверка источника запроса недоступна." |
| origin not allowed | 403 | ORIGIN_REJECTED "Источник запроса не подтверждён." |
| content type | 415 | INVALID_CONTENT_TYPE "Требуется JSON." |
| bad length | 400 | INVALID_LENGTH "Некорректная длина запроса." |
| > 4096 | 413 | REQUEST_TOO_LARGE "Запрос превышает 4 КБ." |
| body read / not exactly one object | 400 | INVALID_BODY "Запрос получен не полностью." / "Требуется один JSON-объект." |
| keys ≠ ["id","nonce"]; nonce not 32 lowercase hex; id 1–96 chars of `[A-Za-z0-9._-]`, not starting with `.` or `-` | 400 | INVALID_REQUEST "Некорректные параметры задания." |
| server-service.sh fails to load | 503 | STATE_UNAVAILABLE "Служба серверов недоступна." |
| id≠all and `/opt/broray/servers/<id>.json` not a regular file | 404 | SERVER_NOT_FOUND "Сервер не найден." |
| snapshot/context failure | 503 | STATE_UNAVAILABLE "Не удалось подтвердить список серверов." |

These use the standard error envelope (`broray_api_error`).

Routing (operations/common.sh:54-59):
* id ≠ "all" **and** it is the active server with a running, identifiable Xray
  (`broray_active_proxy_context <id>` non-empty, §5.7) → action
  `servers:active-health`, context = that 64-hex runtime context.
* otherwise action `servers:quality`, context = `broray_quality_context <id|all> USER`
  (sha256 over `{servers:[{id,sha256}…sorted],autoConfigSha256:""}`, server-check-job.sh:81-135).
* `queue-submit <action> <id> USER <context> <nonce>` (operation-scheduling.sh:128-164):
  priority `active-health`=0, `quality` USER=2; stage `checking`/`probe`.
  Same nonce → same receipt (input hash must match, else `REQUEST_MISMATCH`);
  identical pending request (same action/target/source/context) → coalesced into it.
  ≥64 queued → `QUEUE_FULL`.

Success: **HTTP 202**, raw body (no envelope):

```json
{"ok":true,"requestId":"q-<nonce>","state":"queued","priority":2,"coalesced":false,"accepted":true}
```

(operation-scheduling.sh:115-126 + operations/common.sh:102-105,112).
Coordinator refusals: `{"ok":false,"errorCode":"<CODE>"}` with 409 for
OPERATION_BUSY/DOMAIN_OPERATION_BUSY/CANCEL_NOT_SUPPORTED/RECOVERY_BLOCKED,
404 for REQUEST_UNCONFIRMED, else 503 (operations/common.sh:106-111). Non-object
coordinator output → 503 STATE_UNAVAILABLE "Состояние операций временно недоступно.".

JS acceptance (common.js:71-74): `ok===true`, `requestId` matches
`/^q-[0-9a-f]{32}$/`, `state ∈ {queued,running,completed,failed,cancelled}` else
"Ответ на запрос задания не подтверждён.". Status 400/401/403/404/409/413/415/422
→ nonce discarded and error thrown; other failures → lookup by nonce via
`POST /api/operations/status.cgi {"nonce":…}` (same headers, `X-BROray-Queue: 0`).
Nonce persisted in `sessionStorage["broray.queue.v1:/api/servers/check.cgi:<id>"]`
before sending; on page load `servers.js renderSummary` resumes any stored
request per server (servers.js:384-389) and check-all resumes `all`
(servers-check-all.js:412).

Execution (queue workers, lib/operation-worker.sh:22-28):
* `servers:quality:probe` → `broray_quality_step` (server-check-job.sh:137-224):
  one server per stage; snapshot of `{id,sha256}` of target server(s) (max 4096);
  each stage runs `broray_server_check <id> manual <sha256>` (source USER→`manual`)
  and yields; progress published to `server-auto-switch-state.json.qualityRefresh`
  (§3.5) — **also for a single-server manual check**; on completion
  `status:"success"`, `lastCompletedAt`, `nextCheckEpoch = now + interval*60`.
  Catalog change between stages (snapshot/context mismatch) → rc 76 → request failed.
* `servers:active-health:checking` → `broray_auto_health_step` (active-proxy-health.sh:76-147):
  measures through the **running** Xray SOCKS (§5.4), publishes active-state
  (§3.4). It does **not** rewrite the active server's quality file (ping/jitter
  unchanged); it updates the "connected" evidence used by summary.

Polling (common.js:131-183): every 5 s (skipped while `document.hidden`)
`GET /api/operations/status.cgi`; requires `data.complete === true` and
`Array.isArray(data.queue)`; finds row by `requestId`; row missing →
`{state:"unknown", message:"Задание не найдено в текущем снимке. Проверьте состояние повторно."}`;
request failure → `{state:"unknown", message:"Не удалось обновить состояние задания. Проверьте состояние повторно."}`.
Queue row fields (operation-public.jq:54-60): `requestId, priority, type, source, stage, state, operationId, reason`
(`reason ∈ awaiting_resource|active_connection|automation_paused`).
Labels (common.js:78-85): queued+automation_paused "На паузе", queued+active_connection
"Отложено ради активного подключения", queued "В очереди", running "Выполняется",
completed "Завершено", failed "Ошибка", cancelled "Отменено", else "Результат запроса не подтверждён".

Per-server UI (servers.js:724-756): all buttons with `data-check-server=<id>` and
`#check-active-server[data-server-id=<id>]` show the label and stay disabled
while queued/running; on terminal: toast "Проверка сервера завершена."
(success) or `result.message || queueLabel` (warning), delete details cache,
reload summary. Check-all (servers-check-all.js:363-380): id `"all"`, note text
"<label>. Серверы проверяются по одному, уступая активному подключению.";
terminal toast "Проверка всех серверов завершена." / warning; clicks `#refresh-servers`.
Cancel: not offered on this page (operations area `cancel.cgi` accepts `{requestId}`).

### 2.7b POST /api/servers/check.cgi (direct, legacy)

No `X-BROray-Queue: 1`: body `{"id"}` (≤64 KB, wrapper rules §0.3), worker
`check` → `broray_server_check <id> manual` (server-check-job.sh:5-77) synchronous;
success data = check result (§3.3 result object incl. `quality`), exit 1 =
completed negative measurement → **400 SERVER_OPERATION_FAILED** with details
= stderr. If the server is active, the result also measures the current SOCKS
path and stores `activeHealth` in its quality file (server-check-job.sh:51-60).
Not used by the current JS.

### 2.8 POST /api/servers/quality-batch-complete.cgi

quality-batch-complete.cgi:1-6. No body read. Worker `quality-batch-complete`
(protected `committing` immediately, server-job-worker.sh:37-42) →
`broray_server_publish_snapshot` (server-service.sh:1109-1117): runs
`bin/broray-home-snapshot refresh servers`, validates
`/opt/broray/run/home-snapshots/servers.json`; data
`{"published":true,"totalServers":N,"capturedAt":"…"}`. **Not called by any JS.**

### 2.9 GET /api/servers/auto-switch-status.cgi

auto-switch-status.cgi:1-72. GET, session. Not a job, no lock.

Data:

| field | source |
|---|---|
| config | `/opt/broray/config/system/server-auto-switch.json` verbatim if a JSON object, else `{"enabled":false}` |
| state | `broray_auto_switch_public_state(/opt/broray/run/server-auto-switch-state.json)` (lib/auto-switch-status.sh:3-70): file verbatim, or `null` if missing/invalid; adjustments in §5.8 |
| monitor | `/opt/broray/run/connection-status.json` if object else `null` |
| service | `broray_service_status_json` for service `auto-switch` (lib/service-lifecycle.sh:131-163): `{service:"auto-switch",state:"running"\|"starting"\|"stopping"\|"stopped"\|"ambiguous"\|…,running:bool\|null,ready:bool,complete:bool,pid:number\|null,errorCode:null\|"SERVICE_IDENTITY_UNCONFIRMED"\|"SERVICE_RECOVERY_REBOOT_REQUIRED"\|"SERVICE_STOP_UNCONFIRMED"}` |
| connectionMonitor | `{running: kill -0 $(cat /opt/broray/run/connection-monitor.pid), pid: number\|null}` |
| logTail | last 20 lines of `/opt/broray/logs/server-auto-switch.log` or null |
| checkedAt | `%Y-%m-%dT%H:%M:%S%z` |

JS (`render`, servers-auto-switch.js:236-319) reads: `config.{enabled,
failureThreshold (default 3), cooldownMinutes (10), minimumRating
("acceptable"), selectionRule ("best-quality"), preferredServerId}`;
`service.{complete,running,state}`; `state.{status, consecutiveFailures,
lastSwitchAt, lastSwitchName, lastReason, lastError}`. Status titles
(servers-auto-switch.js:78-98): disabled "Выключен", healthy "Соединение
работает", manual-off "VPN отключён", paused "Приостановлен", waiting-threshold
"Ожидание порога", attempt-guard "Ожидание проверки", cooldown "Защитный
интервал", checking-candidates "Проверка кандидатов", no-candidate "Кандидат не
найден", recovered "Связь восстановилась", cancelled "Отменено", dry-run
"Пробный расчёт", switching "Переключение", switched "Сервер переключён",
error "Ошибка", anything else (incl. `rolled-back`) "Ожидание". A non-empty
`state.lastError` un-hides `#auto-switch-error` and opens the `<details>`.
Polling: 15 s `setTimeout` chain, paused while hidden, immediate reload on
visibility (servers-auto-switch.js:409-449); each load also GETs summary when
`refreshSummary` or no cached summary.

### 2.10 POST /api/servers/auto-switch-save.cgi

auto-switch-save.cgi:1-200. POST, session; acquires routes-api global lock
`servers:auto-switch-save` scope `servers` (lock rc 2 → `409 OPERATION_BUSY`,
other → `500 GLOBAL_LOCK_FAILED` "Не удалось установить общую блокировку
BROray."; lib missing → `500 GLOBAL_LOCK_UNAVAILABLE` "Общий координатор
операций недоступен."). Body limit **16384**: rc 2/3 → `400 INVALID_CONTENT_LENGTH`
"Тело запроса пусто или имеет некорректный размер.", 4 → `413 REQUEST_TOO_LARGE`
"Запрос слишком большой.", else `400 REQUEST_BODY_INCOMPLETE` "Тело запроса
передано не полностью."; not object → `400 INVALID_JSON` "Тело запроса должно
быть объектом JSON.".

Body (JS servers-auto-switch.js:374-382):
`{"enabled":bool,"failureThreshold":number,"cooldownMinutes":number,"minimumRating":string,"selectionRule":string,"preferredServerId":string|null}`.

Validation is on `jq -r` text (so `"true"`/`"3"` strings are accepted):

| field | rule | error |
|---|---|---|
| enabled | text `true`/`false` | 400 INVALID_ENABLED "Поле enabled должно быть логическим." |
| failureThreshold | digits only, 1..10 | 400 INVALID_THRESHOLD "Порог ошибок должен быть числом." / "Порог ошибок должен быть от 1 до 10." |
| cooldownMinutes | digits only, 1..120 | 400 INVALID_COOLDOWN "Защитный интервал должен быть числом." / "…от 1 до 120 минут." |
| minimumRating | excellent\|good\|acceptable\|poor | 400 INVALID_MINIMUM_RATING "Некорректное минимальное качество." |
| selectionRule | best-quality\|lowest-ping\|preferred | 400 INVALID_SELECTION_RULE "Некорректное правило выбора." |
| preferredServerId | required (non-empty) if rule=preferred | 400 PREFERRED_SERVER_REQUIRED "Выберите предпочтительный сервер." |
| preferredServerId (if non-empty) | `[A-Za-z0-9._-]` only | 400 INVALID_SERVER_ID "Некорректный идентификатор сервера." |
| " | `/opt/broray/servers/<id>.json` exists | 400 SERVER_NOT_FOUND "Предпочтительный сервер не найден." |

Preserves `qualityRefreshEnabled` (bool else false) and
`qualityRefreshIntervalMinutes` (30/60/180/360 else 60) from the existing file.
Writes atomically (`<file>.new.$$`, 0600, mv):

```json
{"schemaVersion":3,"enabled":true,"failureThreshold":3,"cooldownMinutes":10,
 "minimumRating":"acceptable","selectionRule":"best-quality","preferredServerId":null,
 "qualityRefreshEnabled":false,"qualityRefreshIntervalMinutes":60,
 "updatedAt":"2026-11-02T14:10:00+0300"}
```

Write failure → `500 CONFIG_WRITE_FAILED` "Не удалось сформировать настройки." /
"Не удалось сохранить настройки.". Then `/opt/etc/init.d/S27broray-auto-switch start`
(errors ignored) → `bin/broray-service auto-switch start`. Response data = the
written file. JS: toast "Автовыбор сервера включён." / "…выключен.", clicks
`#refresh-servers`, reloads status (servers-auto-switch.js:389-402).

### 2.11 GET /api/servers/quality-refresh-status.cgi

quality-refresh-status.cgi:1-76. Data:

```json
{"config":{"enabled":true,"intervalMinutes":60,"updatedAt":"2026-11-02T14:10:00+0300"},
 "state":{"status":"success","backgroundOperationId":"op-q-…","requestId":"q-…","intervalMinutes":60,
   "runStartedAt":null,"lastCompletedAt":"2026-11-02T13:12:40+0300","nextCheckEpoch":1793963560,
   "totalCount":5,"checkedCount":5,"availableCount":4,"unavailableCount":1,"errorCount":0,
   "lastError":null,"lastResult":"Проверено: 5 из 5","queueState":"completed"},
 "service":{"service":"auto-switch","state":"running","running":true,"ready":true,"complete":true,"pid":1234,"errorCode":null},
 "automation":{"paused":false,"complete":true},
 "checkedAt":"2026-11-02T14:12:00+0300"}
```

* `config` from auto-switch config: `enabled` = `qualityRefreshEnabled` if bool
  else false; `intervalMinutes` ∈ {30,60,180,360} else 60; `updatedAt // null`;
  unreadable → `{"enabled":false,"intervalMinutes":60,"updatedAt":null}`.
* `state` = `broray_auto_switch_public_state(...) | .qualityRefresh // null` (§5.8).
* `automation` from `${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/background-automation.json`:
  absent → `{paused:false,complete:true}`; valid `{paused:bool}` →
  `{paused, complete:true}`; anything else (symlink, invalid) →
  `{paused:true,complete:false}`.

JS (`render`, servers-quality-refresh.js:115-174): effective status = disabled
if `!config.enabled`; paused if `automation.paused`; "unavailable" if not
(`service.complete===true` and bool `service.running`); "stopped" if not
`running && ready`; else `state.status || "scheduled"`. Titles
(servers-quality-refresh.js:66-79): disabled "Выключена", scheduled
"Запланирована", running "Выполняется", success "Завершена", partial
"Завершена с ошибками", paused "На паузе", unavailable "Состояние не
подтверждено", stopped "Служба остановлена", error "Ошибка", else "Ожидание".
Reads `state.{status,totalCount,checkedCount,availableCount,unavailableCount,errorCount,lastResult,lastCompletedAt,nextCheckEpoch (epoch seconds),lastError}`,
`service.errorCode === "SERVICE_RECOVERY_REBOOT_REQUIRED"` (fixed ru message).
Result text: running → "Проверено c из t. Серверы проверяются последовательно."; else
`lastResult`; else counts text; else "Результатов автоматической проверки пока нет.".
When `lastCompletedAt` changes from a previously seen non-null value it clicks
`#refresh-servers` (servers-quality-refresh.js:104-113). Poll 15 s.

### 2.12 POST /api/servers/quality-refresh-save.cgi

quality-refresh-save.cgi:1-123. Lock `servers:quality-refresh-save` (same
errors as 2.10). Body limit **4096** (same body error codes as 2.10). Body must
be an object with **exactly** keys `enabled` (bool) and `intervalMinutes`
(number) → else `400 INVALID_JSON` "Ожидались поля enabled и intervalMinutes.";
interval text ∈ 30|60|180|360 else `400 INVALID_INTERVAL` "Допустимые
интервалы: 30, 60, 180 или 360 минут.".

The existing config must be valid (`enabled` bool, numbers, strings, preferred
null|string, threshold 1..10, cooldown 1..120, rating/rule enums, id charset),
else `500 CONFIG_INVALID` with one of: "Текущие настройки серверов повреждены;
автоматическая проверка не изменена.", "Текущий порог автовыбора некорректен.",
"Текущий защитный интервал некорректен.", "Текущий порог автовыбора вне
допустимого диапазона.", "Текущий защитный интервал вне допустимого
диапазона.", "Текущее минимальное качество некорректно.", "Текущее правило
автовыбора некорректно.", "Идентификатор предпочтительного сервера
некорректен." (quality-refresh-save.cgi:57-82). Note: a config created by
deactivate (`selectionRule:"manual"`) makes this endpoint fail with CONFIG_INVALID.

Rewrites the whole schemaVersion-3 config (same shape as 2.10) with
`qualityRefreshEnabled`, `qualityRefreshIntervalMinutes`, new `updatedAt`;
write failure → `500 CONFIG_WRITE_FAILED` "Не удалось сформировать настройки
автопроверки." / "Не удалось сохранить настройки автопроверки."; starts
S27broray-auto-switch. Data: `{"enabled":bool,"intervalMinutes":n,"updatedAt":"…"}`.
JS toast "Расписание автоматической проверки сохранено." / "Автоматическая
проверка качества выключена.".

---------------------------------------------------------------------------

## 3. Persistent data model

### 3.1 Server record `/opt/broray/servers/<id>.json` (mode 0600)

Common (all protocols): `schemaVersion: 2`, `id` (= filename stem,
`[a-zA-Z0-9._-]+`), `name` (string), `source` (`{"type":"manual"}` or
`{"type":"subscription","subscriptionId":s,"nodeIndex":n}` + `importKey` for
subscription-synced records, server-subscription-service.sh:176), `uri`
(original link), `protocol`, `address`, `port` (number 1..65535), `network`,
`security`.

| protocol | record fields (server-import.sh) |
|---|---|
| vless (:98-117) | `uuid`, `encryption` ("none" or ML-KEM string), `flow` (null\|"xtls-rprx-vision"\|"xtls-rprx-vision-udp443"), `tls:{serverName,fingerprint,allowInsecure:false,alpn:[…]}`, `reality:{serverName,fingerprint,publicKey,shortId,spiderX}`, `transport:{host,path,serviceName,mode,headerType,extra}`, `xhttp:{path,mode,extra}` |
| vmess (:350-423) | `uuid`, `alterId` (number), `encryption` (auto\|aes-128-gcm\|chacha20-poly1305\|none or legacy `scy`), `tls{…}`, `reality{…}`, `transport{…}` (no `xhttp` block, no `flow`) |
| trojan (:539-598) | `password`, `tls{…}`, `transport{…}`; `reality{serverName,fingerprint,publicKey,shortId,spiderX}` only when security=reality (:1178-1179) |
| hysteria2 (:708-768) | `auth`, `network:"hysteria"`, `security:"tls"`, `tls:{serverName,fingerprint,alpn,allowInsecure:false[,pinnedPeerCertSha256]}`, `hysteria:{version:2,ports:"" \| "443,1000-2000",obfs:""\|"salamander"\|"gecko",obfsPassword,upMbps:"",downMbps:"",finalMask:{…}}` |
| shadowsocks (:851-884) | `method`, `password`, `network:"raw"`, `security:"none"` (no tls/transport) |

Stream extensions added by `broray_server_save_stream_extensions`
(server-import.sh:1170-1192) for every protocol except shadowsocks:
`transport.header` (from `header=` JSON), `transport.kcp`,
`transport.kcpSeed`, `transport.kcpLegacy` (only network=kcp),
`tls.echConfigList` (`ech`), `tls.verifyPeerCertByName` (`vcn`),
`tls.pinnedPeerCertSha256` (`pcs`, colons removed, lowercased; conflict with
an existing hy2 pin → error "conflicting certificate pins"),
`reality.mldsa65Verify` (`pqv`), `transport.finalMask` (`fm`, unless `{}`).

Full validation `broray_server_validate` (server.sh:230-309) +
`broray_server_stream_fields_valid` (util.sh:191-266) + jq
`subscription-xray-json.jq --arg validate_model yes` (util.sh:264-265,
subscription-xray-json.jq:309-320). Key rules:
* trojan: non-empty password; hysteria2: string auth, network "hysteria",
  security "tls", `hysteria.version==2`, non-empty `tls.serverName`, pin 64
  lowercase hex; shadowsocks: method/password non-empty, raw/none; vless:
  encryption "none" or valid ML-KEM-768 string (§4.1), reality only with
  raw/grpc/xhttp, flow only with (raw or encryption≠none) and tls/reality; other: non-empty uuid.
* `tls.allowInsecure` must not be true (all protocols).
* vless/vmess/trojan: xhttp mode ∈ auto|packet-up|stream-up|stream-one; grpc
  mode ∈ gun|multi|auto; `transport.extra` non-empty only for xhttp; raw
  headerType none|http; kcp any listed; other networks headerType none.
* kcp: security none|tls; kcp options keys ⊆ {mtu,tti,uplinkCapacity,downlinkCapacity,cwndMultiplier,maxSendingWindow},
  integers 0..4294967295, mtu 21..65507 (default 1350), tti 10..1000 (default 20),
  cwndMultiplier ≥1, maxSendingWindow ≥ mtu; seed printable; headerType ∈
  none|srtp|utp|wechat-video|wechat|dtls|wireguard|dns; host only with
  headerType dns; kcpLegacy ⇒ no finalMask.
* `echConfigList`, `verifyPeerCertByName`, `pinnedPeerCertSha256` only when
  security=="tls" and printable; pins = comma list of 64 lowercase hex.
* `mldsa65Verify` only with reality, exactly 2603 chars `[A-Za-z0-9_-]`.
* finalMask object keys ⊆ {tcp,udp,quicParams}; tcp item types
  header-custom|fragment|sudoku|xmc; udp item types
  header-custom|mkcp-legacy|noise|salamander|sudoku|xdns|xicmp|realm|udphop;
  items `{type,settings?}`.
* hysteria2: obfs ""|salamander|gecko and obfsPassword both-or-none; with obfs
  no `salamander` in finalMask.udp; with `ports` no `udphop` in finalMask.udp
  and ports = 1..128 comma items of `n` or `a-b` (a≤b, 1..65535) and
  `port` == first number of `ports`.
* xhttp extra (subscription-xray-json.jq:100-160): keys ⊆ headers,
  xPaddingBytes, noGRPCHeader, noSSEHeader, scMaxEachPostBytes,
  scMinPostsIntervalMs, scMaxBufferedPosts, scStreamUpServerSecs, xmux,
  xPaddingObfsMode, xPaddingKey, xPaddingHeader, xPaddingPlacement,
  xPaddingMethod, uplinkHTTPMethod, sessionIDPlacement, sessionIDKey,
  sessionIDTable, sessionIDLength, seqPlacement, seqKey, uplinkDataPlacement,
  uplinkDataKey, uplinkChunkSize, serverMaxHeaderBytes, downloadSettings (+ a
  redundant `mode` equal to the outer mode); ≤4096 bytes JSON; headers must not
  contain Host; ranges `n` or `"a-b"`; xmux keys maxConcurrency,
  maxConnections (not both >0), cMaxReuseTimes, hMaxRequestTimes,
  hMaxReusableSecs, hKeepAlivePeriod; enum placements; non-packet-up modes may
  not use cookie/header uplinkDataPlacement or GET uplink; sessionIDTable
  entropy rule; downloadSettings only at depth 0 and mode≠stream-one with its
  own xhttp/protection validation.
* raw `header` (subscription-xray-json.jq:169-181 & 316-319): only with
  network raw, headerType none, empty host, path ""|"/"; `{}`, `{type:"none"}`
  or `{type:"http",request:{version,method,path,headers},response:{version,status,reason,headers}}`.

Example (VLESS REALITY, manual):

```json
{"schemaVersion":2,"id":"manual-20261101120000-1a2b3c4d","name":"NL REALITY",
 "source":{"type":"manual"},
 "uri":"vless://0f6c…@nl.example.net:443?type=tcp&security=reality&pbk=…&sid=ab12&sni=www.microsoft.com&fp=chrome&flow=xtls-rprx-vision#NL%20REALITY",
 "protocol":"vless","address":"nl.example.net","port":443,"uuid":"0f6c…","encryption":"none",
 "flow":"xtls-rprx-vision","network":"raw","security":"reality",
 "tls":{"serverName":"www.microsoft.com","fingerprint":"chrome","allowInsecure":false,"alpn":[]},
 "reality":{"serverName":"www.microsoft.com","fingerprint":"chrome","publicKey":"Q2l…","shortId":"ab12","spiderX":""},
 "transport":{"host":"","path":"/","serviceName":"","mode":"auto","headerType":"none","extra":{}},
 "xhttp":{"path":"/","mode":"auto","extra":{}}}
```

### 3.2 Quality record `/opt/broray/run/server-quality/<id>.json` (0600)

Written by `broray_server_measure` (server-service.sh:831-867) inside the check
helper and published by the coordinator (operation-publication.sh:44-46):

```json
{"status":"available","ping":42,"jitter":5,"disconnects":0,"successfulChecks":7,"failedChecks":1,
 "lastCheckedAt":"2026-11-02T13:50:10+0300","lastSuccessAt":"2026-11-02T13:50:10+0300",
 "durationMs":4000,"rating":"excellent","measurementMethod":"https-proxy+icmp",
 "measurementSource":"manual","error":null,
 "serverFingerprint":"<sha256 of server file, queue path only>",
 "activeHealth":{ "...": "direct legacy check of active server only" }}
```

`status` available|unavailable; `rating` excellent|good|acceptable|poor|unavailable;
`measurementSource` manual|auto-switch|scheduled; `error` null or string.

### 3.3 Check result object (stdout of `broray_server_check`)

`{"serverId":id,"success":bool,"stage":"proxy-https"|…,"quality":<3.2>,"checkedAt":"…"}`
(server-service.sh:876-889). Stages: validation, xray-config, proxy-https,
probe-config, probe-workdir, probe-xray, probe-port, probe-xray-config,
probe-xray-start, probe-socks-listen, proxy-dns, proxy-connect, proxy-timeout,
proxy-tls, socks-handshake (bin/broray-server-probe:101-424).

### 3.4 Auto-switch config — §2.10 shape; seed runtime/state-seed/config/system/server-auto-switch.json:
`{"schemaVersion":3,"enabled":false,"failureThreshold":3,"cooldownMinutes":10,"minimumRating":"acceptable","selectionRule":"best-quality","preferredServerId":null,"qualityRefreshEnabled":false,"qualityRefreshIntervalMinutes":60,"updatedAt":null}`.

### 3.5 Auto-switch state `/opt/broray/run/server-auto-switch-state.json`

Composite object updated by coordinator field publications
(operation-publication.sh:17-46, 143-145):
* active-state (merges these top-level keys): `schemaVersion:3,
  backgroundOperationId, enabled, status ∈ healthy|waiting-threshold|disabled|paused,
  activeServerId, activeHealth{method:"current-socks-https",serverId,status
  healthy|unhealthy|unknown,errorCode,endpoint,attempts,curlCode,checkedAt
  (UTC Z),checkedEpoch,context}, consecutiveFailures, lastProxyContext,
  lastEvaluationAt, lastEvaluationEpoch, lastReason, lastError:null,
  autoConfigSha256` (active-proxy-health.sh:128-134).
* `.qualityRefresh` (replaced wholesale, server-check-job.sh:202-210):
  `{status:"running"|"success", backgroundOperationId, requestId,
  intervalMinutes, runStartedAt (running only), lastCompletedAt (success
  only, `%z` format), nextCheckEpoch (success: now+interval*60, else 0),
  totalCount, checkedCount, availableCount, unavailableCount, errorCount (always 0),
  lastError:null, lastResult:"Проверено: c из t"}`.
* `.failover` (replaced wholesale, active-proxy-health.sh:518-531):
  `{schemaVersion:1,requestId,operationId,sourceContext,serverId,
  autoConfigSha256,activeHealth,status ∈ probing|preparing|prepared|no-candidates|recovered,
  checkedCount,totalCount,candidateCount,selectedServerId,lastAttemptEpoch,updatedAt,updatedEpoch}`.
* failover completion (auto-state, active-proxy-health.sh:607-640): sets
  `status` switched|rolled-back, `activeServerId`, `activeHealth:null`,
  `lastProxyContext:null`, `consecutiveFailures:0`, `candidateCount`,
  `lastAttemptEpoch`, `failover.{status,operationId,updatedAt,updatedEpoch}`,
  on switch `lastSwitchEpoch,lastSwitchAt (UTC Z),lastSwitchFrom,lastSwitchTo,lastSwitchName`,
  `lastReason` "Выбран резервный сервер; ожидается проверка активного
  соединения" / "Переключение не подтверждено; прежняя конфигурация
  восстановлена", `lastError` null / "FAILOVER_ACTIVATION_ROLLED_BACK";
  ensures `qualityRefresh` exists with defaults `{status:"idle",…0}`.
* Legacy `--once`/`--job` mode of bin/broray-server-auto-switch writes the
  full object itself (bin/broray-server-auto-switch:341-520; statuses incl.
  manual-off, attempt-guard, cooldown, checking-candidates, no-candidate,
  switching, switched, recovered, error, dry-run; qualityRefresh statuses
  scheduled/partial/paused/error/disabled). Daemon mode does not use it.

---------------------------------------------------------------------------

## 4. Share links → record → Xray

Dispatcher `broray_server_detect_protocol` (server-import.sh:172-205) is
**case-sensitive**: `vless://`, `vmess://`, `trojan://`, `ss://`,
`hysteria2://`, `hy2://`; `tuic://`, `socks://`, `socks5://`, `http(s)://` →
die "парсер … ещё не реализован"; anything else "неподдерживаемый формат
конфигурации". (The browser frame check is case-insensitive,
servers-import-preflight.js:468.)

Query parsing (util.sh:85-106): `&`-separated, keys and values
percent-decoded (`+` is **not** a space); a repeated key (or alias) → error
"повторяющийся параметр URI: <key>"; percent-decoding rejects `%00`, `%0A`,
malformed `%xx`, and a trailing LF (util.sh:45-83). Fragment = name
(percent-decoded).

### 4.1 vless:// (lib/parser-vless.sh:7-108)

`vless://<uuid>@<host>:<port>[/]?<query>#<name>` (path other than `/` → error).
Address = text before last `:` (IPv6 brackets are **kept** for vless), port after.

| param | record | notes / defaults |
|---|---|---|
| type | network | ""\|tcp\|raw→raw, ws\|websocket→ws, grpc, kcp, httpupgrade\|httpUpgrade→httpupgrade, xhttp\|splithttp→xhttp |
| security | security | ""\|none→none, tls, reality |
| encryption | encryption | default "none"; else `mlkem768x25519plus.<native\|xorpub\|random>.<0rtt\|1rtt>[.padding…].<keys>` keys 43 or 1579 chars url-b64, padding groups `a-b-c` rules (util.sh:145-164) |
| flow | flow | ""\|xtls-rprx-vision\|xtls-rprx-vision-udp443; needs tls/reality and (raw or encryption≠none) |
| sni | tls.serverName & reality.serverName | tls default = address |
| fp | tls.fingerprint & reality.fingerprint | default chrome |
| pbk / sid / spx | reality.publicKey/shortId/spiderX | reality requires sni & pbk |
| host | transport.host | |
| authority | transport.host (grpc only; must equal host if both) | ignored for other networks |
| path | transport.path, xhttp.path | default "/" |
| serviceName / service_name | transport.serviceName | |
| mode | transport.mode, xhttp.mode | grpc: ""\|gun\|multi; default "auto" (stored) |
| headerType | transport.headerType | default none |
| extra | transport.extra, xhttp.extra | JSON object, default {} |
| alpn | tls.alpn | comma list |
| allowInsecure / insecure | — | only ""\|0\|false\|FALSE accepted, else error |
| header, ech, pcs, vcn, pqv, fm, seed, mtu, tti, uplinkCapacity, downlinkCapacity, cwndMultiplier, maxSendingWindow | §4.6 | |

Name default `address:port`.

### 4.2 vmess:// (lib/parser-vmess.sh)

* Modern form (authority contains `@`, :343-373): `encryption` ∈
  auto|aes-128-gcm|chacha20-poly1305|none (default auto, explicit empty → error);
  `flow`, `aid`, `alterId` → error; rest re-parsed as VLESS (all §4.1 params);
  `alterId=0`; grpc mode default gun (gun|multi).
* Legacy base64 JSON (:39-341): base64/url-safe, padding fixed; JSON keys:
  `ps|remarks|name`, `add|address`, `port`, `id|uuid`, `aid|alterId` (digits),
  `scy|securityCipher|cipher` (default auto, not validated), `net|network`
  (default tcp; tcp→raw, ws/websocket, grpc, httpupgrade/httpUpgrade,
  xhttp/splithttp→xhttp, raw, kcp), `tls|security` (none/tls/reality),
  `sni|serverName`, `fp|fingerprint` (chrome), `alpn` (array or comma string,
  Unicode-whitespace trimmed), `host`, `path` (default "/"),
  `serviceName|service_name|path`, `mode` (grpc: type multi→multi else gun;
  xhttp: type or auto), `type|headerType` (grpc: gun/multi must equal mode →
  none; xhttp: must equal mode or none → none), `pbk|publicKey`,
  `sid|shortId`, `spx|spiderX`, `extra` (object, xhttp only), `ech`,`pcs`,`vcn`,`pqv`
  (strings), `fm` (object), kcp: `seed` (= path if both), `mtu…maxSendingWindow`,
  kcpLegacy=true always; `allowInsecure|allow_insecure|insecure` must be falsy;
  `congestion|readBufferSize|writeBufferSize` → error. Reality needs sni + pbk.

### 4.3 trojan:// (lib/parser-trojan.sh)

`trojan://<password>@<host>:<port>[/]?…#name`; IPv6 brackets stripped.
type (default raw; same aliases incl. kcp), security (default tls; tls|reality;
reality needs sni+pbk and raw/grpc/xhttp), sni (default address), fp
(chrome), host, authority (grpc only, else error), serviceName|service_name,
headerType (none any; http raw; srtp/utp/wechat-video/wechat/dtls/wireguard/dns kcp),
mode (grpc gun|multi default gun; xhttp auto|packet-up|stream-up|stream-one default auto),
path (default "/"), alpn (trimmed comma list), allowInsecure|insecure (falsy only),
pbk/sid/spx, extra (only with xhttp; JSON object), plus §4.6.

### 4.4 ss:// (lib/parser-shadowsocks.sh)

SIP002 `ss://<base64(method:password)>@host:port[/]?plugin=…#name` or
`ss://method:password@…` (percent-encoded) or legacy
`ss://<base64(method:password@host:port)>#name`. Length ≤16384, no control
chars; strict canonical base64; decoded text printable UTF-8 ≤8192.
`plugin` with non-empty value (or without `=`) → "Shadowsocks SIP003 plugins
пока не поддерживаются". IPv6 must be bracketed; host validated as IPv4/IPv6/DNS
label; port 1..65535 (leading zeros stripped). Methods: aes-128-gcm,
aes-256-gcm, chacha20-poly1305, chacha20-ietf-poly1305, xchacha20-poly1305,
2022-blake3-aes-128-gcm (16-byte keys), 2022-blake3-aes-256-gcm /
2022-blake3-chacha20-poly1305 (32-byte; key chains `k1:k2` only for AES-2022);
2022 keys re-encoded canonical base64. Name default `address:port`.

### 4.5 hysteria2:// | hy2:// (lib/parser-hysteria2.sh)

`hy2://[auth@]host[:ports][/]?…#name`; port default 443; ports list
`443,1000-2000` → `hysteria.ports` (only when it contains `,` or `-`), `port` =
first number. Name default = address (not addr:port). Params: security
(default tls; non-tls rejected at save), sni → peer → address, fp (chrome),
alpn (default `["h3"]`), obfs (""|salamander|gecko) + obfs-password (both or
none), upmbps/downmbps → error if non-empty, pinSHA256 (colons removed,
lowercased, 64 hex) → tls.pinnedPeerCertSha256, insecure=1/true/yes requires
pinSHA256 (allowInsecure always stored false), fm (JSON object) →
hysteria.finalMask, plus `ech`, `pcs`, `vcn`, `pqv`, `header` per §4.6 (`fm`
not read there for hysteria).

### 4.6 Shared stream extensions (util.sh:167-187, 279-305)

* `header=<json>` → transport.header; only network raw, non-empty, and not
  combined with `host`/`path`/`headerType` params.
* `ech` → tls.echConfigList; `pcs` → tls.pinnedPeerCertSha256; `vcn` →
  tls.verifyPeerCertByName (all require security tls); `pqv` → reality.mldsa65Verify.
* `fm=<json>` → transport.finalMask (non-hysteria).
* mKCP: `congestion|readBufferSize|writeBufferSize` → error always;
  `seed|mtu|tti|uplinkCapacity|downlinkCapacity|cwndMultiplier|maxSendingWindow`
  outside kcp → error; in kcp: integers → transport.kcp; `seed` →
  transport.kcpSeed; kcpLegacy=true if `seed` or `headerType` present.

### 4.7 Xray config generation (lib/server-config-generator.sh:11-462)

Output file (`BRORAY_SERVER_TMP_CONFIG`):

```json
{"log":{"access":"/opt/broray/logs/access.log","error":"/opt/broray/logs/error.log","loglevel":"<settings.logLevel|warning>"},
 "inbounds":[{"tag":"socks","listen":<settings.listenAddress>,"port":<settings.socksPort|2080>,"protocol":"socks","settings":{"auth":"noauth","udp":true}}],
 "outbounds":[<outbound tag "proxy">]}
```

Outbounds: vless `settings.vnext[{address,port,users[{id,encryption(//none),flow?}]}]`;
vmess `users[{id,alterId(//0),security:(encryption//auto)}]`; trojan
`settings.servers[{address,port,password}]`; shadowsocks
`settings.servers[{address,port,method,password}]` (no streamSettings);
hysteria2 → `protocol:"hysteria"`, `settings{version:2,address,port,obfsPassword?}`.

streamSettings `{network, security}` +
* xhttp → `xhttpSettings{path,mode,host?,extra?}` (vless from `xhttp.*`, others from `transport.*`)
* raw → `rawSettings.header` = transport.header, else `{type:headerType}` + for
  http `request{path:[path],headers{Host:host.split(",")}}`
* ws → `wsSettings{path,host?}`; grpc → `grpcSettings{serviceName,authority(host),multiMode}` (empties dropped);
  httpupgrade → `httpupgradeSettings{path,host}`; kcp → `kcpSettings` = transport.kcp;
  hysteria → `hysteriaSettings{version:2,auth}`
* tls → `tlsSettings{serverName,fingerprint,alpn,echConfigList,verifyPeerCertByName,pinnedPeerCertSha256}` (empties dropped);
  reality → `realitySettings{serverName,fingerprint,publicKey,shortId,spiderX,mldsa65Verify}` (empties dropped)
* `finalmask`: hysteria → hysteria.finalMask with udp = [salamander{password[,packetSize:"512-1200" for gecko]}] + fm.udp + [udphop{mode:"intervalremote",interval:30,remotePorts:ports}];
  kcp legacy → `{udp:[{type:"mkcp-legacy",settings:{value:seed}?}, {type:"mkcp-legacy",settings:{header:(wechat-video→wechat),value:host if dns}} if headerType≠none]}`;
  else transport.finalMask if non-empty.

---------------------------------------------------------------------------

## 5. Algorithms and rules

### 5.1 Isolated quality measurement (`broray_server_measure`, server-service.sh:577-892)

Runs only in the supervised helper (`BRORAY_OPS_SUPERVISED=ptrace/1`, else 73);
helper timeout 120 s (server-check-job.sh:31-32). Steps: validate record →
generate config → `xray run -test` → `bin/broray-server-probe`:
* copies config, replaces inbounds with one SOCKS `127.0.0.1:<port>`
  (`udp:false`, sniffing http/tls/quic routeOnly), port = 35000 + (pid % 18000),
  incremented while in use (netstat), wrap at 54999, ≤200 tries (broray-server-probe:151-215);
* `xray run -test`, then start; wait ≤12 × 1 s for the listener;
* curl via `--socks5-hostname` (or `socks5h://`) to, in order,
  `https://cp.cloudflare.com/generate_204`, `https://www.gstatic.com/generate_204`,
  `https://connectivitycheck.gstatic.com/generate_204`,
  `https://www.cloudflare.com/cdn-cgi/trace`; `--location --connect-timeout 6
  --max-time 16`; success = curl rc 0 and HTTP 2xx/3xx; curl rc → stage
  mapping 5/6 proxy-dns, 7 proxy-connect, 28 proxy-timeout,
  35,51,53,58,59,60,64,66,77,80,82,83,90,91 proxy-tls, 97 socks-handshake,
  else proxy-https; final error "Реальный HTTPS-запрос через сервер не прошёл: <curl stderr tail>".
* On success only: `ping -c 3 <address>` (direct ICMP, not via proxy);
  `ping` = round(avg), `jitter` = round(max) − round(min) (int(x+0.5)), both
  null if output unparsable.
* Counters: success → successfulChecks+1, status available, lastSuccessAt=now,
  error null; failure → failedChecks+1, status unavailable, rating
  "unavailable", lastSuccessAt kept, error = message (default "Проверка
  завершилась ошибкой"). `disconnects` copied. `durationMs` = whole seconds ×1000.
* **Rating** (server-service.sh:793-803): ping null → acceptable; ≤80
  excellent; ≤150 good; ≤300 acceptable; >300 poor.
* Result is published only if complete and consistent (server-check-job.sh:38-50);
  crash/timeout/cancel keep the old quality file.

### 5.2 Freshness (server-service.sh:25-72, status-contract.sh:155-170)

Age = now − mtime(quality file). fresh if age ≤ stale; stale if ≤ expired;
else expired. Defaults stale 1800 s, expired 10800 s. If
`qualityRefreshEnabled` is true: stale = interval·60 + 900; expired =
max(interval·60·3, 10800). Env overrides `BRORAY_SERVER_QUALITY_STALE_SECONDS`/`…EXPIRED…`.
Keenetic: 60 s/300 s.

UI mapping (servers.js:110-127): available+fresh "Доступен"/success;
available+stale "Данные устаревают"/warning; available+expired "Проверка
устарела"/warning; unavailable "Недоступен"/danger; else "Не проверялся"/neutral.
Active card rating text (servers.js:284-304): excellent "Отличное", good
"Хорошее", fair "Среднее", poor "Низкое", bad "Плохое" — backend never emits
fair/bad; **acceptable/unavailable/unknown fall through** to status
("Доступен"/"Недоступен"/"Нет данных"); suffixes for stale/expired/unknown freshness.
Source labels (servers.js:40-47): manual "Ручная проверка", auto-switch
"Автовыбор после отказа", scheduled "Автоматическая проверка", else
"Источник не зафиксирован".

### 5.3 Check-all / single check through the queue

See §2.7. Snapshot of up to 4096 servers sorted by id with sha256 of each
file; per stage exactly one server; each stage re-verifies the context (file
hashes + for SERVER_CHECK_AUTO the config hash and `qualityRefreshEnabled==true`),
else rc 76 → request failed. Counts: success → availableCount, any complete
negative → unavailableCount. Scheduler priorities (operation-scheduling.sh:5-18):
active-health 0, failover 1, quality USER 2, quality scheduled 3,
subscriptions USER 2 / auto 4, dot 5. Queued rows with priority ≥2 show reason
`active_connection` while a P0/P1 is live; non-USER rows show
`automation_paused` while automation is paused (operation-scheduling.sh:214-219).

### 5.4 Active-connection health (active-proxy-probe.sh:1-60)

SOCKS endpoint from the running config (`inbounds[] protocol socks`, exactly
one, noauth, numeric local address; 0.0.0.0→127.0.0.1). curl `-q --noproxy ''
--proxy socks5h://… --proto =https --connect-timeout 3 --max-time 6
--max-filesize 4096` to `https://cp.cloudflare.com/generate_204` then
`https://www.gstatic.com/generate_204`; healthy iff HTTP 204; both fail →
unhealthy (`PROXY_HTTPS_FAILED`); setup issues → unknown (exit 2) with codes
CONFIG_UNAVAILABLE, CURL_UNAVAILABLE, SOCKS_ENDPOINT_UNSUPPORTED,
LOCAL_ADDRESS_UNAVAILABLE, SOCKS_ADDRESS_NOT_LOCAL. Helper timeout 25 s.
Sample accepted only if the runtime context is unchanged before/after
(active-proxy-health.sh:18-49).

Health step (active-proxy-health.sh:110-123): config disabled → status
`disabled` "Автовыбор выключен", count 0; unhealthy → count = previous+1 if
previous `lastProxyContext` equals the context else 1, status
`waiting-threshold`, reason "Ожидание порога ошибок: n из T" or, at threshold,
"Порог отказов достигнут; ожидается проверка резервных серверов"; healthy →
`healthy` "Проверка трафика через активный VPN успешна", count 0; unknown →
`paused` "Не удалось проверить трафик через активный VPN". Threshold =
config `failureThreshold` if 1..10 else 3. Enabled && count ≥ threshold →
submit `servers:failover` (P1, source AUTO_SWITCH).

### 5.5 Auto-switch daemon & failover

Daemon `bin/broray-server-auto-switch` (no args), loop every **15 s**
(:47,105-111): `broray_auto_enqueue_due continue` (active-proxy-health.sh:224-302),
`queue-recover`, dispatch next stage. Enqueue rules:
* automation paused → nothing.
* config file must exist & be an object, else rc 74 (logged "Цикл очереди
  завершился ошибкой").
* active server + identifiable running Xray → enqueue `servers:active-health`
  (P0, AUTO_SWITCH) unless state has a same-config, same-context sample younger
  than 15 s, or a bound failover is pending (independent of `enabled`).
* `qualityRefreshEnabled` → enqueue `servers:quality all SERVER_CHECK_AUTO` (P3)
  unless `qualityRefresh.intervalMinutes == interval` and `nextCheckEpoch > now`
  and status ∈ success|scheduled|partial|error. (A manual single/all check
  also sets these, postponing the schedule.)

Failover (`broray_auto_failover_step`, active-proxy-health.sh:306-514), stages:
* **verify**: requires enabled, same config hash/context, unhealthy, count ≥
  threshold; policy: cooldown = cooldownMinutes (1..120 else 10),
  attemptGuard = last attempt <60 s ago, cooldown active = last switch <
  cooldown·60 s ago → silently finish. Fresh active sample; healthy → status
  `recovered`. Else candidate list = snapshot of all servers minus active; for
  rule `preferred` the preferred id first. Empty → `no-candidates`.
* **probe** (one candidate per stage): `broray_server_check <id> auto-switch <sha>`;
  candidate if success and rank ≥ minimumRank (excellent 4, good 3, acceptable 2,
  poor 1; minimumRating default acceptable=2). Decision when all probed, or
  immediately when rule=preferred and the preferred server qualified:
  preferred if qualified, else sort by `lowest-ping` → [ping, −rank, jitter, id],
  otherwise [−rank, ping, jitter, id] (null ping/jitter = 999999). None →
  `no-candidates`. Probing stops if the newest active sample (≤120 s) is healthy.
* **preparing**: generate + `xray -test` candidate config (30 s helper),
  re-measure active; healthy → `recovered`; else `prepared`.
* **activate** (protected): verify hashes of runtime config, settings.json,
  xray binary, candidate file; `broray_xray_apply_prepared_server`; confirm
  running/configValid/socks/config hash; else roll back to the exact previous
  runtime → `rolled-back`. Publish completion (§3.5).

`deactivate` sets `enabled=false` (manual VPN-off disables auto-switch).

### 5.6 Summary connectionState and health (server-service.sh:355-495)

* connection evidence = `broray_active_proxy_cached` (active-proxy-health.sh:53-72):
  newest `activeHealth` from the active quality file or the state file with
  method current-socks-https, same server, same current runtime context, age
  ≤120 s, status healthy|unhealthy; else unknown.
* connected: active present ∧ xray running ∧ SOCKS listening ∧ evidence
  healthy. disabled: no active server. degraded: xray+SOCKS up but evidence
  not healthy/current. error: otherwise.
* reasons (in order): ACTIVE_SERVER_MISSING, XRAY_NOT_OPERATIONAL,
  ACTIVE_PROXY_HEALTH_UNAVAILABLE ("Текущий VPN ещё не проверен HTTPS-запросом.
  Нажмите «Проверить» у активного сервера."), CONNECTION_DOWN,
  CONNECTION_STATUS_STALE, PROXY0_MISSING, PROXY0_NOT_READY, PROXY0_MISMATCH,
  PROXY0_STATUS_STALE, QUALITY_INCOMPLETE, QUALITY_STALE, QUALITY_EXPIRED
  (exact texts server-service.sh:445-456).
* severity: error if active present and Xray/SOCKS not operational; warning
  if not (operational ∧ consistent) or any quality unknown/stale/expired; else ok.
  operational/consistent true only when connected and keenetic healthy+fresh
  (consistent = keenetic.matchesExpected). freshness = worst(connection, keenetic).
* UI: live text "Xray и SOCKS работают" / "Xray запущен, SOCKS не подтверждён" /
  "Xray остановлен"; badge labels connected "Подключено", degraded
  "Подключение не подтверждено", error "Ошибка", else "Нет подключения";
  badge colour by `health.severity` (servers.js:203-232).

### 5.7 Runtime context (active-proxy-health.sh:4-16)

sha256 of `"<activeId>\n<pid>:<start ticks>\n<config path>\n<sha256sum line of config>\n"`;
fails if no active id, id ≠ requested, no xray pid, or config missing.

### 5.8 Public state adjustments (lib/auto-switch-status.sh)

If `backgroundOperationId` is an `op-*` job in
`/opt/var/lib/broray/operations/<id>/state.json` of kind background,
operation `auto-switch`, not running and terminal: add
`backgroundOperationState`; status checking-candidates/switching → cancelled
(aborted) or error with reason "Автоматическое задание отменено|прервано.
Сохранены результаты завершённых проверок."; `qualityRefresh` running
without requestId → error. If `qualityRefresh.requestId` exists: look up the
receipt; set `qualityRefresh.queueState` (queued|running|completed|cancelled|failed|unknown);
running but queue not queued/running → status "error", runStartedAt null,
lastError: cancelled "Проверка отменена. Сохранены результаты завершённых
измерений.", failed "Проверка прервана. …", completed "Задание завершено, но
итог проверки не подтверждён.", else "Продолжение проверки не подтверждено. …".

### 5.9 Canonical import key (server-subscription-service.sh:66-102)

`sha256( jq -cS <object> + "\n" )` of:
`{protocol,address(lowercased),port,network(//transport.type),security(//none),uuid,encryption,flow,alterId(//0),password,method,auth,serverName,fingerprint,alpn,allowInsecure,publicKey,shortId,spiderX,pinnedPeerCertSha256,host,path,serviceName,mode,headerType(//none),header,extra,ports,obfs,obfsPassword,finalMask,echConfigList,verifyPeerCertByName,mldsa65Verify,kcp,kcpSeed,kcpLegacy}`
with exact fallbacks per line 70-99 (`serverName`/`fingerprint` from reality
when security=reality, else tls//reality//sni; default fingerprint "chrome").
Name is excluded. Byte-exact jq `-cS` serialization (sorted keys, compact) is
required to match keys already stored in subscription records (`source.importKey`).

### 5.10 Ordering & ids

List order = shell glob `/opt/broray/servers/*.json` (sorted; UNCONFIRMED
collation, assume byte order). Files starting with `.` are skipped.
Ids: §2.4.

### 5.11 Client-side import limits (servers-import-preflight.js:425-474)

Total ≤262144 chars ("Общий объём конфигураций слишком большой."), ≤50
non-empty lines ("За один раз можно проверить не более 50 серверов."), line
≤16384 ("Строка конфигурации слишком длинная."), scheme regex
`/^(vless|vmess|trojan|hysteria2|hy2|ss):\/\//i` ("Неподдерживаемая схема URI.").
Preview requests sequential; import sequential with 160 ms delay.

---------------------------------------------------------------------------

## 6. Dynamically built URLs

* `"/api/servers/details.cgi?id=" + encodeURIComponent(server.id)` (servers.js:573).
* Queue nonce storage key `"broray.queue.v1:" + url + ":" + id` (common.js:91, 133, 141) — URL is the literal `/api/servers/check.cgi`, id = server id or `"all"`.
* No other concatenated endpoint; all other URLs are constants.

---------------------------------------------------------------------------

## 7. DOM / wording dependencies

* All server cards, badges and the import preview are built client-side; the
  backend produces no HTML.
* servers-completion.js normalizes badge text in `#servers-list`
  (selectors `.status-badge, .server-status-badge, .server-card-status,
  [class*="server-status-"], [data-server-status]`) by Russian substrings
  ("не провер", "нет данных", "проверяется", "недоступ", "ошиб", "таймаут",
  "отказ", "доступен", "доступна", "успеш") (servers-completion.js:233-273);
  any button whose text contains "провер" inside `.server-card` flips its
  badge to "Проверяется" (servers-completion.js:713-761). It wraps
  `window.fetch` to observe responses whose URL contains
  `/api/servers/summary.cgi` (servers-completion.js:662-711) — the summary
  must stay a JSON body at that exact path.
* Recursive timestamp scan of the summary for keys `lastCheckedAt|checkedAt|…`
  (§2.1) — adding new keys with those names anywhere in the summary changes the
  "Последняя проверка" label.
* `#refresh-servers` is clicked programmatically by preflight, check-all,
  auto-switch save and quality-refresh completion; text forced to
  "Обновить данные".
* Preview success requires `data.canonical` 64 lowercase hex; `data.duplicate`
  text is displayed verbatim; error text displayed = `error.message`.
* Auto-switch: raw `state.lastError` code strings (e.g.
  "FAILOVER_ACTIVATION_ROLLED_BACK") and `state.lastReason` texts are shown
  verbatim; `state.status` values must match §2.9 titles.
* Quality refresh: `state.lastResult` shown verbatim ("Проверено: c из t");
  `service.errorCode === "SERVICE_RECOVERY_REBOOT_REQUIRED"` triggers a fixed text.
* `servers-selection-rule` is written by both servers.js (from
  `autoSwitch.selectionRuleLabel`) and servers-auto-switch.js (`ruleTitle`,
  "Ручной выбор" when disabled); last writer wins.
* Auto-switch `<select id=auto-switch-rule>` only has best-quality /
  lowest-ping / preferred; a stored `"manual"` (deactivate-created config)
  leaves it blank and the next save fails INVALID_SELECTION_RULE.

---------------------------------------------------------------------------

## 8. Dead code / unconfirmed

* Dead JS: `servers.js importServer` (preflight's capturing `submit`/`click`
  listeners with `stopImmediatePropagation` run first,
  servers-import-preflight.js:1432-1488); `servers-check-all.js checkNext`,
  `extractServerIds`, `BATCH_COMPLETE_URL`.
* `quality-batch-complete.cgi`, direct (non-queue) `check.cgi` path: unused by JS; keep for CLI/compat.
* UNCONFIRMED: exact stdout of Entware rc.func for `S24broray stop/restart`
  (affects whether deactivate data is the JSON object or `{message}`); helper
  stderr propagation through `bin/broray-ops-supervisor` into `details`;
  glob collation; `/api/operations/status.cgi` full schema (operations area —
  coordinator emits `{ok,complete,capturedAt,operations,queue,errors,automationPaused,globalFence}`,
  operation-coordinator.sh:1199).
* Legacy job/once mode of the auto-switch binary (statuses manual-off,
  cooldown, attempt-guard, checking-candidates, partial…) is reachable only via
  `S27broray-auto-switch run-once|dry-run`.
