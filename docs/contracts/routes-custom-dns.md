# Contract: Custom routes (BAT import) and DNS-over-TLS

Source: `/home/claude/broadmin/broray` @ f505928 (BROray 3.2.0-r12). All paths below are relative to
`runtime/app/` unless absolute. Runtime paths are under `/opt/broray` (`$ROOT`).

Abbreviations: `RCJ` = `web-new/assets/js/routes-custom-page.js`, `DJ` = `web-new/assets/js/dns.js`,
`CC` = `web-new/api/routes/custom-common.sh`, `DC` = `web-new/api/routes/dot-common.sh`,
`UI` = `lib/routes-user-import.sh`, `RD` = `lib/routes-dot.sh`, `DA` = `lib/dot-auto.sh`,
`AC` = `web-new/api/auth-common.sh`, `CJ` = `web-new/assets/js/common.js`.

---

## 0. Common rules that apply to every endpoint here

### 0.1 Envelope (AC)
* Success (HTTP 200, no `Status:` line): headers `Content-Type: application/json; charset=utf-8`,
  `Cache-Control: no-store, no-cache, must-revalidate`, `Pragma: no-cache`, `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` (AC:7-14). Body `{"success":true,"data":<object>,"error":null}` (AC:96-111).
* Error: `Status: <code>` + same headers; body `{"success":false,"data":null,"error":{"code":C,"message":M,"details":D|null}}`
  (AC:25-57). `details` is `null` when empty string.
  Exception: `custom-summary.cgi` when `auth-common.sh` is unreadable emits a hand-written body **without** a
  `details` key: `{"success":false,"data":null,"error":{"code":"AUTH_MODULE_UNAVAILABLE","message":"Модуль авторизации недоступен."}}` (custom-summary.cgi:17-23).
* If finalising the operation lock fails while sending success → `503 OPERATION_FINALIZATION_FAILED`
  "Результат операции сохранён, но её завершение ещё не подтверждено." (AC:99-101).
* Common errors: `405 METHOD_NOT_ALLOWED` "Для этой операции требуется метод X." details "Получен метод Y." (AC:59-70);
  `500 SESSION_BACKEND_UNAVAILABLE` "Модуль проверки сессии недоступен." (AC:73-78);
  `401 AUTH_REQUIRED` "Требуется авторизация." (cookie `BRORAY_SESSION`, AC:82-91). Method is checked before session.

### 0.2 JS side (CJ:4-60)
`apiRequest` throws when `!response.ok` or payload not an object; error gets `.message` = `error.message`,
`.code` = `error.code`, `.status` = HTTP status, `.details` = `error.details`. Both pages wrap with `unwrap()`
that additionally throws on `success===false` and returns `payload.data` (RCJ:56-70, DJ:25-35).
Both pages redirect to `/` on `status===401` or code `AUTH_REQUIRED`/`SESSION_REQUIRED` (RCJ:34-36, 616, DJ:431-433).
Both pages call `GET /api/session.cgi` first and read `session.user` (RCJ:1034-1041, DJ:671-673) — other area.

### 0.3 Request-body reader (`lib/web-request-body.sh:5-34`)
`CONTENT_LENGTH` must be all digits (else rc2), >0 (rc3), ≤ limit (rc4); exactly that many bytes read (rc5 read
failure, rc6 short). Mapping in CC:19-26 / DC:54-62:

| rc | HTTP | code | message (custom) | message (DoT) |
|---|---|---|---|---|
| 2 | 400 | CONTENT_LENGTH_INVALID | Некорректный размер запроса. | same |
| 3 | 400 | REQUEST_BODY_REQUIRED | Тело запроса отсутствует. | same |
| 4 | 413 | REQUEST_TOO_LARGE | Размер запроса превышает допустимый предел. | Запрос DNS-over-TLS слишком велик. |
| 5 | 400 | REQUEST_BODY_READ_FAILED | Не удалось прочитать тело запроса. | same |
| other | 400 | REQUEST_BODY_INCOMPLETE | Тело запроса получено не полностью. | same |

Limits: custom = 4194304 bytes (CC:9); DoT = 65536 bytes (DC:10). lighttpd `server.max-request-size = 5120` KB
(`runtime/state-seed/config/lighttpd.conf:8`).
Then body must be a JSON object: custom → `400 REQUEST_JSON_INVALID` "Запрос не является корректным JSON."
(custom-commit.cgi:15-19, custom-preview.cgi:15-19); DoT → `400 REQUEST_JSON_INVALID`
"Запрос должен содержать корректный JSON-объект." (DC:63-64). Content-Type is never checked.

### 0.4 Global operation lock (`lib/routes-api-operation.sh:132-172`, `lib/route-api-job.sh`)
Mutating custom/DoT CGIs call `broray_routes_api_lock_acquire "<scope>:<action>" <bundle>`:
* If `/opt/var/lock/broray/global-operation.lock` exists (file or symlink) → rc 2 (routes-api-operation.sh:139-143).
* Otherwise the whole CGI is re-executed inside a protected supervised route job (`broray_route_api_enter`,
  route-api-job.sh:6-54; allowed CGIs listed at :14-25; `broray_route_job_run` supervisor timeout 3600 s,
  route-job.sh:58-86) and only its buffered reply is printed.
* rc 2 → `409 ROUTES_OPERATION_BUSY`. Custom message: "Другая конфликтующая операция уже выполняется или ожидает продолжения." (CC:84-85); custom-remove: "Другая операция выполняется или ожидает продолжения." (custom-remove.cgi:29-30); DoT: "Другая конфликтующая операция уже выполняется." (DC:76).
* other rc → `500 ROUTES_API_LOCK_FAILED` "Не удалось установить блокировку операции." (CC:86-87, DC:77) / custom-remove: "Не удалось установить блокировку удаления." (:31-32).
* Library unreadable → `500 ROUTES_API_LOCK_UNAVAILABLE` "Модуль блокировки операций недоступен." (CC:74-76, DC:70).

Lock actions/bundles used: `custom:preview` (bundle = request file path, used as stdin body, route-api-job.sh:27-29),
`custom:commit` (same), `custom:validate` (bundleId), `custom:remove-finalize` (bundleId), `dot:test|dot:apply|dot:delete`
(bundle `dns-over-tls`, body = `$BRORAY_DOT_API_REQUEST_FILE`), `dot:delete-preview` (bundle `dns-over-tls`, no body).
Not locked: `custom-list`, `custom-status`, `custom-summary`, `dot-status` (any form). `dot-auto-settings` uses a
different job (`broray_job_begin system dot:auto-settings dns-over-tls USER protected`, dot-auto-settings.cgi:21).

Go daemon: one global mutex/queue is sufficient for the user-visible contract: if any conflicting operation is
active → 409 with the exact code/message above. Also the user-import resource lock
`/opt/broray/routes/locks/operation.lock` (UI:14, 90-96) → CLI error `ROUTES_BUSY` → 409 (CC:133).

### 0.5 Async model
**Every endpoint in this area is synchronous.** No operation id, no job polling, no cancel endpoint. The JS waits for
the HTTP reply. Timeouts on the JS side: custom preview/commit/validate 180000 ms (`REQUEST_TIMEOUT_MS`, RCJ:6,
612, 941, 964) → rejects with "Операция не завершилась за отведённое время." (RCJ:82); custom-remove/summary and all
DoT calls have no JS timeout, except the pre-delete `dot-status?force=1` read: 15000 ms → "Роутер не ответил на проверку перед удалением." (DJ:527-532).
Long route installs for custom bundles go through the shared routes engine (preflight/export/delete/resume + 
`operation-status.cgi`) — documented in the routes area, see §3.

---

## Part A — Custom routes (user BAT sets)

### A.0 Endpoint table

| # | URL | Method | Lock | Used by JS |
|---|---|---|---|---|
| A1 | `/api/routes/custom-summary.cgi` | GET | none | yes (RCJ:554) |
| A2 | `/api/routes/custom-preview.cgi` | POST JSON | custom:preview | yes (RCJ:939) |
| A3 | `/api/routes/custom-commit.cgi` | POST JSON | custom:commit (+user-import resource lock) | yes (RCJ:962) |
| A4 | `/api/routes/custom-validate.cgi?bundleId=ID` | POST | custom:validate | yes (RCJ:814) |
| A5 | `/api/routes/custom-remove.cgi?bundleId=ID` | POST | custom:remove-finalize (+resource lock) | yes (RCJ:776) |
| A6 | `/api/routes/custom-list.cgi` | GET | none | **no** (not referenced by any JS) |
| A7 | `/api/routes/custom-status.cgi?bundleId=ID` | GET | none | **no** (not referenced by any JS) |

**Per-item add/remove of individual routes: NOT PRESENT.** There is no endpoint, CLI verb or JS code that adds or
removes a single route inside a user set. The unit is the *bundle* (1–16 BAT files). "Replace" = new preview +
commit with `bundleId` (whole content replaced). "Remove" = delete the whole bundle card. Router installation/
removal of the bundle's routes is done by the shared routes engine (§3).

### A.0.1 Bundle id rules (query string)
`bundleId` is taken from the **raw** `QUERY_STRING`, which must be exactly `bundleId=<id>` — no URL decoding, no
other parameters (CC:29-65). `<id>` = `user-` + one or more of `[a-z0-9_-]`, total length ≤ 63.
* Query not starting with `bundleId=` → `400 ROUTES_BUNDLE_REQUIRED` "Не указан идентификатор пользовательского набора." (CC:36-39)
* bad prefix/charset/length → `400 ROUTES_BUNDLE_INVALID` "Некорректный идентификатор пользовательского набора." (CC:48-61)
The same rule is enforced again in CLI `broray_route_user_cli_enter` (lib/route-job.sh:40-43) and `broray_user_routes_id_valid` (UI:60-76).

### A.0.2 CLI error → HTTP mapping (CC:67-147)
The CLI writes `BRORAY_ERROR:<CODE>:<message>` as stderr line 1. `code` = text up to first `:`; message = rest.
`details` = stderr lines 2..30 joined by `\n`. If line 1 is not of that form: code `CUSTOM_ROUTES_OPERATION_FAILED`,
message "Операция с пользовательскими маршрутами завершилась ошибкой.", details = line1 + lines 2..30.
HTTP: `ROUTES_BUSY`→409, `BUNDLE_NOT_FOUND`→404, `DEPENDENCY_MISSING|MODULE_UNAVAILABLE|INDEX_INVALID|RUNTIME_PREPARE_FAILED`→500,
**everything else →400** (CC:131-138). If CLI exits 0 but stdout is not a JSON object → `500 CUSTOM_ROUTES_RESPONSE_INVALID`
"Модуль пользовательских маршрутов вернул некорректный ответ." details = raw stdout (CC:94-103).
Missing library → `MODULE_UNAVAILABLE` "Модуль пользовательских маршрутов недоступен." (bin/broray-routes-user:24-27).

JS shows errors via `globalMessage(error)` = `message + " " + details` (RCJ:605-607) in a toast or in the upload
modal error box ("Файл отклонён" + text, RCJ:912-915). So `details` text is user-visible and must be reproduced.

