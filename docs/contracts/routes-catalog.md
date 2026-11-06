# Contract: ROUTES CATALOG (ready-made route bundles) — BROray 3.x → 3.5.0

Source: `/home/claude/broadmin/broray` @ f505928. All paths below are relative to
`runtime/app/` unless they start with `/opt/...` (runtime paths on the router) or
`native/`, `runtime/state-seed/`.

Scope: `web-new/routes.html`, `web-new/routes-import.html`,
`web-new/assets/js/routes-catalog.js`, `routes-overview.js`, `routes-operation.js`,
CGIs `web-new/api/routes/{catalog-summary,check,delete,download,export,operation-status,overview,plan,preflight,progress,resume,status,stop,summary,verify}.cgi`
and the libraries/binaries they reach (`lib/routes-*.sh` except `routes-dot.sh`,
`routes-user*`; `bin/broray-routes`, `bin/broray-routes-check-source`,
`bin/broray-route-prepare`, `bin/broray-ndmc-run` (`native/broray-ndmc-run.c`),
`bin/broray-system-ndmc`).

Conventions: "JS" = browser code; "CGI" = shell endpoint. `file:line` citations are exact.
Anything not proven by code is marked **UNCONFIRMED**.

---

## 0. Common envelope, auth, CSRF, transport

