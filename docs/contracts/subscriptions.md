# SUBSCRIPTIONS area — backend contract (BROray 3.x → 3.5.0 Go daemon)

Source: `/home/claude/broadmin/broray` @ f505928. All paths below are relative to
`runtime/app/` unless stated otherwise. `svc` = `lib/subscription-service.sh`,
`ssvc` = `lib/server-subscription-service.sh`, `sjs` = `web-new/assets/js/subscriptions.js`,
`api/` = `web-new/api/`. On the router `runtime/app` is installed as `/opt/broray`.

Pages/assets: `web-new/subscriptions.html`, `sjs`, `web-new/assets/js/subscription-provider.js` (pure renderer, no network, `subscription-provider.js:1`).

---

## 0. Common rules for this area

### 0.1 Envelope (all `api/subscriptions/*.cgi` except the queue branch of refresh.cgi)
- Success: `{"success":true,"data":<service JSON>,"error":null}` (`api/auth-common.sh:96-111`), HTTP 200, or **202** when an async worker was handed off (`api/subscriptions/common.sh:99`, flag `BRORAY_SUB_ASYNC_ACCEPTED` set in `lib/subscription-web-job.sh:41`).
- Error: `{"success":false,"data":null,"error":{"code":C,"message":M,"details":null}}` (`api/auth-common.sh:25-57`). `details` is always `null` in this area (no 4th arg is passed except METHOD_NOT_ALLOWED, whose details = `"Получен метод X."`).
- Headers on every JSON reply: `Content-Type: application/json; charset=utf-8`, `Cache-Control: no-store, no-cache, must-revalidate`, `Pragma: no-cache`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` (`api/auth-common.sh:7-14`).

### 0.2 Auth / method
- Method check first: wrong method → `405 METHOD_NOT_ALLOWED` "Для этой операции требуется метод POST|GET." (`api/auth-common.sh:59-70`).
- Session cookie `BRORAY_SESSION` validated via `lib/web-auth.sh` → `401 AUTH_REQUIRED` "Требуется авторизация." (`api/auth-common.sh:72-95`); missing lib → `500 SESSION_BACKEND_UNAVAILABLE`.
- **No CSRF/custom-header/Origin/Content-Type check** on create/update/delete/details/list/servers/summary and on the non-queue refresh branch (only cookie). The queue branch of refresh.cgi and `/api/operations/*` POSTs do check `X-BROray-Request: operations` + Origin (`api/operations/common.sh:12-27`).
- `sjs:87-90`: HTTP 401 → `BROrayUI.redirectToLogin()` (location.replace("/")).

### 0.3 Global operation lock (mutating CGIs)
`broray_subscriptions_api_lock <action>` (`api/subscriptions/common.sh:9-24`) calls
`broray_job_begin system "subscriptions:<action>" subscriptions USER cooperative`:
- rc 2 → `409 OPERATION_BUSY` "Другая конфликтующая операция BROray уже выполняется."
- other rc≠0 → `500 GLOBAL_LOCK_FAILED` "Не удалось установить общую блокировку BROray."
- Order matters: lock is taken **before** the body is read and before the id is validated (create.cgi:5 vs :11; update.cgi:5 vs :12; delete.cgi:5-6; refresh.cgi:10-11). A busy system returns 409 even for invalid input.
- Actions: `create`, `update`, `delete`, `refresh`. On success `broray_api_success` finishes the job `completed`; on error `broray_api_error` finishes it `failed` (`api/auth-common.sh:16-23,31,99`). Coordinator semantics belong to the operations area.

### 0.4 Body reading (create, update)
`broray_subscriptions_api_read_body_to_file` (`api/subscriptions/common.sh:26-43`, reader `lib/web-request-body.sh:5-34`), limit **65536** bytes:

| condition | HTTP | code | message |
|---|---|---|---|
| CONTENT_LENGTH empty/non-digit | 411 | CONTENT_LENGTH_REQUIRED | Для запроса требуется корректный Content-Length. |
| length 0 | 400 | REQUEST_BODY_REQUIRED | Тело запроса отсутствует. |
| > 65536 | 413 | REQUEST_TOO_LARGE | Тело запроса слишком большое. |
| dd failure | 400 | REQUEST_BODY_READ_FAILED | Не удалось прочитать тело запроса. |
| short read | 400 | REQUEST_BODY_INCOMPLETE | Тело запроса получено не полностью. |
| not a JSON object (`jq -e 'type=="object"'`) | 400 | REQUEST_JSON_INVALID | Тело запроса должно быть корректным JSON-объектом. |

Content-Type is not checked. Unknown body keys are ignored.

### 0.5 Query parameter `id`
`broray_subscriptions_api_query id` (`api/subscriptions/common.sh:45-51`): splits `QUERY_STRING` on `&`, first pair whose name is exactly `id`, value taken **raw (no URL-decoding)**. Ids are `sub-<16 hex>` so `encodeURIComponent` is a no-op in practice. Missing/empty → service error `INVALID_SUBSCRIPTION_ID` "Не указан идентификатор подписки."; chars outside `[a-zA-Z0-9._-]` → `INVALID_SUBSCRIPTION_ID` "Идентификатор подписки содержит недопустимые символы." (`svc:87-105`).

### 0.6 Service error → HTTP mapping
Service functions print `BRORAY_ERROR:<CODE>:<message>` on stderr; the CGI takes the **last** such line (`api/subscriptions/common.sh:115-119`). If none: code `SUBSCRIPTION_OPERATION_FAILED`, message "Операция с подпиской завершилась ошибкой.". Exit-code overrides (`:106-127`): rc 130 → `OPERATION_CANCELLED` "Обновление подписки остановлено."; rc 75 → `OPERATION_UNRESOLVED` "Завершение вспомогательных процессов ещё не подтверждено." (overrides any message printed, e.g. the launcher's own OPERATION_UNRESOLVED text). Successful exit with non-JSON stdout → `500 INVALID_SERVICE_RESPONSE` "Модуль подписок вернул некорректный ответ." (`:89-96`).

Status table (`api/subscriptions/common.sh:53-82`):
- 404: `SUBSCRIPTION_NOT_FOUND`
- 409: `UPDATE_ALREADY_RUNNING`, `ACTIVE_SERVER_CONFLICT`, `SERVER_SYNC_BUSY`, `SERVER_ID_CONFLICT`, `OPERATION_CANCELLED`
- 504: `DOWNLOAD_TIMEOUT`, `PARSE_TIMEOUT`
- 503: `OPERATION_UNRESOLVED`
- 502: `HTTP_ERROR`, `DOWNLOAD_SECURITY`
- 413: `CONTENT_TOO_LARGE`
- 500: `INTERNAL_ERROR`, `PERSISTENCE_ERROR`, `SERVER_SYNC_ERROR`, `SERVER_SOURCE_REMOVE_FAILED`, `SERVER_SOURCE_STATE_FAILED`
- **400 for everything else**, including `INVALID_SUBSCRIPTION_ID`, `INVALID_NAME`, `INVALID_URL`, `INVALID_INTERVAL`, `INVALID_REQUEST`, `INVALID_USER_AGENT`, `INVALID_DEVICE_INFO_SETTING`, `CLIENT_ID_ERROR`, `SUBSCRIPTION_OPERATION_FAILED`, `SERVER_SYNC_RECOVERY_REQUIRED`, `INVALID_ENABLED_STATE`, `SERVER_LIST_FAILED` (these last ones are surprising but are the 3.x behaviour).

### 0.7 How sjs consumes responses (`sjs:54-110`)
- 30 s AbortController; timeout → message "Ответ роутера не получен за 30 секунд. Операция могла запуститься — проверьте список подписок перед повтором."; network/JSON failure → "Сервер вернул некорректный ответ." (got a response) or "Не удалось связаться с роутером. Проверьте состояние подписки перед повтором.".
- Error if `!response.ok || payload.success===false || payload.ok===false`; message = `payload.error.message` or "Операция завершилась ошибкой."; `error.code` = `payload.error.code` or `payload.errorCode` or `"HTTP_ERROR"`. Code `CANCEL_NOT_SUPPORTED` → message replaced by "Сейчас применяются изменения. Дождитесь завершения этого этапа." (`sjs:100-102`).
- Returns `payload.data` if present, else the payload itself.
- Backend error **messages are shown verbatim** in toasts / form status (`sjs:407,453-454,470,488,502,525,545,562`).

---

## 1. Endpoint table

| # | URL | Method | Lock | Async | JS caller |
|---|---|---|---|---|---|
| 1 | `/api/subscriptions/list.cgi` | GET | no | – | `loadAll` sjs:345 |
| 2 | `/api/subscriptions/summary.cgi` | GET | no | – | `loadAll` sjs:345 (also home snapshot) |
| 3 | `/api/subscriptions/details.cgi?id=` | GET | no | – | `openEdit` sjs:392 |
| 4 | `/api/subscriptions/servers.cgi?id=` | GET | no | – | `toggleServers` sjs:521 |
| 5 | `/api/subscriptions/create.cgi` | POST | yes | 202 + worker when `updateImmediately` | `saveForm` sjs:447 |
| 6 | `/api/subscriptions/update.cgi?id=` | POST | yes | sync | `saveForm` sjs:441, `patchSubscription` sjs:463 |
| 7 | `/api/subscriptions/delete.cgi?id=` | POST | yes | sync | `removeSubscription` sjs:554 |
| 8a | `/api/subscriptions/refresh.cgi` + header `X-BROray-Queue: 1` | POST | queue | queue request | `BROrayUI.followQueued` (common.js:65-183) from `refreshSubscription` sjs:477 |
| 8b | `/api/subscriptions/refresh.cgi?id=` (no queue header) | POST | yes | 202 + worker | **not used by the 3.x JS** (legacy/compat) |

Cross-area endpoints used by this page (contracts owned elsewhere, summarised in §1.9): `GET /api/session.cgi` (sjs:635), `POST /api/operations/cancel.cgi` (sjs:499), `GET|POST /api/operations/status.cgi` (common.js:105,153).

---

### 1.1 GET `/api/subscriptions/list.cgi`
`api/subscriptions/list.cgi:1-5` → `broray_subscription_list` (`svc:1284-1300`).
- Params: none (query ignored).
- Reads every `/opt/broray/config/subscriptions/*.json`; files failing `broray_subscription_validate_file` (`svc:121-152`) are **silently skipped**. Each valid file → public projection with `includeUrl=false` (§2.3). Result array sorted `sort_by(.createdAt) | reverse` (string sort; newest first given same TZ offset).
- No writes, no recovery (stale "running" records are only overlaid, §2.4).
- Response `data`: array of public subscription objects. Example (constructed from the code; keys sorted as stored by `jq -S`, appended keys last):
```json
[{
  "autoUpdateEnabled": true,
  "backgroundOperationId": "op-1730541791-5f2c9a",
  "createdAt": "2026-11-01T10:12:03+0300",
  "enabled": true,
  "httpUserAgent": "",
  "id": "sub-3f9a0c1d2e4b5a67",
  "lastError": null,
  "lastUpdateResult": {
    "received": 12, "parsed": 12, "accepted": 11, "rejected": 1,
    "added": 2, "updated": 1, "unchanged": 8, "removed": 0, "retained": 3,
    "catalogTotal": 14, "deletionPolicy": "retain-unmatched",
    "warnings": ["Узел 7: Ошибка: неподдерживаемый формат конфигурации",
                 "Сохранено прежних серверов: 3. Ответ разобран не полностью; отсутствие этих серверов не подтверждено. Их параметры могут быть устаревшими. Удаление отсутствующих серверов возможно после обновления без отклонённых узлов."],
    "durationMs": 4000, "trigger": "manual", "errorCode": null, "activeServerImpact": "none"
  },
  "lastUpdateStatus": "partial",
  "lastUpdatedAt": "2026-11-02T09:41:20+0300",
  "name": "Мой провайдер",
  "nextUpdateAt": "2026-11-02T15:41:20+0300",
  "providerMetadata": {
    "schemaVersion": 1, "title": "Premium VPN",
    "usage": {"upload": 1048576, "download": 52428800, "total": 107374182400, "expire": 1767225600},
    "suggestedUpdateMinutes": 720, "supportUrl": "https://t.me/support",
    "invalidFields": [], "ignoredDirectives": ["routing"],
    "updatedAt": "2026-11-02T09:41:20+0300"
  },
  "schemaVersion": 1,
  "sendDeviceInfo": false,
  "serversReceived": 14,
  "updateIntervalMinutes": 360,
  "updatedAt": "2026-11-02T09:41:20+0300",
  "updateOperation": {"id": "op-1730541791-5f2c9a", "running": false, "phase": "working", "cancelRequested": false, "canCancel": false},
  "parseProgress": {},
  "displayUrl": "https://example.com/sub/***?***",
  "serversCount": 14
}]
```
- JS reads (renderSubscriptions sjs:263-332, renderResult sjs:191-211, renderWarnings sjs:174-189, provider render `subscription-provider.js:12-28`, removeSubscription sjs:530-541, loadAll sjs:349-365):
  `id, name, displayUrl, lastUpdateStatus, enabled, autoUpdateEnabled, serversCount, updateIntervalMinutes, lastUpdatedAt, nextUpdateAt, lastError, updateOperation.{id,phase,canCancel,cancelRequested}, parseProgress.{processed,total}, lastUpdateResult.{errorCode,durationMs,received,accepted,added,updated,removed,rejected,retained,warnings[] (string or object with message/details)}, providerMetadata.*`.
- Polling: if any item has `lastUpdateStatus==="running"`, `loadAll(true)` again after **3000 ms** (sjs:357-365); on failure retry after **5000 ms** with panel "Не удалось загрузить подписки" + error.message (sjs:366-370). list and summary are fetched in parallel with `Promise.allSettled`; any rejection fails the whole load (sjs:345-348).
- Errors: only generic (405/401/500 INVALID_SERVICE_RESPONSE).

### 1.2 GET `/api/subscriptions/summary.cgi`
`api/subscriptions/summary.cgi` → `broray_subscription_summary` (`svc:1957-2123`). Also used by `bin/broray-home-snapshot:36-39` (home page; `home.js:169-185` reads `lastUpdateStatus, lastUpdatedAt, lastWarnings[0], enabled, total, serversReceived, health`).
- Response `data`:
```json
{"total":2,"enabled":2,"serversReceived":20,"lastUpdateStatus":"partial",
 "lastUpdatedAt":"2026-11-02T09:41:20+0300","lastWarnings":["…"],
 "partialCount":1,"errorCount":0,"runningCount":0,"staleCount":0,"neverUpdatedCount":0,
 "autoUpdateEnabled":true,
 "health":{"schemaVersion":1,"module":"subscriptions","availability":"available","severity":"warning",
   "operational":true,"consistent":false,"actionRequired":true,
   "freshness":{"state":"fresh","checkedAt":"2026-11-02T09:41:20+0300"},
   "reasons":[{"code":"SUBSCRIPTION_UPDATE_PARTIAL","message":"Одна или несколько подписок обновлены частично.","details":null}],
   "facts":{"total":2,"enabled":2,"partial":1,"errors":0,"running":0,"stale":0,"neverUpdated":0,"lastUpdateStatus":"partial"},
   "lastOperation":null}}
```
- sjs reads `total, enabled, serversReceived, lastUpdateStatus, lastUpdatedAt` (renderSummary sjs:164-172; null → "—").
- Algorithm (§3.6).

### 1.3 GET `/api/subscriptions/details.cgi?id=<id>`
`details.cgi` → `broray_subscription_get` (`svc:1302-1318`).
- Errors: `400 INVALID_SUBSCRIPTION_ID`; missing file → `404 SUBSCRIPTION_NOT_FOUND` "Подписка не найдена.".
- Response `data`: public object (§2.3) **with `url` (full, unmasked)**. `clientHwid`, `lastUpdatedEpoch`, `nextUpdateEpoch` removed. Note: details does **not** run `validate_file`; an invalid file is still returned.
- JS reads (openEdit sjs:388-409): `name, url, httpUserAgent, updateIntervalMinutes (fallback 360), enabled===true, autoUpdateEnabled===true, sendDeviceInfo===true`.

### 1.4 GET `/api/subscriptions/servers.cgi?id=<id>`
`servers.cgi` → `broray_subscription_servers` (`svc:1940-1955`) → `broray_server_subscription_list` (`ssvc:271-340`).
- Errors: `400 INVALID_SUBSCRIPTION_ID`; no record → `404 SUBSCRIPTION_NOT_FOUND`; jq failure → `SERVER_LIST_FAILED` (HTTP 400 via default mapping).
- Response `data`: `{"items":[…],"total":N}`. Items: first all files in `/opt/broray/servers/*.json` (glob = filename order) with `source.type=="subscription" && source.subscriptionId==id`, then `/opt/broray/config/disabled-subscription-servers/<id>/*.json`. Each item (after `broray_server_mask_json`, `lib/server-service.sh:126`):
  `{id, name: (.name // .id), protocol, address, port, network: (.network // "unknown"), security: (.security // "none"), source: <full source object>, enabled: <true for live dir, false for disabled dir>, active: (.id == first line of /opt/broray/config/active-server)}`.
  `source` = `{type:"subscription",subscriptionId,importKey,nodeIndex,enabled,updatedAt}` (its `enabled` is the staging-time value, not refreshed on toggle).
- JS (renderNodes sjs:213-253) reads `items[].{name,protocol,address,port,network,active,enabled}`, `total`. On error caches `{items:[],total:0}` and toasts.

### 1.5 POST `/api/subscriptions/create.cgi`
`create.cgi:1-14`: lock `create` → body (§0.4) → `BRORAY_SUB_WEB_ASYNC=true` → `broray_subscription_create body` (`svc:1340-1480`).

Body (JS sends all fields, sjs:424-435):

| field | type | default | rule / error |
|---|---|---|---|
| name | string | – | `.name // empty`; 1..128 chars (`${#}` shell count — **UNCONFIRMED** whether BusyBox counts bytes or characters; `validate_file` later enforces ≤128 codepoints) → `INVALID_NAME` "Название подписки должно содержать от 1 до 128 символов." Not trimmed server-side (JS trims). |
| url | string | – | non-empty, ≤4096 → else `INVALID_URL` "URL подписки не указан или слишком длинный."; then syntax rules §3.1 (no DNS at create). |
| httpUserAgent | string | "" | ≤128 chars, each codepoint 32..126 → `INVALID_USER_AGENT` "User-Agent должен содержать не более 128 печатных ASCII-символов." (`svc:1329-1333`) |
| sendDeviceInfo | boolean | false | non-boolean present → `INVALID_DEVICE_INFO_SETTING` "Разрешение отправки сведений об устройстве должно быть логическим." Stored as `.sendDeviceInfo == true`. |
| enabled | bool | true | `jq -r` text must be `true`/`false` (so JSON strings "true"/"false" are also accepted) → else `INVALID_REQUEST` "Поля enabled, autoUpdateEnabled и updateImmediately должны быть логическими." |
| autoUpdateEnabled | bool | true | same |
| updateImmediately | bool | **false** | same (JS sends `true` by default, checkbox `subscription-immediate`) |
| updateIntervalMinutes | integer | 360 | `.updateIntervalMinutes // 360` (so `null`/`false` → 360); text must be digits only → `INVALID_INTERVAL` "Интервал обновления должен быть целым числом минут."; range 5..10080 → `INVALID_INTERVAL` "Интервал обновления должен быть от 5 до 10080 минут." |

Validation order (`svc:1344-1401`): body object → UA → sendDeviceInfo → name → url length → url syntax → booleans → interval digits → interval range.

Side effects: new id `sub-` + 16 hex of sha256(time,pid,urandom) (`svc:185-195`); `clientHwid` = `broray-` + 32 hex (`svc:197-207`); record written to `/opt/broray/config/subscriptions/<id>.json` (§2.1) via `broray-ops-guard --replace-file`, mode 600 (`svc:154-183`); log line `subscription=<id> action=create url=<masked>` in `/opt/broray/logs/subscriptions.log` (`svc:1467-1468`).

Responses:
- `updateImmediately=false` → **200**, `data` = details projection (with `url`) of the new record.
- `updateImmediately=true` → `broray_subscription_launch_update id initial` (`lib/subscription-web-job.sh:4-43`) → **202**, `data` = `{"accepted":true,"id":"sub-…","backgroundOperationId":"op-…"}`. The record was first set `lastUpdateStatus:"running"`, `backgroundOperationId`, `lastError:null` (`subscription-web-job.sh:18-20`), op phase `waiting`, then a worker `lib/subscription-web-worker.sh <id> initial <nonce>` was handed the operation (`:27-28`). Handoff failure → if cancel already requested: rc 130 → `409 OPERATION_CANCELLED`; else rc 75 → `503 OPERATION_UNRESOLVED` (record stays saved and "running").
- JS: `result.accepted ? "Подписка сохранена. Обновление запущено; его можно остановить в карточке." : "Подписка добавлена."` (sjs:447-448); then `resetForm()` and `loadAll(false)`. Errors → form message + toast, `loadAll(true)` (sjs:452-455).

### 1.6 POST `/api/subscriptions/update.cgi?id=<id>` (settings / toggles)
`update.cgi:1-15` → `broray_subscription_update_settings id body` (`svc:1482-1614`). PATCH semantics: each field is taken from body if the key is present, else from the stored record.
- JS full edit sends `{name,url,httpUserAgent,sendDeviceInfo,updateIntervalMinutes,enabled,autoUpdateEnabled}` (sjs:424-444); toggles send `{enabled:!item.enabled}` or `{autoUpdateEnabled:!item.autoUpdateEnabled}` (sjs:599-614).
- Validation: id → body (UA, sendDeviceInfo as in create) → record exists (`404 SUBSCRIPTION_NOT_FOUND`) → name 1..128 → url `parse_url` syntax only (length ≤4096 enforced only later by `validate_file` → `500 PERSISTENCE_ERROR` "Данные подписки не прошли проверку.") → enabled/auto must be `true|false` text → `INVALID_REQUEST` "Поля enabled и autoUpdateEnabled должны быть логическими." → interval digits/range (explicit `null` → INVALID_INTERVAL, unlike create).
- If `enabled` changes: `broray_server_subscription_set_enabled id <bool>` (`ssvc:342-423`) **before** the record is saved — see §3.9. Its errors propagate: `ACTIVE_SERVER_CONFLICT` (409) "Нельзя выключить подписку: её сервер сейчас активен. Сначала отключите или замените активный сервер.", `SERVER_ID_CONFLICT` (409) "Не удалось включить подписку: идентификатор сервера уже занят.", `SERVER_SOURCE_STATE_FAILED` (500), `SERVER_SYNC_BUSY` (409) "Прежняя синхронизация серверов требует восстановления.", `SERVER_SYNC_RECOVERY_REQUIRED` (400) "Прежнее восстановление серверов не завершено. Резервная копия сохранена.".
- Record update (`svc:1575-1597`): if url changed → `providerMetadata` deleted; set name, httpUserAgent, sendDeviceInfo, url, enabled, autoUpdateEnabled, updateIntervalMinutes; **nextUpdateAt/Epoch recomputed from now** (any save, even a rename, resets the schedule; null when not enabled&&auto); updatedAt=now. `lastUpdateStatus` etc. untouched. Log `action=update-settings …`.
- Response 200, `data` = details projection (with `url`). JS toasts "Настройки подписки сохранены." / "Подписка отключена." / "Подписка включена." / "Автообновление отключено." / "Автообновление включено." and reloads.
- Note: no check that an update is "running" (the global lock is the only guard). Changing the record while a queued refresh is in fetch/parse invalidates that refresh (§4.2).

### 1.7 POST `/api/subscriptions/delete.cgi?id=<id>`
`delete.cgi:1-7` (body ignored; JS sends `{}`) → `broray_subscription_delete` (`svc:1891-1938`).
- Errors: `400 INVALID_SUBSCRIPTION_ID`, `404 SUBSCRIPTION_NOT_FOUND`, `409 UPDATE_ALREADY_RUNNING` "Состояние прежней операции подписки требует восстановления." if `/opt/broray/run/subscriptions/<id>.lock` exists (legacy marker, `svc:1114-1128`), then `broray_server_subscription_remove` errors: `409 ACTIVE_SERVER_CONFLICT` "Нельзя удалить подписку: её сервер сейчас активен. Сначала отключите или замените активный сервер.", `500 SERVER_SOURCE_REMOVE_FAILED`, `409 SERVER_SYNC_BUSY`, `400 SERVER_SYNC_RECOVERY_REQUIRED`; record removal failure → `500 PERSISTENCE_ERROR` "Серверы удалены, но запись подписки удалить не удалось.".
- Side effects (`ssvc:425-480`): delete every live server of this subscription and every file in `config/disabled-subscription-servers/<id>/` plus `/opt/broray/run/server-quality/<serverId>.json`; `rmdir` the disabled dir; `rm -f config/subscriptions/<id>.json` (**permanent; no trash**, see §2.6); log `action=delete`.
- Response 200 `data`: `{"subscriptionId":"sub-…","removed":<int>,"activeServerImpact":"none"}`. JS ignores data; toasts "Подписка удалена.", drops server cache, `loadAll(false)`.
- JS confirm dialog text uses `item.name` and `item.serversCount` (sjs:531-543).

### 1.8a POST `/api/subscriptions/refresh.cgi` — queue branch (what the JS uses)
Selected when request header `X-BROray-Queue` == `1` (`refresh.cgi:2-6`) → `broray_operations_api POST subscription-refresh` (`api/operations/common.sh:5-117`). **Response is NOT the `{success,data,error}` envelope** for coordinator replies.
- Request (built by `common.js:65-77,117`): headers `Content-Type: application/json`, `X-BROray-Request: operations`, `X-BROray-Origin: <location.origin>`, `X-BROray-Queue: 1`; body `{"id":"<subId>","nonce":"<32 lowercase hex>"}`; no query string.
- Checks in order (all produce the auth-common error envelope): method POST (405), session (401), non-empty `QUERY_STRING` → `400 INVALID_REQUEST` "Параметры запроса не поддерживаются."; `X-BROray-Request != operations` → `403 CSRF_REJECTED` "Обновите страницу и повторите действие."; origin module missing → `503 ORIGIN_VALIDATION_UNAVAILABLE`; origin check (`lib/operations-origin.sh:134-164`, operations area) → `403 ORIGIN_REJECTED` "Источник запроса не подтверждён."; Content-Type not exactly `application/json` or `application/json; charset=utf-8` → `415 INVALID_CONTENT_TYPE` "Требуется JSON."; CONTENT_LENGTH non-digit/≥9 digits → `400 INVALID_LENGTH`; >4096 → `413 REQUEST_TOO_LARGE` "Запрос превышает 4 КБ."; read failure → `400 INVALID_BODY` "Запрос получен не полностью."; not exactly one JSON object → `400 INVALID_BODY` "Требуется один JSON-объект."; keys must be exactly `["id","nonce"]`, nonce 32 chars `[0-9a-f]`, id 1..96 chars `[A-Za-z0-9._-]` not starting with `.` or `-` → `400 INVALID_REQUEST` "Некорректные параметры задания."; subscription lib load failure → `503 STATE_UNAVAILABLE` "Служба подписок недоступна."; `broray_subscription_path` invalid → `400 INVALID_REQUEST` "Некорректная подписка."; record missing → `404 SUBSCRIPTION_NOT_FOUND` "Подписка не найдена."; sha256 failure → `503 STATE_UNAVAILABLE` "Не удалось прочитать подписку." (`api/operations/common.sh:9-70`).
- Then `queue-submit subscriptions:refresh <id> USER <sha256 of record file> <nonce>` (`:71,100`; `lib/operation-scheduling.sh:128-164`).
- Success: **HTTP 202**, raw body `{"ok":true,"requestId":"q-<nonce or coalesced>","state":"queued|running|completed|failed|cancelled","priority":2,"coalesced":<bool>,"accepted":true}` (`operation-scheduling.sh:115-126`, `api/operations/common.sh:102-112`).
- Coordinator failure: body `{"ok":false,"errorCode":"<CODE>"}` (`lib/operation-coordinator.sh:32-36`); HTTP 409 for `OPERATION_BUSY|DOMAIN_OPERATION_BUSY|CANCEL_NOT_SUPPORTED|RECOVERY_BLOCKED`, 404 for `REQUEST_UNCONFIRMED`, else **503** (e.g. `QUEUE_FULL` at 64 queued, `REQUEST_MISMATCH` nonce reused with different input, `QUEUE_STATE_INVALID`). Unparseable coordinator output → `503 STATE_UNAVAILABLE` "Состояние операций временно недоступно." (envelope form).
- Idempotency/coalescing (`operation-scheduling.sh:138-161`): same nonce → returns its receipt (input hash must match); same `(action,target,source,context)` as a live request → reuses that `requestId` with `coalesced:true`; receipts capped at last 128.
- JS validation of the reply (`common.js:71-74`): must have `ok===true`, `requestId` matching `^q-[0-9a-f]{32}$`, `state` in the 5 states, else error "Ответ на запрос задания не подтверждён.".
- Polling/cancel model: §4.

### 1.8b POST `/api/subscriptions/refresh.cgi?id=<id>` — legacy direct branch
`refresh.cgi:7-13`: lock `refresh` → `broray_subscription_launch_update id manual` (same launcher as create). Success **202** `data` = `{"accepted":true,"id","backgroundOperationId"}`. Errors: 400 INVALID_SUBSCRIPTION_ID, 404 SUBSCRIPTION_NOT_FOUND, 409 OPERATION_BUSY, 409 OPERATION_CANCELLED, 503 OPERATION_UNRESOLVED. Not called by 3.x JS (common.js always sends `X-BROray-Queue: 1`), but reachable; keep for compatibility.

### 1.9 Cross-area endpoints used by this page (summary only)
- `GET /api/session.cgi` → page requires `ok===true`; shows `user || "admin"` in `#current-user`; any failure → redirect to `/` (sjs:630-646).
- `POST /api/operations/cancel.cgi` (sjs:493-506): headers `Accept`, `X-BROray-Request: operations`, `X-BROray-Origin`; body exactly `{"requestId":"q-…"}` (queued request) or `{"operationId":"op-…"}` (running legacy op) (`api/operations/common.sh:72-78`). Success 202 → toast "Ожидание отменено." / "Остановка запрошена. Дождитесь завершения операции."; errors toasted (CANCEL_NOT_SUPPORTED → 409, message rewritten by sjs); always `loadAll(true)` after.
- `POST /api/operations/status.cgi` body `{"nonce":…}` = queue-lookup → 200 receipt (same shape as submit, without `accepted`), or 404 `{"ok":false,"errorCode":"REQUEST_UNCONFIRMED"}` (`common.js:104-113`).
- `GET /api/operations/status.cgi` → `{ok,complete,capturedAt,operations,queue:[{requestId,priority,type,source,stage,state,operationId,reason}],errors,automationPaused,globalFence}` (`lib/operation-coordinator.sh:1199`, `lib/operation-public.jq:54-60`). JS requires `complete===true` and `Array.isArray(queue)` (`common.js:153-154`).

---

## 2. Data model and files

### 2.1 Subscription record `/opt/broray/config/subscriptions/<id>.json`
Directory `config/subscriptions` (mode 700, `lib/package-setup.sh:129,165`), file mode 600, written atomically via `bin/broray-ops-guard --replace-file` after `jq -S` (sorted keys) and `validate_file` (`svc:154-183`). Paths overridable by `BRORAY_SUB_DIR`, run dir `BRORAY_SUB_RUN=/opt/broray/run/subscriptions`, tmp `/opt/broray/tmp` (`svc:15-18`).

| key | type | written by | notes |
|---|---|---|---|
| schemaVersion | 1 | create | must equal 1 |
| id | string | create | `sub-[0-9a-f]{16}` (validate only: non-empty string) |
| name | string 1..128 | create/settings | |
| url | string 1..4096 | create/settings | secret; only in details/create/update replies |
| clientHwid | string | create / ensured lazily | `broray-` + 32 hex; valid = 10..64 chars `[a-zA-Z0-9=-]` (`svc:209-219`); never public; sent as `x-hwid` |
| httpUserAgent | string | create/settings | "" = default `BROray/<version>` |
| sendDeviceInfo | bool | create/settings | |
| enabled | bool | create/settings | |
| autoUpdateEnabled | bool | create/settings | |
| updateIntervalMinutes | int 5..10080 | create/settings | |
| lastUpdateStatus | `never|running|success|partial|error` | create/launch/update | |
| lastUpdatedAt / lastUpdatedEpoch | ISO string / int, or null | update end | epoch never public |
| nextUpdateAt / nextUpdateEpoch | ISO / int, or null | create/settings/update end | null unless enabled&&auto; epoch never public |
| lastError | string or null | update | |
| serversReceived | number ≥0 | update success | = catalog total after sync (incl. retained) |
| lastUpdateResult | object or null | update | §3.4 |
| createdAt, updatedAt | ISO string | | |
| backgroundOperationId | string | launch / update start | op id of last attempt; stays after completion |
| providerMetadata | object | update success | §3.3; deleted when url changes |

Timestamp format: `date '+%Y-%m-%dT%H:%M:%S%z'` → e.g. `2026-11-02T09:41:20+0300` (no colon in offset; router-local TZ) (`svc:50-60`). Coordinator `finishedAt` values copied by the overlay are UTC `YYYY-MM-DDTHH:MM:SSZ` (`lib/operation-coordinator.sh:41`). JS parses with `new Date()`; if invalid it prints the raw string (sjs:112-126) — whether all browsers parse `+0300` is **UNCONFIRMED**; Go must emit the identical format.

### 2.2 Related files
- Live servers: `/opt/broray/servers/<serverId>.json` (`ssvc:8`); disabled subscription servers: `/opt/broray/config/disabled-subscription-servers/<subId>/<serverId>.json` (`ssvc:9`); quality cache `/opt/broray/run/server-quality/<serverId>.json` (`ssvc:10`); active server id: first line of `/opt/broray/config/active-server` (`ssvc:210-215`, `lib/server.sh:8`).
- Legacy marker locks: `/opt/broray/run/subscriptions/<id>.lock` (existence → UPDATE_ALREADY_RUNNING, never created by 3.x) and `/opt/broray/run/server-subscription.lock` (existence → SERVER_SYNC_BUSY) (`svc:1108-1128`, `ssvc:11,42-59`). Unfinished rollback marker `/opt/broray/tmp/server-subscription-sync.*/rollback-required` → SERVER_SYNC_RECOVERY_REQUIRED.
- Parse progress: `/opt/broray/run/subscriptions/progress-<opId>.json` = `{"processed":n,"total":m}` (`svc:959-967`); removed by cleanup (`lib/subscription-job.sh:81-82`). For queued refreshes it is written under the RAM request dir instead (§4.2) and therefore never public.
- Preparation dir (legacy launch): `/opt/broray/tmp/subscription-op-<opId>-XXXXXX/` with `operation-id, input?, download, nodes, stage/, warnings.txt, provider-metadata.json, fetch-result.json, parse-result.json` (`lib/subscription-job.sh:15-22`, `lib/subscription-prepare.sh`).
- Logs: `/opt/broray/logs/subscriptions.log` lines `<iso> subscription=<id> action=<create|update-settings|update|delete> …url=<masked>`; when >262144 bytes keep last 600 lines (`svc:71-85`). Scheduler log `/opt/broray/logs/subscription-scheduler.log` (`bin/broray-subscription-scheduler:10`).
- Coordinator state read by projections: `${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/operations/<opId>/state.json` and `cancel.json` (`svc:1163,1191-1195`).

### 2.3 Public projection `broray_subscription_public_file(file, includeUrl)` (`svc:1178-1231`)
Output = stored record with:
1. `lastUpdateStatus` replaced by effective status (§2.4); if the overlay applies: `lastError`=overlay message, `lastUpdateResult={errorCode, durationMs:null, warnings:[]}`, `lastUpdatedAt`=op `finishedAt` (if non-empty).
2. Added: `updateOperation`, `parseProgress`, `sendDeviceInfo` (`==true`), `displayUrl` (§3.2), `serversCount` (= number of this subscription's files in live dir + disabled dir, `ssvc:228-249`).
3. Removed: `url` (unless includeUrl), `clientHwid`, `lastUpdatedEpoch`, `nextUpdateEpoch`. `backgroundOperationId` and `httpUserAgent` **remain**.

`updateOperation` (`svc:1188-1200`): `{}` unless `backgroundOperationId` starts `op-`, matches `[A-Za-z0-9._-]` and its `state.json` exists (not symlink) with `kind=="background"`, `operationId==id`, `operation` starting `subscriptions:`. Then:
`{id, running:(.running==true), phase: one of fetching|parsing|committing|waiting else "working", cancelRequested: (cancel.json has cancelRequested==true), canCancel: (running && cancelability=="cooperative" && !cancelRequested)}`.
It is produced even for finished operations (running:false).

`parseProgress` (`svc:1201-1204`): `{}` unless effective status is `running` and the progress file exists and satisfies `0≤processed≤total≤500` → `{processed,total}`.

### 2.4 Effective status overlay (`svc:1151-1176`)
Only when stored `lastUpdateStatus=="running"` and `backgroundOperationId` is a valid `op-…` (≤96 chars) whose state.json shows `kind=="background"`, same operationId, `running==false`, `state ∈ {aborted,failed,recovered,completed}`:
- status → `error`, code `OPERATION_INTERRUPTED`, message "Обновление прервано. Сохранены последние доступные данные подписки.";
- if `state=="aborted" && errorCode=="CANCELLED"` → code `CANCELLED`, message "Обновление остановлено пользователем. Сохранены последние доступные данные подписки.";
- finished time = op `finishedAt`.
Used by list/details/summary. Persisted only by `broray_subscription_recover_stale` (`svc:1233-1282`), which runs only from the legacy `--job`/`scheduler-once`/CLI `recover` paths (not from the queue daemon loop) — so on disk the record can stay `running` indefinitely; reads must keep overlaying.

### 2.5 Server file produced by a subscription node
Parser output (`lib/server-import.sh:63-121` for VLESS; analogous for others) + subscription fields (`svc:1047-1063`): `id = "subscription-<subId>-" + first 16 hex of importKey`, `source = {type:"subscription", subscriptionId, importKey, nodeIndex (1-based line index), enabled (subscription enabled at staging), updatedAt}`. During sync the id may be rewritten to the old server's id (identity continuity, §3.8).

### 2.6 Deleted-subscriptions ("trash") and legacy `/opt/broray/subscriptions`
- `/opt/broray/deleted-subscriptions/` is only **created and preserved** by setup/runtime-prepare/package code (`bin/broray-runtime-prepare:14`, `lib/package-setup.sh:132,168,508`, `lib/package-transaction.sh:8395`, `lib/broray-page.sh:1336`). **No 3.x code writes to or reads from it**; WebUI delete is a permanent `rm -f` (`svc:1925`). No restore/trash endpoint exists. Go: keep creating/preserving the directory, never use it, unless a 3.5.0 spec says otherwise.
- `/opt/broray/subscriptions/<name>/{raw.txt,nodes.txt,meta.json}` belongs to the **legacy CLI** `bin/broray subscription-add|subscription-update|subscriptions|subscription-nodes` (`lib/subscription.sh:7,195-342`, `bin/broray:25,248-288`): curl `-A BROray/0.1.0`, URI-or-Base64 only, nodes imported as `subscription-<name>-%04d`, stale indices deleted except the active one. Not used by the WebUI/API. Directory is preserved by packaging.

---

## 3. Algorithms and rules

### 3.1 URL validation (`broray_subscription_parse_url`, `svc:398-502`)
- Must start `http://` or `https://` → else `INVALID_URL` "URL подписки должен использовать HTTP или HTTPS."
- Any whitespace → `INVALID_URL` "URL подписки содержит пробельные символы."; any of `" ' \` \\` → `INVALID_URL` "URL подписки содержит недопустимые символы."
- authority = text after `://` up to first `/`, `?`, `#`; empty → "В URL подписки отсутствует адрес сервера."; contains `@` → "Данные пользователя в URL подписки запрещены."
- host: `[v6]` form or `host:port` or `host`; empty → "В URL подписки отсутствует имя хоста."; lowercased; `localhost, *.localhost, *.local, *.lan, *.home, metadata, metadata.google.internal` → `DOWNLOAD_SECURITY` "Локальные и служебные адреса запрещены."
- port default 443/80; non-digit → "Порт в URL подписки имеет неправильный формат."; outside 1..65535 → "Порт в URL подписки находится вне допустимого диапазона."
- At fetch time additionally (`svc:504-571`): literal IPs must be public (else `DOWNLOAD_SECURITY` "Локальный или служебный IP-адрес запрещён."); names resolved via `getent ahosts` or `nslookup`; **every** resolved address must be public (else `DOWNLOAD_SECURITY` "Имя подписки разрешается в локальный или служебный адрес."); none → `HTTP_ERROR` "Не удалось определить IP-адрес сервера подписки."; no resolver → `DOWNLOAD_SECURITY` "На устройстве отсутствует безопасный DNS-резолвер.". The first resolved IP is pinned with curl `--resolve`.
- Public-IP rule (`svc:333-385`): IPv4 dotted-quad strictly (no leading zeros) not in 0/8, 10/8, 127/8, 169.254/16, 192.168/16, 224-255.x, 100.64/10, 172.16/12, 198.18/15, 192.0.0/24, 192.0.2/24, 198.51.100/24, 203.0.113/24. IPv6: full syntactic check, first group must be 4 hex digits starting `2` or `3` (i.e. 2000::/3 only), not 2001:db8::/32.

### 3.2 displayUrl masking (`svc:254-331`)
- Not http(s) → `***`.
- Drop `#fragment`. Userinfo `user@host` → `***@host`.
- If query present → path cut at `?` and `?***` appended.
- Last path segment replaced with `***` when the path starts with `/sub/`, `/subscription/`, `/subscriptions/`, `/subscribe/`, `/link/`, `/s/`, **or** the last segment length ≥12; only if the last segment is non-empty.
- Example: `https://example.com/sub/AbCdEf123?x=1` → `https://example.com/sub/***?***`; `https://h.io/api/v1/abcdefghijkl` → `https://h.io/api/v1/***`; `https://h.io/x` → `https://h.io/x`.

### 3.3 Fetch (`broray_subscription_fetch`, `svc:609-819`; helpers `lib/subscription-http.sh`)
- Runs inside supervised helper `lib/subscription-prepare.sh fetch <prepDir> <input.json>` (`lib/subscription-prepare.sh:17-24`), helper timeout **180 s** (`lib/subscription-job.sh:24`, `:192`); timeout → `DOWNLOAD_TIMEOUT` "Превышено время загрузки подписки. Предыдущий список серверов сохранён." (legacy) / `PREPARATION_TIMEOUT` "Превышено время подготовки подписки." (queue, `lib/subscription-job.sh:202-203`).
- curl: `--disable --globoff --silent --show-error --noproxy '*' --proto =http,https --connect-timeout 10 --max-time 35 --max-redirs 0 --user-agent <UA> --resolve host:port:ip`, header `Accept: application/json, text/plain, */*`; `--compressed` only if curl ≥ 8.20 else `Accept-Encoding: identity` (`subscription-http.sh:48-50`); `--max-filesize 2097152` if supported.
- UA: record `httpUserAgent` if non-empty else `BROray/<version>` where version = `share/release/manifest.json .version` (≤64, `[a-zA-Z0-9.+_-]`) or first line of `config/version`, else `unknown` (`subscription-http.sh:6-13`).
- Always header `x-hwid: <clientHwid>`.
- If `sendDeviceInfo`: headers `X-Device-OS`, `X-Device-Model`, `X-Ver-OS`, `X-App-Version` (each only if non-empty) — sent only while the request origin (`scheme:host:port`) equals the first request's origin; a redirect to another origin stops them for the rest of the chain (`svc:656-666,694-699`). Values come from `ndmc -c 'show version'` (30 s timeout, ≤16 KiB) filtered by `lib/subscription-device-info.jq`: `os:"KeeneticOS"`, `model` = `KN-dddd` from `hw_id` or `model "(KN-dddd)"` (must agree), `osVersion` from `title` else `release` (starts with digit, `[0-9A-Za-z .+-_()]`, ≤64); only if vendor/manufacturer Keenetic or a KN code is present; `appVersion` = BROray version (`lib/subscription-device-info.sh:3-47`).
- curl exit 28 → `DOWNLOAD_TIMEOUT` "Сервер подписки не ответил вовремя."; 63 → `CONTENT_TOO_LARGE` "Ответ подписки превышает допустимый размер."; other → `HTTP_ERROR` "Не удалось скачать подписку.".
- Body >2097152 bytes → CONTENT_TOO_LARGE; dumped headers >65536 bytes → `CONTENT_TOO_LARGE` "Заголовки ответа подписки превышают допустимый размер.".
- Header parsing uses only the **last** HTTP response block; a header repeated with different values is "ambiguous" (`subscription-http.sh:14-27`).
- On 2xx/4xx/5xx, HWID denial headers are checked first (`subscription-http.sh:32-46`): any of `x-hwid-max-devices-reached`, `x-hwid-limit`, `x-hwid-not-supported` ambiguous → `HTTP_ERROR` "Провайдер вернул противоречивые служебные заголовки."; `x-hwid-max-devices-reached: true` or `x-hwid-limit: true` (case-insensitive value) → `SUBSCRIPTION_DEVICE_LIMIT_REACHED` "Провайдер отклонил подписку: достигнут лимит зарегистрированных устройств."; `x-hwid-not-supported: true` → `SUBSCRIPTION_DEVICE_ID_REJECTED` "Провайдер не принял анонимный идентификатор клиента подписки.". This applies even to HTTP 200.
- `Content-Encoding` present and not `identity` while not using `--compressed` → `UNSUPPORTED_CONTENT` "Сервер прислал сжатый ответ; для безопасной распаковки требуется curl 8.20 или новее.".
- 2xx → save body, collect provider metadata from headers. 301/302/303/307/308 → follow `Location` (absolute, `//host`, `/path`, `?query`, relative), max **3** redirects ("Сервер подписки выполнил слишком много перенаправлений."), missing Location → "Сервер подписки вернул перенаправление без адреса.", https→http → "Переход с HTTPS на незащищённый HTTP отклонён." (all `HTTP_ERROR`); each hop re-validates URL + DNS. Any other status → `HTTP_ERROR` "Сервер подписки вернул HTTP <code>.".

### 3.4 Provider info (`broray_subscription_metadata_collect`, `subscription-http.sh:51-72`; `lib/subscription-metadata.jq`)
- Sources, in order, all merged into one object (later overrides earlier for the same key): (1) final response headers; (2) body lines after BOM/CR removal; (3) Base64-decoded body lines (if decoded). Metadata file is reset to `{"schemaVersion":1}` at the start of every fetch (`svc:626-629`).
- Recognised keys (case-insensitive; body lines may be prefixed by `#` and whitespace; line format `key: value`, value truncated to 4097 chars): `profile-title, subscription-userinfo, profile-update-interval, support-url, profile-web-page-url, announce, announce-url, routing, routing-enable, update-always, custom-tunnel-config`.
- Same key repeated with differing (trimmed) values → key added to `invalidFields`.
- `decode_text(n)`: value may be `base64:<b64>` (URL-safe chars accepted, strict padding/canonical form); result must be 1..n chars with no control chars, DEL or U+FFFD (`subscription-metadata.jq:3-13`).
- Mapping (`subscription-metadata.jq:24-27`):
  - `profile-title` → `title` (≤128)
  - `announce` → `announcement` (≤1024)
  - `support-url` → `supportUrl`, `profile-web-page-url` → `webPageUrl`, `announce-url` → `announcementUrl`: decode_text(2048), must start `http://`/`https://`, authority non-empty, no `@`, no space/`"`/`'`/`<`/`>`/`\` (`:15-18`)
  - `subscription-userinfo` → `usage` object: `;`-separated `k=v`, 1..4 unique keys from `upload, download, total, expire`, values decimal digits ≤16 chars, ≤ 9007199254740991 (`:19-22`)
  - `profile-update-interval` → `suggestedUpdateMinutes = hours*60`, hours integer 1..168
  - `routing, routing-enable, update-always, custom-tunnel-config` → `ignoredDirectives:[key]` (never applied)
  - any parse failure → `invalidFields:[key]`
- After any record group is processed, `invalidFields` and `ignoredDirectives` arrays exist (unique-sorted); with no recognised records the object is just `{"schemaVersion":1}`.
- Persisted only after a successful update: `providerMetadata = metadata + {updatedAt: <finish ISO>}` (`svc:1856`). Failed updates keep the previous providerMetadata; settings URL change deletes it (`svc:1586`). Provider metadata lines are stripped from the body before node extraction (`subscription-http.sh:68-72`; also every line starting with `#` is dropped).
- **Provider interval never changes `updateIntervalMinutes`.** JS shows it only if 60..10080 (`subscription-provider.js:23`).
- JS rendering rules (`subscription-provider.js:12-28`): only `schemaVersion===1`; title ≤128; bytes formatting with КиБ…ПиБ base 1024, 2 decimals; `usage.total===0` → "Провайдер не указал лимит"; "Осталось" = max(0,total-upload-download) when all present and total>0; `expire*1000` → locale date, 0 → "Провайдер не указал срок"; links only http(s) without credentials, ≤2048; notes for `invalidFields` / `ignoredDirectives`; header note "Сведения из последнего успешного обновления; сейчас они не подтверждены." when status is `error` or `running`.

### 3.5 Payload formats (`broray_subscription_extract_nodes`, `svc:857-936`)
Parse helper timeout **300 s** (`lib/subscription-job.sh:24`) → `PARSE_TIMEOUT` "Превышено время разбора серверов подписки. Предыдущий список серверов сохранён." (legacy) / `PREPARATION_TIMEOUT` (queue).
1. Strip UTF-8 BOM, delete `\r`; collect body metadata; strip metadata and `#` lines.
2. If content matches `<!doctype html` / `<html` (case-insensitive) → `UNSUPPORTED_CONTENT` "Сервер вернул HTML вместо подписки.".
3. **URI list**: if any line matches `^\s*(vless|vmess|trojan|ss|hysteria2|hy2|tuic|socks|socks5|http|https)://` → every line (trimmed) becomes a node candidate (non-URI junk lines are kept and later rejected).
4. Else, if no line starts with `{` or `[`, try **Base64** of the whole body: remove `\r\n\t ` , map `_`→`/`, `-`→`+`, pad to multiple of 4, `base64 -d` (`svc:821-843`). If decoding succeeds: BOM/CR strip, metadata collect/strip again; then if URI lines → URI list. (Only one level of Base64; HTML check is not repeated.)
5. Else **Xray JSON** (`jq -rs --argjson max_nodes 500 -f lib/subscription-xray-json.jq`) on the (decoded or raw) text; jq failure → `PARSE_ERROR` "Содержимое подписки не удалось распознать.".
6. Remove empty and `#` lines; count N. N=0 → `NO_VALID_NODES` "Подписка не содержит узлов."; N>500 → `CONTENT_TOO_LARGE` "Подписка содержит слишком много узлов.".

Xray JSON → servers (`lib/subscription-xray-json.jq:321-334`): document must be exactly one JSON value: an object or array of objects, each with `outbounds` array of objects (else jq error → PARSE_ERROR). For each profile, outbounds with `protocol` `freedom`/`blackhole` are ignored; every other outbound becomes one URI per endpoint/user, or the marker line `broray-json-error://unsupported-node` if anything is unsupported (marker counts as a received node and is rejected with warning "Узел N: параметры JSON-конфигурации не поддерживаются.", `svc:994-998`). Output limited to 501 lines.
- Name: `profile.remarks` else `outbound.tag` else "Сервер"; if the profile has >1 non-freedom/blackhole outbound: `name + " / " + (tag or 1-based index)` (`:233-234,295-296`).
- Supported: `vless` (keys protocol/tag/settings/streamSettings/fragment; settings `vnext[]` or flat `{address,port,id,encryption,flow}`; users keys id/encryption/flow; id unreserved chars; encryption `none` or valid `mlkem768x25519plus.*`; flow `""|xtls-rprx-vision|xtls-rprx-vision-udp443` only with tls/reality and tcp or non-none encryption) (`:228-261`); `vmess` (vnext or flat; security auto/aes-128-gcm/chacha20-poly1305/none; alterId 0; level 0) ; `trojan` (servers or flat; password; flow empty); `shadowsocks` (servers or flat; method+password → base64 userinfo; tcp only, security none, no finalmask) (`:265-306`). Anything else (incl. hysteria2, dns, socks…) → marker.
- Transports (`:42-48,182-219`): tcp/raw (header none or http object), ws/websocket (path default "/", host from `host` or single `Host` header), httpupgrade, xhttp/splithttp (mode auto/packet-up/stream-up/stream-one, strict `extra` validator), grpc (serviceName, authority, multiMode→mode multi/gun), kcp. Security (`:63-90`): none | tls (allowInsecure must be false; sni, fp default chrome, alpn, ech, pcs, vcn) | reality (only tcp/grpc/xhttp; serverName + publicKey/password required; fp, sid, spx, pqv). `finalmask` passed as `fm` JSON.
- Address: unreserved hostname ≤255 or IPv4 or bracketed/unbracketed IPv6; port integer 1..65535.
- The JSON path adds the first warning "Из JSON импортируются отдельные серверы. DNS, маршрутизация, балансировка и фрагментация профиля не применяются." (`svc:981-983`).

### 3.6 Node staging (`broray_subscription_stage_nodes`, `svc:969-1106`)
For each candidate line i=1..N (progress file written before each node and at the end):
- marker → rejected (warning above);
- parse via `broray_server_import_dispatch uri subscription <subId> i` (`lib/server-import.sh:998-1060`): schemes `vless`, `vmess`, `trojan`, `ss`, `hysteria2|hy2` supported; `tuic`, `socks|socks5`, `http|https` and anything else rejected ("Ошибка: парсер TUIC ещё не реализован", "…SOCKS…", "…HTTP-прокси…", "Ошибка: неподдерживаемый формат конфигурации"). Parser specifics = servers area (`lib/parser-*.sh`). Failure → rejected, warning `"Узел i: " + last stderr line (≤180 chars)` (default "формат узла не поддерживается");
- no output file → rejected "Узел i: импортёр не создал сервер.";
- provider denial marker: `address=="0.0.0.0" && port==1` and name contains "Приложение не поддерж" or (lowercased, spaces/`_`/`-` removed) contains `appnotsupported|applicationnotsupported|enablehwid` → rejected "Узел i: провайдер вернул служебный отказ вместо сервера." (`svc:938-957`);
- importKey failure → "Узел i: не удалось вычислить importKey."; duplicate stable id → "Узел i: дубликат уже полученного сервера."; validation failure → "Узел i: нормализованный сервер не прошёл проверку.";
- else accepted.
Accepted 0 → `SUBSCRIPTION_ACCESS_DENIED` "Провайдер не выдал серверы: приложение или идентификатор устройства не поддержан." if any denial marker, else `NO_VALID_NODES` "Подписка не содержит ни одного поддерживаемого и валидного узла.".

importKey = sha256 of canonical `jq -cS` object of connection fields (`ssvc:66-102`); continuity key = sha256 of `{protocol,address(lower),port,network,security,serverName,host,path,serviceName,mode}` (`ssvc:104-119`).

### 3.7 Update result (`broray_subscription_update`, `svc:1675-1889`)
- Start: record `lastUpdateStatus:"running"`, `backgroundOperationId`, `lastError:null`, `updatedAt` (`svc:1718-1725`); ensure `clientHwid`.
- Rejected>0 → sync policy `retain-unmatched`, else `replace` (`svc:1741-1742`).
- Status `success` if rejected==0 else `partial` (`svc:1796-1799`).
- Success `lastUpdateResult` (`svc:1810-1827`): `{received, parsed, accepted, rejected, added, updated, unchanged, removed, retained, catalogTotal, deletionPolicy, warnings: stagingWarnings + syncWarnings, durationMs: (finishEpoch-startEpoch)*1000, trigger: initial|manual|automatic, errorCode:null, activeServerImpact: none|display-name-changed|configuration-changed}`; `serversReceived = sync.total`; `lastUpdatedAt/Epoch = finish`; `lastError=null`; next = finish + interval (if enabled&&auto); providerMetadata replaced.
- Failure (`svc:1616-1673`): `lastUpdateStatus:"error"`, `lastUpdatedAt/Epoch = now`, `lastError = message`, `lastUpdateResult = {received:0,parsed:0,accepted:0,rejected:0,added:0,updated:0,unchanged:0,removed:0,warnings:[],durationMs,trigger,errorCode,activeServerImpact: ("requires-decision" if ACTIVE_SERVER_CONFLICT else "none")}`, next recomputed from now; servers catalog and `serversReceived` unchanged.
- Cancel (rc 130) or unresolved (rc 75) during preparation → returns without writing a result: record stays `running` and the overlay (§2.4) supplies the visible error.
- JS renderResult: if `errorCode` → `"Код: <code>"` + `" · длительность: <ms> мс"` when finite ≥0; else `"получено R · принято A · добавлено X · обновлено U · удалено D · отклонено J"` + `" · сохранено прежних K"` when retained>0 (sjs:191-211). `null` result → "Обновление ещё не выполнялось.".

### 3.8 Catalog sync and active-server conflict (`broray_server_subscription_sync`, `ssvc:607-1000`)
- Pre-checks: every staged file valid and owned by this subscription (else `SERVER_SYNC_ERROR`), ≥1 staged (`NO_VALID_NODES` "Серверный модуль не получил ни одного узла."), lock markers (§2.2).
- Backup all current live + disabled servers of the subscription into `/opt/broray/tmp/server-subscription-sync.<pid>.<updateId>/backup`.
- Identity remap (`ssvc:123-181`): exact importKey match first; then for the remaining old/new, a unique 1-to-1 continuity-key match keeps the **old server id** (so an active server survives credential rotation). Duplicate exact keys or many-to-one continuity → `SERVER_SYNC_AMBIGUOUS_IDENTITY` "Не удалось однозначно сопоставить серверы подписки. Прежний каталог сохранён."
- Counters: added (no old key), unchanged (same hash ignoring `source.nodeIndex/updatedAt`), updated, removed (old keys absent).
- `retain-unmatched` (`ssvc:549-605`): old servers whose key is absent are kept (copied from backup), counted `retained`; staged+retained >500 → `PARTIAL_UPDATE_LIMIT`; foreign file collisions → `SERVER_SYNC_CONFLICT`; message "Не удалось безопасно объединить частичный ответ с прежними серверами. Прежний каталог сохранён."; success adds warning "Сохранено прежних серверов: K. Ответ разобран не полностью; …" (`ssvc:996`).
- Active server (first line of `config/active-server`) belongs to this subscription:
  - missing from new set → `ACTIVE_SERVER_CONFLICT` "Активный сервер исчез из подписки. Обновление отменено, сохранена прежняя рабочая конфигурация." (nothing written)
  - name changed → impact `display-name-changed`; after commit `broray_interface_sync_description` (failure → rollback + `ACTIVE_SERVER_CONFLICT` "Новое имя активного сервера не удалось синхронизировать с управляемым интерфейсом. Восстановлена прежняя версия подписки.") and `broray_server_refresh_keenetic_status force` (best effort)
  - config hash (all fields except id,name,uri,source) changed → impact `configuration-changed`; before commit generate config + `broray_xray_test_file` (failure → `ACTIVE_SERVER_CONFLICT` "Новая конфигурация активного сервера не прошла проверку Xray. Обновление отменено."); after commit `broray_xray_apply_server <id>` (restarts Xray; failure → rollback + `ACTIVE_SERVER_CONFLICT` "Новая версия активного сервера не была применена. Восстановлена прежняя рабочая версия.")
  - id changed without config change → `configuration-changed`, rewrite `config/active-server`
  - subscription disabled → rollback + `ACTIVE_SERVER_CONFLICT` "Активный сервер не может быть перенесён в выключенный источник."
- Commit: target dir = live if enabled else disabled dir; files identical except `source.updatedAt` are **not rewritten** (keeps queued quality-check snapshots valid, `ssvc:850-860`); others atomically replaced; then delete this subscription's files not in the keep set (and their quality cache) from both dirs. Commit failure → rollback → `SERVER_SYNC_ERROR` "Не удалось атомарно сохранить серверы подписки."; rollback failure → `SERVER_SYNC_ROLLBACK_FAILED` "Восстановление серверов не завершено. Резервная копия сохранена: <path>" and the `rollback-required` marker blocks later writers.
- Returns `{subscriptionId, updateId, accepted, rejected:0, added, updated, unchanged, removed, retained, total, deletionPolicy, warnings, activeServerImpact}`.

### 3.9 Enable/disable (subscription level only) (`ssvc:342-423`)
- Disable: if the active server belongs to the subscription → `ACTIVE_SERVER_CONFLICT` (409); else move every live file of the subscription into `config/disabled-subscription-servers/<id>/`.
- Enable: move every file back; name already present in live dir → `SERVER_ID_CONFLICT` (409); remove empty disabled dir.
- **There is no per-node enable/disable** in the 3.x subscriptions API or UI; `items[].enabled` in servers.cgi only reflects which directory a node is in (all nodes of a subscription move together). Individual subscription servers can only be deleted through the servers area, and reappear on the next refresh.
- Disabled subscriptions are still refreshable (manual), servers land in the disabled dir; they are not auto-refreshed (scheduler requires enabled).

### 3.10 Summary algorithm (`svc:1957-2123`)
Over valid records: `total`; `enabled` count; `autoUpdateEnabled` = any enabled&&auto; effective status counts `partial/error/running`. For **enabled** records (regardless of autoUpdate): interval floored at 15 min; `epoch = lastUpdatedEpoch` (or overlay finish time parsed as UTC, with warnings reset to []); `epoch≤0` → neverUpdated; `now-epoch > interval*120` seconds (= 2× interval) → stale. Latest = record with strictly greatest epoch (ties keep first in glob order): its effective status/At/warnings → `lastUpdateStatus` (default "never"), `lastUpdatedAt` (null if none), `lastWarnings` (`lastUpdateResult.warnings // []`). `serversReceived` = count of all subscription servers (live `source.type=="subscription"` + all disabled dirs) (`ssvc:251-269`).
Health: severity `error` if errors>0 (operational false, actionRequired true); else `warning` if partial|stale|never>0 (operational true, actionRequired true); else `busy` if running>0 (actionRequired false); else `ok`. consistent = no partial/errors/stale/never. freshness `unknown` if never>0, else `stale` if stale>0, else `fresh`; checkedAt = latest lastUpdatedAt. Reasons in order: SUBSCRIPTION_UPDATE_ERROR "Последнее обновление одной или нескольких подписок завершилось ошибкой.", SUBSCRIPTION_UPDATE_PARTIAL "Одна или несколько подписок обновлены частично.", SUBSCRIPTION_UPDATE_RUNNING "Обновление подписки выполняется.", SUBSCRIPTION_UPDATE_STALE "Автоматическое обновление одной или нескольких подписок просрочено.", SUBSCRIPTION_NEVER_UPDATED "Одна или несколько активных подписок ещё не обновлялись." (`svc:2036-2042`; contract shape `lib/status-contract.sh:17-99`, schemaVersion 1).

### 3.11 Scheduler (`bin/broray-subscription-scheduler`)
- Daemon loop every **60 s** (`BRORAY_SUB_SCHEDULER_INTERVAL`, `:11,83-93`): `queue-recover`, `broray_subscription_enqueue_due`, `broray_job_dispatch_step continue` (`:36-46`).
- Due rule (`lib/subscription-job.sh:290-300`): `enabled==true && autoUpdateEnabled==true && nextUpdateEpoch>0 && nextUpdateEpoch<=now`. Each due record is enqueued as `subscriptions:refresh <id> SUBSCRIPTION_AUTO <sha256(record)>` priority **4**, reusing a still queued/running request (`lib/active-proxy-health.sh:151-200`). Nothing is enqueued while `${BRORAY_STATE_ROOT}/background-automation.json` has `paused:true` (`lib/subscription-job.sh:278-285`); also DoT auto-check enqueue lives here (other area).
- Next time: create/settings → now + interval; update end (success or failure) → finish + interval (`svc:1136-1149`). Because the record hash is the queue context, any record change between enqueue and apply makes the apply step exit 76 (request `failed`, record unchanged, still due → re-enqueued next tick). AUTO apply also requires the record to still be enabled&&auto (`lib/subscription-job.sh:130-134`).
- `--once` / `--job` legacy path: `broray_subscription_scheduler_once` (`svc:2125-2154`) recovers stale running records, then updates due records sequentially (trigger `automatic`) inside a `subscriptions:scheduler` job. Not used by the daemon loop.
- Dispatch latency of a queued USER refresh = next dispatcher tick (this daemon 60 s, `bin/broray-server-auto-switch` loop `INTERVAL_SECONDS=15` at `:47`, or chained worker). Exact guarantee **UNCONFIRMED** (operations area).

### 3.12 Limits summary
Body 64 KiB (CGI), 4 KiB (queue); name ≤128; url ≤4096; UA ≤128 printable ASCII; interval 5..10080 (default 360; UI min 5 / max 10080 / step 1, `subscriptions.html:148`); download ≤2 MiB, headers ≤64 KiB; ≤3 redirects; curl connect 10 s / 35 s per hop; helper 180 s fetch / 300 s parse; ≤500 nodes (also ≤500 staged+retained); metadata file ≤64 KiB; queue ≤64 queued requests.

---

## 4. Async / progress model

### 4.1 Legacy launch (create with updateImmediately; refresh without queue header)
1. CGI owns global op `subscriptions:create|refresh` (cooperative), marks record running, checkpoint `waiting`, spawns `subscription-web-worker.sh` and hands ownership over (nonce) (`lib/subscription-web-job.sh`, `lib/operation-coordinator.sh:195-250`); replies 202 `{accepted,id,backgroundOperationId}`.
2. Worker: checkpoints `fetching` → helper fetch; `parsing` → helper parse; `committing` → sync + record write (`lib/subscription-job.sh:3-65`, `svc:1675-1889`).
3. UI: list polling every 3 s while any record is `running`; card shows phase text from `updateOperation.phase`: `fetching` "Загрузка подписки…", `parsing` "Разбор серверов…" + " Обработано P из T." when parseProgress integers present, `committing` "Сохранение серверов…", `waiting` "Ожидание запуска…", other "Обновление подписки…" (sjs:279-284).
4. Cancel: button shown when record running and `updateOperation.id`; label "Остановить обновление" (canCancel), "Останавливается…" (cancelRequested), "Применяются изменения" disabled when !canCancel (sjs:319-320); POST cancel.cgi `{operationId}`. Cancelled update leaves record running → overlay `CANCELLED`.

### 4.2 Queue refresh (manual from the UI, and all automatic refreshes)
- Submit (§1.8a) → request `q-<nonce>` priority 2 (USER) / 4 (SUBSCRIPTION_AUTO), stage `fetch` (`lib/operation-scheduling.sh:13-14`).
- Stages run as separate finite workers (`lib/operation-worker.sh:32-34` → `broray_subscription_step`, `lib/subscription-job.sh:115-228`):
  - `fetch` (resource background-prepare, cancelable/cooperative, phase fetching): copies record (+generated hwid if missing) to RAM `/tmp/broray-operations/requests/<req>/preparation/input.json`, runs fetch helper; result recorded and hashed; yields to `parse`.
  - `parse` (cooperative, phase parsing): runs parse helper (skipped if fetch failed — outcome carried forward); progress goes to `<prep>/progress/` (not public); yields to `apply`.
  - `apply` (global lock, **protected / not cancelable**, phase committing): persists generated hwid if needed, then `broray_subscription_update` using the prepared outcome (`broray_subscription_queue_use_prepared`, `lib/subscription-job.sh:230-272`); fetch/parse failures are persisted here as a normal failed update.
  - Every stage first verifies the record sha256 equals the submit-time context; mismatch/missing record → exit 76 → request `failed`, record unchanged (`lib/subscription-job.sh:128-129,216-217`).
- During fetch/parse the record is **not** modified (status stays as before; any `updateOperation` shown comes from the previous operation id). During apply the record is `running` with the apply operation id (canCancel false).
- Terminal receipt mapping: op `aborted` → `cancelled`, `recovered` → `failed`, else op state (`lib/operation-scheduling.sh:659`).
- JS (`common.js:86-183`, sjs:474-491): nonce persisted in `sessionStorage["broray.queue.v1:/api/subscriptions/refresh.cgi:<id>"]` before sending; on lost reply (status not in 400/401/403/404/409/413/415/422) it looks up `POST status.cgi {nonce}`; then polls `GET status.cgi` every **5 s** (skipped while `document.hidden`), matching `queue[].requestId`; row missing → state `unknown` ("Задание не найдено в текущем снимке. Проверьте состояние повторно."). While `queued|running` the card shows `BROrayUI.queueLabel`: queued → "В очереди" / reason `automation_paused` "На паузе" / `active_connection` "Отложено ради активного подключения"; running "Выполняется". Terminal: `completed` → toast "Обновление подписки завершено." (success), otherwise `result.message || queueLabel` ("Ошибка", "Отменено", …) as warning; then `loadAll(true)`. On page load, any id with a stored nonce resumes watching (sjs:352-356).
- Cancel while queued: "Отменить ожидание" → POST cancel.cgi `{requestId}` (sjs:319,493-500). Running queue stages are not cancelable from this page except via `updateOperation` of a legacy op.

---

## 5. Dynamically built endpoints / keys (JS)
- `paths.details + "?id=" + encodeURIComponent(id)` (sjs:392)
- `paths.update + "?id=" + encodeURIComponent(state.editingId|id)` (sjs:441,463)
- `paths.servers + "?id=" + encodeURIComponent(id)` (sjs:521)
- `paths.remove + "?id=" + encodeURIComponent(item.id)` (sjs:554)
- `/api/subscriptions/refresh.cgi` body `{id,nonce}` via `submitQueued` (common.js:87-88 whitelist, :117)
- sessionStorage key `"broray.queue.v1:" + url + ":" + id` (common.js:91,133,141)

## 6. UI dependencies on backend values / HTML
- HTML ids used by sjs (all static in `subscriptions.html`): `app, page-loader, current-user, subscriptions-list, subscription-form-panel, subscription-form, subscription-form-title, subscription-name, subscription-url, subscription-user-agent, subscription-interval, subscription-enabled, subscription-auto, subscription-device-info, subscription-immediate, subscription-immediate-row, subscription-submit, subscription-cancel, add-subscription, subscriptions-total, subscriptions-enabled, subscriptions-servers, subscriptions-last-status, subscriptions-last-time, toast-root, confirm-*` (dialogs.js); `subscription-form-message` is created by JS (sjs:415-421). Backend produces no HTML.
- Enum values the JS depends on: `lastUpdateStatus ∈ {never,running,success,partial,error}` (sjs:128-136 labels "Не обновлено","Обновляется","Обновлено","Обновлено частично","Ошибка"; unknown → "Не определено"); `updateOperation.phase ∈ {fetching,parsing,committing,waiting}`; queue `state` and `reason` enums; `providerMetadata.schemaVersion===1`.
- Card `data-id` = `item.id` (escaped) and button `data-action` values are JS-generated (sjs:295-326).
- Verbatim backend text shown: `error.message`, `lastError`, `lastUpdateResult.warnings[]`, `lastUpdateResult.errorCode`, provider `title/announcement`, server `name/protocol/address/port/network`.
- Only code-based branch: `CANCEL_NOT_SUPPORTED` (sjs:100-102); `REQUEST_UNCONFIRMED` in `payload.errorCode` with 404 (common.js:107).

## 7. Open points (UNCONFIRMED)
- BusyBox `${#name}` byte vs character counting for the 128 name limit (`svc:1357,1512`).
- Browser parsing of `+HHMM` offsets in `lastUpdatedAt/nextUpdateAt`.
- Exact queue dispatch latency after a USER submit (operations area).
- Origin/KeenDNS validation details for queue/cancel POSTs (operations area, `lib/operations-origin.sh`).