---

### A1. `GET /api/routes/custom-summary.cgi`
No params. No lock. Calls `broray_routes_page_summary custom` (`lib/routes-page-summary.sh:317-560`), which reads
`routes/custom.json`, `routes/state/<id>.json`, `routes/installed/bundles/<id>.json`, `routes/installed/routes.json`,
`routes/config.json`, `/opt/broray/config/interface.json`, the global lock state and progress files, and one Keenetic
static-route snapshot (`broray_routes_config_snapshot`, cache TTL 30 s via `BRORAY_ROUTES_PAGE_CONFIG_TTL`, :341-352).
This function is shared with the catalog routes page (scope `catalog`); only `scope:"custom"` is described here.

Bundle ids = `custom.json.bundles[].id` filtered by `[a-z0-9_-]{1,63}` (:45-57), in index order.

Errors: `500 ROUTES_PAGE_SUMMARY_UNAVAILABLE` "Модуль сводки маршрутов недоступен." (cgi:28);
`500 ROUTES_CUSTOM_SUMMARY_FAILED` "Не удалось получить состояние пользовательских маршрутов." details = last 30 stderr lines (:32-34);
`500 ROUTES_CUSTOM_SUMMARY_INVALID` "Модуль маршрутов вернул некорректную сводку пользовательских наборов." when not
`{schemaVersion:1, scope:"custom", bundles:array, totals:object}` (:35-36).

Success `data` (routes-page-summary.sh:493-553):
```json
{
  "schemaVersion": 1, "scope": "custom", "generatedAt": "2026-11-02T12:00:00+0300",
  "managedInterface": "Proxy0", "managedInterfaceDisplay": "BROray", "managedMetric": 1200,
  "routerSnapshot": {"available": true, "source": "running-config", "checkedAt": "2026-11-02T11:59:50+0300", "routeCount": 412},
  "globalOperation": {"active": false, "pending": false, "resumable": false, "pid": null, "scope": null, "action": null,
                      "bundleId": null, "startedAt": null, "updatedAt": null, "stale": false},
  "operation": {"schemaVersion": 1, "generatedAt": "…", "canStop": false, "active": false, "pending": false,
                "resumable": false, "running": false, "bundleId": null, "bundleType": null, "bundleName": null,
                "action": null, "globalOperation": {…}, "progress": {…idle progress…}},
  "totals": {"bundleCount": 1, "installedCount": 0, "verifiedInstalledCount": 0, "recordedInstalledCount": 0,
             "registeredCount": 0, "attentionCount": 1, "expectedRouteCount": 0, "presentRouteCount": 0,
             "uniqueRouteCount": 0, "sharedOwnershipCount": 0},
  "health": {"schemaVersion": 1, "module": "routes", "availability": "available", "severity": "warning",
             "operational": true, "consistent": false, "actionRequired": true,
             "freshness": {"state": "fresh", "checkedAt": "…"},
             "reasons": [{"code": "KEENETIC_UPDATE_REQUIRED", "message": "Набор «Discord» требует установки или обновления в Keenetic.", "details": "user-3f2a9c0d1e4b5a6c"}],
             "facts": {"bundleCount": 1, "verifiedInstalledCount": 0, "recordedInstalledCount": 0, "attentionCount": 1, "presentRouteCount": 0},
             "lastOperation": {…same as operation…}},
  "bundles": [{
    "schemaVersion": 1, "bundleId": "user-3f2a9c0d1e4b5a6c", "status": "downloaded", "routeCount": 21,
    "availableVersion": {"sourceCommit": "<canonical sha>", "sourceDate": "…", "contentSha256": "<export sha>", "sourceFiles": [ … ]},
    "downloadedVersion": {"sourceCommit": "…", "sourceDate": "…", "contentSha256": "…"},
    "installedVersion": null, "lastCheckedAt": "…", "lastVerifiedAt": null, "lastDownloadedAt": "…",
    "lastExportedAt": null, "lastDeletedAt": null, "lastError": null,
    "checkResult": {…}, "verifyResult": null, "downloadResult": {…}, "exportBuild": null, "exportResult": null,
    "deleteResult": null, "customImport": {…preview report, see A2…}, "updatedAt": "…",
    "id": "user-3f2a9c0d1e4b5a6c",
    "metadata": {"id": "user-3f2a9c0d1e4b5a6c", "name": "Discord", "description": "Пользовательский набор из загруженных BAT-файлов.",
                 "sourceProvider": "local-upload", "canonicalRouteCount": 38, "exportRouteCount": 21, "sourceFileCount": 1,
                 "createdAt": "…", "updatedAt": "…"},
    "routerPresence": {"available": true, "registered": false, "source": "running-config", "checkedAt": "…",
                       "cacheAgeSeconds": 10, "expectedRouteCount": 0, "presentRouteCount": 0, "missingRouteCount": 0,
                       "duplicateRouteCount": 0, "complete": null, "actualInstalled": false, "drift": null,
                       "status": "not_registered", "missingRoutes": []},
    "ownership": {"uniqueRouteCount": 0, "sharedRouteCount": 0, "totalOwnedRouteCount": 0},
    "recordedInstalled": false, "verifiedInstalled": false, "sourceUpdateAvailable": false,
    "keeneticUpdateRequired": true, "actionRequired": true, "attention": true,
    "operationProgress": {"schemaVersion": 2, "kind": "routes", "bundleId": "user-…", "operation": null, "phase": "idle",
        "current": 0, "total": 0, "percent": 0, "currentRoute": null, "message": "Операция не выполняется.",
        "running": false, "success": null, "rolledBack": false, "resumable": false, "stopRequested": false,
        "stoppedByUser": false, "resumed": false, "errorRoute": null, "pid": null, "startedAt": null,
        "updatedAt": null, "completedAt": null},
    "globalOperation": {…}
  }]
}
```
Per-bundle field set: projected state fields (:215-237) + the overlay at :461-491. If a state file is missing, the
fallback state is `{bundleId, status:"not_checked", routeCount:0}` (:415).

Algorithms (routes-page-summary.sh):
* expected key from registry `routeKeys` entry `a|dest|iface|…|metric:N` → `dest|iface|N` (metric default 1200) (:392-395);
  actual key `destination|interface|metric(default 1000)` over routes with gateway `0.0.0.0` and proto static (:396-412).
* `registered` = registry exists ∧ installedVersion≠null ∧ expectedCount>0 (:423). `complete` = expected>0 ∧ no missing ∧ no duplicates (:432).
* `drift` = registered ∧ snapshot available ∧ ¬complete (:433). `actualInstalled`/`status` (:477-479).
* `keeneticUpdateRequired` = downloadedVersion≠null ∧ (installedVersion=null ∨ versions differ) (:431); `version_equal` compares
  contentSha256, else sourceSetSha256, else sourceCommit+sourceDate (:382-391).
* `attention` = lastError≠null ∨ drift ∨ downloadRequired ∨ keeneticUpdateRequired ∨ (verifyResult filtered to null ∧ downloadedVersion≠null) (:458).
  NOTE: for a freshly committed custom bundle `verifyResult` is null → `attention` is always true until a verify exists.
* `operationProgress` is the real progress only when `operation.bundleId == id`, else idle (:489).
* health.severity: error if any lastError; `busy` if `globalOperation.running==true` — that field is never produced by
  `broray_routes_api_lock_read_json`, so "busy" is effectively unreachable (:518) — reproduce as-is.

JS reads (RCJ):
* `totals.installedCount|attentionCount|bundleCount` (:495-497); `operation.active|bundleName|bundleId|resumable` (:494-511);
  `globalOperation.active` (+`bundleId|pending|resumable|action`) (:576-578, :188-199).
* per bundle: `id`, `metadata.name|description|canonicalRouteCount|exportRouteCount|sourceFileCount|createdAt|updatedAt` (:561-574);
  `routeCount` (:568, 397, 428); `routerPresence.available|registered|actualInstalled|drift|expectedRouteCount|presentRouteCount` (:108-130, 287-294, 383);
  `operationProgress.running|resumable|stopRequested|operation|message|current|total` (:112-116, 171-185, 203-218, 247-251, 283);
  `installedVersion`, `downloadedVersion.contentSha256|sourceCommit` (:101-106, 132-135); `lastError` (string or `{message}`) (:257, 284-286);
  `customImport.sourceFiles[].originalName|name|routeLineCount|routeCount|sizeBytes|sha256`, `.exportRouteCount|canonicalRouteCount|sourceRouteLineCount|normalizedNetworkCount|duplicateCount|broadRouteCount|warning` (:281, 299, 360, 382, 397, 425-445);
  `ownership.uniqueRouteCount|sharedRouteCount` (:433-434); `lastCheckedAt` (:435); `exportBuild.targetInterfaceDisplay|managedMetric`, `checkResult.managedInterfaceDisplay` (:436-438).
* Polling: passive reload every 30000 ms when the tab is visible and nothing is busy (RCJ:9, 978-993); reload on
  `visibilitychange` and on `broray:routes-operation-finished` (RCJ:1016-1028). Concurrent loads are coalesced (RCJ:552).

---

### A2. `POST /api/routes/custom-preview.cgi` — BAT import preview
Body (JSON object, ≤ 4 MiB):
```json
{"name": "Discord", "files": [{"name": "discord.bat", "contentBase64": "cm91dGUgYWRk…"}]}
```
JS sends `{name: <trimmed input>, files: [{name: file.name, contentBase64: btoa(bytes)}]}` (RCJ:938-941, 829-839).
JS pre-checks (RCJ:829-835): ≥1 file "Выберите хотя бы один BAT-файл."; ≤16 "Разрешено не более 16 BAT-файлов.";
sum of sizes ≤ 2 MiB "Общий размер файлов превышает 2 МБ."; every name `/\.bat$/i` "Разрешены только файлы с расширением .bat.";
empty name → "Укажите название карточки." (RCJ:934). Input `maxlength=80` (RCJ:852).

Server validation, in order (`broray_user_routes_preview`, UI:564-857):
1. `prepare_runtime` (UI:98-156): mkdir `routes/{catalog,manifests,state,installed/bundles,backup,tmp}`, previews dir
   `routes/tmp/user-previews` (chmod 700), `/opt/broray/tmp` — failure `RUNTIME_PREPARE_FAILED` "Не удалось подготовить каталоги пользовательских маршрутов." (500);
   create `routes/custom.json` = `{"schemaVersion":1,"bundles":[]}` if missing (failure `INDEX_CREATE_FAILED` "Не удалось создать реестр пользовательских наборов.", 400);
   validate index (schema 1, ids valid, names non-empty strings, unique ids) else `INDEX_INVALID` "Реестр пользовательских маршрутов повреждён." (500).
2. delete preview dirs whose `report.json.createdEpoch` is older than 1800 s (UI:158-175).
3. shape: object, `name` string, `files` non-empty array of `{name:string, contentBase64:string}` else
   `REQUEST_INVALID` "Некорректный запрос загрузки BAT-файлов." (UI:571-583).