### 0.1 Envelope (all endpoints in this area)
* Success (`web-new/api/auth-common.sh:96-111`): headers
  `Content-Type: application/json; charset=utf-8`, `Cache-Control: no-store, no-cache, must-revalidate`,
  `Pragma: no-cache`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy: no-referrer` (`auth-common.sh:7-14`); no `Status:` line ⇒ HTTP 200; body
  `{"success":true,"data":<payload>,"error":null}` (pretty-printed by `jq`, formatting irrelevant).
* Error (`auth-common.sh:25-57`): `Status: <code text>` + same headers; body
  `{"success":false,"data":null,"error":{"code":"…","message":"…","details":<string|null>}}`
  (`details` is `null` when empty string). Before printing, it calls
  `broray_api_release_operation_lock failed` (`auth-common.sh:16-23,31`).
* Bootstrap failure when `auth-common.sh` is unreadable (every CGI, e.g. `check.cgi:31-42`):
  HTTP 500, body `{"success":false,"data":null,"error":{"code":"AUTH_MODULE_UNAVAILABLE","message":"Модуль авторизации недоступен."}}`
  (no `details` key; some variants omit `Cache-Control`).
* Method check `broray_api_require_method` (`auth-common.sh:59-70`): 405
  `METHOD_NOT_ALLOWED`, message `"Для этой операции требуется метод <M>."`, details `"Получен метод <actual>."`.
* Session check `broray_api_require_session` (`auth-common.sh:72-95`): cookie `BRORAY_SESSION`;
  401 `AUTH_REQUIRED` "Требуется авторизация."; 500 `SESSION_BACKEND_UNAVAILABLE`
  "Модуль проверки сессии недоступен." if `/opt/broray/lib/web-auth.sh` unreadable.
* **No CSRF/Origin check** exists in any CGI of this area (only cookie session). The JS
  sends `Content-Type: application/json` and body `{}` on POSTs (`common.js:14-23`,
  `routes-catalog.js:1429-1434`); bodies are never read by these CGIs (except none).
  The `X-BROray-*` queue headers (`common.js:68-70`) are **not** used by this area.

### 0.2 JS request wrapper
`window.BROrayUI.apiRequest` (`common.js:4-60`): `credentials:"same-origin"`, `cache:"no-store"`;
non-2xx or non-object body ⇒ throws `Error` with `.message = payload.error.message`
(fallback `"Ошибка запроса (HTTP n)."`), `.status = HTTP status`, `.code = payload.error.code`
(fallback `"HTTP_RESPONSE_INVALID"`), `.details = payload.error.details`, `.payload`.
Each page then `unwrap`s: if `payload.success === false` throw, else return `payload.data`
(`routes-catalog.js:140-154`, `routes-operation.js:19-29`, `routes-overview.js:11-19`).
HTTP 401 anywhere ⇒ `BROrayUI.redirectToLogin()` (`location.replace("/")`, `common.js:329-331`).

### 0.3 `bundleId` validation (shared rule)
Pattern (e.g. `check.cgi:79-86`): reject if empty, contains any char outside `[a-z0-9_-]`,
or length ≥ 64 (the glob has 64 `?`). Max length 63. No URL-decoding is performed;
the JS uses `encodeURIComponent` which is identity for valid ids.
Two query-parsing styles exist (important for exact compatibility):
* **Prefix style** (`check`, `download`, `verify`, `plan`, `progress`, `status`, `summary`):
  `case "$QUERY_STRING" in bundleId=*) bundle_id="${QUERY_STRING#bundleId=}"`. The query must
  *start* with `bundleId=`; everything after it is the id (so `bundleId=x&y=1` ⇒ id `x&y=1`
  ⇒ 400 `ROUTES_BUNDLE_INVALID`; `y=1&bundleId=x` ⇒ 400 `ROUTES_BUNDLE_REQUIRED`).
* **Split style** (`preflight`, `export`, `delete`, `resume`): split on `&`, last
  `bundleId=` / `preflightToken=` / `action=` wins (`routes-web-action.sh:77-89`,
  `preflight.cgi:56-67`).
Then the id must be present in `/opt/broray/routes/bundles.json` `.bundles[]`
(404 `ROUTES_BUNDLE_NOT_FOUND` "Набор маршрутов не найден."). Catalog bundle ids
(fixed, ordered): `telegram whatsapp youtube chatgpt facebook instagram meta tiktok speedtest wikipedia`
(`runtime/state-seed/routes/bundles.json`, `lib/routes-page-summary.sh:13`). The same
`preflight/export/delete/resume` endpoints are also used by custom bundles `user-*`
(`routes-custom-page.js:687,719`) — out of this area but the Go handler is shared.

### 0.4 Job admission (protected route job) — applies to check, download, verify, plan, preflight, export, delete, resume
1. CGI calls `broray_routes_api_lock_acquire <action> <bundle>` (`lib/routes-api-operation.sh:132-172`).
   For actions `check|download|verify|plan|export|delete|resume|preflight:*|custom:*|dot:*`:
   if not already inside a job and the legacy global lock path
   `/opt/var/lock/broray/global-operation.lock` exists ⇒ rc 2 (busy) (`:139-143`); else
   `broray_route_api_enter` (`lib/route-api-job.sh:6-54`).
2. `broray_route_api_enter` only accepts the real CGI path `$0` ∈ the list in
   `route-api-job.sh:14-25` (else rc 73). It runs `broray_route_job_run` (`lib/route-job.sh:58-86`):
   coordinator `begin routes <action> <bundle> USER protected` (`operation-client.sh:135-168`);
   structured rejection ⇒ rc 2. Then `bin/broray-ops-supervisor --protected-route` executes
   `lib/route-api-worker.sh <cgi> <replyfile>` with hard deadline **3600 s**, cooperative
   grace 0, TERM grace 2 (`route-job.sh:71-72`; `native/broray-ops-supervisor.c:165-169`).
   The worker re-executes the same CGI under the job (same env: method, cookie, query),
   validates the buffered HTTP reply, exit 0 for 2xx+success, 1 otherwise
   (`route-api-worker.sh:1-14`). After `helpers-drain` (5 attempts, 1 s) and `finish`, the
   outer CGI prints the buffered reply verbatim (headers + body, incl. `Status:`) and exits
   (`route-api-job.sh:45-50`).
3. The coordinator refuses `begin` (⇒ 409 busy) while any `/opt/broray/routes/operations/*.json`
   has `running==true` or `resumable==true`, except `resume` / `preflight:resume` for that
   same bundle (`lib/operation-coordinator.sh:549-600`), while another operation owns the
   global fence, or while the updater lock / legacy lock exists.
4. Inside the job, the CLI `broray-routes <cmd>` re-checks that `<cmd>` is allowed for the
   job action (`route-job.sh:18-31`): `resume`→{resume,export,delete,build-export};
   `preflight:resume|preflight:export`→{plan,build-export}; `export`→{export,build-export};
   `plan|verify`→{plan,build-export}; otherwise exactly the action.
5. Mapping in each CGI: lock rc 0 ok; rc 2 ⇒ **409 `ROUTES_OPERATION_BUSY`**
   "Другая конфликтующая операция уже выполняется."; any other rc ⇒
   **500 `ROUTES_API_LOCK_FAILED`** "Не удалось установить блокировку операции."
   (e.g. `check.cgi:103-111`). Note: if the job ran but drain/finish failed (rc 75) the
   reply is discarded and the client sees 500 `ROUTES_API_LOCK_FAILED` although the
   mutation may have happened (`route-api-job.sh:45-53`, `route-job.sh:81-83`).
6. Fence visible to readers: the coordinator publishes a symlink
   `/opt/var/lock/broray/global-operation.lock -> <op>/fence` containing files `pid`, `scope`,
   `action`, `bundle`, `startedAt` (`operation-coordinator.sh:903-917`). Route jobs use
   `scope=routes`, `action=<job action>` (e.g. `check`, `preflight:export`, `export`).
   System jobs use `scope=system` (action e.g. `update`, `restore`, `uninstall`, `cleanup`,
   `reinstall`, `xray:*` …). This is what `globalOperation` reports (§2.3).

### 0.5 Low-level resource lock (inside CLI)
`/opt/broray/routes/locks/operation.lock/` (dir with `pid`, `action`, `bundle`, `owner.json`,
for delete also `operation`, `startedAt`) — `lib/routes-resource-lock.sh:53-72`,
`lib/routes-resource-control.sh:79-129`. Allowed lease names:
`check|download|build-export|preflight|sync|export|delete|user-import`. Existing lock ⇒ exit 2
⇒ CLI prints `"Другая операция с маршрутами уже выполняется."` (the CGIs grep this text).
`/opt/broray/routes/rollback-required.json` blocks every acquire (exit 74) (`routes-resource-control.sh:82`).

---

## 1. Endpoint table

| # | URL | Method | Used by JS in scope | Async / job | Purpose |
|---|-----|--------|---------------------|-------------|---------|
| 1 | `/api/routes/catalog-summary.cgi` | GET | yes `routes-catalog.js:1269` | sync read | All 10 catalog cards + totals |
| 2 | `/api/routes/overview.cgi` | GET | yes `routes-overview.js:82` | sync read | Hub page summary (catalog+custom) |
| 3 | `/api/routes/operation-status.cgi` | GET | yes `routes-operation.js:128` | poll target (1 s) | Global route operation window |
| 4 | `/api/routes/check.cgi?bundleId=` | POST | yes `routes-catalog.js:1413,1485` | job, synchronous HTTP | Source update check (GitHub) |
| 5 | `/api/routes/download.cgi?bundleId=` | POST | yes `routes-catalog.js:1413,1492` | job, synchronous HTTP | Download checked version |
| 6 | `/api/routes/verify.cgi?bundleId=` | POST | yes `routes-catalog.js:1413` (action `verify`) | job, synchronous HTTP | Build export + plan, record verifyResult |
| 7 | `/api/routes/preflight.cgi?bundleId=&action=` | POST | yes `routes-catalog.js:1538` | job, synchronous HTTP | Preflight + 120 s confirmation token |
| 8 | `/api/routes/export.cgi?bundleId=&preflightToken=` | POST | yes `routes-catalog.js:1413-1417` | job, long synchronous HTTP (progress polled via #3) | Install / update / restore in Keenetic |
| 9 | `/api/routes/delete.cgi?bundleId=&preflightToken=` | POST | yes (same) | job, long synchronous HTTP | Delete bundle routes from Keenetic |
| 10 | `/api/routes/resume.cgi?bundleId=&preflightToken=` | POST | yes (same, action `resume`) | job, long synchronous HTTP | Resume paused operation (see §5 bug) |
| 11 | `/api/routes/plan.cgi?bundleId=` | POST | **no caller** in web-new | job | Raw sync plan |
| 12 | `/api/routes/progress.cgi?bundleId=` | GET | **no caller** | read | Raw progress file |
| 13 | `/api/routes/status.cgi?bundleId=` | GET | **no caller** | read | Single bundle state + presence |
| 14 | `/api/routes/summary.cgi[?bundleId=]` | GET | **no caller** | read | Status-contract summary |
| 15 | `/api/routes/stop.cgi` | POST | **no caller** | — | Always 409 (stop unsupported) |

Also called by these pages (other areas): `GET /api/session.cgi` (`routes-catalog.js:1706`,
`routes-overview.js:133`) — JS reads only `session.user` (`routes-catalog.js:1664`,
`routes-overview.js:112`).

---

## 2. Endpoint details

### 2.1 GET `/api/routes/catalog-summary.cgi`
* No params. `GET` + session (`catalog-summary.cgi:25-26`).
* Calls `broray_routes_page_summary catalog OUT` (`lib/routes-page-summary.sh:317-560`).
* Errors: 500 `ROUTES_PAGE_SUMMARY_UNAVAILABLE` "Модуль сводки маршрутов недоступен."
  (`:28`); 500 `ROUTES_CATALOG_SUMMARY_FAILED` "Не удалось получить состояние готовых маршрутов."
  details = last 30 stderr lines (`:32-34`); 500 `ROUTES_CATALOG_SUMMARY_INVALID`
  "Модуль маршрутов вернул некорректную сводку готовых наборов." unless
  `schemaVersion==1 && scope=="catalog" && bundles array && totals object` (`:35-36`).
* Side effects: reads `/opt/broray/routes/{state/<id>.json, installed/bundles/<id>.json,
  manifests/<id>.json, installed/routes.json, config.json, operations/<id>.json(+.counter)}`,
  `/opt/broray/config/interface.json`; Keenetic read `show running-config` through the shared
  cache (TTL **30 s** for page summaries, `routes-page-summary.sh:341-349`;
  cache file `/opt/broray/run/routes-router-config-cache.json`, refresh serialized by
  `broray-ops-guard /opt/broray/run/routes-router-config.lock.guard` + `lib/routes-config-cache.sh`;
  `routes-router-config.sh:494-530`). Unavailable/failed read ⇒ snapshot
  `{source:"unavailable",routes:[]}` and `actualAvailable=false` (`:354-356`). No writes
  except cache/temp files in `/opt/broray/tmp`.
* Response `data` (built `routes-page-summary.sh:493-553`):
```
schemaVersion:1, scope:"catalog", generatedAt:str,
managedInterface: "ProxyN"|null, managedInterfaceDisplay: "BROray"|"BROray - …",
managedMetric: number (config.managedMetric, default 1200),
routerSnapshot:{available:bool, source:str|null, checkedAt:str|null, routeCount:int},
globalOperation: <§2.3 globalOperation>,
operation: <full operation-status object, §2.3>,
totals:{bundleCount, installedCount, verifiedInstalledCount, recordedInstalledCount,
        registeredCount, attentionCount, expectedRouteCount, presentRouteCount,
        uniqueRouteCount, sharedOwnershipCount},
health:{schemaVersion:1, module:"routes", availability:"available"|"partial",
        severity:"error"|"busy"|"warning"|"ok", operational, consistent, actionRequired,
        freshness:{state:"fresh"|"unknown", checkedAt}, reasons:[{code,message,details}],
        facts:{bundleCount, verifiedInstalledCount, recordedInstalledCount, attentionCount,
        presentRouteCount}, lastOperation},
bundles:[ <bundle card object> ×10 in fixed order ]
```
  Bundle card object = state projection (`:215-237`) merged with computed fields (`:461-491`):
```
schemaVersion, bundleId, status, routeCount, availableVersion, downloadedVersion,
installedVersion, lastCheckedAt, lastVerifiedAt (null unless verification current),
lastDownloadedAt, lastExportedAt, lastDeletedAt, lastError, checkResult,
verifyResult (null unless "current", see §3.4), downloadResult, exportBuild, exportResult,
deleteResult, customImport, updatedAt,
id, metadata:{id,name,description:null,sourceProvider,sourceDirectory,sourceFileCount,targetInterface},
routerPresence:{available, registered, source, checkedAt, cacheAgeSeconds, expectedRouteCount,
  presentRouteCount, missingRouteCount, duplicateRouteCount, complete, actualInstalled,
  drift, status:"unavailable"|"not_registered"|"complete"|"absent"|"partial", missingRoutes:[]},
ownership:{uniqueRouteCount, sharedRouteCount, totalOwnedRouteCount},
recordedInstalled, verifiedInstalled, sourceUpdateAvailable, keeneticUpdateRequired,
actionRequired, attention, operationProgress:<progress object §2.3 or idle>, globalOperation
```
  If a state file is missing the card is `{bundleId,status:"not_checked",routeCount:0}` + computed fields (`:415`).
  Fields **not** included (vs state file): `preflight`, `resumeOperation`, `operation`.
* Example (one bundle shown):
```json
{"success":true,"error":null,"data":{"schemaVersion":1,"scope":"catalog","generatedAt":"2026-11-02T12:00:00+0300",
"managedInterface":"Proxy0","managedInterfaceDisplay":"BROray","managedMetric":1200,
"routerSnapshot":{"available":true,"source":"running-config","checkedAt":"2026-11-02T11:59:50+0300","routeCount":412},
"globalOperation":{"active":false,"pending":false,"resumable":false,"pid":null,"scope":null,"action":null,"bundleId":null,"startedAt":null,"updatedAt":null,"stale":false},
"operation":{"schemaVersion":1,"generatedAt":"2026-11-02T12:00:00+0300","canStop":false,"active":false,"pending":false,"resumable":false,"running":false,"bundleId":null,"bundleType":null,"bundleName":null,"action":null,"globalOperation":{"active":false,"pending":false,"resumable":false,"pid":null,"scope":null,"action":null,"bundleId":null,"startedAt":null,"updatedAt":null,"stale":false},"progress":{"schemaVersion":2,"kind":"routes","bundleId":"","operation":null,"phase":"idle","current":0,"total":0,"percent":0,"currentRoute":null,"message":"Операция не выполняется.","running":false,"success":null,"rolledBack":false,"resumable":false,"stopRequested":false,"stoppedByUser":false,"resumed":false,"errorRoute":null,"pid":null,"startedAt":null,"updatedAt":null,"completedAt":null}},
"totals":{"bundleCount":10,"installedCount":1,"verifiedInstalledCount":1,"recordedInstalledCount":1,"registeredCount":1,"attentionCount":0,"expectedRouteCount":391,"presentRouteCount":391,"uniqueRouteCount":391,"sharedOwnershipCount":0},
"health":{"schemaVersion":1,"module":"routes","availability":"available","severity":"ok","operational":true,"consistent":true,"actionRequired":false,"freshness":{"state":"fresh","checkedAt":"2026-11-02T11:59:50+0300"},"reasons":[],"facts":{"bundleCount":10,"verifiedInstalledCount":1,"recordedInstalledCount":1,"attentionCount":0,"presentRouteCount":391},"lastOperation":{"…":"same as data.operation"}},
"bundles":[{"schemaVersion":1,"bundleId":"telegram","status":"installed","routeCount":391,
 "availableVersion":{"sourceCommit":"0f3c…40hex","sourceDate":"2026-10-20T08:11:02Z","contentSha256":"ab…64hex","sourceSetSha256":"cd…64hex","sourceFileCount":1,"sourceFiles":[{"name":"telegram.bat","parser":"windows-route-bat","sha256":"ef…","sizeBytes":20480,"routeCount":391,"htmlUrl":"https://github.com/RockBlack-VPN/ip-address/blob/0f3c…/Global/Telegram/telegram.bat"}]},
 "downloadedVersion":{"sourceCommit":"0f3c…","sourceDate":"2026-10-20T08:11:02Z","contentSha256":"ab…","sourceSetSha256":"cd…","sourceFileCount":1,"sourceFiles":[…]},
 "installedVersion":{"…":"copy of downloadedVersion at install"},
 "lastCheckedAt":"…","lastVerifiedAt":"…","lastDownloadedAt":"…","lastExportedAt":"…","lastDeletedAt":null,"lastError":null,
 "checkResult":{"result":"no_changes","…":"§2.4"},"verifyResult":{"result":"complete","…":"§2.6"},"downloadResult":{"…":"§2.5"},
 "exportBuild":{"…":"§4.4"},"exportResult":{"result":"installed","operation":"install","message":"Маршруты установлены в Keenetic","…":"§2.8"},
 "deleteResult":null,"customImport":null,"updatedAt":"…",
 "id":"telegram","metadata":{"id":"telegram","name":"Telegram","description":null,"sourceProvider":"github","sourceDirectory":"Global/Telegram","sourceFileCount":1,"targetInterface":"Proxy0"},
 "routerPresence":{"available":true,"registered":true,"source":"running-config","checkedAt":"…","cacheAgeSeconds":10,"expectedRouteCount":391,"presentRouteCount":391,"missingRouteCount":0,"duplicateRouteCount":0,"complete":true,"actualInstalled":true,"drift":false,"status":"complete","missingRoutes":[]},
 "ownership":{"uniqueRouteCount":391,"sharedRouteCount":0,"totalOwnedRouteCount":391},
 "recordedInstalled":true,"verifiedInstalled":true,"sourceUpdateAvailable":false,"keeneticUpdateRequired":false,"actionRequired":false,"attention":false,
 "operationProgress":{"schemaVersion":2,"kind":"routes","bundleId":"telegram","operation":null,"phase":"idle","current":0,"total":0,"percent":0,"currentRoute":null,"message":"Операция не выполняется.","running":false,"success":null,"rolledBack":false,"resumable":false,"stopRequested":false,"stoppedByUser":false,"resumed":false,"errorRoute":null,"pid":null,"startedAt":null,"updatedAt":null,"completedAt":null},
 "globalOperation":{"active":false,"…":"…"}}, "…9 more…"]}}
```
* JS reads (`routes-catalog.js`):
  `loadAllStates` 1266-1298: `bundles[]` keyed by `.id` (not `bundleId`), `globalOperation`
  (only if `.active`); `renderSummary` 1192-1233: `totals.installedCount`,
  `totals.attentionCount`, `totals.bundleCount`, `operation.active`, `operation.bundleName`,
  `operation.bundleId`, `operation.resumable`; `renderCard` 976-1135: `managedInterfaceDisplay`
  (top level), and per card all fields used by §3 functions plus `routeCount`,
  `availableVersion.sourceFiles[].{name,htmlUrl,routeCount,sizeBytes,sha256}`
  (`renderSourceFiles` 924-950), `routerPresence.{registered,expectedRouteCount,presentRouteCount,
  missingRouteCount,available,drift,actualInstalled,complete}`, `installedVersion`,
  `lastCheckedAt`, `lastVerifiedAt`, `exportBuild.{targetInterfaceDisplay,managedMetric}`,
  `checkResult.{managedInterfaceDisplay,fileChanges.{addedFiles,changedFiles,removedFiles},
  routeChanges.{added,removed,unchanged},result,routesChanged,sourceChanged,message}`,
  `exportResult.{targetInterfaceDisplay,managedMetric,message}`,
  `ownership.{uniqueRouteCount,sharedRouteCount}`, `operationProgress.*`,
  `lastError` (string or `{message,details}`), `downloadResult.message`, `deleteResult.message`,
  `operation.output` (not present here; present in mutation responses).
  `state.status` is **never read** by the JS.
* Refresh model: on load after session; passive every 30 s (`PASSIVE_REFRESH_INTERVAL_MS`,
  `:8,1610-1625`, skipped while busy, paused when hidden); after every action
  (`refreshAllStatesAfterOperation` 1185-1190); on `broray:routes-operation-finished`
  (1688-1690); in-flight de-duplication (`:1267`); aborted on hide/pagehide.

### 2.2 GET `/api/routes/overview.cgi`
* No params. GET + session. Calls `broray_routes_page_overview` (`routes-page-summary.sh:562-607`),
  which runs `broray_routes_page_summary all` (catalog ids + custom ids from
  `/opt/broray/routes/custom.json` `.bundles[].id`, `:45-78`).
* Errors: 500 `ROUTES_PAGE_SUMMARY_UNAVAILABLE`; 500 `ROUTES_OVERVIEW_FAILED`
  "Не удалось получить обзор маршрутов." (+details); 500 `ROUTES_OVERVIEW_INVALID`
  "Модуль маршрутов вернул некорректный обзор." unless `schemaVersion==1`, `custom` and
  `catalog` objects (`overview.cgi:28-36`).
* Response:
```json
{"schemaVersion":1,"generatedAt":"…","managedInterface":"Proxy0","managedInterfaceDisplay":"BROray",
 "globalOperation":{…},"operation":{…§2.3…},
 "totalManagedRoutes":391,"sharedRoutes":0,
 "custom":{"total":0,"installed":0,"attention":0,"routeCount":0},
 "catalog":{"total":10,"installed":1,"attention":0,"routeCount":391},
 "lastUpdatedAt":"…","health":{…}}
```
  `totalManagedRoutes` = `installed/routes.json .routes|length`; `sharedRoutes` = routes with
  `owners|length > 1`; `custom`/`catalog` split by `id` starting with `user-`;
  `installed` = `verifiedInstalled==true`; `attention` = `attention==true`
  (`:578-602`).
* JS reads (`routes-overview.js:37-64`): `managedInterfaceDisplay` (fallback "BROray"),
  `totalManagedRoutes`, `sharedRoutes`, `operation.{active,resumable,running,bundleName}`,
  `custom.{total,installed,attention}`, `catalog.{total (fallback 10),installed,attention}`.
  Message texts: active ⇒ `"<bundleName|Набор маршрутов>: <operationText lower>. Прогресс показан в общем окне операции."`;
  attention>0 ⇒ "Некоторые наборы требуют внимания…"; else "Сводка получена…".
  operationText: `!active`→"Нет", `resumable`→"Приостановлена", `running`→"Выполняется", else "Подготовка" (`:30-35`).
* Polling: every 60 s while visible (`:4,71-74`), on refresh button (toast
  "Состояние маршрутов обновлено."), on `broray:routes-operation-finished`, on visibility.

### 2.3 GET `/api/routes/operation-status.cgi`
* No params. GET + session. `broray_routes_page_operation_status` (`routes-page-summary.sh:130-196`).
* Errors: 500 `ROUTES_PAGE_SUMMARY_UNAVAILABLE` "Модуль состояния операции недоступен.";
  500 `ROUTES_OPERATION_STATUS_FAILED` "Не удалось получить состояние операции маршрутов.";
  500 `ROUTES_OPERATION_STATUS_INVALID` "Модуль маршрутов вернул некорректное состояние операции."
  unless `schemaVersion==1`, `active` boolean, `progress` object (`operation-status.cgi:28-36`).
* Algorithm:
  1. `globalOperation` = `broray_routes_api_lock_read_json` (`routes-api-operation.sh:190-282`):
     * fence dir at `/opt/var/lock/broray/global-operation.lock` exists and its `pid` is alive ⇒
       `{active:true,pending:false,resumable:false,pid:int,scope,action,bundleId,startedAt,updatedAt:null,stale:false}`;
     * else, if any `routes/operations/*.json` has `kind=="routes" && running==false && resumable==true`
       (first match in glob order) ⇒
       `{active:true,pending:true,resumable:true,pid:null,scope:"routes",action:"resume-required",operation:<saved op>,bundleId,startedAt,updatedAt,stale:false}`;
     * else `{active:false,pending:false,resumable:false,pid:<int|null>,scope,action,bundleId,startedAt,updatedAt:null,stale:<bool>}`
       (stale=true if fence dir exists with dead pid).
     Default if the library fails: all-false object with `stale:false` (`routes-page-summary.sh:116`).
  2. `bundleId` = `globalOperation.bundleId`; `bundleType` = `"custom"` if id is in
     `routes/custom.json`, else `"catalog"`; `bundleName` = custom `.name` or manifest `.name`
     (e.g. "Telegram") else id (`:141-156`).
  3. `progress` = `broray_routes_progress_read <bundleId>` (§4.6) or idle object (`:80-110`).
     **If globalOperation has no bundleId, progress is always idle**, so a running sync is only
     visible while the coordinator fence exists.
  4. Output (`:169-191`):
```
schemaVersion:1, generatedAt, canStop:false,
active: g.active || p.running || p.resumable,
pending: g.pending, resumable: p.resumable // g.resumable // false,
running: p.running // false,
bundleId: g.bundleId if non-empty else (p.bundleId or null),
bundleType / bundleName: null when both ids empty,
action: g.action // p.operation // null,
globalOperation: g, progress: p
```
* Example during an export of 391 routes:
```json
{"success":true,"error":null,"data":{"schemaVersion":1,"generatedAt":"2026-11-02T12:01:10+0300","canStop":false,
"active":true,"pending":false,"resumable":false,"running":true,"bundleId":"telegram","bundleType":"catalog","bundleName":"Telegram","action":"export",
"globalOperation":{"active":true,"pending":false,"resumable":false,"pid":23456,"scope":"routes","action":"export","bundleId":"telegram","startedAt":"2026-11-02T12:00:40+0300","updatedAt":null,"stale":false},
"progress":{"schemaVersion":2,"kind":"routes","backgroundOperationId":"op-…","canStop":false,"bundleId":"telegram","operation":"install","phase":"applying","current":120,"total":391,"percent":30,"currentRoute":"91.108.4.0/22","message":"Установка маршрутов в Keenetic.","running":true,"success":null,"rolledBack":false,"resumable":false,"stopRequested":false,"stoppedByUser":false,"resumed":false,"errorRoute":null,"pid":23999,"startedAt":"…","updatedAt":"…","completedAt":null}}}
```
* JS (`routes-operation.js`):
  * Poll: `refresh` (124-170); next poll after 1000 ms (`POLL_INTERVAL_MS`, :7) only when
    `current.active && !document.hidden && current.localOnly!==true && (current.running ||
    (current.globalOperation.active && !current.resumable))` (116-122).
  * Dispatches `broray:routes-operation-authoritative` with the data (137-139) and
    `broray:routes-operation-finished` when active→inactive (148-152).
  * `render` (46-109) reads `active`, `pending`, `running`, `resumable`, `bundleName`,
    `bundleId`, `bundleType`, `localPhase` (client-only), `action`,
    `progress.{operation,current,total,message,currentRoute,stopRequested}`.
    Title by `progress.operation` (if `progress` present) else `action`: delete→"Удаление маршрутов",
    update→"Обновление маршрутов", restore→"Восстановление маршрутов", resume→"Продолжение операции",
    export|sync|install→"Установка маршрутов", preflight→"Предварительная проверка",
    else "Операция с маршрутами" (35-44). Counter `"<current> из <total>"` (or "Подготовка" when
    `total==0 && pending`). Message `progress.message || "Операция выполняется."` + `" Остановка операций с маршрутами недоступна."` when `running`.
    Badge: running ⇒ "Выполняется"/"Останавливается"(stopRequested); else "Можно продолжить"
    (resumable) / "Ожидание". Stop button always hidden/disabled (98-99). Resume button visible
    iff `resumable && !running`.
  * Resume click (176-192): fires cancelable `broray:routes-operation-resume`
    `{bundleId,bundleType,bundleName}`; if not handled navigates to
    `/routes-custom.html?v=WebUI-3.1.0-r09c02&resume=<id>` (bundleType `"custom"`) else
    `/routes-import.html?v=WebUI-3.1.0-r09c02&resume=<id>`.
  * Catalog page consumes the authoritative event: `globalOperation = detail.globalOperation` if
    active, else `detail` if `detail.active` (`routes-catalog.js:1640-1652`).

### 2.4 POST `/api/routes/check.cgi?bundleId=<id>` — source update check
* Prefix-style query (§0.3). Body ignored.
* Validation order and errors (`check.cgi`): 405/401 (§0.1); 500 `ROUTES_CLI_UNAVAILABLE`
  "Команда управления маршрутами недоступна." (`:49-54`); 500 `ROUTES_CONFIGURATION_UNAVAILABLE`
  "Конфигурация маршрутов недоступна." if `routes/bundles.json` or `routes/config.json` unreadable
  (`:56-63`); 400 `ROUTES_BUNDLE_REQUIRED` "Не указан идентификатор набора маршрутов.";
  400 `ROUTES_BUNDLE_INVALID` "Некорректный идентификатор набора маршрутов."; 404
  `ROUTES_BUNDLE_NOT_FOUND`; 500 `ROUTES_API_LOCK_UNAVAILABLE` "Модуль блокировки операций недоступен.";
  409 `ROUTES_OPERATION_BUSY` / 500 `ROUTES_API_LOCK_FAILED` (§0.4, action `check`);
  500 `ROUTES_INTERFACE_POLICY_INVALID` "Разрешённый интерфейс маршрутов настроен неверно."
  unless `config.managedInterface` matches `^Proxy[0-9]+$` (`:113-131`);
  500 `ROUTES_BUNDLE_FILES_UNAVAILABLE` "Файлы выбранного набора маршрутов недоступны." if
  manifest or state unreadable (`:133-143`); 500 `ROUTES_BUNDLE_POLICY_INVALID`
  "Набор маршрутов не соответствует политике BROray." unless manifest
  `id==bundleId && targetInterface==managedInterface && exportComment=="BROray"` (`:145-157`).
  Runs `/opt/broray/bin/broray-routes check <id>` (`:159-168`). Then state validity
  (`schemaVersion==1`, `bundleId`, `status` string) else 500 `ROUTES_STATE_INVALID`
  "Состояние набора маршрутов повреждено." (details: last 20 stderr + 20 stdout lines, tail 30)
  (`:170-190`). CLI failure: rc 73|75 ⇒ 409 `ROUTES_OPERATION_BUSY` (+details); other ⇒
  **502 `ROUTES_CHECK_FAILED`** "Не удалось проверить обновление маршрутов." (+details)
  (`:192-217`). Final 500 `ROUTES_STATE_READ_FAILED` "Не удалось прочитать результат проверки маршрутов.".
* Success `data` = **entire state file** `+ {operation:{type:"check",completedAt:"%Y-%m-%dT%H:%M:%S%z",output:<last 30 stdout lines>}}` (`:219-244`).
* Side effects (CLI `broray_routes_check_run`, `lib/routes-source-check.sh:585-1139`):
  * Creates `/opt/broray/routes/state/<id>.json` if missing (`:453-500`, `:571-574`).
  * Resource lease `check` (§0.5); work dir `/opt/broray/routes/tmp/check-<id>.<pid>`.
  * HTTP (curl `-f -s -S -L --connect-timeout 5 --max-time 20`, headers
    `Accept: application/vnd.github+json`, `User-Agent: BROray-routes/1.1`; wget fallback
    `-T 20`) (`:128-184`):
    1. `https://api.github.com/repos/<repository>/commits?sha=<branch>&path=<directory>&per_page=1`
       — must be array with `[0].sha` (40 hex) and `[0].commit.committer.date // author.date` (`:705-737`).
    2. if manifest `source.discovery.mode=="all-bat"` (all 10 manifests):
       `https://api.github.com/repos/<repo>/contents/<dir>?ref=<sha>`; files = entries
       `type=="file"` with name ending `.bat` (case-insensitive), sorted by lowercase name
       (`:739-799`). Else manifest `source.files[]` with `enabled==true`.
    3. For each file `https://raw.githubusercontent.com/<repo>/<sha>/<dir>/<file>`; size>0;
       cumulative bytes ≤ `limits.maxSourceBytes`; parse (§4.1) (`:806-868`).
  * Computes `content_sha256` = sha256 of `LC_ALL=C sort -u` union of `network/prefix` lines;
    `source_set_sha256` = sha256 of TSV `name\tsha256\tsizeBytes` lines sorted by lowercase name (`:870-916`).
  * Baseline = the **downloaded** catalog (`routes/catalog/<id>/{normalized.txt,source-files.json,version.json}`),
    must exist & match `downloadedVersion.contentSha256` if `downloadedVersion != null` (`:918-946`).
  * File changes by name (`addedFiles`, `removedFiles`, `changedFiles` = sha or size differs,
    `unchangedFiles`); route changes `added`, `removed`, `unchanged = route_count - added` (`:948-974`).
  * Result (`:993-1013`):
    | condition | `checkResult.result` | `status` | message |
    |---|---|---|---|
    | no downloadedVersion | `initial_available` | `available` | "Найдено файлов: N. Маршрутов: M." |
    | no file & no route change | `no_changes` | `no_changes` | "Проверено файлов: N. Новых или изменённых маршрутов нет." |
    | files changed, routes same | `source_changed_routes_unchanged` | `update_available` | "Источник изменился, но итоговый список маршрутов не изменился. Обновление Keenetic не требуется." |
    | routes changed | `changed` | `update_available` | "На GitHub добавлено A, изменено C, удалено R файлов. Маршруты: добавлено a, удалено r, без изменений u." |
  * Writes state (`:1028-1094`): `status`, `availableVersion={sourceCommit,sourceDate,contentSha256,sourceSetSha256,sourceFileCount,sourceFiles[{name,parser:"windows-route-bat",sha256,sizeBytes,routeCount,htmlUrl}]}`,
    `routeCount`, `lastCheckedAt`, `lastError=null`, `checkResult={result,baselineContentSha256,currentContentSha256,baselineSourceSetSha256,currentSourceSetSha256,sourceFileCount,sourceFiles,fileChanges{addedFiles,removedFiles,changedFiles,unchangedFiles},routeChanges{before,after,added,removed,unchanged},sourceChanged,routesChanged,downloadRequired,keeneticUpdateRequired,discoveryMode:"all-bat" (constant),managedInterface,message}`, `updatedAt`.
    `htmlUrl = https://github.com/<repo>/blob/<sha>/<dir>/<file>`.
  * On any failure after binding: `status="check_error"`, `lastCheckedAt`, `lastError={message,at}` (`:425-451`), exit 1 with message on stderr (examples: "Не удалось получить сведения о версии <name> с GitHub.", "GitHub вернул ошибку: <api message>", "Файл X не прошёл проверку. Строка N: …").
  * stdout (becomes `operation.output`): message, `Набор: id`, `Управляемый интерфейс: ProxyN`, `Версия источника: <7 chars>`, `Дата источника`, `Файлов источника`, `Маршрутов`, `SHA-256 списка маршрутов`, `SHA-256 состава файлов` (`:1125-1133`).
* "New commit detection": there is no short-circuit on unchanged commit; every check re-downloads all files at the newest commit touching `<dir>` and compares content to the downloaded baseline.
* JS: action `check` → `executeOperation` (`routes-catalog.js:1409-1464`), 180 s timeout,
  stores returned state if `bundleId` present, toast "Проверка обновления «Name» завершена.";
  also step 1 of first download (`executeFirstDownload` 1466-1514) when `availableVersion` absent.

### 2.5 POST `/api/routes/download.cgi?bundleId=<id>`
* Identical validation/flow to check.cgi (diff: lease/action `download`, `download.cgi:101,156`).
  CLI failure: if details contain "Другая операция с маршрутами уже выполняется." ⇒ 409
  `ROUTES_OPERATION_BUSY`; else **502 `ROUTES_DOWNLOAD_FAILED`** "Не удалось загрузить файлы маршрутов." (`:187-211`).
  Read error: 500 `ROUTES_STATE_READ_FAILED` "Не удалось прочитать результат загрузки маршрутов.".
  (No rc 73/75 mapping here, unlike check.)
* Success: state file + `operation:{type:"download",completedAt,output}`.
* Side effects (`lib/routes-download.sh:117-690`):
  * Requires state `availableVersion.sourceCommit` (40-hex) else "Сначала выполните проверку новых маршрутов."; `sourceDate`, `contentSha256` (64-hex), `routeCount` integer (`:143-173`).
  * all-bat mode: file list from `availableVersion.sourceFiles` (else error "Сначала выполните проверку источника…") (`:211-230`).
  * Downloads each file from `raw.githubusercontent.com/<repo>/<availableVersion.sourceCommit>/<dir>/<file>`, re-parses, enforces limits, requires union count == state.routeCount, sha == availableVersion.contentSha256 ("Содержимое источника не совпало с проверенной версией. Экспорт запрещён."), source-set sha equality (`:270-439`).
  * Writes `routes/catalog/<id>/` atomically: `source/<files>`, `normalized.txt`, `manifest.json`, `source-files.json`, `routes.json` `{schemaVersion,bundleId,targetInterface,routeComment,contentSha256,routeCount,routes:[{family:"ipv4",network,prefix}]}`, `version.json` `{schemaVersion,bundleId,sourceCommit,sourceDate,contentSha256,sourceSetSha256,routeCount,sourceFileCount,sourceFiles,downloadedAt,targetInterface}`. Previous catalog moved to `routes/backup/catalog-<id>-<YYYYmmdd-HHMMSS>-<pid>` (never pruned here — UNCONFIRMED elsewhere) (`:542-566`).
  * State: `status = installed` if `installedVersion.contentSha256 == new sha` else `downloaded`; `downloadedVersion={sourceCommit,sourceDate,contentSha256,sourceSetSha256,sourceFileCount,sourceFiles}`; `routeCount`; `lastDownloadedAt`; `lastVerifiedAt=null`; `verifyResult=null`; `lastError=null`; `downloadResult={result,message,catalogPath,managedInterface,routeCount,sourceFileCount,sourceSetSha256,keeneticUpdateRequired}`; result/message: `already_downloaded` "Проверенная версия файлов уже загружена." / `files_updated_routes_unchanged` "Файлы маршрутов обновлены. Итоговый список маршрутов не изменился; обновление Keenetic не требуется." / `updated` "Файлы маршрутов обновлены. Набор готов к обновлению в Keenetic." / `downloaded` "Файлы маршрутов загружены. Набор готов к установке в Keenetic." (`:568-639`).
  * Failure: `status="download_error"`, `lastError={message,at}` (`:72-98`).
* JS: if no `availableVersion` → check then download (toast "Маршруты «Name» скачаны.", errors toasted directly, `routes-catalog.js:1466-1514`); otherwise `executeOperation(download)` (toast "Файлы маршрутов «Name» загружены.").

### 2.6 POST `/api/routes/verify.cgi?bundleId=<id>` ("Подготовить к установке")
* Prefix-style query. Errors (`verify.cgi`): 500 `ROUTES_CLI_UNAVAILABLE`; 400 `ROUTES_BUNDLE_REQUIRED`/`ROUTES_BUNDLE_INVALID`; 404 `ROUTES_BUNDLE_NOT_FOUND` (also when bundles.json unreadable or schemaVersion≠1, `:80-92`); 500 `ROUTES_API_LOCK_UNAVAILABLE`; 409/500 lock (action `verify`); 500 `ROUTES_STATE_UNAVAILABLE` "Локальное состояние маршрутов недоступно."; 500 `ROUTES_STATE_INVALID` "Локальное состояние маршрутов повреждено."; **409 `ROUTES_SET_NOT_DOWNLOADED`** "Сначала скачайте набор маршрутов." (`:130-135`).
* Step 1 `broray-routes build-export <id>` (§4.4). Failure ⇒ busy text ⇒ 409 `ROUTES_OPERATION_BUSY`; else state written with
  `verifyResult={result:"invalid_local",success:false,message:"Локальный набор маршрутов не прошёл проверку. Скачайте файлы заново.",checkedAt,contentSha256:downloadedVersion.contentSha256,local:{valid:false,routeCount,sourceFileCount,duplicateRouteCount:null,invalidRouteCount:null},keenetic:{checked:false,available:null,status:"not_checked",…null}}`,
  `lastError={code:"ROUTES_SET_INVALID",message,details}`, then **422 `ROUTES_SET_INVALID`** (message as above, details) (`:139-211`). State-write failure ⇒ 500 `ROUTES_VERIFY_STATE_WRITE_FAILED` "Не удалось сохранить результат проверки набора.".
* Step 2 `broray-routes plan <id>` (§4.5). Failure ⇒ busy text ⇒ 409; else `reason`/message by details substring (`:229-256`):
  `ROUTES_CONFIG_NDMC_LOCK_FAILED`→`keenetic_read_busy` "Keenetic временно занят чтением конфигурации. Повторите проверку через несколько секунд.";
  `ROUTES_CONFIG_NDMC_TIMEOUT|ROUTES_CONFIG_RUNNING_CONFIG_READ_FAILED|ROUTES_PLAN_RUNNING_CONFIG_READ_FAILED`→`keenetic_read_failed` "Keenetic не ответил на безопасное чтение конфигурации. Повторите проверку.";
  `ROUTES_PLAN_RUNNING_CONFIG_UNSUPPORTED`→`unsupported_route_format` "В конфигурации Keenetic есть статический маршрут неподдерживаемого формата. BROray не продолжит без безопасного разбора.";
  `ROUTES_PLAN_REGISTRY_INVALID`→`registry_invalid` "Внутренний реестр маршрутов BROray не прошёл проверку. Маршруты Keenetic не изменялись.";
  `ROUTES_PLAN_LOCAL_VERSION_MISMATCH`→`local_version_changed` "Локальные данные набора изменились во время проверки. Повторите подготовку набора.";
  `*откат*|*rollback*`→`rollback_required` "Проверка заблокирована: предыдущий откат маршрутов требует восстановления.";
  default `unknown` "Локальный набор исправен, но проверить его состояние в Keenetic не удалось.".
  State written with `verifyResult={result:"router_unavailable",success:false,failureReason,message,checkedAt,contentSha256,local:{valid:true,routeCount,sourceFileCount,duplicateRouteCount:0,invalidRouteCount:0},keenetic:{checked:false,available:false,status:"unavailable",reason,…null}}`, `lastError={code:"ROUTES_VERIFY_ROUTER_FAILED",message,details}` ⇒ **502 `ROUTES_VERIFY_ROUTER_FAILED`** (message = reason message) (`:258-315`).
* Plan validity else 500 `ROUTES_VERIFY_PLAN_INVALID` "Результат проверки Keenetic повреждён." (`:317-331`).
* Success computation (`:333-414`): `updatePending = installed && downloaded.contentSha256 != installed.contentSha256`;
  `keeneticStatus = conflict (if !canApply) | not_installed | update_pending | restore_required (toCreate>0 or sharedToRestore>0) | complete`;
  messages: conflict "Локальный набор исправен, но в Keenetic обнаружены конфликты: N."; not_installed "Набор исправен: T маршрутов, ошибок и дубликатов нет. В Keenetic набор не установлен."; update_pending "Набор исправен. Загруженная версия содержит T маршрутов и ожидает обновления в Keenetic."; restore_required "Набор исправен. В Keenetic найдено P из T маршрутов; отсутствует M."; complete "Набор исправен. В Keenetic найдено T из T маршрутов.".
  Persists `lastVerifiedAt`, `verifyResult={result,success:(status!="conflict"),message,checkedAt,contentSha256,local:{valid:true,routeCount:T,sourceFileCount,duplicateRouteCount:0,invalidRouteCount:0},keenetic:{checked:true,available:true,status,expectedRouteCount:T,presentRouteCount:managedExisting,missingRouteCount:toCreate,conflictCount,externalRouteCount:externalExisting,complete,updatePending,mode,canApply,targetInterface,managedMetric}}`, `lastError=null`, **`operation={type:"verify",completedAt,output:message}` stored in the state file**, `updatedAt`. Failures: 500 `ROUTES_VERIFY_STATE_BUILD_FAILED` "Не удалось сформировать результат проверки набора.", `ROUTES_VERIFY_STATE_WRITE_FAILED`, `ROUTES_STATE_READ_FAILED` "Не удалось прочитать результат проверки набора.".
* Success `data` = whole state file (no extra wrapper).
* Side effects include all Keenetic reads of build-export/plan (§4.5) and the side effects of `build-export` (writes `catalog/<id>/keenetic-routes.bat`, `export-plan.json`, `state.exportBuild`).
* JS: toast "Набор «Name» подготовлен к установке." unless `verifyResult.success===false` ⇒ `showPreparationFeedback({code:verifyResult.errorCode||verifyResult.code, message:verifyResult.message, details:verifyResult.details||verifyResult},"verify")` (`routes-catalog.js:1440-1442`).

### 2.7 POST `/api/routes/preflight.cgi?bundleId=<id>&action=export|delete|resume`
* Split-style query. Order (`preflight.cgi`): 405/401; 500 `ROUTES_CLI_UNAVAILABLE`; 500 `ROUTES_API_LOCK_UNAVAILABLE`; 500 `ROUTES_PREFLIGHT_UNAVAILABLE` "Модуль предварительной проверки недоступен."; 400 `ROUTES_BUNDLE_INVALID` (empty also); 400 `ROUTES_PREFLIGHT_ACTION_INVALID` "Некорректный тип предварительной проверки."; 404 `ROUTES_BUNDLE_NOT_FOUND`; lock action `preflight:<action>` ⇒ 409/500 (`:69-92`).
* Resolve (`lib/routes-operation-preflight.sh:63-87`): `export`→export; `delete`→delete; `resume`→ requires `routes/operations/<id>.json` with `resumable==true && running==false`, `operation` install|update|restore ⇒ export (saved op), delete ⇒ delete; else **409 `ROUTES_RESUME_NOT_READY`** "Для этого набора нет операции, которую можно продолжить."; other ⇒ 400 `ROUTES_PREFLIGHT_ACTION_INVALID` "Не удалось определить тип операции.".
* Plan: export ⇒ `broray-routes plan <id>` (§4.5); failure ⇒ busy text ⇒ 409 `ROUTES_OPERATION_BUSY` "Другая операция с маршрутами уже выполняется." (note different message text); else **409 `ROUTES_PREFLIGHT_PLAN_FAILED`** "Предварительная проверка установки в Keenetic не завершена." (+details). Delete ⇒ in-process delete plan (`routes-operation-preflight.sh:89-240`; reads `show running-config` fresh, cache removed) failure ⇒ **409 `ROUTES_PREFLIGHT_DELETE_FAILED`** "Предварительная проверка удаления из Keenetic не завершена.".
* Finalize (`:258-501`), failure ⇒ 500 `ROUTES_PREFLIGHT_BUILD_FAILED` "Не удалось сформировать результат предварительной проверки."; read failure 500 `ROUTES_PREFLIGHT_READ_FAILED`.
  * Interface check: `/opt/broray/bin/broray-system-ndmc -c "show interface <ProxyN>"` **directly (no ndmc-run lane, no timeout)**; `available` = rc 0; `connected` = line `^\s*connected:\s*yes\s*$`; `stateUp` = `^\s*state:\s*up\s*$` (`:316-323`).
  * Storage: `freeKb` = `df -Pk /opt/broray` col 4; `required_kb = max(4096, 2048 + 2*du -sk routes/catalog/<id> + (routeCount+3)/4)` (`:242-256`).
  * `localOk` = contentSha non-empty and routeCount > 0 (contentSha from plan or `downloadedVersion`; for delete from `installed/bundles/<id>.json .installedVersion.contentSha256`).
  * Write policy: `broray_keenetic_write_policy_static_routes_profile_check` rc 0 and 64-hex sha (`lib/keenetic-write-policy.sh:78-136`; staged SHA `dc85…ddfa`, candidate `3.0.0-r15c16`).
  * `ready = plan.canApply && interfaceOk && storageOk && localOk && writePolicyOk`. Message priority: ready "Предварительная проверка завершена. Операция готова к запуску."; !canApply "Операция заблокирована: обнаружены конфликты маршрутов или владения."; !interface "Операция заблокирована: Keenetic или управляемый интерфейс недоступен."; !storage "Операция заблокирована: недостаточно свободного места в /opt."; !policy "Операция заблокирована: протокол записи статических маршрутов Keenetic CLI не привязан."; else "Операция заблокирована: локальный набор не готов." (`:380-392`).
  * Token = sha256 of `"<bundle>|<requested>|<resolved>|<savedOperation>|<mode>|<epoch>|<contentSha>|<routeCount>|<resumeCurrent>|<resumeTotal>|none|<policySha>|<pid>"` (`:376-378`). Always produced (also when `ready=false`).
  * Persists result to `/opt/broray/routes/preflight/<id>.json` (mode 600, atomic) (`:494-499`).
  * DoT is never required (`dnsOverTls:{required:false,ok:true,binding:"none"}`, `:351-353`).
* Response (HTTP 200 even when `ready:false`):
```json
{"schemaVersion":1,"token":"<64 hex>","bundleId":"telegram","requestedAction":"export","resolvedAction":"export",
 "operation":"install","mode":"install","ready":true,"checkedAt":"…","checkedEpoch":1793606400,"expiresAfterSeconds":120,
 "contentSha256":"ab…","targetInterface":"Proxy0","targetInterfaceDisplay":"BROray","managedMetric":1200,
 "checks":{"operationLock":{"ok":true,"message":"Конфликтующих операций не обнаружено."},
   "ndmc":{"ok":true,"path":"/opt/broray/bin/broray-system-ndmc"},
   "keenetic":{"ok":true,"available":true,"interface":"BROray","technicalInterface":"Proxy0","connected":true,"stateUp":true},
   "storage":{"ok":true,"freeKb":812000,"requiredKb":4096},
   "localSet":{"ok":true,"routeCount":391,"duplicateRouteCount":0,"invalidRouteCount":0},
   "dnsOverTls":{"required":false,"ok":true,"binding":"none"},
   "keeneticWritePolicy":{"ok":true,"path":"static-routes","checkRc":0,"protocolSha256":"dc85…"},
   "ownership":{"ok":true,"conflictCount":0}},
 "summary":{"total":391,"alreadyPresent":0,"toCreate":391,"toDelete":0,"sharedKept":0,"alreadyAbsent":0,"conflicts":0,"externalKept":0,"unchanged":0,"addedToBundle":391,"removedFromBundle":0},
 "resume":{"operation":null,"current":0,"total":0},"message":"Предварительная проверка завершена. Операция готова к запуску."}
```
  `operation` = saved operation (delete/install/update/restore) or plan `mode` for export (`install|update|restore|none`).
* JS (`prepareLongOperation` 1516-1577, `confirmPreflight` 1388-1407, `preflightMessage` 1350-1386, `preflightTitle` 1342-1348): reads `ready`, `token`, `operation`, `requestedAction`, `message`, `checks.{operationLock.ok,ndmc.ok,keenetic.{ok,interface},storage.{ok,freeKb,requiredKb},localSet.{ok,routeCount,invalidRouteCount,duplicateRouteCount},dnsOverTls.{required,ok}}`, `summary.{total,alreadyPresent,toCreate,toDelete,sharedKept,alreadyAbsent,externalKept,conflicts}`, `resume.{current,total}`. `ready!==true` ⇒ rejected with the composed multi-line text as `Error.message` (shown in feedback panel). Confirm dialog texts: confirmText "Продолжить" (resume) / "Удалить" / "Обновить" / "Восстановить" / "Установить"; variant danger for delete. Then POST to the mutation with `preflightToken`.

### 2.8 POST `/api/routes/export.cgi|delete.cgi|resume.cgi?bundleId=<id>&preflightToken=<64hex>`
`export.cgi`/`delete.cgi` source (`.`) `lib/routes-web-action.sh` with `ROUTES_ACTION`; `resume.cgi` **execs** it (`resume.cgi:3-5`).
* Validation order (`routes-web-action.sh`): 405/401; 500 `ROUTES_ACTION_INVALID` "Операция с маршрутами настроена неверно."; 500 `ROUTES_CLI_UNAVAILABLE`; 500 `ROUTES_CONFIGURATION_UNAVAILABLE`; 400 `ROUTES_BUNDLE_REQUIRED`; 400 `ROUTES_BUNDLE_INVALID`; 404 `ROUTES_BUNDLE_NOT_FOUND`; 500 `ROUTES_API_LOCK_UNAVAILABLE`; 500 `ROUTES_PREFLIGHT_UNAVAILABLE`; lock (action `export|delete|resume`) 409/500 (`:47-131`).
* Token validation `broray_routes_operation_preflight_validate` (`routes-operation-preflight.sh:503-573`):
  rc 2 ⇒ **409 `ROUTES_PREFLIGHT_REQUIRED`** "Перед запуском выполните предварительную проверку и подтвердите актуальный план." (token not 64 lowercase hex, no stored file, or file mismatch: `bundleId`, `requestedAction`, `token`, `ready==true`); rc 3 ⇒ **409 `ROUTES_PREFLIGHT_EXPIRED`** "Предварительная проверка устарела. Выполните её повторно." (age < 0 or > `expiresAfterSeconds`=120); rc 4 ⇒ **409 `ROUTES_PREFLIGHT_CHANGED`** "Состояние набора изменилось после проверки. Выполните предварительную проверку повторно." (write-policy sha changed; export: `state.downloadedVersion.contentSha256` ≠ stored; delete: `installed/bundles/<id>.installedVersion.contentSha256` ≠ stored; resume: progress `current/total/operation` ≠ stored `resume.*`); other ⇒ 500 `ROUTES_PREFLIGHT_VALIDATE_FAILED` "Не удалось подтвердить предварительную проверку.". On success the preflight file is **deleted (single use)**.
* Policy (`:148-173`): `managedInterface` `^Proxy[0-9]+$` and `managedMetric == 1200` else 500 `ROUTES_POLICY_INVALID` "Политика управляемых маршрутов настроена неверно.". Manifest/state/`installed/bundles/<id>.json` readable else 500 `ROUTES_BUNDLE_FILES_UNAVAILABLE`; manifest policy else 500 `ROUTES_BUNDLE_POLICY_INVALID`.
* Readiness (`:206-255`): export needs `state.downloadedVersion != null` else **409 `ROUTES_EXPORT_NOT_READY`** "Маршруты ещё не загружены и не готовы к установке в Keenetic."; delete needs registry `installedVersion != null && managedRouteKeys|length>0` else **409 `ROUTES_DELETE_NOT_READY`** "Управляемые маршруты этого набора не установлены."; resume needs progress `resumable==true && running==false` else **409 `ROUTES_RESUME_NOT_READY`**; progress `operation` install|update|restore ⇒ result action export, delete ⇒ delete, else **409 `ROUTES_RESUME_TYPE_INVALID`** "Тип сохранённой операции не поддерживается.".
* Execution: `/opt/broray/bin/broray-routes <export|delete|resume> <id>` synchronously (`:257-266`). Exit 74/76 = paused ⇒ treated as success (`:276-279`). Failure: details (last 40 stderr + 40 stdout, tail 60) containing "Другая операция с маршрутами уже выполняется." or rc 73/75 ⇒ 409 `ROUTES_OPERATION_BUSY` (+details); else **502** `ROUTES_EXPORT_FAILED` "Не удалось установить маршруты в Keenetic." / `ROUTES_DELETE_FAILED` "Не удалось удалить маршруты из Keenetic." / `ROUTES_RESUME_FAILED` "Не удалось продолжить операцию с маршрутами." (+details) (`:281-322`).
* Post-checks (not when paused): state valid else 500 `ROUTES_STATE_INVALID` "Состояние набора маршрутов повреждено после операции."; export ⇒ `status=="installed" && installedVersion!=null && exportResult.result=="installed"` else 500 `ROUTES_EXPORT_RESULT_INVALID` "Результат установки не подтверждён локальным состоянием."; delete ⇒ `status=="downloaded" && installedVersion==null && deleteResult.result=="removed"` else 500 `ROUTES_DELETE_RESULT_INVALID` "Результат удаления не подтверждён локальным состоянием." (`:324-369`). Resume uses `result_action` for this check.
* Success `data` (`:378-414`): projection
```
{schemaVersion,bundleId,status,availableVersion,downloadedVersion,installedVersion,routeCount,
 lastCheckedAt,lastVerifiedAt,lastDownloadedAt,lastExportedAt,lastDeletedAt,lastError,checkResult,
 verifyResult,downloadResult,exportBuild,preflight,exportResult,deleteResult,resumeOperation,
 operationProgress:<raw routes/operations/<id>.json or null>,updatedAt,
 operation:{type:"export"|"delete"|"resume",completedAt,output:<last 60 stdout lines>}}
```
  `exportResult` (sync, `routes-router-sync.sh:1513-1528`): `{result:"installed",operation:"install|update|restore|none",message:"Маршруты установлены в Keenetic"|"…обновлены…"|"…восстановлены…"|"Маршруты в Keenetic проверены",total,created,deleted,sharedKept,sharedRestored,alreadyAbsent,unchanged,targetInterface,managedMetric:1200,configurationSaved:true,completedAt}`; export also sets `verifyResult` complete (`:1482-1511`) and `preflight = <sync plan>` (large: full `desired[]/obsolete[]`).
  `deleteResult` (`routes-router-delete.sh:298-345,1484-1531`): `{result:"removed",message:"Маршруты удалены из Keenetic",total,deleted,sharedKept,externalKept,alreadyAbsent,targetInterface,managedMetric:1200,configurationSaved:(deleted>0),completedAt}`; state `status = downloaded` if `downloadedVersion` else `not_checked` (the latter makes the CGI fail with `ROUTES_DELETE_RESULT_INVALID` — reproduce), `installedVersion=null`, `verifyResult=null`, `exportResult=null`, `preflight=null`.
* Async model: the HTTP request stays open for the whole operation (bounded by the 3600 s job deadline). JS timeout 8 h (`LONG_OPERATION_TIMEOUT_MS`, `routes-catalog.js:6,1411-1412`). Progress comes only from polling `operation-status.cgi` every 1 s (JS calls `BROrayRoutesOperationUI.notifyStarted` → `showPending(...,"starting",poll=true)`, `routes-operation.js:281-291`). Cancel: none — "Остановка операций с маршрутами недоступна. Дождитесь завершения операции." (`routes-catalog.js:1579-1582`, `routes-operation.js:172-174`).
* JS after response (`routes-catalog.js:1436-1463`): if `newState.operationProgress.resumable` ⇒ warning toast `operationProgress.message || "Операция приостановлена и может быть продолжена."`; else success toast "Маршруты «Name» применены в Keenetic." / "Маршруты «Name» удалены." / "Операция «Name» продолжена."; errors ⇒ `BROrayActionFeedback.show("routes-feedback", describe(error,"mutation"))`. Then `clearPending(true)` and reload catalog-summary.
* **resume.cgi defect (code reading; runtime UNCONFIRMED):** because `resume.cgi` uses `exec`, `$0` is `/opt/broray/lib/routes-web-action.sh`, which is not in the allowed list of `route-api-job.sh:14-25`; `broray_route_api_enter` returns 73 ⇒ `ROUTES_API_LOCK_FAILED` 500 "Не удалось установить блокировку операции." for every resume. `export.cgi`/`delete.cgi` source the file so `$0` stays correct. Decide whether 3.5.0 reproduces or fixes this.

### 2.9 POST `/api/routes/plan.cgi?bundleId=<id>` (no JS caller)
* Prefix query. Errors: 500 `ROUTES_CLI_UNAVAILABLE`; 500 `ROUTES_CONFIGURATION_UNAVAILABLE` (bundles.json only); 400/404 as usual; lock action `plan`; CLI rc 73|75 ⇒ 409 `ROUTES_OPERATION_BUSY`; other ⇒ **409 `ROUTES_PLAN_FAILED`** "Не удалось подготовить безопасный план установки в Keenetic." (details = last 40 stderr lines); 500 `ROUTES_PLAN_INVALID` "План установки маршрутов повреждён."; 500 `ROUTES_PLAN_READ_FAILED` (`plan.cgi:97-141`).
* Success `data` = sync plan object (§4.5) verbatim. Side effects: build-export + running-config read.

### 2.10 GET `/api/routes/progress.cgi?bundleId=<id>` (no JS caller)
* Prefix query; 400/404; 500 `ROUTES_PROGRESS_UNAVAILABLE` "Модуль прогресса операций недоступен."; 500 `ROUTES_PROGRESS_INVALID` "Состояние прогресса операции повреждено." (read failure or shape check `schemaVersion 1|2, kind routes, bundleId, running bool, current/total/percent numbers`) (`progress.cgi:68-94`). `data` = §4.6 progress object.

### 2.11 GET `/api/routes/status.cgi?bundleId=<id>` (no JS caller)
* No `set -u`. Prefix query; 400/404; 500 `ROUTES_STATE_UNAVAILABLE`; 500 `ROUTES_STATE_INVALID` "Локальное состояние маршрутов повреждено." (details = jq stderr); 500 `ROUTES_STATE_READ_FAILED` "Не удалось прочитать локальное состояние маршрутов." (`status.cgi:72-208`).
* `data` = state projection (same field list as §2.8 minus `resumeOperation`,`operation`) + `routerPresence` (from `lib/routes-router-presence.sh:20-192`: `{available,registered,source,checkedAt,cacheAgeSeconds,expectedRouteCount,presentRouteCount,missingRouteCount,duplicateRouteCount,complete,actualInstalled,drift,status:"complete|absent|partial|not_registered|unavailable",missingRoutes:[≤50 {key,destination,interface,metric,matchCount}]}`; default unavailable object `status.cgi:103-116`) + `operationProgress` (§4.6, fallback idle without `resumable` fields `:128-150`) + `globalOperation` (§2.3).
  Presence match: per registry `routeKeys` (`ipv4|<net>/<p>|<if>|gateway:none|metric:1200|…`) count running-config routes with same destination, interface, gateway 0.0.0.0, metric (null⇒1000), proto static; `present` iff exactly 1. Uses running-config cache TTL 5 s (`routes-router-config.sh:11`).

### 2.12 GET `/api/routes/summary.cgi[?bundleId=<id>]` (no JS caller; `lib/broray-page.sh:3502` uses the library directly)
* Query: empty ⇒ all (requires valid `managedInterface` else 500 `ROUTES_INTERFACE_POLICY_INVALID`); `bundleId=…` ⇒ one (400 `ROUTES_BUNDLE_INVALID`); anything else ⇒ 400 `ROUTES_QUERY_INVALID` "Некорректные параметры запроса." (`summary.cgi:46-86`). Library failure ⇒ 500 with code/message parsed from the last stderr line `BRORAY_ERROR:<CODE>:<message>` (default `ROUTES_SUMMARY_FAILED` "Не удалось получить summary маршрутов."); shape failure ⇒ 500 `ROUTES_SUMMARY_INVALID` "Модуль маршрутов вернул некорректный summary." (`:94-155`); module missing ⇒ 500 `ROUTES_SUMMARY_UNAVAILABLE` "Summary маршрутов недоступен.".
* Defect: in `bundleId` mode `managed_interface` is never assigned but referenced under `set -u` at `:147` ⇒ the shell aborts with no output (HTTP result depends on lighttpd; UNCONFIRMED, presumably 500 with no JSON).
* `data`: one-bundle object per `lib/routes-summary.sh:397-479` (fields `schemaVersion,bundleId,state,sourceState,healthState,installed,recordedInstalled,verifiedInstalled,downloaded,available,updateAvailable,sourceUpdateAvailable,keeneticUpdateAvailable,downloadRequired,downloadActionRequired,verificationRequired,verificationCurrent,localSetValid,verificationConflict,verificationRetryRequired,keeneticUpdateRequired,exportRequired,operationRunning,recommendedAction,routeCount,installedRouteCount,managedRouteCount,externalRouteCount,globalOwnedRouteCount,managedInterface,managedMetric,availableVersion,downloadedVersion,installedVersion,verifyResult,lastCheckedAt,lastVerifiedAt,verificationFreshness{state,ageSeconds},lastDownloadedAt,lastExportedAt,lastDeletedAt,lastOperation,registryConsistent,consistent,actionRequired,error,updatedAt`); all-mode object `routes-summary.sh:487-663` (`schemaVersion,statusContractVersion,bundles,availableBundles,installedBundles,verifiedInstalledBundles,recordedInstalledBundles,installed,recordedInstalled,downloaded,updatesAvailableCount,sourceUpdateAvailable,keeneticUpdateRequired,operationRunning,actionRequired,attentionCount,installedRouteCount,healthy,registryConsistent,consistent,error,health{…}`). Full derivation not reproduced here — port `routes-summary.sh` 1:1 if needed (it belongs to the home/broray status area).

### 2.13 POST `/api/routes/stop.cgi`
* Method POST + session, then always **409 `ROUTES_STOP_NOT_SUPPORTED`** "Остановка операций с маршрутами недоступна. Дождитесь завершения операции." (`stop.cgi:16-21`). No query parsing, no side effects. CLI `broray-routes stop <id>` prints the same text and exits 1 (`bin/broray-routes:165-169`).

---

## 3. Client-side catalog state machine (must be satisfied by response data)

All in `routes-catalog.js`; the backend must provide the fields with identical semantics.

### 3.1 Version comparison
* `sameRouteVersion(a,b)` (185-192): both present; if both have `contentSha256` compare it; else `sourceCommit` and `sourceDate` string-equal.
* `sameSourceVersion` (194-200): prefer `sourceSetSha256` when both present, else `sameRouteVersion`.
* Server side twin `version_equal` (`routes-page-summary.sh:382-391`): contentSha256, then sourceSetSha256, then commit+date — **different precedence** from `sameSourceVersion` (server prefers content). Keep both as-is.

### 3.2 Derived predicates
* `downloadRequired` = `availableVersion` && !sameSourceVersion(available, downloaded) (259-262).
* `sourceUpdateAvailable` = downloadedVersion && downloadRequired (264-266).
* `localSetInvalid` = `verifyResult.local.valid===false` && downloadedVersion && `verifyResult.contentSha256 == downloadedVersion.contentSha256` (274-279).
* `verificationCurrent` (281-307): requires `verifyResult.local.valid===true`, contentSha match; if no `routerPresence` or no `verifyResult.keenetic` ⇒ true; presence unavailable ⇒ `keenetic.available===false && status=="unavailable"`; `keenetic.available!==true` ⇒ false; not registered ⇒ status ∈ {not_installed, conflict}; complete ⇒ status ∈ {complete, update_pending, conflict}; else ∈ {restore_required, conflict}. (Server applies the same filter before sending `verifyResult`, `routes-page-summary.sh:434-457`.)
* `verificationRequired` = downloadedVersion && !localSetInvalid && !verificationCurrent.
* `verificationConflict` = current && `keenetic.status=="conflict"`; `verificationRetryRequired` = current && (`keenetic.available===false` || status `unavailable`).
* `hasRouterDrift` = installedVersion && presence.available && presence.registered && presence.drift===true.
* `isActuallyInstalled` = presence.available && registered && `actualInstalled===true`.
* `routerStateUnknown` = installedVersion && (!presence || !available || !registered).
* `keeneticAction` (334-352): null without downloadedVersion or when routerStateUnknown; drift ⇒ `{mode:"restore",text:"Восстановить в Keenetic"}`; !installedVersion or `actualInstalled===false` ⇒ install "Установить в Keenetic"; !sameRouteVersion(downloaded, installed) ⇒ update "Обновить в Keenetic".
* `nextAction`, `actionVisibility` (354-383): buttons download/verify/export(keenetic)/check visibility; check ("Проверить обновления") only when actually installed and nothing else pending.

### 3.3 Catalog states (user-visible badge, `statusPresentation` 389-434, first match wins)
| condition | badge text | class |
|---|---|---|
| progress resumable & !running | "Остановлено" (stoppedByUser) / "Можно продолжить" | warning |
| localSetInvalid | "Набор повреждён" | danger |
| retry required or routerStateUnknown | "Состояние Keenetic не определено" | warning |
| `lastError` | "Ошибка" | danger |
| conflict | "Обнаружен конфликт" | danger |
| drift (partially installed) | "Требуется восстановление" | warning |
| sourceUpdateAvailable (outdated) | "Источник изменился" (checkResult.routesChanged===false) / "Доступно обновление" | warning |
| verificationRequired (downloaded) | "Требуется проверка" | warning |
| keenetic update | "Готово обновление" | warning |
| keenetic install | "Готово к установке" | neutral |
| actually installed | "Установлено" | success |
| availableVersion (not downloaded) | "Доступно для загрузки" | neutral |
| else | "Источник не проверен" | neutral |
Overrides in `renderCard` (1023-1029): running ⇒ "Выполняется"/"Останавливается"; operation of this card ⇒ "Подготовка"; other card busy ⇒ "Временно недоступно".
Server-side `state.status` values (not shown by JS): `not_checked`, `available`, `no_changes`, `update_available`, `check_error`, `download_error`, `downloaded`, `installed`. Server attention flag (`routes-page-summary.sh:458`) = lastError || drift || downloadRequired || keeneticUpdateRequired || (downloadedVersion && verification not current).

### 3.4 Messages and details
`messageFor` (452-537), `operationOutput` (952-974), detail rows (630-649, 1032-1062) depend on server wording: `verifyResult.message`, `lastError` (string or `.message`, `.details`), `checkResult.{message,result,fileChanges,routeChanges,sourceChanged,routesChanged}`, `routerPresence.{missingRouteCount,expectedRouteCount}`, `operation.output`, `exportResult.message`, `downloadResult.message`, `deleteResult.message`. Keenetic verification labels map `keenetic.status`: complete→"Соответствует", not_installed→"Не установлен", update_pending→"Ожидает обновления", restore_required→"Требуется восстановление", conflict→"Конфликт", unavailable→"Недоступен", other→"Проверен" (1042-1051). Interface row fallback chain: summary `managedInterfaceDisplay` → `exportBuild.targetInterfaceDisplay` → `checkResult.managedInterfaceDisplay` → `exportResult.targetInterfaceDisplay` → "BROray" (1009-1012; the latter three fields are never produced by the backend). Metric: `exportBuild.managedMetric` → `exportResult.managedMetric` → 1200.

### 3.5 Counters
* Card metrics: `route-count` = `state.routeCount||0`; `source-count` = number of `availableVersion.sourceFiles` (fallback `checkResult.sourceFiles`); `presence-count` = `"<presentRouteCount>/<expectedRouteCount>"` when `routerPresence.registered` else "—" (1032-1034).
* "Уникальных маршрутов" = `ownership.uniqueRouteCount` = registry routes owned by this bundle with exactly one owner; "Пересечений с другими наборами" = `ownership.sharedRouteCount` = owned routes with >1 owner (`routes-page-summary.sh:424-426,482`; JS 1060-1061).
* Page totals: installed = `totals.installedCount` (= verifiedInstalled count), attention = `totals.attentionCount`, total = `totals.bundleCount` (1200-1202).
* Hub: "Управляемых маршрутов" = `totalManagedRoutes`; "Пересечений наборов" = `sharedRoutes` (§2.2).
* `verifiedInstalled` = actualAvailable && registered && complete; `registered` = bundle registry `installedVersion!=null` and `routeKeys` non-empty; `complete` = expected>0, no missing (count==0), no duplicates (count>1). Matching key: `<network/prefix>|<interface>|<metric>` where registry key metric parsed from `metric:1200` segment and running-config metric defaults to **1000** when absent (`routes-page-summary.sh:392-433`).

### 3.6 Per-bundle icons
Icons are **client-side only**: inline SVG strings in `ROUTE_LOGOS` (`routes-catalog.js:26-37`), bound by bundle id in `BUNDLES` (39-110, also `name`, `description`, GitHub `source` link per card). The backend sends no icon data; unknown ids fall back to `data-icon="routes"` (588-593). Button icons are `data-icon` names (`update`, `backup`, `status`, `routes`, `restore`, `delete`, `chevron`, `security`, `access`, `search`) resolved by `icons.js`.

### 3.7 Busy / global operation messages (`globalOperationMessage` 775-800)
Depends on `globalOperation.{active,pending,resumable,scope,action,bundleId}`: pending/resumable ⇒ "Операция с маршрутами «Name» приостановлена. Завершите её кнопкой «Продолжить»."; `scope=="system"` with action update/restore/uninstall/cleanup/reinstall/other; action prefix `preflight:` / `custom:`; actions `check`, `download`, `verify`/`plan`, `delete`, `resume`, default. Exact strings are in the source lines cited.

---

## 4. Algorithms and rules (backend)

### 4.1 Source `.bat` parser (`routes-source-check.sh:203-423`)
Per line: strip `\r`, trim; skip empty, `#…`, `::…`, `rem`, `echo`/`@echo`, `chcp`, `title`, `pause`, `cls`, `setlocal`, `endlocal`, `exit /b` (case-insensitive). Required: ≥6 fields, `route add <IPv4> mask <mask> 0.0.0.0`; extra fields only `& rem …` (≥8 fields, field7 `&`, field8 `rem`). Mask must be contiguous; gateway exactly `0.0.0.0`; network normalized to mask boundary; `0.0.0.0/0` rejected. Errors `"Строка N: <reason>"` (first 8 lines appended to the message). Output `network/prefix`, `LC_ALL=C sort -u`; 0 routes ⇒ "В источнике не найдено маршрутов."; > maxRoutes ⇒ "Количество маршрутов превышает лимит: n > max".
Validators: repository `owner/name` `[A-Za-z0-9_.-]`; branch `[A-Za-z0-9_.-]+`; directory no leading `/`, no trailing `/`, no `..`, chars `[A-Za-z0-9_./()-]`; filename no `/`, no `..`, chars `[A-Za-z0-9_.()-]` (`:38-92`).

### 4.2 Route manifests (`share/routes/manifests/*.json`, identical to `runtime/state-seed/routes/manifests/`)
Shape: `{schemaVersion:1,id,name,source:{provider:"github",repository:"RockBlack-VPN/ip-address",branch:"main",directory,files:[{name,type:"windows-route-bat"|"domain-list",enabled,required?}],discovery:{mode:"all-bat",recursive:false}},targetInterface:"Proxy0",exportComment:"BROray",limits:{maxSourceBytes,maxRoutes}}` (telegram also `bundleType:"extended"`).
| id | name | directory | limits bytes / routes |
|---|---|---|---|
| telegram | Telegram | Global/Telegram | 1048576 / 2048 |
| whatsapp | WhatsApp | Global/Whatsapp | 4194304 / 10000 |
| youtube | YouTube | Global/Youtube | 4194304 / 10000 |
| chatgpt | ChatGPT | Global/ChatGPT(OpenAI) | 4194304 / 10000 |
| facebook | Facebook | Global/Facebook | 4194304 / 10000 |
| instagram | Instagram | Global/Instagram | 4194304 / 10000 |
| meta | Meta | Global/Meta | 4194304 / 10000 |
| tiktok | TikTok | Global/TikTok | 4194304 / 10000 |
| speedtest | Speedtest | Global/speedtest | 4194304 / 10000 |
| wikipedia | Wikipedia | Global/wikipedia | 65536 / 256 |
Install copies them to `/opt/broray/routes/manifests/` replacing `targetInterface` with `config.managedInterface` (`lib/package-setup.sh:384-400`; `lib/routes-runtime-repair.sh:356-372`). Bind validation in check/download (`routes-source-check.sh:502-583`): provider github, files non-empty, discovery null or `all-bat`/non-recursive, `targetInterface == config.managedInterface`, positive numeric limits.
Seed state per bundle (`routes-runtime-repair.sh:382-406`): all null, `status:"not_checked"`; seed registry `installed/bundles/<id>.json` `{schemaVersion:1,bundleId,installedVersion:null,routeKeys:[],managedRouteKeys:[],externalRouteKeys:[],targetInterface,managedMetric:1200,installedAt:null,removedAt:null,updatedAt}` (`:441-456`).
`routes/config.json` seed: `{schemaVersion:1,managedInterface:"Proxy0",routeComment:"BROray",routeIdentity:[…],ownershipPolicy:{adoptExistingRoutes:false,modifyExternalRoutes:false,deleteExternalRoutes:false,touchOtherInterfaces:false,deleteOnlyExactManagedMatch:true,restoreMissingRoutesAutomatically:false},managedMetric:1200}` (`runtime/state-seed/routes/config.json`).

### 4.3 Managed interface requirement
* `config.managedInterface` must match `^Proxy[0-9]+$` (all CGIs/libs, e.g. `routes-web-action.sh:158-164`), `managedMetric == 1200`, `routeComment == "BROray"`, ownershipPolicy flags as seeded (`routes-router-sync.sh:1107-1124`).
* Manifest `targetInterface == managedInterface`, `exportComment == "BROray"`.
* Display name: `/opt/broray/config/interface.json .description` if exactly `BROray` or `BROray - …`, else `BROray` (`routes-page-summary.sh:20-33`, `routes-operation-preflight.sh:292-293`).
* Before any write: `show interface <ProxyN>` must contain `connected: yes` and `state: up` (preflight `routes-operation-preflight.sh:316-323`; apply `routes-router-sync.sh:960-976`). Only commands for that interface are authorized (§4.7).

### 4.4 build-export (`lib/routes-export-build.sh:68-538`, CLI `broray-routes build-export`)
Catalog validity (`routes-runtime-repair.sh:116-142`) else "ROUTES_CATALOG_INVALID: локальный каталог повреждён или не скачан. Проверить обновления / Скачать заново."; lease `build-export`; writes `catalog/<id>/keenetic-routes.bat` (CRLF lines `route add <net> mask <mask> 0.0.0.0 metric 1200 :: rem BROray`, validated by regex) and `catalog/<id>/export-plan.json` `{schemaVersion:1,bundleId,sourceCommit,sourceDate,contentSha256,targetInterface,managedMetric:1200,routeComment:"BROray",routeFile:"keenetic-routes.bat",routeFileSha256,routeCount,preparedAt,routerApplied:false,routes:[{key:"ipv4|<net>/<p>|<if>|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",family:"ipv4",network,prefix,mask,targetInterface,gatewayToken:"0.0.0.0",metric:1200,automatic:null,exclusive:null,comment:"BROray"}]}`; state `exportBuild={result:"prepared",message:"Маршруты готовы к экспорту",sourceCommit,contentSha256,routeFileSha256,routeFile,planFile,routeCount,targetInterface,managedMetric:1200,routeComment,routerApplied:false,preparedAt}`, `lastError=null`. **The `.bat` file is never served to the browser** (no endpoint; only `lib/interface-owner.sh:424` references it). No Keenetic command.

### 4.5 Sync plan (`broray-routes plan` = build-export + `broray_routes_sync_plan`, `routes-router-sync.sh:1083-1374`)
Lease `sync`; blocked by `routes/rollback-required.json`; ensures/validates global registry `installed/routes.json` (rebuilds from bundle registries + export plans if missing, `:978-1081`; failure "ROUTES_PLAN_REGISTRY_INVALID…"). Requires state `downloadedVersion.contentSha256 == export-plan.contentSha256` (else stderr `ROUTES_PLAN_LOCAL_VERSION_MISMATCH`). Deletes the config cache and reads `show running-config` (stderr `ROUTES_PLAN_RUNNING_CONFIG_READ_FAILED`); any unparseable `ip route` line ⇒ `ROUTES_PLAN_RUNNING_CONFIG_UNSUPPORTED count=N reasons=…`.
Classification per desired route (identity = destination + interface + gateway 0.0.0.0 + proto static; attribute = metricExplicit && metric==1200; target = destination + interface): `conflict` | `managed_existing` (registered & createdByBROray & managed) | `external_existing` | `create`. Obsolete (old `managedRouteKeys` not desired): `conflict`/`shared_keep`/`shared_restore`/`delete`/`already_absent`. `blocking` = desired conflict or external_existing + obsolete conflict. `mode` = install (no installedVersion) | update (sha differs) | restore (any create) | none.
Output `{schemaVersion:1,bundleId,mode,targetInterface,managedMetric:1200,identityModel:"destination-interface-gateway",contentSha256,checkedAt,canApply,summary:{total,addedRoutes,removedRoutes,unchangedRoutes,toCreate,managedExisting,externalExisting,conflicts,toDelete,sharedKept,sharedToRestore,alreadyAbsent},desired:[{route,status,identityMatchCount,attributeMatchCount,targetMatchCount,registered,existingOwners}],obsolete:[…],blocking:[…],message}`; messages "Обнаружены конфликты или внешние маршруты на управляемом интерфейсе." / "План установки маршрутов подготовлен." / "План обновления…" / "План восстановления…" / "Изменения в Keenetic не требуются.".
Delete preflight plan (`routes-operation-preflight.sh:139-230`): per `managedRouteKeys`: `conflict` (registry mismatch, not createdByBROray/managed, wrong interface, not owner, identity>1, attr mismatch), `shared_keep` (>1 owners), `delete`, `already_absent`; `summary.total = routeKeys|length`, `externalExisting = externalRouteKeys|length`, `removedRoutes = total`.

### 4.6 Progress file (`lib/routes-operation-progress.sh`)
* Path `/opt/broray/routes/operations/<id>.json` (atomic, mode 600 in a job else 644) + counter `<id>.counter` (`"<current>\t<route>"`, updated per tick). Written only on begin/update/complete/fail/pause (`:116-193`); ticks only rewrite the counter (`:83-114`).
* Object `{schemaVersion:2,kind:"routes",backgroundOperationId,canStop:false,bundleId,operation:"install|update|restore|delete",phase,current,total,percent,currentRoute,message,running,success,rolledBack,resumable,stopRequested,stoppedByUser,resumed,errorRoute,pid,startedAt,updatedAt,completedAt}`.
* `percent = floor(current*100/total)`, `total==0` ⇒ 100 if success else 0.
* Phases: `preparing` (begin), `applying`, `verifying`, `saving`, `completed`, `failed`, `rolled_back`, `paused` (stoppedByUser), `failed_resumable`, `stopping`, `interrupted` (read-time), `idle` (no file).
* Read (`:438-527`): validates shape; overrides `current`/`currentRoute` from counter, recomputes percent, `canStop=false`; if `running==true` and (pid dead or `routes/locks/operation.lock/pid` ≠ pid) ⇒ `phase:"interrupted",running:false,success:false,resumable:false,message:"Операция неожиданно завершилась. Выполните проверку набора перед продолжением."`.
* Resume base: `broray_routes_progress_resume_values` (`:374-402`) — if saved file resumable, same operation, and `total-current == remaining`, continue from `current`; else start at 0.
* Stop: `broray_routes_progress_stop_requested` always false; `request_stop` returns 4 (`:361-372`).
* Note: during the phase before `progress_begin` (save + plan + baseline + interface check) the previous progress file (e.g. a completed one) is still reported by operation-status.

### 4.7 Keenetic commands, authorization and execution
Exact allowed grammar (`routes-router-config.sh:68-126`):
* Reads: `show running-config`, `more startup-config`, `show interface ProxyN`.
* Writes (require write policy, §2.7): `ip route <A.B.C.D> <mask> ProxyN 1200`, `no ip route <A.B.C.D> <mask> ProxyN`, `system configuration save`. Anything else ⇒ rc 126.
Execution path: `broray_routes_ndmc_capture` (`lib/routes-ndmc.sh:3-25`) ⇒
`/opt/broray/bin/broray-ndmc-run /opt/broray/run/routes-router-ndmc.lock.guard /opt/broray/run/routes-router-ndmc.lock <wait=limit+5> <limit> /opt/broray/bin/broray-system-ndmc "<cmd>"` ⇒ `/bin/ndmc -c "<cmd>"` with `LD_LIBRARY_PATH`/`LD_PRELOAD` unset (`bin/broray-system-ndmc`).
`broray-ndmc-run` (`native/broray-ndmc-run.c:56-136`): returns 125 if the legacy dir `<lane>` exists or flock not obtained within wait; kills the process group with TERM at `limit` (exit 124) then KILL after 1 s; drains descendants before releasing the lane. rc 124 ⇒ appends `ROUTES_CONFIG_NDMC_TIMEOUT`, 125 ⇒ `ROUTES_CONFIG_NDMC_LOCK_FAILED` to stderr.
Timeouts: reads 10 s (baseline/rollback reads 15 s), `ip route` 10 s (sync), `no ip route` 10 s (sync) / 8 s (delete), `system configuration save` 15 s.
Exception: preflight's `show interface` is run directly without the runner/lane and without timeout (`routes-operation-preflight.sh:317`).
Running-config parser (`routes-router-config.sh:220-453`): lines `ip route <net> <if> [opts]` (host form ⇒ /32), `ip route <net> <mask> [<gw>] <if> [opts]`; opts: `auto`, `exclusive`, one integer metric; anything else ⇒ unknown (serialization incomplete ⇒ mutations refused).

### 4.8 Apply = export (`broray-routes export` ⇒ build-export then `broray_routes_sync_apply`, `routes-router-sync.sh:1577-1922`)
Order of Keenetic interaction:
1. Write policy check; lease `sync`; ensure global registry.
2. `system configuration save` + convergence: up to 12 rounds (1 s apart) of `show running-config` + `more startup-config`; needs 2 consecutive identical (byte-equal running==startup) digests (`routes-router-config.sh:655-708`).
3. Build plan (fresh `show running-config`); abort unless `canApply`.
4. Baseline: `show running-config` + `more startup-config` (15 s); route state must equal plan snapshot and startup; non-route config (ignoring `! $$$ Last change/Username/Md5 checksum` and `ip route` lines) must be identical; digest stored (`:392-434`).
5. `show interface ProxyN` (connected yes, state up).
6. Transaction dir `/opt/broray/routes/transactions/<YYYYmmdd-HHMMSS>-sync-<id>-<pid>/` with `original/{routes.json,bundle.json,state.json,export-plan.json,router-export-result.json|result.missing}`, `plan.json`, `running-config-before.json`, `transaction.json` phases `planned → router_verified → local_committed → committed` (or `paused`, `rolled_back`, `resume_attempt_rolled_back`, `rollback_failed`).
7. Progress begin (operation = mode, `none`→`install`; total = creates+shared_restore+deletes, or resumed values), update `applying`.
8. For each `create` (export-plan order = C-locale sorted `net/prefix`) then each `shared_restore`: append rollback intent, `ip route <net> <mask> ProxyN 1200`, tick progress (`current += 1`, `currentRoute = net/prefix`). **Batching**: after every `BRORAY_SYNC_VERIFY_BATCH_SIZE` (default 16, clamped 1..128) commands, verify the batch via `show running-config` (≤12 attempts, 1 s sleep; each route identity==1 and attribute==1) (`:840-886`). Remainder batch verified at the end.
9. For each obsolete `delete` (old managedRouteKeys order): `no ip route <net> <mask> ProxyN`, same batch verification for absence.
10. Phase `verifying`: fresh `show running-config`; all desired + shared_keep/shared_restore present with metric 1200, delete/already_absent absent.
11. Commit local files atomically: `installed/routes.json` (owners updated), `installed/bundles/<id>.json` (`installedVersion=downloadedVersion`, `routeKeys=managedRouteKeys=desired keys`, `externalRouteKeys:[]`), state (§2.8), `catalog/<id>/export-plan.json` (`routerApplied:true`, `configurationSaved:true`, `appliedAt`, `applyResult`), `catalog/<id>/router-export-result.json`.
12. Phase `saving`: `system configuration save` + convergence. Progress `completed` "<progress message> Выполнено T из T.".
Failure anywhere ⇒ `broray_routes_sync_abort` (`:900-947`): restore local backups, rollback router (re-add deleted in reverse, remove added in reverse, save, then up to 12 (≤30) verification rounds comparing to baseline incl. running/startup and non-route digest, `:465-580`). Success ⇒ progress `rolled_back` "<msg> Изменения операции отменены." (or resumed attempt: restore previous resumable progress). Rollback failure ⇒ `routes/rollback-required.json` `{schemaVersion:1,kind:"routes-rollback-required",blocksNewMutations:true,work,message}` (`routes-resource-lock.sh:17-25`), progress `failed` "…Требуется восстановление; новые изменения маршрутов заблокированы.", lease possibly kept. Exit 1 (or 129/130/143 for signals).
Pause in sync is effectively unreachable (stop never requested; route failures abort) — `broray_routes_sync_pause` exits 74/76 only via `stop_if_requested`/`route_failure`, neither of which fires (`:827-898`).

### 4.9 Delete (`broray-routes delete` ⇒ `broray_routes_router_delete_run`, `routes-router-delete.sh:869-1379`)
Pre-checks: write policy, bundles.json, files, interface `ProxyN`, config policy, registry `installedVersion != null`, every managed key owned in global registry. Lease `delete` (lock also writes `operation`, `startedAt`). Fresh `show running-config` (complete serialization). Candidate list = registry routes owned **only** by this bundle (shared ones are kept), filtered to those physically present (`broray_routes_delete_filter_configured`). `already_absent = managed - shared - physical`.
Loop (registry order): verify interface/metric 1200, append rollback intent, `no ip route <net> <mask> ProxyN` (8 s). Command failure ⇒ **pause** (`failed_resumable`, exit 76). Every 16 (`BRORAY_ROUTES_DELETE_VERIFY_BATCH_SIZE`, clamp 1..128) verify absence via `show running-config` (≤12 attempts); failure ⇒ pause (exit 76). Progress `current` advances only for verified batches (`:776-796`).
Pause (`:664-711`): if any delete happened, `system configuration save` + convergence; partial commit (state `status:"installed"`, `resumeOperation:{operation:"delete",resumable:true,current,total,deleted,alreadyAbsent,message,errorRoute,updatedAt}`, `lastError={code:"ROUTES_DELETE_PAUSED_AFTER_ERROR",message,route,resumable:true}` if errorRoute; registries reduced), progress `failed_resumable`, `resumable:true`; exit 76. HTTP layer returns **200 success** with `operationProgress.resumable==true`.
Finish: verify result against `show running-config` (cache snapshot after cache deletion), install local state (§2.8), phase `saving`, `system configuration save` + convergence (only if something was deleted), progress `completed` "Удаление маршрутов из Keenetic завершено: T из T.". Transaction file `/opt/broray/routes/transactions/delete-<id>-<stamp>.json` phases `checked → deleting → paused|committed`. Errors before commit roll back (re-add) and restore local files; rollback failure ⇒ rollback-required marker.

### 4.10 Resume
CLI `broray-routes resume <id>` reads progress; requires `resumable && !running`; dispatches `delete` (operation delete) or `export` (install|update|restore) (`bin/broray-routes:170-201`). The underlying run continues from `current` via resume values. Coordinator only admits `resume`/`preflight:resume` for the paused bundle (§0.4). JS entry points: float "Продолжить" button or `?resume=<id>` in `routes-import.html` (auto-starts `prepareLongOperation(bundle,"resume", exportButton)` after first summary load, `routes-catalog.js:1711-1716`; listener 1676-1686).

### 4.11 Timers / limits summary
| item | value | source |
|---|---|---|
| JS short request timeout | 180 000 ms | `routes-catalog.js:5` |
| JS long op timeout (export/delete/resume) | 8 h | `routes-catalog.js:6` |
| Catalog passive refresh | 30 000 ms | `routes-catalog.js:8` |
| Operation window poll | 1000 ms | `routes-operation.js:7` |
| Hub overview refresh | 60 000 ms | `routes-overview.js:4` |
| `PROGRESS_POLL_INTERVAL_MS` | 750 (declared, unused) | `routes-catalog.js:7` |
| Preflight token TTL | 120 s, single use | `routes-operation-preflight.sh:11,571` |
| Route job hard deadline | 3600 s | `route-job.sh:72` |
| Config cache TTL | 5 s default; 30 s for page summaries | `routes-router-config.sh:11`, `routes-page-summary.sh:344` |
| Convergence attempts | 12 × 1 s | `routes-router-config.sh:15` |
| Verify batch size | 16 (1..128) | `routes-router-sync.sh:37,1706-1709`; `routes-router-delete.sh:1154-1157` |
| GitHub HTTP | connect 5 s, total 20 s | `routes-source-check.sh:158-168` |
| Storage minimum | 4096 KB | `routes-operation-preflight.sh:253-254` |
| Toasts | 6 s, max 2 visible, history 10 | `common.js:189-191` |

---

## 5. Endpoints built dynamically by the JS
* `"/api/routes/" + action + ".cgi?bundleId=" + encodeURIComponent(id)` and, for `export|delete|resume`, `+ "&preflightToken=" + encodeURIComponent(token||"")` — `routes-catalog.js:1413-1417`. `action` ∈ {`check`, `download`, `verify`, `export`, `delete`, `resume`} (button `data-action` values `check|download|verify|export|delete`, `resume` via `prepareLongOperation`; 1584-1608). Note: the "Восстановить/Обновить/Установить в Keenetic" button is `data-action="export"` in all modes (626, 1102-1106).
* `"/api/routes/check.cgi?bundleId=" + id`, `"/api/routes/download.cgi?bundleId=" + id` — 1485, 1492.
* `"/api/routes/preflight.cgi?bundleId=" + id + "&action=" + action` — 1538-1539 (`action` ∈ export|delete|resume).
* Navigation (not API): `/routes-custom.html?v=WebUI-3.1.0-r09c02&resume=<id>` / `/routes-import.html?v=WebUI-3.1.0-r09c02&resume=<id>` — `routes-operation.js:188-190`.
* Same patterns from out-of-scope page `routes-custom-page.js:687,719` (custom `user-*` ids).
* `dns.js:463-468` builds `"/api/routes/" + {dot-test|dot-apply|dot-delete}.cgi` (DoT area, not this one).

---

## 6. JS dependencies on backend wording / ids / classes
* No backend-produced HTML. All DOM ids are static in the pages (`routes.html:106-183`, `routes-import.html:107-161`) or created by JS (`createCard`, `routes-catalog.js:558-664`: `data-route-card`, `data-bundle-id`, `data-field=…`, `data-action=…`, ids `route-title-<id>`, `route-details-<id>`). Feedback panel id `routes-feedback` is created by `action-feedback.js:45-78` inside `.workspace-content`.
* Dead hooks (no effect, keep behaviour): `renderOperationProgress` looks for `[data-field="operation-progress"]`, `[data-action="stop"]`, `[data-action="resume"]` inside cards which `createCard` never creates (867-922, 1070-1071, 1115-1123); `initialOperationProgress`, `stopProgressWatcher`, `watchProgress`, `loadProgress`, `dispatchPeerControl` are unused.
* Error codes the JS branches on: `AUTH_REQUIRED`, `SESSION_REQUIRED` (redirect, `routes-catalog.js:118,1447,1497`); `action-feedback.js:16-20` special-cases `ROUTES_OPERATION_BUSY`, `OPERATION_BUSY`, `DOMAIN_OPERATION_BUSY` ("…другая конфликтующая операция уже выполняется.") and `ROUTES_PREFLIGHT_PLAN_FAILED`, `ROUTES_API_LOCK_FAILED` ("…не смог завершить предварительную проверку маршрутов."). Feedback details show `"Код: <code>"`, `"Сообщение: <message>"`, and `details` verbatim (string or JSON).
* Enum values the JS compares: `progress.operation` ∈ install|update|restore|delete; `progress.{running,resumable,stoppedByUser,stopRequested,success,phase=="idle"}`; `verifyResult.keenetic.status` ∈ complete|not_installed|update_pending|restore_required|conflict|unavailable; `verifyResult.keenetic.available`; `verifyResult.local.valid`; `checkResult.result == "source_changed_routes_unchanged"`; `checkResult.routesChanged/sourceChanged`; `globalOperation.scope == "system"`; `globalOperation.action` values `update|restore|uninstall|cleanup|reinstall|check|download|verify|plan|delete|resume` and prefixes `preflight:`, `custom:`; `operation-status.bundleType == "custom"`.
* Server strings shown verbatim: every `error.message`; `verifyResult.message`; `lastError`/`.message`/`.details`; `checkResult.message`; `downloadResult.message`; `exportResult.message`; `deleteResult.message`; `operation.output`; `progress.message`; `preflight.message`; `operationProgress.message` (toast); `bundleName`; `metadata`-independent card names (from JS `BUNDLES`).

---

## 7. Notable risks / quirks to reproduce or consciously fix
1. `resume.cgi` uses `exec`, breaking the `$0` allow-list ⇒ 500 `ROUTES_API_LOCK_FAILED` (§2.8). UNCONFIRMED at runtime.
2. `summary.cgi?bundleId=` aborts under `set -u` (§2.12). UNCONFIRMED HTTP result.
3. Delete of a bundle whose `downloadedVersion` is null sets `status:"not_checked"` and the CGI then reports 500 `ROUTES_DELETE_RESULT_INVALID` after a successful router change (`routes-router-delete.sh:298-305` vs `routes-web-action.sh:354-366`).
4. A successful mutation can be reported as 500 `ROUTES_API_LOCK_FAILED` if job drain/finish fails (§0.4 item 5).
5. `operation-status` exposes progress only for the fence's `bundleId`; between admission and `progress_begin` the stale previous progress file is shown.
6. `catalog-summary` `health.severity` never becomes `busy` (`$globalData.running` does not exist, `routes-page-summary.sh:518`).
7. Long operations are synchronous HTTP requests (JS 8 h, job 3600 s); progress is only via 1 s polling; no cancel.
8. `checkResult.discoveryMode` is always `"all-bat"` regardless of manifest (`routes-source-check.sh:1086`).
9. Unused / legacy code reachable only from CLI: `lib/routes-router-export.sh`, `lib/routes-router-preflight.sh` (`broray-routes preflight`), `lib/routes-bundle-registry.sh` (never sourced). `bin/broray-routes-check-source [check [id]]` just execs `broray-routes check <id>` (default `telegram`). `bin/broray-route-prepare` is unrelated to the catalog (adds a Linux `ip route replace <hysteria server IP>/32 dev <wanInterface> metric 1` from `config/config.json` + `config/system/settings.json`); no caller found in this area.

## Dev6 Go implementation delta — остановка маршрутов (2026-11-04)

Разделы выше фиксируют поведение исходной реализации 3.x. В текущем исходнике dev6 `POST /api/routes/stop.cgi?bundleId=<id>&operationId=<id>` принимает запрос сессии только для выполняемой операции с совпадающими идентификаторами и возвращает HTTP 202. Отсутствующий ID даёт 400, завершённая или устаревшая операция — 409 `ROUTES_STOP_NOT_RUNNING`. `operation-status` показывает `canStop` и `stopRequested` только для текущего владельца операции.

При установке/обновлении запрос обрабатывается на границе проверенной группы: изменения попытки откатываются к сохранённому running/startup baseline и исходным локальным байтам, затем операция становится доступной для продолжения. При удалении уже подтверждённые группы сохраняются, а оставшаяся часть доступна для продолжения. Если откат либо запись его итогового состояния не подтверждены, операция не объявляется безопасно остановленной: сохраняется блокирующий маркер и нерезюмируемый lease. Если маркер не удалось записать, lease удерживается в памяти, а незавершённая транзакция, записанная до изменения маршрутов, блокирует новые записи после перезапуска.

Доказательство ограничено изолированной ARM64 VM с имитацией Keenetic: полный пакет `areas/routes` — `artifacts/stage-b/route-stop-current96-full.json` SHA-256 `7B499DBC84875C1582F93E5C29C2969829C587CAE4D55112C967049ED3CC7439`; браузерные проверки плавающей панели и карточки — `route-stop-ui-current98-float.json` и `route-stop-ui-current99-card.json`. Физический KN-1811 и длительная операция на нём не проверены; этот раздел не подтверждает готовность кандидата.