4. name: `.name`, must be a single line (multi-line → awk exit 2 → generic `CUSTOM_ROUTES_OPERATION_FAILED`), trimmed
   of leading/trailing whitespace; 1..80 (`${#name}`; byte-vs-character counting in BusyBox ash is **UNCONFIRMED**),
   no control chars in C locale → else `NAME_INVALID` "Название набора должно содержать от 1 до 80 печатных символов." (UI:530-562, 585-589).
5. file count ≤ 16 (`BRORAY_USER_ROUTES_MAX_FILES`) else `TOO_MANY_FILES` "Разрешено не более 16 BAT-файлов в одном наборе." (UI:591-595).
6. per file i (0-based), in order:
   * name non-empty and ≤160 (`${#}`) else `FILENAME_INVALID` "Некорректное имя загружаемого файла."; contains `/` or `\` → `FILENAME_INVALID` "Имя файла не должно содержать путь." (UI:624-635)
   * lower-case name must end `.bat` else `FILE_EXTENSION_INVALID` "Разрешены только файлы с расширением .bat." (UI:637-645)
   * `base64 -d` failure → `FILE_DECODE_FAILED` "Не удалось декодировать файл <name>." (UI:650-653)
   * 0 bytes → `FILE_EMPTY` "Файл <name> пуст." (UI:665-668)
   * running total decoded bytes > 2097152 → `SOURCE_TOO_LARGE` "Общий размер файлов превышает 2 МБ." (UI:670-674)
   * contains NUL byte → `FILE_NOT_TEXT` "Файл <name> содержит нулевые байты и не является текстовым BAT-файлом." (probe failure: "Не удалось безопасно подтвердить текстовый формат файла <name>.") (UI:676-690)
   * parse (A.R1). Errors → `FILE_REJECTED` with stderr line 1 = "Файл <name> не прошёл безопасную проверку. <first error line>"
     and further lines (details) = remaining up to 12 error lines `Строка N: <reason>` (UI:696-704). If the file
     has no route lines and no errors → message "Файл <name> не прошёл безопасную проверку. В файле не найдено ни одной допустимой команды route add."
7. total route lines > 100000 → `TOO_MANY_ROUTES` "Количество строк маршрутов превышает лимит 100000." (UI:724-727)
8. canonical = `LC_ALL=C sort -u` of all `net/prefix` lines; 0 → `NO_ROUTES` "В загруженных файлах не найдено маршрутов."; > 100000 →
   `TOO_MANY_ROUTES` "Количество уникальных маршрутов превышает лимит 100000." (UI:729-744)
9. CIDR collapse (A.R2); failure `CIDR_OPTIMIZE_FAILED` "Не удалось безопасно оптимизировать CIDR для экспорта." (UI:746-749)
All codes in this list → HTTP 400 except those listed in A.0.2.

Side effects: writes `/opt/broray/routes/tmp/user-previews/<token>/{normalized.txt,optimized.txt,source-files.json,report.json,source/01.bat…}`
(files 0600, dir 0700) (UI:766-852). Temp work dir `routes/tmp/user-preview-build-<pid>` removed. No Keenetic access.

Token: `sha256( createdEpoch\n pid\n name\n normalizedSha\n optimizedSha\n <metadata tsv lines> )` → 64 hex (UI:759-764).
Any unpredictable 64-hex token is acceptable for the Go version as long as commit binds to the stored preview.

Success `data` = the report (UI:799-843):
```json
{"schemaVersion":1,"token":"9f2c…(64 hex)","name":"Discord","createdAt":"2026-11-02T12:00:00+0300",
 "createdEpoch":1793610000,"expiresInSeconds":1800,"fileCount":1,"totalBytes":2048,
 "sourceRouteLineCount":40,"canonicalRouteCount":38,"exportRouteCount":21,"normalizedNetworkCount":3,
 "duplicateCount":2,"rejectedLineCount":0,"broadRouteCount":0,"broadRoutes":[],
 "normalizedSha256":"<sha256 of canonical list file>","exportSha256":"<sha256 of optimized list file>",
 "sourceFiles":[{"storedName":"01.bat","originalName":"discord.bat","parser":"windows-route-bat",
   "sha256":"<sha256 of raw bytes>","sizeBytes":2048,"routeLineCount":40}],
 "ready":true,"warning":null}
```
`warning` when `broadRouteCount>0`: "Набор содержит очень широкие сети /7–/8. Через Xray может направляться значительная часть интернет-трафика." (UI:837-841; note the check is prefix ≤ 8, i.e. /1…/8, despite the text).
Timestamps use `date '+%Y-%m-%dT%H:%M:%S%z'` (offset without colon) throughout UI/RD.
List files: lines `a.b.c.d/p\n`, sha256 is over file bytes including the trailing newline.

JS reads: `token` (RCJ:963), `sourceRouteLineCount, canonicalRouteCount, exportRouteCount, normalizedNetworkCount, duplicateCount, broadRouteCount, warning` (RCJ:917-924). Changing the name or files resets the preview (RCJ:871-872).

#### A.R1 BAT parser (UI:180-336) — exact rules
Per line: strip trailing `\r`, trim spaces/tabs. Skip if empty, starts with `#` or `::`, or (case-insensitive) matches
`^rem([ \t]|$)`, `^@?echo([ \t]|$)`, `^chcp…`, `^title…`, `^pause…`, `^cls…`, `^setlocal…`, `^endlocal…`, `^exit[ \t]+/b([ \t]|$)`.
Otherwise split on `[ \t]+`:
* need ≥6 fields with f1=`route`, f2=`add`, f4=`mask` (case-insensitive) else error "разрешена только команда route add <IPv4> mask <маска> 0.0.0.0"
* if >6 fields: must be ≥8 with f7=`&` and f8=`rem` (ci) else "после маршрута разрешён только комментарий & rem"
* f3 IPv4: 4 dot-separated all-digit octets 0..255 (leading zeros allowed) else "некорректный IPv4-адрес сети"
* f5 mask: each octet ∈ {255,254,252,248,240,224,192,128,0}, contiguous → prefix; else "некорректная или непоследовательная маска"
* f6 must equal the string `0.0.0.0` else "разрешён только шлюз 0.0.0.0"
* network = ip AND mask (per octet `int(o/block)*block`); `0.0.0.0/0` → "маршрут по умолчанию 0.0.0.0/0 запрещён"
* output `network/prefix`; `normalizedLines++` when the network string differs from the original text (so `010.0.0.0` counts).
Error line text: `Строка <NR>: <reason>` (NR counts every physical line). Only first 20 kept; any error → file rejected (exit 1),
even if valid routes exist; zero route lines and no errors → exit 2.

#### A.R2 CIDR collapse (UI:341-439)
1. Drop every network covered by a broader kept network (process prefixes 0→32).
2. For p = 32 down to 2: merge exact sibling pairs (left aligned on 2·block, right = left+block) into p−1. (So nothing merges to /0.)
3. Output sorted by numeric network then prefix (`%010.0f\t%02d` + `LC_ALL=C sort`), printed as dotted `net/prefix`.
`exportRouteCount` = collapsed count; `canonicalRouteCount` = unique pre-collapse count; `duplicateCount` = total route lines − unique.

---

### A3. `POST /api/routes/custom-commit.cgi` — create or replace a bundle
Body: `{"token":"<64 hex>","name":"Discord","bundleId":null|"user-…"}` (JS RCJ:963 sends `bundleId: replaceBundleId` = null for create).
Validation (`broray_user_routes_commit`, UI:923-1343):
* shape: object, token string, name string, bundleId null or string else `REQUEST_INVALID` "Некорректный запрос сохранения пользовательского набора." (UI:929-937)
* token 64 lowercase hex else `TOKEN_INVALID` "Некорректный токен предварительной проверки." (UI:943-944)
* name rule A2.4 else `NAME_INVALID` "Некорректное название пользовательского набора." (UI:945-946). The committed name is the request's name, not the preview's.
* preview dir must exist and verify (all files present, source BAT sha/size match `source-files.json`, stored names `NN.bat`,
  report `ready:true`, list sha256 match) else `PREVIEW_INVALID` "Предварительная проверка отсутствует, истекла или повреждена." (UI:859-887, 948-952)
* age > 1800 s → `PREVIEW_EXPIRED` "Срок предварительной проверки истёк. Проверьте файлы повторно." (UI:954-960)
* `bundleId` given: invalid → `BUNDLE_INVALID` "Некорректный идентификатор набора."; absent from index → `BUNDLE_NOT_FOUND` (404) "Пользовательский набор не найден." (UI:963-968)
* new id: `user-` + token[0:16]; while taken → `user-` + token[0:12] + `-N`, N=1,2,… (UI:969-976)
* resource lock `routes/locks/operation.lock` busy → `ROUTES_BUSY` (409) "Другая операция с маршрутами уже выполняется."; other failure `LOCK_FAILED` "Не удалось установить блокировку маршрутов." (UI:978-992)
* `routes/config.json.managedInterface` must match `Proxy[0-9]*` else `ROUTES_CONFIG_INVALID` "Некорректный управляемый интерфейс Keenetic." (UI:1002-1010)
* any staged-write failure → rollback all targets → `COMMIT_FAILED` "Не удалось атомарно сохранить пользовательский набор; прежнее состояние восстановлено." (UI:1302-1312)

Storage written (transactional, backed up and restored on failure, UI:1259-1312):
| path | content |
|---|---|
| `/opt/broray/routes/catalog/<id>/source/NN.bat` | raw uploaded bytes (0600) |
| `…/catalog/<id>/canonical.txt` | canonical (unique, uncollapsed) list |
| `…/catalog/<id>/normalized.txt` | **collapsed** export list (naming per export contract, UI:1020-1024) |
| `…/catalog/<id>/source-files.json`, `import-report.json` | from preview |
| `…/catalog/<id>/manifest.json` and `/opt/broray/routes/manifests/<id>.json` | `{schemaVersion:1,id,name,description:"Пользовательский набор из загруженных BAT-файлов.",source:{provider:"local-upload",files:[…]},targetInterface:"ProxyN",exportComment:"BROray",limits:{maxSourceBytes:2097152,maxRoutes:100000},createdAt,updatedAt}` (UI:1029-1053) |
| `…/catalog/<id>/routes.json` | `{schemaVersion:1,bundleId,targetInterface,routeComment:"BROray",contentSha256:<export sha>,routeCount,routes:[{family:"ipv4",network,prefix}]}` (UI:496-528) |
| `…/catalog/<id>/version.json` | `{schemaVersion:1,bundleId,sourceCommit:<canonical sha>,sourceDate:now,contentSha256:<export sha>,routeCount,canonicalRouteCount,sourceFileCount,sourceFiles,downloadedAt:now,targetInterface,sourceProvider:"local-upload"}` (UI:1060-1085) |
| `/opt/broray/routes/state/<id>.json` | new: full object UI:1136-1189 (`status:"downloaded"`, `installedVersion:null`, `checkResult.result:"local_validated"`, `downloadResult.result:"local_saved"`, `customImport:<report>`…); replace: existing state with those fields overwritten, **installedVersion kept** (UI:1089-1134) |
| `/opt/broray/routes/installed/bundles/<id>.json` | kept if exists, else `{schemaVersion:1,bundleId,installedVersion:null,routeKeys:[],managedRouteKeys:[],externalRouteKeys:[],targetInterface,managedMetric:1200,installedAt:null,removedAt:null,updatedAt}` (UI:896-921) |
| `/opt/broray/routes/bundles.json` | id appended to `.bundles` if absent (UI:1217-1227) |
| `/opt/broray/routes/custom.json` | entry replaced/appended `{id,name,description,sourceProvider:"local-upload",canonicalRouteCount,exportRouteCount,sourceFileCount,createdAt(kept),updatedAt}` (UI:1192-1215) |
Then the preview dir is deleted (single-use token). No Keenetic commands.

Success `data` (UI:1323-1342):
```json
{"bundleId":"user-9f2c4b1a7d3e5f60","name":"Discord","replaced":false,"canonicalRouteCount":38,
 "exportRouteCount":21,"sourceFileCount":1,"status":"downloaded","readyForExport":true,"updatedAt":"2026-11-02T12:01:00+0300"}
```
JS ignores the body; toast "BAT-файлы заменены. Набор готов к обновлению." / "Пользовательский набор создан." then reloads summary (RCJ:964-975).

---

### A4. `POST /api/routes/custom-validate.cgi?bundleId=<id>`
Body: JS sends `{}` (`body:{}` → JSON "{}", RCJ:612); not read by the CGI (custom-validate.cgi). Lock `custom:validate`.
CLI `validate` (UI:1366-1475): prepare_runtime; id/bundle checks (`BUNDLE_INVALID`, `BUNDLE_NOT_FOUND`→404); then integrity —
all of `catalog/<id>/{import-report.json,canonical.txt,normalized.txt,routes.json,version.json,source-files.json,source/}` and `state/<id>.json` readable
else `BUNDLE_DAMAGED` "Файлы пользовательского набора отсутствуют."; source BAT sha/size → `BUNDLE_DAMAGED` "Исходные BAT-файлы пользовательского набора изменены или отсутствуют.";
report shape → "Отчёт импорта пользовательского набора повреждён."; state shape → "Состояние пользовательского набора повреждено.";
sha + line counts → "Контрольная сумма или количество маршрутов пользовательского набора не совпало."; routes.json → "Каталог экспорта пользовательского набора повреждён.";
version.json → "Сведения о версии пользовательского набора повреждены." (all 400).
Side effect: state `lastCheckedAt=now, lastError=null, checkResult={result:"local_validated",message:"Исходные и нормализованные файлы пользовательского набора проверены.",sourceFiles,canonicalRouteCount,exportRouteCount}, customImport=<report>, updatedAt` (UI:1453-1472). No Keenetic access.
Success `data` = import-report.json + `"validatedAt": now` (UI:1474).
JS: stores data into `states[id]` (RCJ:613), toast "Пользовательский набор проверен.", then reloads summary (overwrites it). Error → toast `globalMessage`.

---

### A5. `POST /api/routes/custom-remove.cgi?bundleId=<id>` — delete local card
Body `{}` (RCJ:776), not read. Sequence (custom-remove.cgi):
1. lock `custom:remove-finalize` (:21-33).
2. `routes/operations/<id>.json` has `running==true or resumable==true` → `409 ROUTES_CUSTOM_REMOVE_PROGRESS_PENDING` "Сначала завершите или продолжите текущую операцию с этим набором." (:35-40)
3. requires router delete if state.installedVersion≠null, or registry installedVersion≠null or managedRouteKeys non-empty, or live presence registered ∧ (actualInstalled ∨ presentRouteCount>0) → `409 ROUTES_CUSTOM_REMOVE_REQUIRES_DELETE` "Сначала безопасно удалите маршруты набора из Keenetic, затем удалите локальную карточку." (:42-65)
4. CLI `remove` (UI:1477-1594): `BUNDLE_INVALID`/`BUNDLE_NOT_FOUND`(404); state/registry shape → `BUNDLE_DAMAGED` "Состояние пользовательского набора отсутствует или повреждено." / "Реестр установки пользовательского набора отсутствует или повреждён.";
   (installed path calls `broray-routes delete` → `ROUTER_DELETE_FAILED`, unreachable from the CGI because of step 3); resource lock (`ROUTES_BUSY`/`LOCK_FAILED`).
   Moves `catalog/<id>`, `manifests/<id>.json`, `state/<id>.json`, `installed/bundles/<id>.json` into
   `/opt/broray/routes/backup/user-removed-<id>-<YYYYmmdd-HHMMSS>-<pid>/files/{catalog,manifest,state,registry}`, plus copies of
   `bundles.json`/`custom.json`; removes id from `bundles.json` and `custom.json`. Failure → restore → `REMOVE_FAILED` "Не удалось удалить пользовательский набор; локальное состояние восстановлено.".
   Backups are never pruned by this code (pruning elsewhere: UNCONFIRMED).
Success `data`: `{"bundleId":"user-…","removed":true,"backupPath":"/opt/broray/routes/backup/user-removed-user-…-20261102-120500-1234","removedAt":"…"}` (UI:1581-1591).
JS flow (RCJ:760-800): confirm dialog; if `isInstalled(state) || state.installedVersion || hasDrift(state)` it first runs the
routes engine `delete` (preflight → confirm → `delete.cgi`), and only if that completed and the refreshed state is not installed/
resumable calls custom-remove; toast "Пользовательский набор удалён.".

---

### A6. `GET /api/routes/custom-list.cgi` (unused by JS)
No lock. If `routes/custom.json` absent → `{"schemaVersion":1,"bundles":[]}` (no files created). If not a regular file or invalid →
`INDEX_INVALID` (500) "Реестр пользовательских маршрутов повреждён.". Else `{"schemaVersion":1,"bundles":[<custom.json entries verbatim>]}` (UI:1345-1364).

### A7. `GET /api/routes/custom-status.cgi?bundleId=<id>` (unused by JS)
No lock. `404 ROUTES_BUNDLE_NOT_FOUND` "Пользовательский набор маршрутов не найден." if not in custom.json (:21-24);
`500 ROUTES_STATE_INVALID` "Состояние пользовательского набора повреждено." if state file not `{schemaVersion:1,bundleId:id}` (:26-29);
`500 ROUTES_STATE_READ_FAILED` "Не удалось прочитать состояние пользовательского набора." (:72-74).
`data` = `state/<id>.json` + `routerPresence` (from `broray_routes_presence_bundle`, lib/routes-router-presence.sh:20-190; fallback
`{"available":false,"registered":false,"expectedRouteCount":null,"presentRouteCount":null,"missingRouteCount":null,"actualInstalled":null,"drift":null,"status":"unavailable","missingRoutes":[]}`) +
`operationProgress` (progress file or idle object, :41) + `globalOperation` (`broray_routes_api_lock_read_json`, routes-api-operation.sh:190-282; fallback :51).
Presence here (unlike the summary) has `missingRoutes` up to 50 entries `{key,destination,interface,metric,matchCount}`.

### A.R3 How user sets are applied to Keenetic
Not by these endpoints. After commit, the bundle is a normal routes-engine bundle (`bundles.json` + manifest + catalog +
state `status:"downloaded"`). The JS "Установить/Обновить/Восстановить в Keenetic" button calls
`POST /api/routes/preflight.cgi?bundleId=<id>&action=export` → confirm (`preflight.ready`, `.token`, `.checks`, `.summary`,
`.operation`, `.requestedAction`, `.resume`, RCJ:630-669) → `POST /api/routes/export.cgi?bundleId=<id>&preflightToken=<token>`
(JS timeout 8 h, RCJ:7, 718-751); delete/resume analogously. The engine writes `ip route <net> <mask> <ProxyN> 1200` /
`no ip route <net> <mask> <ProxyN>` (lib/routes-router-export.sh:1001,1454; routes-router-delete.sh:492,1184) using
`routes/catalog/<id>/routes.json`, managedInterface from `routes/config.json` (seed `Proxy0`), metric 1200. Contract of those
endpoints belongs to the routes-engine area.

---

## Part B — DNS-over-TLS

### B.0 Endpoint table

| # | URL | Method | Lock | JS |
|---|---|---|---|---|
| B1 | `/api/routes/dot-status.cgi[?force=1]` | GET | none | DJ:446, 533 |
| B2 | `/api/routes/dot-test.cgi` | POST JSON | dot:test | DJ:463-472 |
| B3 | `/api/routes/dot-apply.cgi` | POST JSON | dot:apply | DJ:463-472 |
| B4 | `/api/routes/dot-delete-preview.cgi` | POST (no body) | dot:delete-preview | DJ:544-548 |
| B5 | `/api/routes/dot-delete.cgi` | POST JSON | dot:delete | DJ:463-472, 578-582 |
| B6 | `/api/routes/dot-auto-settings.cgi` | POST JSON + headers | job dot:auto-settings | DJ:654-657 |

**Custom DoT server entry (free address/port/sni/spki/interface/domain): NOT PRESENT.** All write/test requests accept only
`serverIds` from the fixed 8-entry built-in catalog (B.R1); `broray_dot_validate_request` rejects any other id (RD:642-663).
The fields `spki/on(interface)/domain` exist only (a) in the parser for live records, (b) in receipts, and (c) in the command
builder/allowlist (used for rollback re-adds of live records). No UI field exists for them in `dns.html`/`DJ`.

### B.0.1 CLI → HTTP mapping (DC:81-128)
Every B1-B5 success merges `autoCheck` into the CLI JSON: `data = cli_output + {"autoCheck": broray_dot_auto_view}` (DC:102).
Error parse: like A.0.2 but default code `DOT_OPERATION_FAILED` "Операция DNS-over-TLS завершилась ошибкой.", details lines 2..40.
Status: `ROUTES_OPERATION_BUSY`→409; `DOT_TEST_REQUIRED|DOT_LIMIT_EXCEEDED|DOT_RECOVERY_REQUIRED|DOT_PHYSICAL_WRITE_PROTOCOL_REQUIRED|DOT_OBSERVATION_UNDERDETERMINED|DOT_DELETE_AUTHORITY_REFUSED|DOT_DELETE_CONFIRMATION_STALE|DOT_SELECTOR_CONFLICT`→409;
`NDMC_UNAVAILABLE|DEPENDENCY_MISSING|MODULE_UNAVAILABLE|STORAGE_UNAVAILABLE|CONFIG_INVALID|CONFIG_MIGRATION_FAILED|STATE_INVALID|DOT_CATALOG_INVALID|DOT_TEST_FAILED`→500;
`KEENETIC_UNAVAILABLE|KEENETIC_RUNTIME_UNAVAILABLE|DOT_APPLY_FAILED|DOT_DELETE_FAILED`→502; anything else (e.g. `REQUEST_INVALID`,
`DOT_SHARED_WRITE_POLICY_REQUIRED`) → 400 (DC:118-124). Non-object stdout → `500 DOT_RESPONSE_INVALID` "Модуль DNS-over-TLS вернул некорректный ответ." (DC:96-101).
Missing lib → `MODULE_UNAVAILABLE` "Модуль DNS-over-TLS недоступен." (bin/broray-routes-dot:4-8).

### B.0.2 Files
| path | role |
|---|---|
| `/opt/broray/routes/dot/config.json` | selection + receipts (schema 3) |
| `/opt/broray/routes/dot/state.json` | test results, timestamps, lastOperation, autoCheck progress (schema 1) |
| `/opt/broray/routes/dot/auto-check.json` | `{schemaVersion:1,enabled:bool,updatedAt}` |
| `/opt/broray/routes/dot/transaction-recovery-required.json` | transaction marker (presence = recovery required) |
| `/opt/broray/run/dot-status-cache.json` | 30 s status cache `{schemaVersion:1,inputHash,createdEpoch,expiresEpoch,data}` |
| `/opt/var/lib/broray/background-automation.json` | global pause `{paused:bool}` (read only) |
| `/opt/broray/tmp/dot-*.<pid>.*` | temporaries, transaction backups `dot-transaction-{config,state}.<pid>.json` |

`broray_dot_ensure_files` (RD:261-338), run by status/test/apply/preview: creates config if missing/empty:
```json
{"schemaVersion":3,"requestedIds":["google-primary","cloudflare-primary","quad9"],"selectedIds":["google-primary","cloudflare-primary","quad9"],"effectiveIds":[],"migrationState":"none","managed":[],"quarantinedReceipts":[],"updatedAt":null}
```
and state `{"schemaVersion":1,"tests":[],"lastTestedAt":null,"lastAppliedAt":null,"lastDeletedAt":null,"lastOperation":null,"lastError":null,"updatedAt":null}`.
Schema 1/2 config is migrated (RD:206-259): `requestedIds = selectedIds = old.requestedIds // old.selectedIds // []`, `effectiveIds=[]`,
old `managed[]` moved to `quarantinedReceipts[]` as `{reason:"legacy-incomplete-receipt",sourceSchemaVersion,quarantinedAt,receipt}`, `managed=[]`,
`migrationState` = "selection-over-limit" (>8) | "legacy-receipts-quarantined" | "none". Failure → `CONFIG_MIGRATION_FAILED` "Не удалось безопасно обновить схему конфигурации DNS-over-TLS.".
Other schema, `requestedIds != selectedIds`, or any `managed[]` entry not a complete receipt (B.R6) → `CONFIG_INVALID` "Конфигурация DNS-over-TLS повреждена.".
State not `{schemaVersion:1,tests:array}` → `STATE_INVALID` "Состояние DNS-over-TLS повреждено.". Writes are atomic (tmp + mv, chmod 600, RD:196-204).

### B.R1 Catalog (immutable, RD:117-131; validated RD:133-153)
| id | provider | name | address | port | sni |
|---|---|---|---|---|---|
| google-primary | Google | Google 8.8.8.8 | 8.8.8.8 | 853 | dns.google |
| google-secondary | Google | Google 8.8.4.4 | 8.8.4.4 | 853 | dns.google |
| yandex-primary | Yandex DNS | Yandex DNS 77.88.8.8 | 77.88.8.8 | 853 | common.dot.dns.yandex.net |
| cloudflare-primary | Cloudflare | Cloudflare 1.1.1.1 | 1.1.1.1 | 853 | cloudflare-dns.com |
| cloudflare-secondary | Cloudflare | Cloudflare 1.0.0.1 | 1.0.0.1 | 853 | cloudflare-dns.com |
| quad9 | Quad9 | Quad9 9.9.9.9 | 9.9.9.9 | 853 | dns.quad9.net |
| adguard-primary | AdGuard DNS | AdGuard 94.140.14.14 | 94.140.14.14 | 853 | dns.adguard-dns.com |
| adguard-secondary | AdGuard DNS | AdGuard 94.140.15.15 | 94.140.15.15 | 853 | dns.adguard-dns.com |
Each entry also has `effectivePort:853, spki:"", on:"", interface:"", domain:""`. Order is significant (status `servers[]` order).

### B.R2 Keenetic reads (exact commands)
All via `/opt/broray/bin/broray-system-ndmc -c '<cmd>'` (wrapper: unset LD_LIBRARY_PATH/LD_PRELOAD, exec `/bin/ndmc`; RD:13-14).
Any non-empty stderr is treated as failure.
1. `show running-config` (RD:364-484): records are top-level lines `dns-proxy tls upstream …` **or** indented `tls upstream …`
   inside a top-level `dns-proxy` block. DoH lines (`dns-proxy https upstream …` or indented `https upstream …` in the block) are
   **only counted** (`dohCount`), never listed. Tokenise on whitespace: `dns-proxy tls upstream <address> [<port digits>] {sni X|spki X|on X|domain X}`.
   Port absent → `portRaw:""`, `effectivePort:853`, `portState:"omitted"`; present → explicit, must be 1..65535.
   Duplicate key, key without value, unknown token, missing/empty sni, any token containing tab/CR/LF/`"`/`\`, or `:` in the address → `valid:false` / `unknownTokenCount>0`.
   `extraAttributeCount` = count of spki/on/domain present.
   Output `{schemaVersion:2,source:"running-config",parser:"r14c01-strict",dot:[…],dohCount,totalSecure:dot+doh,dotParseErrorCount,selectorCollisionCount,determinate}`,
   `determinate` = no parse errors ∧ no two records with same `[address,effectivePort]`.
2. `show dns-proxy` (RD:486-614): sections `proxy-name: <X>`, `proxy-tls:`, `server-tls:` with `address:`, `port:`, `sni:`, `spki:`,
   `interface:`, `domain:` keys. Only the `System` profile's `proxy-tls:` records are parsed. Determinate iff exactly one `System`
   profile, exactly one System `proxy-tls`, zero unknown schema lines, zero parse errors.
3. `fetch_observed` (RD:616-640) = running-config result + `runtime` + `runtimeReconciled`; overall `determinate` requires both
   determinate, equal counts, and a 1:1 match on (address, effectivePort, sni, spki, interface, domain).
Each record: `{index,address,portRaw,effectivePort,portState,sni,spki,on,interface,domain,extraAttributeCount,unknownTokenCount,valid,parseError}` (`on`==`interface`).

### B.R3 Keenetic writes (exact grammar, RD:910-959, 1276-1306)
Only these strings are ever sent (anything else → `DOT_COMMAND_NOT_AUTHORIZED` "Команда не входит в замороженный R14C01 DoT grammar."):
* `dns-proxy tls upstream <IPv4> <port> sni <sni>[ spki <spki>][ on <iface>][ domain <domain>]` — port always explicit (853 for catalog).
* `no dns-proxy tls upstream <IPv4> <port>`
* `system configuration save`
Values must be non-empty and contain no whitespace, `;`, `"`, `\`. Each write first requires the write policy
(`lib/keenetic-write-policy.sh` dot profile, `BRORAY_DOT_WRITE_PROTOCOL_ENABLED=true`, sha `dc8501…ddfa`, candidate `3.0.0-r15c16`;
RD:27-36, 162-187) → otherwise `DOT_SHARED_WRITE_POLICY_REQUIRED` / `DOT_PHYSICAL_WRITE_PROTOCOL_REQUIRED`
"Изменение DoT заблокировано: в этих bytes не включён замороженный обратимый Keenetic CLI protocol." In this build it is enabled.
Order in apply: all removes, all adds, `system configuration save`, then convergence poll: up to 10 × (`fetch_observed` + 1 s sleep)
until observed == before − removed + added (semantic keys), kept entries keep portRaw/portState, dohCount unchanged, totalSecure matches (RD:1308-1348).

### B.R4 Transactions / recovery (RD:1237-1563)
test/apply/delete write the marker `transaction-recovery-required.json`
`{schemaVersion:1,operation:"test|apply|delete",pid,state:"active",startedAt,recovery:{plan,managed,runningBackup,configBackup,stateBackup}}`
before mutating, back up config/state, and on failure/signal roll back (apply: delete added if present, re-add removed if missing;
delete: re-add removed; then save + 10×1 s verify against pre-state) and restore local files. Marker removed on success or verified
rollback; otherwise it stays and **every** test/apply/delete-preview/delete fails `DOT_RECOVERY_REQUIRED` (409)
"Предыдущая DNS-over-TLS транзакция требует проверки и восстановления." and status reports `recoveryRequired:true`.
No API clears the marker (UNCONFIRMED whether anything outside these files does).
Note: the marker also exists *during* a running test, so a concurrent `dot-status` reports recovery-required/severity error for that period.
Note: re-adds always write an explicit port, so rolling back a record whose original `portState` was `"omitted"` cannot verify (pre-state compares portRaw/portState) → marker remains.

### B.R5 Status classification (`broray_dot_status`, RD:961-1235)
For each live DoT record (`actual.dot[i]`, = running-config records, plus):
* `catalogMatchIds` = catalog ids with same address+effectivePort+sni (only if valid ∧ no unknown tokens).
* `selectorMatchCount` = live records with same address+effectivePort.
* `classification`: `unknown` (parse error) | `selector-collision` (count≠1) | `ambiguous` (>1 catalog) | `catalog-endpoint-with-extra-attributes` (1 catalog but spki/interface/domain non-empty) | `catalog:<id>` | `not-in-broray-catalog`.
* `ownership`: `broray` (exactly one receipt with same writeProtocolSha256, id ∈ catalogMatchIds, full equality incl. portRaw/portState, and selector count 1) | `quarantined` (overlaps a quarantined receipt by address/port(default 853)/sni) | `external`. `managed` = ownership=="broray".
* `inCatalog` = `deleteEligible` = valid ∧ no unknown ∧ selector 1 ∧ exactly 1 catalog match ∧ spki/interface/domain empty.
  (Ownership does **not** restrict delete: external exact catalog entries are deletable.)
* `parseError` boolean.
Server cards `servers[]` = catalog entries + `selected` (id ∈ config.requestedIds), `present` (some inCatalog live record matches id), `managed`, `test` (last `state.tests` entry with same id+address+port+sni+spki+interface+domain, else null).
Derived:
* `testedSelected` = selected with `test.ok` ∧ 0 ≤ now−testedEpoch ≤ **600 s** (`BRORAY_DOT_TEST_TTL`).
* `managedOutsideSelection` = receipts whose id not selected; `selectedMissing` = selected with no inCatalog live match.
* `projectedTotal = totalSecure − |managedOutsideSelection| + |selectedMissing|`; `capacityExceeded = projectedTotal > 8`; `availableSlots = max(0, 8 − totalSecure)`.
* `matchesSelection` = selected>0 ∧ all selected present ∧ no managedOutsideSelection (null when observation not determinate).
* `observationDeterminate` = running read ok ∧ ndmc present ∧ observed.determinate. `installationState` unknown|installed|not-installed; `installed` null|true|false.
* `mutationAvailable` = protocol ∧ running ok ∧ ndmc ∧ ¬recovery ∧ determinate; `mutationBlockedReason` ∈ `physical-write-protocol-required|recovery-required|keenetic-unavailable|dot-observation-underdetermined|null`.
* `deleteEligible` (top-level) = mutationAvailable ∧ selected>0 ∧ no selectedDeleteConflictIds (selected whose selector exists but ≠1 deleteEligible match) ∧ ≥1 selectedDeleteTargetIds;
  `deleteBlockedReason` ∈ above reasons | `selection-required|selected-dot-selector-conflict|selected-dot-record-not-present` | null.
* `drift` = receipts>0 ∧ not all receipts exactly present as deleteEligible+broray.
* severity/consistent/reasons table: RD:1112-1148 (codes `DOT_RECOVERY_REQUIRED`, `KEENETIC_CONFIG_UNAVAILABLE`, `NDMC_UNAVAILABLE`, `DOT_OBSERVATION_UNDERDETERMINED`, `DOT_PHYSICAL_WRITE_PROTOCOL_REQUIRED`, `DNS_SELECTION_OVER_LIMIT`, `DNS_CAPACITY_EXCEEDED`, `DNS_SELECTION_REQUIRED`, `DNS_TEST_REQUIRED` "Проверено X из Y выбранных серверов.", `DNS_APPLY_REQUIRED`; messages verbatim there).
* `configurationState` (RD:1192-1202): recovery-required|unavailable|observation-unknown|selection-over-limit|selection-required|limit-exceeded|matches-selection|test-required|apply-required.
If the router read fails the observed object is the "unavailable" stub (RD:977-983) — status still succeeds (HTTP 200) with `runningConfigAvailable:false`.

### B.R6 Receipts (config.json `managed[]`, RD:1687-1707; validated RD:300-319)
```json
{"receiptSchemaVersion":1,"id":"cloudflare-primary","provider":"Cloudflare","address":"1.1.1.1","portRaw":"853",
 "effectivePort":853,"portState":"explicit","sni":"cloudflare-dns.com","spki":"","on":"","interface":"","domain":"",
 "catalogIdentity":{"address":"1.1.1.1","effectivePort":853,"sni":"cloudflare-dns.com"},
 "selector":{"address":"1.1.1.1","effectivePort":853},"observedAt":"2026-11-02T12:03:00+0300",
 "writeProtocolSha256":"dc8501856b27260738828e9959e10512496410ff44adfceaa8564056bed1ddfa"}
```
Built from the verified live record for **every** desired id after apply — including desired ids that already existed as identical
external records (`reusedExternal`), i.e. apply adopts them, although the JS confirm text says third-party records are not taken over (DJ:512).
Config after apply: `requestedIds=selectedIds=serverIds`, `effectiveIds=unique(receipt ids)`, `migrationState:"none"`, `managed=<receipts>`.

---

### B1. `GET /api/routes/dot-status.cgi[?force=1]`
`[ -x bin/broray-routes-dot ]` else `500 DOT_CLI_UNAVAILABLE` "Команда DNS-over-TLS недоступна." (dot-status.cgi:7).
Query: if `&QUERY_STRING&` contains `&force=1&` → action `status` (fresh), else `status-cached` (dot-status.cgi:8-11).
`status-cached` (RD:852-908): `ensure_files`; inputHash = sha256 of `sha256sum config.json state.json` output; if cache file is a
non-symlink with `schemaVersion==1 ∧ inputHash equal ∧ expiresEpoch>now` → return `.data`; else compute status and store with
`expiresEpoch = now+30`. `status` (force) computes but does **not** write the cache. Both read Keenetic twice (B.R2) on miss.
Side effects: may create/migrate config/state.
Success `data` (RD:1149-1232 + autoCheck):
```json
{"schemaVersion":5,"maxServers":8,"maxServersSource":"Официальный максимум Keenetic: до 8 DoT/DoH-серверов",
 "runningConfigFormat":"flat-or-dns-proxy-block","runningConfigAvailable":true,"ndmcAvailable":true,"testAvailable":true,
 "supported":true,"recoveryRequired":false,
 "writeContract":{"candidateId":"3.0.0-r15c16","protocolEnabled":true,"protocolSha256":"dc85…ddfa"},
 "writeProtocolEnabled":true,"mutationAvailable":true,"mutationBlockedReason":null,
 "deleteEligible":true,"deleteBlockedReason":null,
 "requestedIds":["cloudflare-primary","quad9"],"effectiveIds":["cloudflare-primary","quad9"],
 "selectedIds":["cloudflare-primary","quad9"],"selectedCount":2,"effectiveCount":2,"selectionOverLimit":false,
 "migrationState":"none","selectedTestedCount":2,"selectedPresentCount":2,"missingSelectedCount":0,
 "projectedTotal":4,"capacityExceeded":false,"availableSlots":4,
 "managed":[{…receipt…},{…receipt…}],"quarantinedReceipts":[],"managedPresentCount":2,"externalDotCount":1,
 "servers":[{"id":"google-primary","provider":"Google","name":"Google 8.8.8.8","address":"8.8.8.8","port":853,
   "effectivePort":853,"sni":"dns.google","spki":"","on":"","interface":"","domain":"",
   "selected":false,"present":false,"managed":false,"test":null}, …8 entries…],
 "actual":{"dot":[{"index":1,"address":"1.1.1.1","portRaw":"853","effectivePort":853,"portState":"explicit",
     "sni":"cloudflare-dns.com","spki":"","on":"","interface":"","domain":"","extraAttributeCount":0,
     "unknownTokenCount":0,"valid":true,"parseError":false,"catalogMatchIds":["cloudflare-primary"],"inCatalog":true,
     "classification":"catalog:cloudflare-primary","managed":true,"ownership":"broray","deleteEligible":true,
     "selectorMatchCount":1},
    {"index":3,"address":"208.67.222.222","portRaw":"","effectivePort":853,"portState":"omitted","sni":"dns.opendns.com",
     "spki":"","on":"","interface":"","domain":"","extraAttributeCount":0,"unknownTokenCount":0,"valid":true,
     "parseError":false,"catalogMatchIds":[],"inCatalog":false,"classification":"not-in-broray-catalog",
     "managed":false,"ownership":"external","deleteEligible":false,"selectorMatchCount":1}, …],
   "dohCount":1,"totalSecure":4,"determinate":true,"runtimeReconciled":true,
   "runtime":{"schemaVersion":1,"source":"show-dns-proxy","parser":"r14c01-strict","proxyTlsSections":1,
     "systemProxyCount":1,"systemProxyTlsSections":1,"policyProxyTlsSections":0,"unknownSchemaCount":0,
     "dot":[…],"parseErrorCount":0,"determinate":true}},
 "installed":true,"installationState":"installed","observationState":"determinate","matchesSelection":true,
 "drift":false,"configurationState":"matches-selection",
 "health":{"schemaVersion":1,"module":"dns","availability":"available","severity":"ok","operational":true,
   "consistent":true,"actionRequired":false,"freshness":{"state":"fresh","checkedAt":"2026-11-02T09:05:00Z"},
   "reasons":[],"facts":{"requestedCount":2,"effectiveCount":2,"managedCount":2,"managedPresentCount":2,
   "quarantinedReceiptCount":0,"maxServers":8,"projectedTotal":4},"lastOperation":{…}},
 "tests":[{"id":"cloudflare-primary","address":"1.1.1.1","effectivePort":853,"sni":"cloudflare-dns.com","spki":"",
   "interface":"","domain":"","ok":true,"status":"ok","latencyMs":0,"message":"TLS-соединение и имя сертификата проверены.",
   "testedAt":"2026-11-02T12:00:10+0300","testedEpoch":1793610010}],
 "testTtlSeconds":600,"lastTestedAt":"…","lastAppliedAt":"…","lastDeletedAt":null,
 "lastOperation":{"type":"apply","success":true,"rolledBack":false,"selectedIds":[…],"completedAt":"…"},
 "lastError":null,"updatedAt":"…",
 "autoCheck":{"schemaVersion":1,"enabled":true,"settingsValid":true,"paused":false,"intervalSeconds":300,
   "savedIds":["cloudflare-primary","quad9"],"last":{"lastAttemptEpoch":1793609990,"finishedEpoch":1793610012,
   "status":"success","errorCode":null,"selectedIds":["cloudflare-primary","quad9"]},"observedEpoch":1793610100}}
```
`health.freshness.checkedAt` is jq `now|todateiso8601` (UTC, `Z`). `servers[].test` and `tests[]` entries may also carry
fields from the auto probe (B.R8). JS fields read — see §4. JS polls `status-cached` every 30000 ms while visible and not busy
(DJ:6, 605-620), on visibility change (DJ:621-631), after every action (DJ:490); "Обновить состояние" uses `force=1` (DJ:664-668); delete pre-check uses `force=1` with 15 s timeout.

### B2. `POST /api/routes/dot-test.cgi`
Body ≤64 KiB: `{"serverIds":["cloudflare-primary","quad9"],"allowUntested":false}` (DJ:504).
Validation (RD:642-663): `serverIds` array, 1..8, unique, every id in catalog, `allowUntested` absent/false → else `REQUEST_INVALID`
"Выбран некорректный список DNS-over-TLS серверов." (400). Recovery marker → `DOT_RECOVERY_REQUIRED`. Does **not** need the write protocol or ndmc.
Behaviour (RD:707-850):
* For each id (request order): `broray-timeout 12 openssl s_client -connect <addr>:<port> -servername <sni> -verify_hostname <sni> -verify_return_error -brief </dev/null`
  (without timeout wrapper if `broray-timeout` missing). rc 0 → `{ok:true,status:"ok",message:"TLS-соединение и имя сертификата проверены."}`,
  else `{ok:false,status:"failed",message:"TLS-проверка завершилась ошибкой."}`; openssl missing → `{ok:false,status:"unavailable",latencyMs:null,message:"OpenSSL недоступен."}`.
  `latencyMs = (end_s − start_s)·1000` (whole seconds). `testedAt`/`testedEpoch` are taken **once before the loop** (same for all entries).
  Result object: `{id,address,effectivePort,sni,spki,interface,domain,ok,status,latencyMs,message,testedAt,testedEpoch}`.
  Test hook: `BRORAY_DOT_TEST_FIXTURE_DIR/<id>.json` replaces the probe (RD:747-762).
* **Commits the selection**: config `requestedIds=selectedIds=serverIds`, `migrationState:"none"`, `updatedAt` — even if tests fail. This is the only way the UI saves a selection (DJ:176 hint).
* state: `tests` = **replaced** by this run's results (other ids' results dropped), `lastTestedAt`, `lastTestedEpoch`, `lastError:null`,
  `lastOperation={type:"test",success:all ok,selectedIds,completedAt}`, `updatedAt`.
* Internal failure → rollback local files → `DOT_TEST_FAILED` (500) "Проверка DNS-over-TLS прервана; локальное состояние восстановлено." / "…; требуется восстановление по recovery-marker." details = failure tag; marker close failure → `DOT_RECOVERY_REQUIRED` "Проверка завершена, но транзакционный marker не удалось безопасно закрыть.".
Success `data` = full status (B1 shape, fresh, uncached) + autoCheck. JS replaces `status` and toasts
"TLS/SNI-проверка завершена: подтверждено N из M." (success if all fresh, else warning) (DJ:474-477).

### B3. `POST /api/routes/dot-apply.cgi`
Body: `{"serverIds":[…],"allowUntested":false}` (DJ:518). JS only enables it when all selected are freshly tested, not over limit, `mutationAvailable`, and selection not already matching (DJ:415, 508); confirm dialog text uses `projectedTotal()` (DJ:512).
Checks in order (RD:1565-1648): recovery marker → write protocol → ensure_files → validate request → every requested id has a test
with identical endpoint, `ok:true`, age 0..600 s (else `DOT_TEST_REQUIRED` 409 "Сначала успешно проверьте все выбранные DNS-over-TLS серверы.")
→ `fetch_observed` (errors `NDMC_UNAVAILABLE`/`KEENETIC_UNAVAILABLE`/`KEENETIC_RUNTIME_UNAVAILABLE`) → observed must be determinate ∧ reconciled
(`DOT_OBSERVATION_UNDERDETERMINED` 409 "DoT-записи running-config и runtime не сведены однозначно; изменение запрещено.").
Plan (RD:1604-1636): `conflictIds` = desired whose selector exists but ≠1 semantic match → `DOT_SELECTOR_CONFLICT` 409 "Адрес и порт выбранного DoT уже заняты иной или неоднозначной записью; внешние записи сохранены." details = JSON array of ids;
`remove` = receipts not desired whose full-equal live record exists exactly once; `add` = desired with no semantic live match;
`afterTotal = totalSecure − |remove| + |add|` > 8 → `DOT_LIMIT_EXCEEDED` 409 "После экспорта будет N из 8 DoT/DoH-серверов.".
Execution: B.R3 order + convergence. Failure → rollback → `DOT_APPLY_FAILED` (502) message by stage: "Keenetic отклонил добавление DNS-over-TLS." /
"Keenetic отклонил удаление авторитетной записи BROray." / "Keenetic не сохранил конфигурацию DNS-over-TLS." / "Keenetic не достиг точного состояния DNS-over-TLS за ограниченное время." /
"Не удалось атомарно зафиксировать локальный DoT receipt." / "Экспорт DNS-over-TLS не завершён." + " Выполнен и проверен откат." or " Откат не подтверждён; дальнейшие изменения заблокированы recovery-marker."; details = stage tag (`add-command-rejected`, `remove-command-rejected`, `save`, `verify-timeout`, `local-*`, …) (RD:1732-1757).
Success side effects: receipts (B.R6), state `lastAppliedAt`, `lastError:null`, `lastOperation={type:"apply",success:true,rolledBack:false,selectedIds,completedAt}`.
`data` = fresh status + autoCheck. JS toast "DNS-over-TLS установлен и фактически проверен в Keenetic." (DJ:478).
Env `BRORAY_DOT_RESTORE_EXACT=true` (not reachable via API) skips the test requirement for exact restore.

### B4. `POST /api/routes/dot-delete-preview.cgi`
No body (JS sends none, only `Accept` header, DJ:544-548; CGI never reads one). Lock `dot:delete-preview`.
Checks (RD:1789-1860): recovery → protocol → ensure_files → fresh status; `status.deleteEligible` must be true and selectedIds non-empty,
else `DOT_DELETE_AUTHORITY_REFUSED` 409 "Удаление недоступно: текущий набор DoT нельзя однозначно подтвердить." details = `deleteBlockedReason`
(or `delete-not-eligible`). Entries: for each id in **sorted** `selectedIds`, the single live record with `deleteEligible` and id in
`catalogMatchIds` (ids with 0 or >1 such records are skipped). Zero entries → `DOT_DELETE_AUTHORITY_REFUSED` "Удаление недоступно: подтверждённые записи не найдены.".
**Fingerprint** = sha256 of the bytes of `jq -cS` output (sorted keys, compact, trailing `\n`) of
`{"entries":[{"address","domain","effectivePort","id","interface","sni","spki"}…sorted by id],"schemaVersion":1,"serverIds":[sorted]}`;
fingerprint validation also requires 1..8 ids, `entries[].id == serverIds`, `effectivePort==853` (RD:1767-1787). The Go port must
produce byte-identical canonical JSON (jq `-S` key order, no spaces, jq string escaping).
Success `data`:
```json
{"schemaVersion":1,"expectedFingerprint":"<64 hex>","serverIds":["cloudflare-primary","quad9"],
 "entries":[{"id":"cloudflare-primary","address":"1.1.1.1","effectivePort":853,"sni":"cloudflare-dns.com","spki":"","interface":"","domain":""},
            {"id":"quad9","address":"9.9.9.9","effectivePort":853,"sni":"dns.quad9.net","spki":"","interface":"","domain":""}],
 "autoCheck":{…}}
```
JS (DJ:550-595): first `GET dot-status?force=1` (15 s), recomputes its own `deletionPreview()`; then calls B4 and requires
`schemaVersion===1`, arrays, `entries.length>0`, `expectedFingerprint` string of length 64 else "BROray не смог сформировать безопасное подтверждение удаления.";
confirm dialog lists `entry.address + ":" + entry.effectivePort + " · TLS-домен: " + entry.sni` (DJ:570-572).

### B5. `POST /api/routes/dot-delete.cgi`
Body: `{"schemaVersion":1,"serverIds":<preview.serverIds>,"expectedFingerprint":"<64 hex>"}` (DJ:578-582).
Checks (RD:1862-2003): recovery → protocol → config/state must already exist as regular files with schema 3/1 (else
`DOT_DELETE_CONFIRMATION_STALE` "Состояние DNS-over-TLS изменилось. Обновите список и подтвердите удаление повторно.") → ensure_files →
body regular file → exactly one JSON object with `schemaVersion==1` and string `expectedFingerprint` (`REQUEST_INVALID` "Подтверждение удаления имеет неподдерживаемый формат.") →
`validate_request` on serverIds (`REQUEST_INVALID` "Выбран некорректный список…"; note `allowUntested` must be absent/false) →
fingerprint lowercase-hex 64 (`REQUEST_INVALID` "Запрос удаления не содержит контрольную сумму подтверждённого набора." / "Контрольная сумма подтверждения удаления некорректна.") →
sorted saved `requestedIds` must equal sorted `serverIds` (`DOT_DELETE_CONFIRMATION_STALE` "Выбранный набор DNS-over-TLS изменился после подтверждения. Обновите список и подтвердите удаление повторно.") →
`fetch_observed` → recompute canonical from live (first valid semantic match per id, **without** the deleteEligible filter) and compare fingerprint
(`DOT_DELETE_CONFIRMATION_STALE` "Подтверждённый набор DNS-over-TLS больше не совпадает с текущим. Ничего не удалено." if not computable;
"Список DNS-over-TLS изменился после подтверждения. Ничего не удалено; обновите список и подтвердите операцию повторно." if different) →
observed determinate ∧ reconciled ∧ no conflicts (selector present but selector≠1 or exact≠1) else `DOT_SELECTOR_CONFLICT`
"Удаление запрещено: выбранная DoT-запись не совпадает с единственным точным selector address+port." details = conflict ids.
Execute: `no dns-proxy tls upstream <addr> <port>` per matched record, `system configuration save`, convergence.
Failure → rollback → `DOT_DELETE_FAILED` (502) "Удаление DNS-over-TLS не завершено. Выполнен и проверен откат." / "… Откат не подтверждён; дальнейшие изменения заблокированы recovery-marker.", details = stage tag.
Success: config `requestedIds=selectedIds=managed=effectiveIds=[]` (**selection cleared**), state `lastDeletedAt`, `lastOperation={type:"delete",success:true,rolledBack:false,completedAt}`.
`data` = fresh status + autoCheck. JS resets its checkbox selection from `data.selectedIds` (now empty), toast
"Точные выбранные DNS-over-TLS записи удалены. Остальные DoT/DoH сохранены." (DJ:479-484). On error JS shows feedback; code
`DOT_DELETE_CONFIRMATION_STALE` selects message "Настройки DNS-over-TLS изменились после подтверждения. Ничего не удалено." (DJ:585-592).

### B6. `POST /api/routes/dot-auto-settings.cgi`
Order (dot-auto-settings.cgi): method POST → session → header `X-BROray-Request: operations` else `403 CSRF_REJECTED` "Обновите страницу и повторите действие." →
`lib/operations-origin.sh` regular file else `503 ORIGIN_VALIDATION_UNAVAILABLE` "Проверка источника запроса недоступна." →
`broray_operations_origin_allowed` (Origin / `X-BROray-Origin` must equal `http(s)://$HTTP_HOST` normalised, or the live KeenDNS
`https://broray.<name>.<domain>` origin; lib/operations-origin.sh:134-164) else `403 ORIGIN_REJECTED` "Источник запроса не подтверждён." →
body (≤64 KiB, JSON object) → `broray_job_begin system dot:auto-settings dns-over-tls USER protected`: rc 2 → `409 ROUTES_OPERATION_BUSY`
"Другая операция выполняется. Повторите сохранение позже."; other → `500 DOT_AUTO_SETTINGS_FAILED` "Не удалось начать сохранение настройки.".
Body must be exactly `{"enabled":<bool>}` (keys == ["enabled"]) else `400 REQUEST_INVALID` "Поле enabled должно быть логическим." (DA:62);
other save failure `500 DOT_AUTO_SETTINGS_FAILED` "Настройка автопроверки не сохранена."; finish failure → same code "Не удалось подтвердить завершение операции. Обновите состояние.".
Writes `/opt/broray/routes/dot/auto-check.json` = `{"schemaVersion":1,"enabled":true,"updatedAt":"…"}` (0600, atomic; DA:59-70).
JS sends headers `Accept`, `Content-Type: application/json`, `X-BROray-Request: operations`, `X-BROray-Origin: location.origin` and body `{"enabled":bool}` (DJ:654-655).
Success `data` = the auto view (not the full status, no extra `autoCheck` wrapper):
```json
{"schemaVersion":1,"enabled":true,"settingsValid":true,"paused":false,"intervalSeconds":300,
 "savedIds":["cloudflare-primary","quad9"],"last":{},"observedEpoch":1793610200}
```
JS: `status.autoCheck = data` (DJ:656).

### B.R7 Auto-check view (`broray_dot_auto_view`, DA:41-58)
* `enabled`: auto-check.json missing → false; present but not `{schemaVersion:1, enabled:bool}` (or >64 KiB, symlink, unsafe path) → `settingsValid:false, enabled:false`.
* `paused`: `/opt/var/lib/broray/background-automation.json` absent → false; `.paused` boolean → value; unreadable/invalid/symlink → `null`.
* `savedIds`: config `requestedIds` sorted if schema 3, requestedIds==selectedIds, ≤8, unique strings; else `[]`.
* `last`: `state.autoCheck` projected to `{lastAttemptEpoch,finishedEpoch,status,errorCode,selectedIds}` (missing → null) or `{}`.
* `intervalSeconds` is the constant 300; `observedEpoch` = now.

### B.R8 Auto-check scheduler (300 s)
* Daemon `bin/broray-subscription-scheduler` loops every `BRORAY_SUB_SCHEDULER_INTERVAL` = 60 s (:11, 83-93) → `broray_subscription_enqueue_due`
  (lib/subscription-job.sh:275-308): if not globally paused and `broray_dot_auto_due` → enqueue request `dot:auto-check`, bundle `selected`,
  source `SCHEDULER`, context = `broray_dot_auto_context`, priority 5.
* `broray_dot_auto_due` (DA:26-40): enabled==true ∧ savedIds non-empty ∧ no recovery marker ∧ state readable ∧
  NOT(`state.autoCheck.selectedIds == ids` ∧ `lastAttemptEpoch` within the last 300 s) ∧ fewer than all ids have a test with `testedEpoch` within 300 s.
* context = sha256 of (sha256 config.json, sha256 auto-check.json, sha256 lib/routes-dot.sh) (DA:148-160); a change between steps aborts (rc 76).
* Worker stage `dot:auto-check:probe` → `broray_dot_auto_step` (lib/operation-worker.sh:35-37, DA:162-262): probes **one** id per step
  (cursor `checkedCount`), via `lib/dot-auto-probe.sh` under a 150 s helper limit; then yields to the queue until all ids are done.
  Probe: `broray-timeout -k 2 12 openssl s_client -connect a:p -servername sni -verify_hostname sni -verify_return_error -brief`;
  result `{id,address,effectivePort,sni,spki,interface,domain,ok,status,message,testedAt,testedEpoch(end time),latencyMs}` (dot-auto-probe.sh:35-55).
  `ulimit -f 128`. Timeout binary missing → probe fails.
* Publication (resource `dot-tests`, lib/operation-publication.sh:28-31, 356-366) writes into state.json only
  `tests` (replace same-id entry unless existing is newer; others kept), `lastTestedAt`, `lastTestedEpoch`, `updatedAt`, and
  `autoCheck={requestId,context,selectedIds,totalCount,checkedCount,failedCount,lastAttemptEpoch,status:"running"|"success"|"failed",operationId,errorCode:null,finishedEpoch(null while running)}`.
  Legacy direct path `--dot-job` (`broray_dot_auto_run`, DA:90-140) uses statuses running/success/failed/error with `errorCode:"DOT_AUTO_CHECK_FAILED"`.
* Auto-check never changes Keenetic or the selection.
* JS hint texts (DJ:632-649): settings invalid "Не удалось получить настройки автопроверки."; disabled "Автопроверка выключена. Кнопка «Проверить выбранные» остаётся доступной.";
  `paused===true` "Автопроверка приостановлена общей паузой BROray."; `paused===null` "Состояние общей автоматики не подтверждено. Проверьте страницу BROray.";
  no savedIds "Нет сохранённых серверов для автопроверки."; selection differs "Автоматически проверяется сохранённый выбор. Чтобы изменить его, нажмите «Проверить выбранные».";
  `last.status==="error"` "Последняя автопроверка не завершена. BROray повторит попытку; прежние результаты не объявляются свежими.";
  else "Повторная проверка каждые 5 минут, даже при закрытой странице. Занятые операции имеют приоритет. Настройки DNS в роутере не меняются.".
  The toggle is disabled when busy/saving or `settingsValid!==true` (DJ:636).

### B.R9 8-upstream limit
`BRORAY_DOT_MAX_SERVERS` = 8 (RD:18). Enforced: request `serverIds` ≤ 8 (validate_request); apply `afterTotal` (DoT + DoH) ≤ 8;
status `selectionOverLimit` (saved selection > 8, only reachable from migrated configs), `capacityExceeded` (projectedTotal > 8),
`maxServersSource` text. JS: `selectionOverLimit()` (DJ:96-98), `capacityExceeded()` (DJ:99-102), messages DJ:345-346
("…Keenetic поддерживает максимум 8…", "После экспорта будет N из 8 DoT/DoH-серверов. Снимите выбор минимум с K сервера или удалите лишнюю внешнюю запись в Keenetic."),
`dns-detail-total` = `actual.totalSecure + " из " + maxServers`. Note status `projectedTotal` and apply `afterTotal` use different
formulas (status counts all receipts outside the selection and every selected id without an inCatalog record; apply counts only
removable/addable records) — reproduce both.

### B.R10 How DoH and foreign upstreams are shown
* DoH: never parsed into records; only `actual.dohCount` (shown in `#dns-detail-doh`, DJ:391) and included in `totalSecure`.
  DoH lines are never written or deleted; convergence requires dohCount unchanged.
* Foreign DoT: listed in `actual.dot[]` (every running-config record, valid or not) and rendered by `createActualCard` in
  `#dns-actual-list` (DJ:262-302): title `address:effectivePort`, port text (`portState==="omitted"` → "Порт в конфигурации: не указан · эффективный: 853"),
  SNI/SPKI/Интерфейс (on)/Domain/catalog matches/strict-parse lines, classification badge (DJ:229-247:
  `catalog:<id>` → "В каталоге BROray: <id>", extra attributes, "Не в каталоге BROray", "Неоднозначное совпадение каталога",
  "Коллизия селектора адрес + порт", else "Ошибка разбора записи") and badge from `deleteEligible` only
  ("Точная запись · удаляется при выборе" / "Неоднозначная запись · изменение запрещено").
  `externalDotCount` = records with `managed==false` → `#dns-detail-external-dot`. Foreign records are never removed by apply; delete removes
  only exact catalog-selected records (regardless of who added them).

---

## 3. Endpoints built dynamically by the JS
| file:line | expression | resulting URLs |
|---|---|---|
| RCJ:814 | `"/api/routes/custom-validate.cgi?bundleId=" + encodeURIComponent(id)` | custom-validate.cgi?bundleId=user-… |
| RCJ:776 | `"/api/routes/custom-remove.cgi?bundleId=" + encodeURIComponent(id)` | custom-remove.cgi?bundleId=user-… |
| RCJ:687 | `"/api/routes/preflight.cgi?bundleId=" + … + "&action=" + action` | action ∈ `export`, `delete`, `resume` (routes-engine area) |
| RCJ:719 | `"/api/routes/" + action + ".cgi?bundleId=" + … + "&preflightToken=" + token` | `export.cgi`, `delete.cgi`, `resume.cgi` (routes-engine area) |
| DJ:446 | `"/api/routes/dot-status.cgi" + (force ? "?force=1" : "")` | dot-status.cgi, dot-status.cgi?force=1 |
| DJ:463,468 | `"/api/routes/" + (test→"dot-test.cgi" / apply→"dot-apply.cgi" / else "dot-delete.cgi")` | dot-test/apply/delete.cgi |
Also loaded on routes-custom.html: `assets/js/routes-operation.js` → `/api/routes/operation-status.cgi` (routes-engine area).
Both pages: `GET /api/session.cgi`. Bundle ids contain only `[a-z0-9_-]` so `encodeURIComponent` is identity — the CGI does no decoding.

## 4. JS dependencies on backend wording / ids / fields
DNS page (DJ) reads from status: `servers[].id|name|provider|address|effectivePort|port|sni|spki|interface|on|domain|present|test.{ok,status,testedEpoch,latencyMs}`;
`selectedIds`; `actual.dot[].{address,effectivePort,port,sni,spki,interface,on,domain,deleteEligible,valid,unknownTokenCount,index,portState,portRaw,classification,catalogMatchIds,parseError}`;
`actual.dohCount|totalSecure|determinate`; `managed[]` (address/effectivePort/sni); `projectedTotal`, `capacityExceeded`, `maxServers`, `matchesSelection`,
`runningConfigAvailable`, `installationState`, `installed`, `writeProtocolEnabled`, `mutationAvailable`, `mutationBlockedReason`
(mapped texts for `recovery-required|keenetic-unavailable|dot-observation-underdetermined`, else raw string shown, DJ:128-135),
`deleteEligible`, `deleteBlockedReason` (shown raw in `#dns-detail-delete-gate`, DJ:395), `quarantinedReceipts`, `testAvailable`,
`effectiveIds`, `managedPresentCount`, `externalDotCount`, `lastTestedAt`, `lastAppliedAt`, `autoCheck.{enabled,settingsValid,paused,savedIds,last.status}`.
* Test freshness on the client: `ok===true` and `0 ≤ Date.now() − testedEpoch·1000 ≤ 600000` (DJ:5, 45-52) — must match server TTL 600.
* `latencyMs` must be a multiple of 1000 to be displayed ("время < 1 с" for 0, else "время ≈ N с") (DJ:203-209).
* Client-side delete gate requires saved `selectedIds` (sorted) == checked ids (DJ:139-143) and exactly one live record per selected
  catalog address+port that is `deleteEligible`, valid, and equal in sni/spki/interface/domain (DJ:144-170).
* Error codes checked: `DOT_DELETE_CONFIRMATION_STALE` (DJ:587), `AUTH_REQUIRED`/`SESSION_REQUIRED`/401. All other errors show `error.message` in a toast and
  `code`/`message`/`details` in `#dns-feedback` (DJ:430-444).
* HTML ids (dns.html): `dns-page-status, dns-summary-message, dns-support, dns-selected, dns-tested, dns-installed, dns-operation(-title|-message|-progress), dns-selection-hint, dns-auto-check, dns-auto-hint, dns-server-list, dns-action-state, dns-notice(-message), dns-test, dns-apply, dns-refresh, dns-delete, dns-delete-hint, dns-actual-list, dns-actual-empty, dns-detail-*`. `dns-feedback` / `routes-feedback` are not in the HTML; `action-feedback.js:45-55` creates them inside `.workspace-content` on demand. All rendered client-side; the backend produces no HTML.

Custom page (RCJ): no error-code branching besides 401; all error text comes from `message` + `details`.
HTML ids used: `routes-bundles, routes-custom-upload, routes-page-status, routes-summary-message, routes-installed-count, routes-attention-count, routes-total-count` (+ `routes-feedback`, created on demand by action-feedback.js) + the modal it injects itself (`routes-upload-*`). Query `?resume=<bundleId>` on page load triggers `prepareLong(bundle,"resume")` (RCJ:1007, 1044-1047).
Events: listens `broray:routes-operation-resume|finished|authoritative`, dispatches `broray:routes-operation-state` (RCJ:10-11, 221-228, 1009-1019) — produced by routes-operation.js, not the backend.

## 5. Riskiest behaviours (summary)
1. DoT delete fingerprint = sha256 of jq `-cS` canonical bytes — Go must reproduce jq key sorting/escaping/trailing newline exactly, and recompute it from live config without the deleteEligible filter.
2. Strict two-source DoT observation (running-config flat/block grammar + `show dns-proxy` System profile) and reconciliation; any parse ambiguity blocks writes; status classification/ownership/deleteEligible rules (B.R5).
3. DoT transactions: marker file, ordered remove/add/save, 10×1 s convergence, rollback with explicit-port re-adds, and the sticky `DOT_RECOVERY_REQUIRED` state (also visible during a running test).
4. `dot-test` silently commits the selection (and replaces the whole `tests` array); apply adopts identical pre-existing records as receipts; delete clears the saved selection.
5. BAT import: exact line grammar, error line text, first-line-message/remaining-lines-details split, 16 files / 2 MiB / 100000 routes, CIDR collapse algorithm and the custom.json/catalog/state/registry layout consumed by the routes engine.
