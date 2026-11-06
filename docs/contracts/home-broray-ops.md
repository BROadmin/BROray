# Contract: HOME, BROray page, OPERATIONS, AUTH and the shared shell

Source: `/home/claude/broadmin/broray` at commit f505928 (read-only). The shipped
`share/release/manifest.json` and `web-new/build.json` in this tree say
`version 3.1.0`, `releaseId 3.1.0-r09`, `candidateId 3.1.0-r09c02`,
`webUIBuild WebUI-3.1.0-r09c02` (cache-buster `?v=WebUI-3.1.0-r09c02` is hard-coded in every HTML/JS URL).

Path abbreviations used in citations:

| Prefix | Meaning |
|---|---|
| `web/` | `runtime/app/web-new/` (served as `/` ; installed at `/opt/broray/web-new`) |
| `js/` | `runtime/app/web-new/assets/js/` |
| `api/` | `runtime/app/web-new/api/` |
| `lib/` | `runtime/app/lib/` (installed `/opt/broray/lib`) |
| `bin/` | `runtime/app/bin/` (installed `/opt/broray/bin`) |
| `upd.sh` | `runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh` (installed `/opt/libexec/broray-updater/broray-updater.sh`, invoked via `/opt/bin/broray-updaterctl` = `exec /opt/bin/ash broray-updater.sh "$@"`) |
| `compat.sh` | `runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-compat.sh` |
| `handoff.sh` | `lib/universal-platform-handoff.sh` (invoked as `/opt/broray/current/app/lib/universal-platform-handoff.sh`) |

The web server is lighttpd with `mod_cgi`, `cgi.assign = (".cgi" => "")`, document-root `/opt/broray/web-new`, port 8080, `server.max-request-size = 5120` (KB) (`lib/package-setup.sh:291-323`, seed `runtime/state-seed/config/lighttpd.conf:1-31`). NOTE: only `.cgi` is excluded from static serving, so the helper sources `api/auth-common.sh`, `api/operations/common.sh`, `api/broray/updater-api-common.sh` are downloadable as static text in 3.x (not something the UI uses; do not need to replicate).

---

## 0. Endpoint table

| # | URL | Method | Auth | CSRF/extra header | Envelope family | Async |
|---|---|---|---|---|---|---|
| 1 | `/api/login.cgi` | POST | none | none | A (flat auth) | no |
| 2 | `/api/logout.cgi` | POST | cookie (optional) | none | A | no |
| 3 | `/api/session.cgi` | GET | session | none | A | no |
| 4 | `/api/protected-test.cgi` | GET | session | none | A | no (unused by JS) |
| 5 | `/api/home/summary.cgi` | GET | session | none | B (`success/data/error`) | no (cache read) |
| 6 | `/api/broray/info.cgi` | GET | session | none | C (raw `ok:true` object) / C-error | no |
| 7 | `/api/broray/update-check.cgi` | POST | session | `X-BROray-Request: 1` | C | synchronous network check |
| 8 | `/api/broray/update-start.cgi` | POST | session | `X-BROray-Request: 1` | C (202) | yes: returns `operationId`, poll #9 |
| 9 | `/api/broray/update-status.cgi` | GET | session | none | C | poll target |
| 10 | `/api/broray/reinstall.cgi` | POST | session | `X-BROray-Request: 1` | C (202) | yes, poll #9 |
| 11 | `/api/broray/restore.cgi` | POST | session | `X-BROray-Request: 1` | B-error only (always 409) | n/a |
| 12 | `/api/broray/cleanup-plan.cgi` | POST | session | `X-BROray-Request: 1` | B | no (returns token) |
| 13 | `/api/broray/cleanup.cgi` | POST | session | `X-BROray-Request: 1` | B | no (synchronous delete) |
| 14 | `/api/broray/uninstall.cgi` | POST | session | `X-BROray-Request: 1` | C (always HTTP 200 unless pre-checks fail) | yes, poll #9 |
| 15 | `/api/operations/status.cgi` | GET | session | no query string, no body | D (coordinator `ok/errorCode`) | poll target (10 s / 5 s) |
| 16 | `/api/operations/status.cgi` | POST (queue-lookup) | session | `X-BROray-Request: operations` + origin + JSON | D | lookup |
| 17 | `/api/operations/events.cgi` | GET | session | no query/body | D | snapshot |
| 18 | `/api/operations/report.cgi` | GET | session | no query/body | D + `Content-Disposition` | no |
| 19 | `/api/operations/cancel.cgi` | POST | session | operations CSRF | D (202) | cooperative cancel |
| 20 | `/api/operations/stop-background.cgi` | POST | session | operations CSRF | D (202) | yes (cancel requests) |
| 21 | `/api/operations/automation.cgi` | POST | session | operations CSRF | D (200) | no |
| 22 | `/api/operations/recover.cgi` | POST | session | operations CSRF | D (202 / 409) | no |

Also served through the same operations pipeline (`api/operations/common.sh`) but owned by other areas: `POST /api/servers/check.cgi` (verb `servers-check`, `api/servers/check.cgi:4`) and `POST /api/subscriptions/refresh.cgi` (verb `subscription-refresh`, `api/subscriptions/refresh.cgi:4`). Their validation/HTTP mapping is in `api/operations/common.sh:39-71,100-116` and is described in section 4.2 for completeness.

Static pages in scope: `web/index.html` (login), `web/home.html`, `web/broray.html`, `web/modules.html` (redirect page, `js/modules-redirect.js:1` does `location.replace("/home.html?v=WebUI-3.1.0-r09c02")`).

---

## 1. Common contract (envelopes, headers, CSRF, origin, session)

### 1.1 Response headers

`broray_api_print_json_headers` (`api/auth-common.sh:7-14`), used by envelope B/C/D endpoints:

```
Content-Type: application/json; charset=utf-8
Cache-Control: no-store, no-cache, must-revalidate
Pragma: no-cache
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: no-referrer
```

`broray_json_response` (`lib/web-auth.sh:19-32`, used by envelope A) emits the same six headers after an explicit `Status:` line. Logout success emits only `Content-Type`, `Cache-Control`, `Set-Cookie`, `X-Content-Type-Options` (`api/logout.cgi:19-25`). Login success adds `Set-Cookie` (`api/login.cgi:89-97`). `report.cgi` additionally sends `Content-Disposition: attachment; filename="BROray-diagnostics.json"` (`api/operations/common.sh:114`).

When no `Status:` line is printed, lighttpd answers 200 (`broray_api_success`, `api/auth-common.sh:96-111`; `uninstall.cgi`, `api/broray/uninstall.cgi:32-34`).

### 1.2 Envelope families (the Go daemon must emit each byte-shape exactly as below)

**A. Flat auth shape** (login/logout/session/protected-test):
`{"ok":false,"error":"CODE"}` or `{"ok":false,"error":"CODE","message":"text"}`; success objects are endpoint-specific with `ok:true`.

**B. Standard envelope** (`api/auth-common.sh:25-57, 96-111`):
- success: `{"success":true,"data":<object>,"error":null}` (pretty-printed by `jq` — whitespace irrelevant to JS).
- error (`broray_api_error HTTP CODE MESSAGE [DETAILS]`):
  `{"success":false,"data":null,"error":{"code":"CODE","message":"MESSAGE","details":null|"DETAILS"}}` with the given HTTP status.
- `broray_api_error` is also used for method/session/CSRF/body errors on C- and D-family endpoints, so those endpoints can return either B-errors or their own shapes.

**C. Raw backend object** (info, updater-api, uninstall): success `{"ok":true,...}`; failure `{"ok":false,"error":{"code","message",("details"),("updaterRc"),...}}`.

**D. Operations coordinator object**: success `{"ok":true,...}`; failure `{"ok":false,"errorCode":"CODE"}` (+ extra fields for status/recover) — `lib/operation-coordinator.sh:32-36`.

How the JS parses each:
- `BROrayUI.apiRequest` (`js/common.js:4-60`): treats `!response.ok`, non-JSON, non-object, or array as error. Message = `payload.error.message` if `payload.error` is an object, else `payload.message`, else `"Ошибка запроса (HTTP n)."`/`"Некорректный ответ сервера (HTTP n)."`. `error.code` = `payload.error.code` | `payload.error` (string) | `"HTTP_RESPONSE_INVALID"`; `error.details` = `errorPayload.details`; `error.status`, `error.payload`. Returns the raw payload (does NOT unwrap B). Used by login.js and queue helpers.
- home.js `request` (`js/home.js:94-124`): 401 → redirect `/` (checked before JSON parse); then error if `!ok || success===false || ok===false`, message `payload.error.message` or `"Не удалось получить данные."`; returns `payload.data` when `success===true` else payload.
- broray.js `request` (`js/broray.js:90-138`): reads text; empty → `"Backend вернул пустой ответ (HTTP n)."`; JSON parse failure with 502/503/504 → `"Шлюз не получил ответ от роутера (HTTP n). Результат операции не подтверждён. Обновите состояние перед повтором."` else `"Backend вернул некорректный JSON (HTTP n)."`; 401 (only if body was JSON) → `location.replace("/")` + throw `"Сессия завершена."`; error if `!response.ok || ok===false || success===false`: message `data.error.message || data.error.details || data.message || "Операция завершилась ошибкой."`; `failure.code=data.error.code`, `failure.updaterRc` if numeric. Returns `data.data` iff `data.data && success===true`, else the raw object. `errorMessage()` (`js/broray.js:65-82`) appends `" [updater rc=N]"` when `payload.error.updaterRc` is a finite number.
- background-operations.js `request` (`js/background-operations.js:35-45`): `fetch("/api/operations/"+endpoint+".cgi")`, `response.json()`; error if `!ok || data.ok===false || data.success===false` → `Error("Состояние временно недоступно.")` with `error.data` = parsed body, `error.status`. 60 s abort timeout. No 401 redirect.

### 1.3 Method enforcement

- `broray_api_require_method` (`api/auth-common.sh:59-70`): `REQUEST_METHOD` (default `GET`) must equal the required one, else **405** B-error `METHOD_NOT_ALLOWED`, message `"Для этой операции требуется метод <M>."`, details `"Получен метод <actual>."`.
- Auth CGIs: 405 A-shape `{"ok":false,"error":"METHOD_NOT_ALLOWED"}` (`api/login.cgi:6-11`, `logout.cgi:5-10`, `session.cgi:5-10`, `protected-test.cgi:5-10`).
- Method is checked before session in every B/C/D endpoint.

### 1.4 Session handling

- Cookie: `BRORAY_SESSION=<64 lowercase hex>`; set as `Set-Cookie: BRORAY_SESSION=<token>; Path=/; HttpOnly; SameSite=Strict` (no `Secure`, no `Max-Age`) (`api/login.cgi:93`). Cleared with `BRORAY_SESSION=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0` (`api/logout.cgi:22`).
- Cookie parsing: split `HTTP_COOKIE` on `;`, trim, first `BRORAY_SESSION=` wins (`lib/web-auth.sh:50-81`). Token valid iff 64 chars of `[0-9a-f]` (`lib/web-auth.sh:83-95`).
- Token generation: 32 bytes `/dev/urandom` hex (`lib/web-auth.sh:43-48`).
- Storage: `/opt/broray/run/web-new/sessions/<token>` mode 0600, JSON `{schemaVersion:1,username,createdAt,lastActivity,expiresAt}` (epoch seconds) (`lib/web-auth.sh:135-170`). All create/validate/delete/cleanup run serialized under `broray-ops-guard /opt/broray/run/web-new/sessions.guard` (flock) (`lib/web-auth.sh:102-118`).
- TTL `BRORAY_SESSION_TTL=1800` s, **sliding**: every successful validation rewrites `lastActivity=now`, `expiresAt=now+1800` (`lib/web-auth.sh:172-238`). So any authenticated request (including the 10 s operations poll and 30 s home poll) keeps the session alive indefinitely while a page is open.
- Expired (`expiresAt <= now`), unparsable expiry, or missing username → file deleted, validation fails (`lib/web-auth.sh:186-211`).
- Two different "not authenticated" bodies (both HTTP **401**):
  - Envelope B (`broray_api_require_session`, `api/auth-common.sh:72-95`): `{"success":false,"data":null,"error":{"code":"AUTH_REQUIRED","message":"Требуется авторизация.","details":null}}`. If `/opt/broray/lib/web-auth.sh` unreadable → **500** `SESSION_BACKEND_UNAVAILABLE` "Модуль проверки сессии недоступен.".
  - Envelope A (`broray_session_require`, `lib/web-auth.sh:240-252`, used by session.cgi/protected-test.cgi): `{"ok":false,"error":"SESSION_REQUIRED","message":"Требуется авторизация."}`.
- Login runs `broray_sessions_cleanup` (deletes all expired/invalid session files) before authenticating (`api/login.cgi:72`, `lib/web-auth.sh:263-286`).
- Logout does not require a valid session; invalid/absent token → success (`lib/web-auth.sh:122-125`).

### 1.5 CSRF / origin rules

| Endpoint group | Rule | Failure |
|---|---|---|
| login/logout/session | none (relies on `SameSite=Strict`) | — |
| `/api/home/summary.cgi`, `/api/broray/info.cgi`, `/api/broray/update-status.cgi` | GET + session only | — |
| `/api/broray/*` POST (update-check, update-start, reinstall, restore, cleanup-plan, cleanup, uninstall) | header `X-BROray-Request` must equal exactly `1` (checked after method + session). No Origin/Content-Type check. | **403** B-error `INVALID_REQUEST` "Запрос отклонён." (`api/broray/update-check.cgi:7-9`, etc.) |
| `/api/operations/*` all methods | `QUERY_STRING` must be empty (after session) | **400** `INVALID_REQUEST` "Параметры запроса не поддерживаются." (`api/operations/common.sh:11`) |
| `/api/operations/*` GET | `CONTENT_LENGTH` empty or `0` | **400** `INVALID_REQUEST` "Тело GET-запроса не поддерживается." (`common.sh:97`) |
| `/api/operations/*` POST | (1) `X-BROray-Request: operations` | **403** `CSRF_REJECTED` "Обновите страницу и повторите действие." (`common.sh:15`) |
| | (2) `/opt/broray/lib/operations-origin.sh` present (non-symlink) and sourcable | **503** `ORIGIN_VALIDATION_UNAVAILABLE` "Проверка источника запроса недоступна." (`common.sh:16-20`) |
| | (3) origin check `broray_operations_origin_allowed` (below) | **403** `ORIGIN_REJECTED` "Источник запроса не подтверждён." (`common.sh:21-22`) |
| | (4) `CONTENT_TYPE` exactly `application/json` or `application/json; charset=utf-8` | **415** `INVALID_CONTENT_TYPE` "Требуется JSON." (`common.sh:23-25`) |
| | (5) `CONTENT_LENGTH` digits, < 9 chars | **400** `INVALID_LENGTH` "Некорректная длина запроса." (`common.sh:26`) |
| | (6) `CONTENT_LENGTH <= 4096` | **413** `REQUEST_TOO_LARGE` "Запрос превышает 4 КБ." (`common.sh:27`) |
| | (7) body read completely; exactly one JSON object (`jq -s 'length==1 and (.[0]|type)=="object"'`) | **400** `INVALID_BODY` "Запрос получен не полностью." / "Требуется один JSON-объект." (`common.sh:35-37`) |
| | temp dir creation failure | **503** `STATE_UNAVAILABLE` "Не удалось прочитать запрос." (`common.sh:29`) |

All of the above are envelope-B errors.

Origin algorithm `broray_operations_origin_allowed` (`lib/operations-origin.sh:134-164`):
1. `HTTP_HOST` required.
2. If `Origin` header present: normalize it (below); if `X-BROray-Origin` also present it must normalize to the same value.
   Else (no Origin — KeenDNS may strip it): require `X-BROray-Request: operations` and use normalized `X-BROray-Origin` (`proxy_origin=true`).
3. If origin came from `Origin` header and equals normalized `http://$HTTP_HOST` or `https://$HTTP_HOST` → allow.
4. Otherwise origin must start with `https://broray.` and equal the live KeenDNS publication origin `https://broray.<ndns-name>.<ndns-domain>` computed by `broray_operations_keendns_origin` (`lib/operations-origin.sh:106-132`): runs `broray_web_publish_status_json` (from `lib/web-publish.sh`) using `ndmc -c 'show ndns'` (timeout 30 s) and `ndmc -c 'show running-config'` (timeout 120 s) via `/opt/broray/bin/broray-system-ndmc`, and requires a single object with `schemaVersion==1, state=="enabled", enabled, consistent, recoveryRequired==false, keenDns.available, ownership.{liveBlockPresent,receiptPresent,receiptValid,liveBlockOwnedExact}` all true, `keenDns.name` 1..63 chars without `.`, `keenDns.domain` containing `.`, and `.address == expected + "/"`.

Origin normalizer (`lib/operations-origin.sh:8-54`): max 320 chars, one line, scheme `http://`|`https://`, authority chars `[A-Za-z0-9.:[]-]` only, lowercase host, DNS labels `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$` 1..63 chars, host ≤253, optional port 1..65535 without leading zero, default port (80/443) dropped; bracketed IPv6 validated (hex groups ≤4, at most one `::`, 8 groups if uncompressed). Output `scheme://host[:port]`.

JS senders: `js/background-operations.js:40` and `js/common.js:69-70` send `X-BROray-Request: operations` and `X-BROray-Origin: window.location.origin`. `common.js` also sends `X-BROray-Queue: "1"|"0"` which no backend checks (UNCONFIRMED purpose; ignored by `common.sh`).

---

## 2. AUTH endpoints

### 2.1 `POST /api/login.cgi` (`api/login.cgi`)

Request body JSON `{"login": string, "password": string}` (`js/login.js:160-169`, login trimmed client-side, `maxlength` 128/512 in `web/index.html:51,58`).

Validation (in order):
1. method POST else 405 A `METHOD_NOT_ALLOWED`.
2. Body read (`lib/web-request-body.sh:5-34`) with limit 8192: CONTENT_LENGTH missing/non-digit (rc2), 0 (rc3), >8192 (rc4) → **400** `{"ok":false,"error":"INVALID_REQUEST"}`; short read (rc5/6) → **400** `{"ok":false,"error":"REQUEST_BODY_INCOMPLETE"}` (`api/login.cgi:20-32`).
3. Not a JSON object → **400** `{"ok":false,"error":"REQUEST_JSON_INVALID"}` (`:33-37`).
4. `.login // empty`, `.password // empty` (jq `-r`: a non-string scalar is stringified — UNCONFIRMED whether intended). Either empty → **400** `{"ok":false,"error":"FIELDS_REQUIRED","message":"Введите логин и пароль."}` (`:53-60`).
5. `${#login} > 128` or `${#password} > 512` → **400** `{"ok":false,"error":"INVALID_REQUEST"}` (`:62-70`). (BusyBox `${#}` length unit — bytes vs characters — UNCONFIRMED.)
6. `broray_sessions_cleanup`.
7. `broray_keenetic_authenticate` (`lib/web-auth.sh:297-404`):
   - `broray_native_auth_ensure` (`lib/web-auth-native.sh:165-208`) chooses `http://127.0.0.1:79` if `GET /auth` there returns exactly one `HTTP/x 401`, one `X-NDM-Realm`, one `X-NDM-Challenge` (`[0-9A-Za-z_-]+`) (probe timeouts 3/5 s). Otherwise it starts/uses a private nginx sidecar copied from `/usr/sbin/nginx` on `127.0.0.1:18079` proxying `/auth` to SCGI `/var/run/ndm.auth.socket` (config generated at `lib/web-auth-native.sh:100-124`, files under `/opt/broray/run/web-new/native-auth/`), waiting up to 10 s. Failure → rc 2.
   - `GET <url>/auth` (connect 5 s, max 10 s, proxies disabled), read realm/challenge (must be exactly one each) else rc 2.
   - `password_hash = sha256_hex(challenge + md5_hex(login + ":" + realm + ":" + password))`; `POST <url>/auth` JSON `{"login":login,"password":password_hash}` with the cookie jar from the GET; success iff HTTP 200.
8. Success → create session (failure **500** `{"ok":false,"error":"SESSION_CREATE_FAILED","message":"Не удалось создать сессию."}`), respond **200**:
   ```json
   {"ok":true,"user":"admin","redirect":"/home.html?v=WebUI-3.1.0-r09c02"}
   ```
   (`api/login.cgi:89-99`; `user` is JSON-escaped login).
9. rc 2 → **503** `{"ok":false,"error":"KEENETIC_UNAVAILABLE","message":"Не удалось связаться с KeeneticOS."}`; any other failure → `sleep 1` then **401** `{"ok":false,"error":"INVALID_CREDENTIALS","message":"Неверный логин или пароль."}` (`:104-116`).

JS (`js/login.js:145-196`): on success `location.replace(result.redirect || "/home.html?v=WebUI-3.1.0-r09c02")`. On error shows `error.message` (falls back to `"Ошибка запроса (HTTP n)."` for codes without message), stores `data-error-code` (if `^[A-Z0-9_]{1,80}$`) and `data-http-status` on `#login-error`. Client-side empty check message `"Введите логин и пароль."` (`:152-155`). On load, `GET /api/session.cgi`; success → `location.replace("/home.html?v=WebUI-3.1.0-r09c02")` (`:198-206`).

### 2.2 `POST /api/logout.cgi` (`api/logout.cgi`)

No body validation (JS sends `"{}"` with `Content-Type: application/json`, `js/app-shell.js:265`). Deletes session file for the cookie token. Delete failure → **503** `{"ok":false,"error":"SESSION_REVOKE_FAILED","message":"Не удалось завершить сессию. Повторите выход."}`. Success **200** `{"ok":true}` + clearing cookie. JS ignores the response and always `location.replace("/")` (`js/app-shell.js:264-268`). home.js also stops polling on logout click (capture listener, `js/home.js:593-598`).

### 2.3 `GET /api/session.cgi` (`api/session.cgi`)

Success **200**: `{"ok":true,"authenticated":true,"user":"admin","expiresAt":1793615400}` (`expiresAt` epoch seconds, already slid forward). 401 A-shape `SESSION_REQUIRED`.
JS readers: home.js requires `authenticated===true` and non-empty string `user`, else error "Не удалось подтвердить пользователя." (`js/home.js:521-530`), sets `#current-user`; broray.js uses `session.user || "admin"` (`js/broray.js:962-964`); login.js only checks success.

### 2.4 `GET /api/protected-test.cgi`

Success **200** `{"ok":true,"protected":true,"user":"<user>"}`; 401 A `SESSION_REQUIRED`. Not called by any JS in scope.

---

## 3. HOME

### 3.1 `GET /api/home/summary.cgi` (`api/home/summary.cgi`)

Cache-only: reads 7 module snapshots, never probes the router (`:3-4`). Method GET (405 B), session (401 B).

Snapshots are produced by daemon `bin/broray-home-snapshotd` (service `home-snapshot`), every `BRORAY_HOME_SNAPSHOT_INTERVAL` = 30 s (clamped 15..300) (`bin/broray-home-snapshotd:8,18`), in order `xray keenetic servers subscriptions dns routes broray` (`:86`), each by `bin/broray-home-snapshot refresh <module>` (`bin/broray-home-snapshot:10-78`):

| module | producer | stored `data` |
|---|---|---|
| xray | `broray_xray_status_json` (`lib/xray.sh:318`) | `.data` of `{success:true,data}` |
| keenetic | `broray_keenetic_status_json` (`lib/keenetic-page.sh:629`) | object |
| servers | `broray_server_summary` (`lib/server-service.sh:216`) with `BRORAY_SERVER_SKIP_KEENETIC_REFRESH=true` | object |
| subscriptions | `broray_subscription_summary` (`lib/subscription-service.sh:1957`) | object |
| dns | `broray_dot_status` (`lib/routes-dot.sh:961`) | object |
| routes | `broray_routes_page_summary all` (`lib/routes-page-summary.sh:317`) projected to `{schemaVersion:2,availableBundles,installedBundles,verifiedInstalledBundles,recordedInstalledBundles,installedRouteCount,attentionCount,actionRequired,operationRunning,managedInterface,routerSnapshot,bundles,health,updatedAt}` (`bin/broray-home-snapshot:49-67`) |
| broray | `/opt/broray/bin/broray-system info` with `.ok==true` (`bin/broray-home-snapshot:69-71`) — i.e. `broray_system_info_json` **without** the handoff enrichment that `info.cgi` adds |

The daemon also runs the lighttpd port guard and auto-restarts `S23broray-monitor` with backoff 30..300 s (`bin/broray-home-snapshotd:37-73`). Log: `/opt/broray/logs/home-snapshots.log`.

Snapshot file `/opt/broray/run/home-snapshots/<module>.json` (0600, atomic rename), shape `{schemaVersion:1,module,capturedAt:"YYYY-MM-DDTHH:MM:SSZ",capturedEpoch:<int>,data:<object>}` (`lib/home-snapshot.sh:41-89`).

Read (`lib/home-snapshot.sh:91-126`): valid iff schemaVersion 1, module matches, types correct. `age = max(0, now - capturedEpoch)`; `freshness = "fresh"` if age ≤ `FRESH`=90, `"stale"` if ≤ `EXPIRE`=600, else `"expired"` (env overrides; FRESH min 15, EXPIRE must be > FRESH) (`lib/home-snapshot.sh:8-19`). Returned value is `data + {_snapshot:{schemaVersion:1,module,capturedAt,capturedEpoch,ageSeconds,freshness}}`. Missing/invalid → module `null` and its id appended to `errors`.

Installed release (`api/home/summary.cgi:18-27`): if `/opt/broray/share/release/manifest.json` valid (`lib/release-manifest.sh:5-15`: object, `schemaVersion==3`, string `version/webUIBuild/releaseId`) and `version` non-empty and `!= "unknown"` → `{version,releaseId,candidateId,webUIBuild}` else `null`.

DNS patch (`api/home/summary.cgi:66-89`): in `dns.health.reasons`, any reason with `code=="DNS_TEST_REQUIRED"` gets `message = "Проверено " + (selectedTestedCount//0) + " из " + (selectedCount//0) + " выбранных серверов DNS-over-TLS."`. jq failure → dns `null` + error `dns`.

Aggregation (`api/home/summary.cgi:91-220`):
- `rank`: error 50, busy 40, warning 30, unknown 20, ok 10, other 20.
- per-module `health` (`snapshot_health`):
  - module null → `{schemaVersion:1,module,availability:"unavailable",severity:"error",operational:false,consistent:false,actionRequired:true,freshness:{state:"unknown",checkedAt:null},reasons:[{code:"MODULE_SNAPSHOT_UNAVAILABLE",message:"Валидный снимок модуля ещё не опубликован.",details:null}],facts:{},lastOperation:null}`.
  - base = `value.health` if object else contract_health: `{...availability:"partial",severity:"unknown",operational:false,consistent:false,actionRequired:true,freshness unknown,reasons:[{code:"STATUS_CONTRACT_MISSING",message:"Снимок модуля не содержит единого контракта состояния.",details:null}]...}`.
  - fresh → `freshness={state:"fresh",checkedAt:capturedAt}`.
  - stale/expired → severity raised to at least `warning` (rank), `actionRequired=true`, freshness `{state,checkedAt:capturedAt}`, reason appended `{code:"MODULE_SNAPSHOT_STALE"|"MODULE_SNAPSHOT_EXPIRED", message:"Показано последнее известное состояние; фоновое обновление задерживается." | "...давно не завершалось.", details:{capturedAt,ageSeconds}}`.
  - other freshness → severity ≥ warning, actionRequired true, freshness `{state:"unknown",checkedAt:capturedAt}`.
  - IMPORTANT: these adjusted healths appear only in `modules[]`, `issues[]`, top `health`; the per-module raw objects (`xray`, `servers`, …) are returned unmodified (except dns patch + `_snapshot`).
- overall `health.severity` = max-rank of module severities; `issues` = modules with severity ≠ ok → `{module,severity,actionRequired,reasons}`; `health.reasons` = all issue reasons flattened in module order `xray, servers, subscriptions, dns, routes, keenetic, broray`, each with `module` added.
- `health.availability` = `"available"` if `errors` empty else `"partial"`; `operational`/`consistent` = all modules true; `actionRequired` = any; `freshness.state` = `expired` if any expired, else `stale` if any stale, else `fresh` if all fresh, else `unknown`; `checkedAt` = updatedAt; `facts:{moduleCount:7,unavailableCount:len(errors),issueCount:len(issues)}`.
- jq failure → **500** B `HOME_SUMMARY_FAILED` "Не удалось прочитать сохранённые состояния модулей.".

Success (B envelope), example (abridged module bodies):
```json
{"success":true,"data":{
 "schemaVersion":3,
 "installedRelease":{"version":"3.1.0","releaseId":"3.1.0-r09","candidateId":"3.1.0-r09c02","webUIBuild":"WebUI-3.1.0-r09c02"},
 "xray":{"version":"25.9.11","configValid":true,"socksActive":true,"health":{"schemaVersion":1,"module":"xray","severity":"ok","...":"..."},"_snapshot":{"schemaVersion":1,"module":"xray","capturedAt":"2026-11-02T10:00:00Z","capturedEpoch":1793613600,"ageSeconds":12,"freshness":"fresh"}},
 "servers":{"total":14,"connectionState":"connected","activeServer":{"id":"nl-1","name":"NL 1","quality":{"ping":48,"freshness":"fresh"}},"health":{...},"_snapshot":{...}},
 "subscriptions":{"total":2,"enabled":2,"serversReceived":14,"lastUpdateStatus":"success","lastUpdatedAt":"2026-11-02T06:00:00Z","lastWarnings":[],"health":{...},"_snapshot":{...}},
 "dns":{"selectedCount":2,"selectedPresentCount":2,"selectedTestedCount":2,"maxServers":8,"runningConfigAvailable":true,"observationState":"observed","health":{...},"_snapshot":{...}},
 "keenetic":{"interfaceDisplayName":"BROray","link":true,"state":"up","connected":true,"health":{...},"_snapshot":{...}},
 "routes":{"schemaVersion":2,"availableBundles":12,"installedBundles":3,"verifiedInstalledBundles":3,"recordedInstalledBundles":3,"installedRouteCount":2150,"attentionCount":0,"actionRequired":false,"operationRunning":false,"managedInterface":"Proxy0","routerSnapshot":null,"bundles":[],"health":{...},"updatedAt":"2026-11-02T09:59:40Z","_snapshot":{...}},
 "broray":{"ok":true,"version":"3.1.0","installationHealthy":true,"installedPackageVersion":"3.0.0-r14","availablePackageVersion":null,"updateAvailable":false,"availableVersion":null,"...":"(full info object, §4.1)","_snapshot":{...}},
 "snapshotRuntime":{"readOnly":true,"collectorProcessesStarted":0,"aggregateLock":false},
 "collectorRuntime":{"mode":"snapshot-only","maxConcurrentOperations":0,"budgetSeconds":0,"elapsedSeconds":0},
 "modules":[{"id":"xray","health":{...}},{"id":"servers","health":{...}},{"id":"subscriptions","health":{...}},{"id":"dns","health":{...}},{"id":"routes","health":{...}},{"id":"keenetic","health":{...}},{"id":"broray","health":{...}}],
 "errors":[],
 "issues":[],
 "health":{"schemaVersion":1,"module":"home","availability":"available","severity":"ok","operational":true,"consistent":true,"actionRequired":false,"freshness":{"state":"fresh","checkedAt":"2026-11-02T10:00:12Z"},"reasons":[],"facts":{"moduleCount":7,"unavailableCount":0,"issueCount":0},"lastOperation":null},
 "healthy":true,
 "updatedAt":"2026-11-02T10:00:12Z"
},"error":null}
```
(Module field names other than those listed in 3.2 belong to the other areas' contracts.)

### 3.2 home.js model (what is read; all text is client-generated)

Lifecycle (`js/home.js`): constants `REFRESH_INTERVAL_MS=30000`, `REQUEST_TIMEOUT_MS=60000`, `MAX_RETRY_MS=120000` (`:9-12`). First refresh calls `GET /api/session.cgi` then `GET /api/home/summary.cgi` (`:521-531`); later refreshes only summary (session re-checked after bfcache restore, `:586-591`). On failure, retry delay doubles up to 120 s (`:554`); success resets to 30 s. Polling paused when `document.hidden` (request aborted), resumed on visibility/`online`/`pageshow` (`:575-592`). `#refresh-status` button unhidden by JS, manual refresh toasts "Сводка обновлена." (`:540,569-574`). 401 → `location.replace("/")` (`:104,116-122`).

`validateSummary` (`:474-482`): body must be object with object `health` and own keys `xray,servers,subscriptions,dns,routes,keenetic,broray` each object or null; else "Сервер вернул неполную сводку.".

Overall badge `#home-health` (`renderOverall`, `:401-433`): text by `data.health.severity`: ok "Система работает", warning "Требуется внимание", error "Требуется исправление", busy "Выполняется операция", unknown "Состояние не проверено"; class `status-badge status-{success|warning|error|loading|neutral}`. If severity ≠ ok: `#home-warning` = first 3 `health.reasons[].message` joined by space, else `"Не удалось получить сводку модулей: " + errors.join(", ") + "."`, else "Один или несколько модулей требуют проверки.". `#home-updated-at` = `toLocaleString("ru-RU")` of `data.updatedAt`.

Failure display (`:483-496`): badge "Сводка недоступна" (never succeeded) / "Данные не обновлены", class warning; `#home-refresh-feedback` (created by JS) text "Не удалось обновить сводку. <msg> … Нажмите «Обновить» или дождитесь повторной попытки.".

Per-card fields read (card status element class is `home-card-status status-<tone>`; severity→tone ok/success, warning, error, busy/loading, unknown/neutral; severity labels "Исправно/Требуется внимание/Требуется исправление/Выполняется/Не проверено"; `firstReason` = `health.reasons[0].message`). Null module → status "Недоступно"(error) + module-specific message.

| Card | Fields read (`data.<module>.…`) | Logic (`js/home.js`) |
|---|---|---|
| Xray `#home-xray-*` | `health.severity`, `health.reasons[0].message`, `version`, `configValid`, `socksActive` | ok → "Работает" + "Процесс Xray запущен, конфигурация корректна, SOCKS принимает подключения."; config row "Конфигурация и SOCKS: OK" iff `configValid===true && socksActive===true` else "Требуется проверка"; version or "Не определено" (`:131-146`) |
| Servers `#home-servers-*`, `#home-server-*` | `activeServer{name,id,quality{ping,freshness}}`, `health`, `connectionState`, `total` | status "Подключено" iff `connectionState==="connected"` and severity ok; name = `active.name||active.id` or first reason or "Активный сервер не выбран."; quality `round(ping)+" мс"` or "Не проверено", suffix " · устарело/давно/не проверено" for freshness expired/stale/other ≠ fresh (`:148-167`) |
| Subscriptions | `lastUpdateStatus`, `lastUpdatedAt`, `lastWarnings[0]`, `enabled`, `total`, `serversReceived`, `health` | status "Обновлено частично" when `lastUpdateStatus==="partial"`; main = first warning or "Последнее обновление: <time · status>." with status map never/success/partial/error/running → "ещё не выполнялось/успешно/обновлено частично/ошибка/выполняется", else "состояние не определено"; "<enabled> из <total>" (`:169-185`) |
| DNS | `selectedCount` (or `selectedIds.length`), `selectedPresentCount`, `effectiveCount` (or `managed.length`), `managedPresentCount`, `maxServers` (default 8), `runningConfigAvailable`, `observationState`, `health` | selected "<requested> из <max>"; installed "<present> из <effective>" only when `runningConfigAvailable!==false && observationState!=="unknown" && present!=null` (`:187-209`) |
| Routes | `verifiedInstalledBundles`/`installedBundles`, `recordedInstalledBundles`, `availableBundles`, `updatesAvailableCount`, `operationRunning`, `error`, `actionRequired`, `health` | decision chain at `:261-288` (busy→"Выполняется"; error→"Требуется исправление"; counts unknown (available ≤0 or NaN)→"Не проверено"; verified/recorded > available →error; verified 0 "Не установлено"; partial warning; updates>0 "Доступно обновление"; warning+actionRequired; else "Установлено"); count text adds " · записано N" when recorded≠verified (`:211-315`). NOTE: the routes snapshot projection does not contain `updatesAvailableCount` (`bin/broray-home-snapshot:49-67`) so updates is always 0 → "0" |
| Keenetic | `interfaceDisplayName` / `expected.description` / `description` (default "BROray"), `link`, `state`, `connected`, `health` | ok → "OK" + "<name> работает."; link "Есть/Нет"; state "Подключено" iff `state==="up" && connected===true` (`:317-336`) |
| BROray `#home-broray-*` | `installedRelease.version` (top-level!), `broray._snapshot.freshness`, `installationHealthy`, `installedPackageVersion`, `availablePackageVersion`, `updateAvailable`, `availableVersion`, `version` | version from `data.installedRelease.version`; freshness ≠ fresh → "Данные устарели"(warning) + "Состояние компонентов требует повторной проверки."; `updateAvailable = data.updateAvailable===true && installedPackageVersion and availablePackageVersion non-empty strings and different`; label = `availableVersion` unless empty or equal to `version` then `availablePackageVersion`; status "Доступно обновление"(warning)/"Установлено"(success)/"Установка повреждена"(error) from `installationHealthy`; update row label or "Доступно"/"Нет" (`:338-399`) |

Note for 3.5.0: the BROray card's "update available" rule requires the OPKG package version to differ (`installedPackageVersion != availablePackageVersion`), while the 3.x app-slot updater can offer a newer release with the same package version → card says "Нет" in that case. Preserve exactly.

---

## 4. BROray page

Page scripts: `common.js, icons.js, app-shell.js, theme.js, dialogs.js, broray.js, background-operations.js` (`web/broray.html:439-445`). `broray.js` boot: `GET /api/session.cgi` → bind, show app, `placeOperationCard()` (moves the "Последняя операция" card before the cleanup card, `js/broray.js:395-407`), then `Promise.all([GET info.cgi, GET update-status.cgi])` (`:504-511, 960-978`).

### 4.1 `GET /api/broray/info.cgi` (`api/broray/info.cgi`)

Process: `/opt/broray/bin/broray-system info` → `broray_system_info_json` (`lib/broray-page.sh:1504-1787`; `bin/broray-system:27-34` also sources `compat.sh`, which overrides installed-identity functions). If its stdout is an object with `ok==true`:
- run `handoff.sh status` (`handoff.sh:1198-1222`). If it is `{state:"success",running:false,code:"UNIVERSAL_PLATFORM_READY"}` → add `universalUpdaterReady:true, platformHandoff:<status>`.
- else (missing/invalid status replaced by `{schemaVersion:1,contract:"broray-universal-platform-handoff/1",state:"error",running:false,code:"UNIVERSAL_PLATFORM_STATUS_INVALID",message:"Состояние универсального updater недоступно.",mutationStarted:false,rollbackPerformed:false,operationId:null,candidateId:null,recordedAt:null}`) → add `universalUpdaterReady:false, platformHandoff`, and FORCE `installationHealthy:false, versionsConsistent:false, reinstallSupported:false, updateAvailable:true, availableVersion:(releaseId // version // null)` (`api/broray/info.cgi:55-74`).
- respond **200** raw object (NOT wrapped).
Failure: if backend stdout is `{ok:false,error:{...}}` it is passed through, else replaced with `{"ok":false,"error":{"code":"SYSTEM_INFO_BACKEND_FAILED","message":"Backend страницы BROray не вернул корректный JSON.","details":<last 12 stderr lines>|null}}`; HTTP **503**. jq failure in enrichment → **500** B `RESPONSE_ENCODING_FAILED` ("Не удалось добавить состояние универсального updater." / "...перехода updater."). Temp dir failure → **500** B `RUNTIME_UNAVAILABLE`. Backend error codes: `RUNTIME_ERROR` "Не удалось подготовить служебный каталог." (`lib/broray-page.sh:1505-1508`).

Handoff status shape (`handoff.sh:125-166, 1198-1222`): `{schemaVersion:1,contract:"broray-universal-platform-handoff/1",state:"success"|"running"|"error",running:bool,code,message,mutationStarted,rollbackPerformed,operationId|null,candidateId|null,recordedAt}`. Ready iff platform files under `/opt/{bin/broray-updaterctl,etc/init.d/S22broray-updater,libexec/broray-updater/*}` byte-equal the current slot payload `/opt/broray/current/app/share/updater-platform` (SHA256SUMS verified, updater version `broray-updater/5`) AND `S22broray-updater status` succeeds. Else last `/opt/var/lib/broray-platform-handoff/status.json` if state running/error, else `UNIVERSAL_PLATFORM_NOT_READY` "Переход на универсальный updater не завершён." (exit 1, ignored by CGI).

Algorithm of `broray_system_info_json` (all `lib/broray-page.sh`):
- side effects: `mkdir -p /opt/broray/{run/broray,update,backup}, /tmp/broray-workers, /tmp/broray-scratch, /opt/var/lib/broray, /tmp/broray-locks` (`:85-91`); `broray_system_select_last_operation` republishes `/opt/var/lib/broray/last-operation` (+ legacy `/opt/broray/run/broray/last-operation`) (`:1651`, `lib/operation-manager.sh:57-110`). Runs `S25broray-web status` under `broray-timeout -k 2 50` (`:353-357`) — info can take up to ~50 s.
- `version` = manifest `version` → `/opt/broray/config/version` → `/opt/broray/VERSION` → `opkg list-installed broray` → `broray version` → `"не определена"` (`:193-224`).
- `build` = manifest `releaseId` → `/opt/broray/BUILD` → `.build` → `"Сборка не определена"` (`:226-243`).
- `architecture` = from `/opt/broray/runtime/xray version` token after `linux/` → `uname -m` (`:297-318`).
- `updateChannel` (`:245-295`): if `/opt/var/lib/broray-updater/release-index-url` exists: single line ≤4096 bytes matching `^https://host(:port)?(/path)?$` → `stable` if contains `/stable/`, `staging` if `/staging/`, else `custom`; invalid → `unknown`. Else from `/opt/etc/opkg/broray.conf` `src/gz broray <url>` → stable/staging/custom/unknown. JS labels: stable "Стабильный", staging "Тестовый (staging)", development "Разработка", else raw / "Не определён" (`js/broray.js:326-339`).
- installed identity (compat overrides, `compat.sh:84-112`): `installedPackageVersion` = `Version:` of `/opt/lib/opkg/info/broray.control`; `installedPackageRevision` = `X-BROray-Package-Revision` (digits else null); `installedReleaseId`/`installedWebUIBuild` = `releaseId`/`webUIBuild` of `/opt/broray/share/release/manifest.json`; installed app version = manifest `version`.
- `opkgRegistrationHealthy` (`compat.sh:24-58`): `opkg` present; control file regular; `broray.{preinst,postinst,prerm,postrm}` regular executables; `broray.list` regular and EMPTY; control has exactly one each of Package=broray, Version, Architecture=aarch64-3.10, X-BROray-Package-Revision, X-BROray-Version; `opkg status broray` has Package broray, Version equal to control, Architecture aarch64-3.10, `Status: install user installed`.
- `reinstallSupported` = manifest valid AND candidateId known AND opkgRegistrationHealthy AND installedPackageVersion non-empty AND `broray-updaterctl version` == `broray-updater/5` (`:1547-1554, 1454-1458`).
- update cache `/opt/var/lib/broray-updater/release-index.json` (≤262144 bytes; strict schema `:1460-1502`). From it: `lastCheckedAt=.checkedAt`, `availableVersion=.candidate.releaseId` (sic — release id, not appVersion), `availablePackageVersion=.candidate.packageVersion`, `availableCandidateId`, `releaseRelation`, `candidateRelation` (else both `"not-checked"`). `updateCheckFresh` = `0 ≤ now - checkedEpoch ≤ 3600`. `updateAvailable` = manifest valid AND fresh AND `currentCandidateAtCheck==candidateId` AND `currentReleaseAtCheck==releaseId` AND `releaseRelation=="newer"` AND cached `updateAvailable==true` (`:1568-1599`).
- `lastBackup` = first line of `/opt/var/lib/broray/last-backup` (fallback `/opt/broray/run/broray/last-backup`); `backupValid` via `broray_system_user_backup_valid` (tar.gz contains `.broray-protected-paths`, `.broray-protected-manifest`, `.broray-protected-format` == `BROray protected backup/2`, `:1315-1328`); invalid → `backupProblem="Последняя резервная копия отсутствует, является символической ссылкой или повреждена."`.
- `components` (`:1610-1624`, element builder `:320-385`): `{id,name,path,version,installed:(path exists),required:true,healthy}` for `core "Ядро BROray" /opt/broray/bin/broray`, `xray "Xray" /opt/broray/runtime/xray`, `servers lib/server-service.sh`, `subscriptions lib/subscription-service.sh|subscription.sh`, `keenetic lib/keenetic-page.sh`, `routes bin/broray-routes`, `webui /opt/broray/web-new/home.html` (+ `reasonCode` `WEBUI_LOCAL_HEALTHY|WEBUI_RUNTIME_UNHEALTHY` and `publication` = `broray_web_publish_status_json` or `{"state":"unknown","consistent":false,"reason":{"code":"WEB_PUBLISH_STATUS_UNAVAILABLE"}}`; webui healthy only if S25 status passes), `updater /opt/libexec/broray-updater/broray-updater.sh` (version = `broray-updater.sh version`), `opkg /opt/lib/opkg/info/broray.control`.
- `protocols`: `[{id,supported}]` for `vless vmess trojan hysteria2 shadowsocks` = non-empty `lib/parser-<id>.sh` (`:387-409, 1626-1632`).
- `installationHealthy` = all required components healthy AND opkgRegistrationHealthy; `versionsConsistent` = manifest valid AND package version/revision, installed release/webUI non-empty AND installed app version == version == manifest.version AND release/webUI equal manifest (`:1634-1649`).
- `lastOperation` = selected `/opt/var/lib/broray/operations/<id>/state.json` or legacy `/opt/broray/run/broray/operation.json` (`:1651-1657`).
- `health` = status contract (`lib/status-contract.sh:35-99`) module `broray`, availability `available`, severity: error if !installationHealthy or !versionsConsistent; busy if lastOperation.running; warning if backupProblem / updateAvailable / last op state `error`; else ok. `actionRequired` false only for ok/busy. Reasons (in order): `RELEASE_MANIFEST_INVALID`, `OPKG_REGISTRATION_INVALID`, `INSTALLATION_INCOMPLETE`, `VERSION_MISMATCH`, `BACKUP_INVALID`, `UPDATE_AVAILABLE` "Доступна более новая версия BROray.", `LAST_OPERATION_FAILED` (`:1659-1696`).
- `capabilities`: fixed 9 Russian strings (`:1773-1783`); `links:{github:"https://github.com/BROadmin/BROray",project:"https://docs.brovibe.cloud/broray/",donate:"https://pay.cloudtips.ru/p/09b23d0a"}` (`:70-72`).

Success example:
```json
{"ok":true,"version":"3.1.0","installedPackageVersion":"3.0.0-r14","installedPackageRevision":14,
 "installedReleaseId":"3.1.0-r09","installedWebUIBuild":"WebUI-3.1.0-r09c02","reinstallSupported":true,
 "opkgRegistrationHealthy":true,"build":"3.1.0-r09","releaseId":"3.1.0-r09","candidateId":"3.1.0-r09c02",
 "releaseSource":"exact-r14c18-final-bytes-plus-exact-r26-derived-functional-contract","releaseCommit":null,
 "releaseBuiltAt":"2026-09-10T09:25:00Z","webUIBuild":"WebUI-3.1.0-r09c02","architecture":"arm64",
 "updateChannel":"staging","packageTrack":"compact-app-shared-xray-candidate","updateAvailable":false,
 "updateCheckFresh":true,"availableVersion":"3.1.0-r09","availablePackageVersion":"3.0.0-r14",
 "availableCandidateId":"3.1.0-r09c02","releaseRelation":"same","candidateRelation":"same",
 "lastCheckedAt":"2026-11-02T09:41:07Z","lastBackup":null,"backupValid":false,"backupProblem":null,
 "installationHealthy":true,"versionsConsistent":true,
 "components":[{"id":"core","name":"Ядро BROray","path":"/opt/broray/bin/broray","version":"3.1.0","installed":true,"required":true,"healthy":true},
   {"id":"webui","name":"WebUI","path":"/opt/broray/web-new/home.html","version":"WebUI-3.1.0-r09c02","installed":true,"required":true,"healthy":true,"reasonCode":"WEBUI_LOCAL_HEALTHY","publication":{"state":"enabled","...":"..."}}],
 "protocols":[{"id":"vless","supported":true},{"id":"vmess","supported":true},{"id":"trojan","supported":true},{"id":"hysteria2","supported":true},{"id":"shadowsocks","supported":true}],
 "health":{"schemaVersion":1,"module":"broray","availability":"available","severity":"ok","operational":true,"consistent":true,"actionRequired":false,"freshness":{"state":"fresh","checkedAt":"2026-11-02T10:00:00Z"},"reasons":[],"facts":{"version":"3.1.0","installedPackageVersion":"3.0.0-r14","releaseId":"3.1.0-r09","candidateId":"3.1.0-r09c02","webUIBuild":"WebUI-3.1.0-r09c02","updateChannel":"staging","packageTrack":"compact-app-shared-xray-candidate","releaseManifestValid":true,"opkgRegistrationHealthy":true,"installationHealthy":true,"versionsConsistent":true,"backupValid":false,"updateCheckFresh":true},"lastOperation":null},
 "lastOperation":null,
 "capabilities":["Управление Xray","Импорт подписок","Выбор активного сервера","Интеграция с управляемым ProxyN","Маршрутизация сервисов","Проверка и восстановление компонентов","Обновление через WebUI","Восстановительная переустановка текущей версии","Безопасная очистка резервных копий и временных файлов"],
 "links":{"github":"https://github.com/BROadmin/BROray","project":"https://docs.brovibe.cloud/broray/","donate":"https://pay.cloudtips.ru/p/09b23d0a"},
 "updatedAt":"2026-11-02T10:00:00Z",
 "universalUpdaterReady":true,
 "platformHandoff":{"schemaVersion":1,"contract":"broray-universal-platform-handoff/1","state":"success","running":false,"code":"UNIVERSAL_PLATFORM_READY","message":"Универсальный updater установлен.","mutationStarted":false,"rollbackPerformed":false,"operationId":null,"candidateId":"3.1.0-r09c02","recordedAt":"2026-11-02T10:00:01Z"}}
```

JS readers (`renderInfo`, `js/broray.js:324-370`; `applyControlState` `:193-225`):
- `#installation-status`: healthy iff `installationHealthy===true && versionsConsistent===true && universalUpdaterReady===true` → "Установлено"/`status-success`; else `platformHandoff.running===true` → "Завершение обновления"/`status-loading`; else "Обновление не завершено"/`status-error`.
- `#current-version`=`version`; `#build-description`="Кандидат: " + (`candidateId||webUIBuild||releaseId||build||"не определён"`) + " · релиз: " + (`releaseId||"не определён"`) + " · источник: " + (`releaseSource||"не определён"`).
- `#architecture`, `#update-channel`, `#installed-package-version`, `#webui-build`.
- `#available-version`: `updateAvailable` → `availableVersion||"Доступно"`; else if `lastCheckedAt` → `availableVersion||version||"Обновлений нет"`; else "Не проверено". `#last-update-check` = formatted `lastCheckedAt`.
- `#updater-platform`: `universalUpdaterReady===true` → "Универсальный"; else `platformHandoff.running===true` → "Переход выполняется" else "Переход не завершён".
- Components list: `name||id`, `version||"—"`, `path||"Путь не указан"`, indicator healthy iff `healthy!==false`. Protocols: id→display name map, badge "Установлено"/"Недоступно" from `supported!==false`. Capabilities: string list. Links: `links.project/github/donate` set as `href` of `#project-link`, `#github-link`, `#donate-link` (the latter is a `<details>` element — setting `.href` is a no-op; `web/broray.html:281`).
- Buttons: `#check-update` disabled if busy or no info; `#install-update` disabled unless `updateAvailable`; label "Завершить обновление" when `universalUpdaterReady===false` else "Установить обновление"; `#reinstall-current` disabled unless `reinstallSupported`; `#restore-backup` ALWAYS disabled (title "Восстановление резервной копии временно недоступно для компактной схемы."); uninstall/cleanup disabled only while busy; `#cleanup-run` disabled unless a plan with `candidateCount ≥ 1`.

### 4.2 `POST /api/broray/update-check.cgi`

Body ignored (JS sends `"{}"`, `js/broray.js:642-650`). Runs `broray-updaterctl check` (`api/broray/updater-api-common.sh:93-115`) with all proxy env vars unset. If it fails with `error.code` `UPDATE_CHECK_FAILED` or `UPDATE_SIGNATURE_MISSING`, and a local Xray SOCKS inbound exists (exactly one `socks` inbound, `noauth`, integer port, listen address present on a local interface; `0.0.0.0`→`127.0.0.1`, `::`→`::1`) it retries once with `HTTPS_PROXY=socks5h://host:port` (`:8-32`).

`release_check` (`upd.sh:721-863`): index URL = `$BRORAY_RELEASE_INDEX_URL` or first line of `/opt/var/lib/broray-updater/release-index-url` (must be `https://`); signature URL = index + `.minisig` (query preserved); both fetched with `?broray=<epoch>-<pid>` nonce; minisign verification with fixed public key `RWTlNQ0uUR+MmbfELjB7v4VVML2xgK4Ri1ZmR8ZqDqomMQ0GJPMpYO/O` (`upd.sh:86-88`); `release_index_valid`; relation via `release_relation(currentRelease, candidate.releaseId)` where version pattern `X.Y.Z-rN` compared numerically → `newer|older|same|uncomparable` (`upd.sh:130-174`). Writes `release-index.json` (index + `{checkedAt,checkedEpoch,currentCandidateAtCheck,currentReleaseAtCheck,releaseRelation,candidateRelation,updateAvailable}`), `release-index.signed.json`, `release-index.json.minisig` under `/opt/var/lib/broray-updater/`.

Success **200**:
```json
{"ok":true,"updateAvailable":true,"currentCandidate":"3.1.0-r09c02","currentRelease":"3.1.0-r09",
 "availableVersion":"3.2.0-r12","availablePackageVersion":"3.0.0-r14","candidateId":"3.2.0-r12c01",
 "releaseRelation":"newer","candidateRelation":"newer","checkedAt":"2026-11-02T10:05:00Z","engine":"broray-updater/5"}
```
Errors (C shape + `error.updaterRc`; HTTP map `api/broray/updater-api-common.sh:208-219`): `CHANNEL_INVALID`, `CHANNEL_UNAVAILABLE` "Канал обновлений не настроен.", `UPDATE_SIGNATURE_MISSING`, `UPDATE_SIGNATURE_INVALID`, `RUNTIME_UNAVAILABLE` → **503**; `UPDATE_CHECK_FAILED` "Не удалось загрузить индекс релиза.", `UPDATE_INDEX_INVALID` → **502**; non-JSON output → **503** `UPDATER_BACKEND_FAILED` "Постоянный updater не вернул корректный ответ." with `details` = last 12 stderr lines.

JS (`checkUpdate`, `js/broray.js:631-681`): merges into `state.info`: `updateAvailable = (info.universalUpdaterReady===false) || result.updateAvailable===true`, `availableVersion`, `availablePackageVersion`, `availableCandidateId=result.candidateId`, `candidateRelation||"uncomparable"`, `lastCheckedAt=result.checkedAt||now`, `updateCheckFresh:true`; re-renders. Toast: platform pending → "Требуется завершить переход на универсальный updater." (info); updateAvailable → "Доступна версия <availableVersion>." (info); `candidateRelation==="older"` → "В канале находится более старый кандидат; установка отключена." (warning); else "Установлена актуальная версия." (success).

### 4.3 `POST /api/broray/update-start.cgi` and `POST /api/broray/reinstall.cgi`

Body ignored (JS sends `JSON.stringify({})`, `js/broray.js:592-601`). Both call `broray_updater_api_call '202 Accepted' request <update|reinstall>`.

Handoff continuation first (`api/broray/updater-api-common.sh:57-89`): if `handoff.sh` exists and its status is not READY → `handoff.sh schedule`; success → **202** `{"ok":true,"accepted":true,"operation":"update","state":"queued","operationId":"<handoff operationId // "platform-handoff">","continuation":"universal-platform-handoff"}` (even for reinstall); failure → **503** B-envelope `PLATFORM_HANDOFF_FAILED` "Не удалось продолжить переход на универсальный updater. <details>".

Otherwise `broray-updaterctl request <op>` (`upd.sh:1072-1252`):
- `operationId = "<op>-YYYYmmddHHMMSS-<pid>"`.
- conflict checks (global lock `/opt/var/lock/broray/global-operation.lock`, legacy `/tmp/broray-global-operation.lock`, routes resumable ops) → `OPERATION_BUSY` "Другая операция BROray активна или её блокировка неоднозначна: <state>." (409).
- daemon must be running/ready (`daemon.pid` == `daemon.ready`) else `UPDATER_NOT_RUNNING` "Постоянный updater не запущен; операция не принята." (503).
- `mkdir /opt/var/lib/broray-updater/request.lock` else `OPERATION_BUSY` "Другая операция BROray уже принята или выполняется." (409).
- update: target from cached signed index; must be fresh (≤3600 s), cache `currentReleaseAtCheck` == current slot release, relation newer; else `UPDATE_NOT_AVAILABLE` "Новая версия не обнаружена." (rc2, 409) or `UPDATE_CHECK_REQUIRED` "Сначала выполните свежую проверку обновления." (409). (`upd.sh:865-890, 1170-1185`)
- reinstall: target = current slot metadata `.source` (bundle url/sha256/sizeBytes) else `REINSTALL_SOURCE_UNAVAILABLE` "Источник установленной версии не подтверждён." (503) (`upd.sh:892-908`).
- writes `/opt/var/lib/broray/operations/<id>/{request.json,log.txt,state.json}`, pointer `/opt/var/lib/broray/last-operation`, queue file `/opt/var/lib/broray-updater/queue/<id>.json`; initial state `queued`, progress 0, message "Запрос сохранён и принят постоянным updater.".
- Success **202**: `{"ok":true,"accepted":true,"operationId":"update-20261102100512-12345","operation":"update","state":"queued","engine":"broray-updater/5"}`.
- Other error codes possible: `INVALID_REQUEST`, `INVALID_OPERATION`, `PREPARED_TARGET_CHANGED`, `RUNTIME_UNAVAILABLE` (503); all get `error.updaterRc`.

The daemon (`S22broray-updater` → `upd.sh daemon`) processes the queue: stages/progress/messages written by `status_write` (`upd.sh:408-457`):

| state | stage | progress | message |
|---|---|---|---|
| queued | queued | 0 | Запрос сохранён и принят постоянным updater. |
| running | downloading | 10 | Скачивается компактный архив приложения без Xray. |
| running | verifying | 30 | Проверяются структура и внутренний манифест архива. |
| running | staging | 45 | Приложение распаковывается в новый компактный slot; общий Xray не копируется. |
| running | stopping | 60 | Останавливаются только ранее запущенные службы BROray. |
| running | switching | 70 | Компактный app-slot переключается двумя журналируемыми rename. |
| running | starting | 82 | Восстанавливаются службы и запускается WebUI новой версии. |
| running | health | 92 | Проверяются новый slot, конфигурация Xray, службы, HTTP WebUI и JSON backend страницы BROray. |
| rolling-back | rollback | 90 | Проверка новой версии не пройдена; возвращается предыдущий каталог релиза. |
| success | complete | 100 | BROray переключён на проверенный компактный app-slot. |
| error | rolled-back / rollback-failed / download / verify / space / staging / retention / compatibility / platform / routes / services / pre-switch / recovery-* | 100 | (see `upd.sh:1746-1759, 2660-2712, 2814-3045, 3221-3383`) |

`running` is true for `queued|running|rolling-back` (`upd.sh:419-421`).

### 4.4 `GET /api/broray/update-status.cgi`

`broray-updaterctl status` → `status_output` (`upd.sh:459-494`): reads pointer `/opt/var/lib/broray/last-operation` → `/opt/var/lib/broray/operations/<id>/state.json` + `logTail` = last 80 lines of `log.txt`. No pointer/state → idle object:
```json
{"schemaVersion":1,"engine":"broray-updater/5","durable":true,"ok":true,"operationId":null,"operation":null,"state":"idle","stage":"idle","progress":0,"message":"Операции ещё не выполнялись.","error":null,"running":false,"mutationStarted":false,"rollbackPerformed":false,"logTail":"","updatedAt":"2026-11-02T10:06:00Z"}
```
Running example:
```json
{"schemaVersion":1,"engine":"broray-updater/5","durable":true,"ok":true,"operationId":"update-20261102100512-12345","operation":"update","state":"running","stage":"staging","progress":45,"message":"Приложение распаковывается в новый компактный slot; общий Xray не копируется.","error":null,"running":true,"mutationStarted":false,"rollbackPerformed":false,"updatedAt":"2026-11-02T10:05:40Z","logTail":"2026-11-02T10:05:13Z  ..."}
```
The same pointer/dir is shared with the uninstall worker (`lib/operation-manager.sh:11-13, 85-101`) whose state.json has `{ok,operationId,operation:"uninstall",state,stage,progress,message,error,running,updatedAt}` (no schemaVersion/engine; `running` true for `queued|running|restoring`, `lib/broray-page.sh:1178-1220`).

Handoff overlay (`api/broray/updater-api-common.sh:124-165`): when status is `ok:true` and `handoff.sh status` returns an object:
- `state:"running"` → override `operation:"update",state:"running",stage:"platform",progress:98,running:true,message:<handoff message>,error:null,platformHandoff:<h>`.
- `state:"error"` → override `operation:"update",state:"error",stage:"platform",progress:100,running:false,message:<msg>,error:<msg>,platformHandoff`.
- `state:"success"` → only add `platformHandoff`.
RISK: `handoff.sh status` returns `state:"error", code:"UNIVERSAL_PLATFORM_NOT_READY"` whenever the platform files differ OR `S22broray-updater status` fails (e.g. updater daemon stopped), so update-status then reports a failed "update" regardless of the real last operation.

Failure: non-JSON / `ok:false` → C error with `updaterRc` (503 default).

JS (`loadOperation`/`renderOperation`/`pollOperation`, `js/broray.js:418-590`):
- Reads `operationId, operation, state, running, progress, message, error, logTail`.
- Badge labels: idle "Нет операций", queued "В очереди", running "<Обновление|Переустановка|Восстановление|Удаление|Операция>…" (by `operation`), restoring "Восстановление…", success "Завершено", error "Ошибка", other → raw `state` (e.g. `rolling-back` shows literally). Class success/error/loading (`running || queued || restoring`)/neutral; `data-icon` `close` for error else `logs`.
- `#operation-message` = message or "Операции ещё не выполнялись."; `#operation-log` = `logTail` or "Журнал пока пуст."; progress clamped 0..100; progress box hidden iff `!running && progress===0`; `#operation-error` shown iff `error` truthy.
- `setBusy(running)`; polling: first poll 500 ms after acceptance/render, then every 2000 ms while busy; on fetch error delay `min(5000, 1000 + failures*500)`, show "WebUI временно перезапускается…" with badge "Ожидание WebUI…"; after 6 consecutive failures stop with "Результат операции <id> не подтверждён: WebUI недоступен. При удалении интерфейс может исчезнуть до получения ответа. …" (`:536-577`).
- After a start, if `operation.operationId !== acceptedId` the response is replaced by a synthetic `{state:"queued",running:true,message:"Запрос принят. Ожидается состояние операции."}`; after 15 such polls → stop with "Результат операции <id> не подтверждён. Обновите состояние; не запускайте удаление повторно до проверки." (`:482-501`). For handoff continuation the accepted id is the handoff operationId; NOTE the overlay keeps the source operation's `operationId`, so matching relies on them being equal (UNCONFIRMED).
- When `running` becomes false: clears id, reloads info, toast `message || "Операция завершена."` with type success iff `state==="success"` else error.
- Also resumes polling on `online` / visibilitychange; stops on `pagehide`.

Start toasts: update "Обновление запущено." / "Завершение обновления запущено." (platform pending); reinstall "Восстановительная переустановка запущена."; uninstall "Удаление запущено. WebUI станет недоступен.". `state.operationKind = accepted.operation || ("uninstall" if path contains "uninstall")` (`:612-614`).

Confirm dialogs (`BROrayDialogs.confirm`, variant primary): update title "Установить обновление BROray?"/"Завершить обновление BROray?", confirm "Установить"/"Завершить"; reinstall title "Переустановить текущую версию BROray?", message mentions `candidateId || installedPackageVersion || version` (`:831-875`).

### 4.5 `POST /api/broray/restore.cgi`

After method/session/header checks, ALWAYS **409** B-error `RESTORE_UNAVAILABLE` "Восстановление резервной копии недоступно для компактной схемы." (`api/broray/restore.cgi:14-17`). The UI button `#restore-backup` is permanently disabled (`js/broray.js:216-217`, `web/broray.html:347`); `restoreBackup()` (`js/broray.js:877-896`) exists but is unreachable. `broray-system restore-start` returns the same code as C-shape (`bin/broray-system:21-24`).

### 4.6 `POST /api/broray/cleanup-plan.cgi`

Body (`js/broray.js:691-698, 751-759`): `{"temp":bool,"backups":bool,"routeBackups":bool,"logs":bool}` (checkbox states; missing key defaults to `true`, `api/broray/cleanup-plan.cgi:26-29`).
Validation: body limit 4096: missing/zero length → **400** `INVALID_CONTENT_LENGTH` "Тело запроса пусто или имеет некорректный размер."; >4096 → **413** `REQUEST_TOO_LARGE` "Тело запроса слишком большое."; short → **400** `REQUEST_BODY_INCOMPLETE` "Тело запроса передано не полностью."; non-object → **400** `INVALID_JSON` "Тело запроса содержит некорректный JSON.". Values must be JSON `true|false` (anything else, incl. `null`, `"true"` is accepted only if jq `-r` prints `true`/`false` — a string `"true"` prints `true` and is accepted) else **400** `CLEANUP_OPTIONS_INVALID` "Параметры очистки некорректны."; all false → **400** `CLEANUP_NOTHING_SELECTED` "Выберите хотя бы одну категорию очистки.".
Busy: global lock `/opt/var/lock/broray/global-operation.lock` is a dir whose `pid` file holds a live PID → **409** `CLEANUP_OPERATION_BUSY` "Другая конфликтующая операция уже выполняется.". Other failures **500** (`CLEANUP_PLAN_FAILED`, `CLEANUP_TOKEN_FAILED`, `CLEANUP_JQ_MISSING`, `CLEANUP_SHA256_MISSING`, `CLEANUP_RUNTIME_FAILED`, `CLEANUP_RESULT_FAILED`, `CLEANUP_EMPTY_RESPONSE`, `CLEANUP_RESPONSE_FAILED`).

Plan algorithm (`lib/broray-cleanup.sh`):
- Expired plans in `/opt/broray/run/cleanup/plans/` removed first (`:354-368`).
- token = first 32 hex of `sha256("<epoch>:<pid>:<ppid>:<uptime>")` (`:69-74`); `ttlSeconds = BRORAY_CLEANUP_TTL = 120` (`:12`).
- "old" = `floor((now - mtime)/86400) > days` (`:103-115`).
- temp (days 0, i.e. ≥1 day old) under `/opt/broray`: `tmp/broray-*`, `tmp/routes-*`, `tmp/.broray-*`, `tmp/*.part`, `tmp/*.download`, `tmp/*.tmp.*`, `routes/tmp/*`, `routes/transactions/*`, `run/broray/*.tmp.*` (`:212-226`).
- backups (keep newest 3 per group by reverse lexicographic name, older than 1 day): groups `backup/system-before-*.tar.gz + system-before-reinstall-*`; `user-before-*`(+reinstall); `web-new-before-*`+`web-before-*`; `xray-before-*`+`xray-binaries/*`; `lighttpd.conf.before-*`+`package-before-*`+`before-update-*`+`before-restore-*`+`before-uninstall-*` (`:228-251`).
- routeBackups (keep 3, >1 day): `routes/backup/{catalog-*,user-removed-*,rollback-*,transaction-*}` (`:253-263`).
- logs (>6 days, i.e. ≥7): `logs/*.gz`, `logs/*.old`, `logs/*.log.[0-9]*`, `logs/*.log-*`, `run/broray/operation.log.*`, `run/broray/*.old` (`:265-276`).
- symlinks, non-file/dir, paths with `..`/control chars skipped; size = bytes for files, `du -sk*1024` for dirs. List lines `category\tkind\tsize\trelative`, `sort -u`; digest = sha256 of list.
- Writes `/opt/broray/run/cleanup/plans/<token>.json` + `<token>.list` (0600).

Success (B envelope) data example:
```json
{"schemaVersion":1,"token":"3f9c0a7d1e2b4c5d6e7f8091a2b3c4d5","createdAt":"2026-11-02T10:10:00Z","createdEpoch":1793614200,
 "expiresEpoch":1793614320,"ttlSeconds":120,"digest":"<sha256>",
 "options":{"temp":true,"backups":true,"routeBackups":true,"logs":true},
 "candidateCount":2,"estimatedBytes":1835008,
 "categories":{"backups":{"count":1,"estimatedBytes":1048576},"logs":{"count":1,"estimatedBytes":786432}},
 "candidates":[{"category":"backups","kind":"file","path":"backup/user-before-20260901-101010.tar.gz","sizeBytes":1048576},
               {"category":"logs","kind":"file","path":"logs/xray.log.1","sizeBytes":786432}]}
```
JS (`renderCleanupPlan`, `js/broray.js:709-739`): `#cleanup-count`=`candidateCount||0`, `#cleanup-bytes`=formatBytes(`estimatedBytes`) (base 1024, units Б/КБ/МБ/ГБ, ru-RU, 0 or 1 fraction digits), `#cleanup-expiry`="действует <ttlSeconds||120> секунд", list items `path` + formatBytes(`sizeBytes`), empty → "Подходящих файлов не найдено."; badge "Готово к очистке"(warning)/"Очистка не требуется"(success); stores `plan.token`. Toast "План очистки сформирован."/"Очистка не требуется.". Changing any checkbox invalidates the plan client-side (`:700-707, 945-947`).

### 4.7 `POST /api/broray/cleanup.cgi`

Body `{"token":"<32 hex>"}` (`js/broray.js:802-810`). Same body-size/JSON errors as 4.6; missing/empty token → **400** `CLEANUP_TOKEN_REQUIRED` "Не передано подтверждение очистки.".
Execute (`lib/broray-cleanup.sh:584-715`): token format → else `CLEANUP_TOKEN_INVALID`; plan missing → `CLEANUP_PLAN_NOT_FOUND` "План очистки не найден. Выполните проверку заново."; expired (`expiresEpoch < now`) → `CLEANUP_PLAN_EXPIRED` "Срок подтверждения истёк. Выполните проверку заново."; list re-built with the plan options and compared (digest and byte size) → `CLEANUP_PLAN_CHANGED` "Состав файлов изменился. Выполните проверку заново." — all **409**. Lock: refuses (rc2 → **409** `CLEANUP_OPERATION_BUSY`) if any `/opt/broray/routes/operations/*.json` has `kind=="routes",running==false,resumable==true`, or `/opt/var/lib/broray-updater/request.lock` exists, or `/tmp/broray-global-operation.lock` exists; then `broray_ops_begin system system:cleanup cleanup USER protected` registers a background operation (appears in the Operations panel as type `unknown` → "Фоновая операция") (`:396-411`); other lock failures **500** `CLEANUP_LOCK_FAILED`. Each line re-validated against an allow-list (`:555-582`), deleted with `rm -f`/`rm -rf`; stops at first failure → **500** `CLEANUP_DELETE_FAILED` "Очистка остановлена на объекте: <relpath>". Finalization failure → **500** `CLEANUP_FINALIZATION_FAILED`. Plan files are always removed after execution.
Success data: `{"ok":true,"deletedCount":2,"freedBytes":1835008,"completedAt":"2026-11-02T10:11:00Z"}`.
JS: badge "Очищено", `#cleanup-count`=`deletedCount`, `#cleanup-bytes`=`freedBytes`, `#cleanup-expiry`="завершено", toast "Очистка завершена: освобождено <bytes>." (`:811-819`). Confirm dialog title "Удалить найденные служебные файлы?" (`:786-793`). Any error → plan invalidated.

### 4.8 `POST /api/broray/uninstall.cgi`

Body: `{"mode":"normal"|"full","confirmation":"<phrase>"}` (`js/broray.js:925-931`). Phrases (must match exactly; the JS sends the constant, after the user typed it exactly — `dialogs.js` compares `input.value === phrase` without trimming, `js/dialogs.js:117-124`):
- `normal` → `УДАЛИТЬ BROray`
- `full` → `УДАЛИТЬ BROray ПОЛНОСТЬЮ`
(`js/broray.js:900`, `lib/broray-page.sh:2585-2601`).

CGI validation: body ≤4096 (`INVALID_CONTENT_LENGTH` 400, `REQUEST_TOO_LARGE` 413, `REQUEST_BODY_INCOMPLETE` 400), JSON object else **400** `REQUEST_JSON_INVALID` "Тело запроса должно быть корректным JSON-объектом.". Then headers (no Status → **200**) and stdout of `/opt/broray/bin/broray-system uninstall-start "$mode" "$confirmation"` streamed as body (`api/broray/uninstall.cgi:32-34`). So every backend error is HTTP 200 with `ok:false`; an empty body is possible when the backend returns 1 without JSON (e.g. missing `/opt/var/lib/broray/operations.guard`, worker spawn/handoff timeouts in `lib/broray-page.sh:2434-2527`) → JS "Backend вернул пустой ответ (HTTP 200)."

`broray_system_uninstall_start` (`lib/broray-page.sh:2581-2645`):
- `INVALID_MODE` "Неизвестный режим удаления."; `CONFIRMATION_REQUIRED` "Фраза подтверждения не совпала.".
- Re-executes itself under `broray-ops-guard --scope /opt/var/lib/broray/operations.guard` (kernel flock held by the worker until the end).
- Any of `/tmp/broray-global-operation.lock`, `/opt/var/lock/broray/global-operation.lock`, `/opt/var/lib/broray-updater/request.lock` present → `OPERATION_CONFLICT` "Другая операция BROray выполняется или требует восстановления.".
- If `/opt/var/lib/broray-platform-handoff/phase` == `boot-pending` → `handoff.sh settle` else `PLATFORM_HANDOFF_UNCONFIRMED`.
- Stops `S22broray-updater` (records whether it was running) and requires a JSON stop proof `{ok:true,phase:"SERVICE_STOP_COMPLETED",serviceStopped:true,platformReady:false,generationId}` else `UPDATER_STOP_UNCONFIRMED`.
- `broray_system_start_worker uninstall` (`:2390-2529`): errors `RUNTIME_ERROR`, `STATUS_STATE_INVALID`, `OPERATION_CONFLICT` (3 messages), `OPKG_CONTROL_PREFLIGHT_FAILED` (`broray_system_error_details_json` adds `reason`, `evidence`, `mutationStarted:false`). On admission failure the updater is restarted.
- Success: `{"ok":true,"accepted":true,"operationId":"uninstall-20261102101500-23456","operation":"uninstall","engine":"canonical-current-operation"}`.

Worker (`broray_system_worker_uninstall`, `lib/broray-page.sh:2647-3967`), progress published to `/opt/var/lib/broray/operations/<id>/state.json` (readable via update-status):

| state | stage | progress | message |
|---|---|---|---|
| running | backup | 10 | Сохраняются данные перед удалением. |
| running | dot | 25 | Удаляются управляемые записи DNS-over-TLS BROray. |
| running | keenetic | 38 | Удаляется управляемый ProxyN; связанные маршруты удаляет Keenetic. |
| running | publish | 48 | Удаляется KeenDNS HTTP Proxy BROray. |
| running | servers | 58 | Отключается активный сервер. |
| running | xray | 68 | Останавливается Xray. |
| running | services | 74 | Останавливаются вспомогательные сервисы BROray. |
| running | remove | 82 | Удаляется пакет BROray. |
| error | preflight/backup/acceptance/signal/<stage> | 100 | "Удаление отменено: …" texts (`:3440-3690`) / abort with rollback |

- Both modes: removes owned DoT entries, the managed ProxyN, the KeenDNS HTTP proxy publication, deactivates server, stops Xray and S23/S24/S25/S27/S28 services, writes OPKG authorization `/opt/var/lib/broray-opkg/uninstall-authorized.json`, runs `opkg remove broray`, restores lighttpd baseline, then deletes `/opt/etc/init.d/S22broray-updater S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions`, `/opt/bin/{broray,broray-routes,broray-routes-dot,broray-routes-user,broray-server,broray-servers,broray-subscriptions,broray-system,broray-updaterctl}`, `/opt/etc/opkg/broray.conf`, `/opt/libexec/broray-updater`, `/opt/broray`, `/opt/var/lib/broray`, `/opt/var/lib/broray-updater`, `/opt/var/lock/broray` (`:3203-3238`).
- `normal`: before mutations creates `/opt/broray-preserved/broray-user-data-<YYYYmmdd-HHMMSS>-<opid>.tar.gz` (+ `.sha256`, mode 600, dir 700) from the protected roots (`:1330-1340`: backup, backups, config/…, data, subscriptions, servers, routes/…), publishes `/opt/broray-preserved/LATEST` (file name) at the end.
- `full`: removes `/opt/broray-preserved` entirely.
- On full success the status dir is deleted with `/opt/var/lib/broray`, so no terminal "success" state is ever observable; the WebUI disappears → JS ends in the 6-failure "не подтверждён" message (by design).

JS dialog (`uninstall`, `js/broray.js:898-932`): `BROrayDialogs.confirmPhrase` with eyebrow "Необратимое действие", title "Полностью удалить BROray?"/"Удалить BROray?", input label "Введите контрольную фразу", hint "Введите точно: <phrase>", mismatch "Контрольная фраза не совпадает.", confirm "Удалить полностью"/"Удалить BROray".

---

## 5. OPERATIONS (background operations panel)

Shared pipeline `broray_operations_api METHOD VERB` (`api/operations/common.sh:5-117`): validation per §1.5, then `broray_ops_call <args>` (`lib/operation-client.sh:49-133`) which runs `broray-ops-guard /opt/var/lib/broray/operations.guard /opt/bin/ash /opt/broray/lib/operation-coordinator.sh <args>` (flock; guard exit 75 with no output = lock wait ended → retried up to 15 attempts ≈ 30 s). For `status|events|report` the state dir and `operations.guard` must already exist, else no output.

HTTP mapping (`common.sh:100-116`):
- coordinator output not a JSON object → **503** B-error `STATE_UNAVAILABLE` "Состояние операций временно недоступно.".
- exit ≠ 0 or `.ok==false` → status by `.errorCode` (default `STATE_UNAVAILABLE`): `OPERATION_BUSY|DOMAIN_OPERATION_BUSY|CANCEL_NOT_SUPPORTED|RECOVERY_BLOCKED` → **409**; `REQUEST_UNCONFIRMED` → **404**; else **503**. Body = coordinator object unchanged (D shape).
- success: POST except `automation` and `queue-lookup` → **202**; otherwise **200**.
- `servers-check`/`subscription-refresh` success: response gets `accepted:(.ok==true)` added.

Coordinator state: `/opt/var/lib/broray/operations/<id>/{state.json,owner.json,cancel.json,fence,…}` (schemaVersion 2, `kind:"background"`), RAM steps `/tmp/broray-operations/steps/<id>/state.json`, automation flag `/opt/var/lib/broray/background-automation.json` `{schemaVersion:1,paused,updatedAt}`, journal `/opt/var/lib/broray/operation-events/{head.json,events.jsonl,events.1.jsonl,events.2.jsonl}`, queue (`lib/operation-scheduling.sh`), global fence `/opt/var/lock/broray/global-operation.lock`.

Public projections (`lib/operation-public.jq`):
- `operation_public` (`:39-52`): `{operationId,type,source,state,phase,running,revision,cancelability,cancelRequested,startedAt,updatedAt,finishedAt,errorCode,ownerStatus,ownerReason,requestId,resourceLocks}`.
  - `operationId` valid only as `op-<14 digits>-<pid 2..2147483647>-<12 hex>` or `op-q-<32 hex>` else null (`:10-17`).
  - `type` mapping from operation string (`:26-36`): `subscriptions:*`→`subscription_update`, `servers:*`→`server_operation`, `auto-switch`→`auto_switch`, `system:platform-preflight`→`updater_maintenance`, `xray:*`→`xray_maintenance`, `dot:*`→`dns_operation`, `keenetic:*`→`router_operation`, route actions (`custom:*`, `preflight:*`, check/download/build-export/verify/plan/export/delete/resume/route_operation)→`route_operation`, else `unknown`.
  - `source` ∈ USER, SCHEDULER, SUBSCRIPTION_AUTO, SERVER_CHECK_AUTO, AUTO_SWITCH, UPDATER, SYSTEM_RECOVERY else `UNKNOWN`.
  - `state` ∈ starting, running, completed, failed, aborted, recovered else `unknown`; `phase` ∈ starting, working, checking, fetching, parsing, committing, switching, waiting, recovering, finished else `unknown`.
  - `cancelability` = `protected` for routes scope/actions else `cooperative|protected`.
  - `ownerStatus` ∈ ACTIVE, STALE, AMBIGUOUS, FINISHED; `ownerReason` enum `:50`; `errorCode` enum `:38`; timestamps must be `YYYY-MM-DDTHH:MM:SSZ` else null.
- `queue_public` (`:54-60`): `{requestId:"q-<32hex>"|null, priority:0..5|null, type, source, stage ∈ checking|verify|probe|fetch|parse|apply|activate|finished|unknown, state ∈ queued|running|completed|cancelled|failed|unknown, operationId (only when running), reason ∈ awaiting_resource|active_connection|automation_paused|null}`.
- Queue policy (`lib/operation-scheduling.sh:5-18`): priorities `servers:active-health` 0 (checking), `servers:failover AUTO_SWITCH` 1 (verify), `servers:quality USER` 2, `SERVER_CHECK_AUTO` 3 (probe), `subscriptions:refresh USER` 2, `SUBSCRIPTION_AUTO` 4 (fetch), `dot:auto-check SCHEDULER` 5 (probe). Queue limit 64 queued requests, receipts last 128 (`:144-160`). `reason` (`:208-223`): non-queued → null; paused and source ≠ USER → `automation_paused`; any live priority<2 item (when not paused, or USER, or running) and own priority ≥2 → `active_connection`; else `awaiting_resource`. Finished receipts not in live list are appended with `stage:"finished"`. Sorted by priority, sequence.

### 5.1 `GET /api/operations/status.cgi` (verb `status`, `lib/operation-coordinator.sh:1124-1206`)

Scans `operations/*/state.json` and `RAM/steps/*/state.json` (max 128 each, else `errors:["HISTORY_LIMIT"]`). Terminal background rows projected with `ownerStatus:"FINISHED"`; non-background (updater/uninstall) records skipped; live rows get owner classification, `phase:"waiting"` while executor pending, `cancelRequested` from `cancel.json`. `automationPaused` = true if the automation file exists and is not exactly `paused:false` (missing file → false). `globalFence` ∈ `absent|managed_active|managed_stale|ambiguous` (ambiguous adds `OWNER_UNCONFIRMED`). Queue errors add `QUEUE_STATE_INVALID`.

Response (always the full object; `ok == complete == (errors empty)`; when not ok → HTTP 503 with this same body):
```json
{"ok":true,"complete":true,"capturedAt":"2026-11-02T10:20:00Z",
 "operations":[{"operationId":"op-20261102101955-4711-a1b2c3d4e5f6","type":"subscription_update","source":"SCHEDULER","state":"running","phase":"fetching","running":true,"revision":3,"cancelability":"cooperative","cancelRequested":false,"startedAt":"2026-11-02T10:19:55Z","updatedAt":"2026-11-02T10:19:58Z","finishedAt":null,"errorCode":null,"ownerStatus":"ACTIVE","ownerReason":"identity_matches","requestId":null,"resourceLocks":["global"]}],
 "queue":[{"requestId":"q-0123456789abcdef0123456789abcdef","priority":2,"type":"server_operation","source":"USER","stage":"probe","state":"queued","operationId":null,"reason":"awaiting_resource"}],
 "errors":[],"automationPaused":false,"globalFence":"managed_active"}
```
`operations` sorted by `startedAt` descending.

JS (`render`, `js/background-operations.js:46-108`): active = `operations` with `running !== false`; waiting = queue `state==="queued"`; history = `historyRows` (terminal ops deduped by `requestId||operationId`, latest by `finishedAt||updatedAt||startedAt`, merged with terminal queue rows; ops states `aborted`→`cancelled`, `recovered`→`failed`; sorted newest first) (`:13-34`). `unknown` = stale fetch OR `complete !== true` OR `globalFence === "ambiguous"`.
- `#bg-badge`: unknown → "Нужно проверить состояние"(warning); else "Выполняется: N"/"В очереди: N"(loading) or "Нет операций"(success).
- `#bg-summary`: unknown → "Не удалось обновить состояние. Ниже — последние полученные данные; они могут быть устаревшими." / "Не удалось получить состояние операций."; else "Текущие задания BROray. В очереди: N." or "Активных фоновых операций нет.".
- `#bg-recover` shown iff unknown or any active op has `ownerStatus !== "ACTIVE"`.
- Active row: type label map (`:4`), source map (`:5`), phase map (`:6`), "Начало" `startedAt`, "Владелец" "Подтверждён"/"Требуется проверка". Cancel button "Остановить операцию"/"Остановка запрошена" only if `type !== "route_operation" && cancelability === "cooperative" && operationId`; else note "Остановка операций с маршрутами недоступна…"/"Выполняется защищённый этап…".
- Queue rows: state label (completed/cancelled/failed/automation_paused/active_connection/queued), source, priority label by index `["Активное подключение","Переключение сервера","Ручное задание","Проверка серверов","Обновление подписок","Проверка DoT"]`; history rows add "Завершение" = `finishedAt`; "Отменить задание" button for queued items with valid `q-` id → `cancel {requestId}`.
- History `<details id="bg-history">` summary "Завершённые — N · С ошибками — M", page size 10 (+10 per "Показать ещё").
- `#bg-automation-badge`: `automationPaused` boolean → "На паузе"(warning)/"Включена"(neutral), else/stale "Состояние неизвестно"; `#bg-resume` visible iff `automationPaused === true`.
- On error, if `error.data.operations` is an array the error body becomes the snapshot (rendered stale).
- Timer: refresh every 10 s while page visible (`:162`); plus initial refresh+journal.

`common.js` queue watcher also polls `GET status.cgi` every 5 s, requiring `complete === true` and array `queue`, matching `requestId` (`js/common.js:146-169`).

### 5.2 `POST /api/operations/status.cgi` (verb `queue-lookup`)

Body exactly `{"nonce":"<32 lowercase hex>"}` (keys must equal `["nonce"]`) else **400** B `INVALID_REQUEST` "Некорректный идентификатор запроса." (`common.sh:79-83`). Response **200** `{"ok":true,"requestId":"q-<nonce or coalesced id>","state":"queued","priority":2,"coalesced":false}` (`lib/operation-scheduling.sh:115-126, 201-206`); unknown nonce → **404** `{"ok":false,"errorCode":"REQUEST_UNCONFIRMED"}`. `js/common.js:104-113` checks `error.status===404 && error.payload.errorCode==="REQUEST_UNCONFIRMED"`; `queuePost` requires `ok===true`, `requestId` `^q-[0-9a-f]{32}$`, `state` in queued/running/completed/failed/cancelled (`:65-77`). `queueLabel` maps reason/state to Russian labels (`:78-85`). Nonce persisted in `sessionStorage["broray.queue.v1:<url>:<id>"]` before submission (`:86-130`).

### 5.3 `GET /api/operations/events.cgi` (verb `events`, `lib/operation-journal.sh:130-166`)

Snapshot (not a stream). Response **200**:
```json
{"ok":true,"complete":true,"truncated":false,"nextCursor":null,"firstSequence":1,"lastSequence":42,"errors":[],
 "events":[{"sequence":42,"eventId":null,"timestamp":"2026-11-02T10:19:58Z","operationId":"op-20261102101955-4711-a1b2c3d4e5f6","operationType":"subscription_update","source":"SCHEDULER","event":"phase_changed","pid":4711,"result":"pending","errorCode":null,"message":"Этап операции изменился"}]}
```
- `events` = last 500 parsed rows of `events.2.jsonl + events.1.jsonl + events.jsonl` (oldest→newest) projected through `event_public` (`lib/operation-public.jq:78-86`); `truncated` if >500 rows.
- `complete=false` & `errors:["JOURNAL_GAP"]` when a gap marker exists (`operation-events/gap` or `/tmp/broray-operations/journal-gap`), head pending/gap, non-object lines, non-consecutive sequences, or head/first/last/hash mismatch.
- Event names: started, lock_acquired, lock_conflict, phase_changed, owner_transferred, cancel_requested, completed, failed, aborted, recovered, ambiguous_owner, heartbeat_problem, term, kill (else `unknown`); `result` success/failure/cancelled/pending/unknown. Record size ≤2048 bytes; segment rotation at 262144 bytes (`lib/operation-journal.sh:3-4, 45-111`).
- Error → **503** `{"ok":false,"errorCode":"JOURNAL_UNAVAILABLE"}`.
JS (`journal`, `js/background-operations.js:115-124`): requires array `events`; badge "Последние события"(neutral) if `complete===true` else "Журнал неполный"(warning); shows last 20 reversed: `toLocaleTimeString` of `timestamp` + client message map by `event` (`:7`, server `message` ignored); empty → "Событий пока нет."; error → badge "Не удалось обновить журнал". Called on init, after every mutation, and via `#bg-journal-refresh`.

### 5.4 `GET /api/operations/report.cgi` (verb `report`, `lib/operation-report.sh:3-60`)

Read-only diagnostics, ≤1 MiB else failure → **503** `{"ok":false,"errorCode":"REPORT_UNAVAILABLE"}`. Header `Content-Disposition: attachment; filename="BROray-diagnostics.json"`. Shape:
```json
{"schemaVersion":1,"reportKind":"broray-diagnostics","capturedAt":"…","redactionPolicy":"allowlist-v1",
 "complete":false,"snapshotConsistent":false,"consistency":{"operations":"serialized","runtime":"sampled"},
 "summary":"0 активных операций; 5 из 5 фоновых служб проверены. Сетевая непрерывность этим отчётом не проверяется.",
 "unavailable":["keeneticOS","vpnContinuity","updaterLiveIdentity"],
 "build":{"appVersion":"3.1.0","candidateId":"3.1.0-r09c02","webuiBuild":"WebUI-3.1.0-r09c02"},
 "platform":{"architecture":"aarch64","kernel":"4.9-ndm-5","uptimeSeconds":86400},
 "services":{"xray":"running","scheduler":"running","updater":"unknown"},
 "serviceDetails":[{"service":"subscriptions","state":"running","running":true,"ready":true,"complete":true,"errorCode":null}],
 "runtimeDetails":{"xray":{"state":"running","identity":{"verified":true,"pid":1234,"startTicks":"…","sameBoot":true,"role":"persistent-xray"},"cachedVersion":"25.9.11","cache":{…},"complete":true,"errorCode":null},
                   "updater":{"lastOperation":null,"cache":{…},"liveState":"unknown","complete":false,"errorCode":"UPDATER_STATUS_UNAVAILABLE"}},
 "automation":{"paused":false,"subscriptionUpdate":true,"serverCheck":true,"autoSwitch":false},
 "automationDetails":{"complete":true,"subscriptionRecordsRead":2,"automaticSubscriptions":1,"errors":[]},
 "operations":[…active + up to 20 terminal…],
 "fences":{"global":"absent","updaterRequest":"absent","domainPending":"clear"},
 "events":[…journal events…],"journal":{"complete":true,"truncated":false},
 "snapshotComplete":true,"errors":["UPDATER_STATUS_UNAVAILABLE"]}
```
Sources: `web-new/build.json` (`build_public`), `uname`, `/proc/uptime`, services `subscriptions auto-switch connection-monitor home-snapshot interface-reconcile` via `service-lifecycle.sh` status, Xray identity from home xray snapshot pid + `/proc`, updater = home `broray` snapshot `.lastOperation` through `updater_public` (`lib/operation-report-facts.sh:10-149`, `lib/operation-report-public.jq`). `complete` is hard-coded `false`.
JS (`report`, `js/background-operations.js:148-161`): requires `reportKind==="broray-diagnostics"` and `JSON.stringify(data,null,2).length ≤ 1048576`; copy → clipboard (fallback textarea `#bg-report-copy`), download → Blob `BROray-diagnostics.json` (client-side, JSON pretty-printed + "\n"). Feedback "Отчёт скопирован."/"Отчёт скачан." + " Недоступные сведения отмечены в отчёте." if `complete===false` (always). Error "Не удалось получить отчёт. Повторите после обновления состояния.".

### 5.5 `POST /api/operations/cancel.cgi`

Body exactly `{"operationId":"<operation_id>"}` or `{"requestId":"q-<32hex>"}` else **400** B `INVALID_REQUEST` "Некорректный идентификатор операции." (`common.sh:72-78`).
- operationId → `ops_cancel` (`lib/operation-coordinator.sh:1046-1057`): not running → `{"ok":true,"alreadyFinished":true}`; protected/route → **409** `{"ok":false,"errorCode":"CANCEL_NOT_SUPPORTED"}`; else writes `cancel.json {cancelRequested:true,requestedAt}`, journal `cancel_requested`, → `{"ok":true,"cancelRequested":true}`; load failure → 503 `STATE_UNAVAILABLE`.
- requestId → `ops_queue_cancel` (`lib/operation-scheduling.sh:746-760`): queued → removed, receipt `cancelled`; already-cancelled receipt → same; running → **409** `OPERATION_BUSY`; unknown → **404** `REQUEST_UNCONFIRMED`. Success `{"ok":true,"requestId":"q-…","state":"cancelled"}`.
- HTTP success **202**.
JS message: `state==="cancelled"` "Задание отменено."; `alreadyFinished` "Операция уже завершена."; else "Остановка запрошена. Ожидаем завершения текущего шага." (`js/background-operations.js:138`). Errors: 409 → "Операция перешла на защищённый этап или занята другой задачей. Обновите состояние."; else "Запрос не подтверждён. Обновите состояние перед повторным действием." (`:145`).

### 5.6 `POST /api/operations/stop-background.cgi`

Body exactly `{"pauseAutomation":true}` else **400** `INVALID_REQUEST` "Требуется пауза автоматики." (`common.sh:84-86`). `ops_stop_background` (`lib/operation-coordinator.sh:1059-1076`): writes automation `paused:true`, then for every running background op (≤128 files, else 503 `HISTORY_LIMIT`) calls `ops_cancel`. **202**:
`{"ok":true,"automationPaused":true,"operations":[{"operationId":"op-…","cancelRequested":true,"protected":false,"ok":true}]}`.
JS sends `{pauseAutomation:true}` from `#bg-stop-all`; message "Новые автоматические задачи поставлены на паузу. Для доступных операций запрошена остановка; операции с маршрутами и защищённые этапы продолжаются." (`js/background-operations.js:139,171`).

### 5.7 `POST /api/operations/automation.cgi`

Body exactly `{"paused":<boolean>}` else **400** `INVALID_REQUEST` "Некорректное состояние автоматики." (`common.sh:87-90`). Writes `background-automation.json {schemaVersion:1,paused,updatedAt}`; **200** `{"ok":true}` (`lib/operation-coordinator.sh:1338-1342`). JS only sends `{paused:false}` (`#bg-resume`), message "Настройка автоматики сохранена." (`js/background-operations.js:137,172`).

### 5.8 `POST /api/operations/recover.cgi`

Body exactly `{}` else **400** `INVALID_REQUEST` "Параметры восстановления не поддерживаются." (`common.sh:91-93`). `ops_emergency_recover` (`lib/operation-coordinator.sh:1078-1122`): runs stop-background (pause + cancels), then `ops_recover_global` (retire stale/terminal global fence after owner/children/publication/domain checks), `ops_recover_orphans`; then blocks if updater `request.lock` exists (`updater_pending`), legacy `/tmp/broray-global-operation.lock` exists (`legacy_domain_pending`), or `/opt/var/lib/broray/last-operation` points to an unsafe/running state (`updater_pending`). Response:
```json
{"ok":true,"result":"recovered","automationPaused":true,"operations":[],"retryable":false,"errorCode":null}
```
`result` values: `absent, recovered, terminal_lock_retired, service_stop_recovered, unissued_stop_aborted_after_boot` (ok) or `unsafe_lock, owner_changed, legacy_owner_ambiguous, invalid_owner, protected_recovery, updater_start_unconfirmed, publication_unconfirmed, children_unconfirmed, platform_domain_pending, domain_pending, ACTIVE, AMBIGUOUS, orphan_unconfirmed, updater_pending, legacy_domain_pending` (blocked). Blocked (rc 2) → `ok:false, errorCode:"RECOVERY_BLOCKED"` → **409**; other failure → `STATE_UNAVAILABLE` → 503; success **202**. `retryable:true` only for `ACTIVE|children_unconfirmed` when the current op is cooperative and not route-protected.
JS (`mutate`, `js/background-operations.js:125-147`): retries up to 5 times with 1 s delay while `errorCode==="RECOVERY_BLOCKED" && retryable===true` (feedback "Автоматика на паузе. Ожидаем завершения фоновых задач; блокировка пока сохранена…"). Success "Проверка завершена. Подтверждённые остаточные блокировки обработаны. Автоматика остаётся на паузе.". Failure text uses `error.data.result` map (`:143`: ACTIVE, children_unconfirmed, protected_recovery, legacy_owner_ambiguous, updater_pending, legacy_domain_pending, domain_pending, AMBIGUOUS, publication_unconfirmed, orphan_unconfirmed) → "[Автоматика на паузе. ]Блокировка сохранена. <detail|Восстановление не подтверждено.> Скачайте диагностический отчёт."; if no data or (no detail and errorCode ≠ RECOVERY_BLOCKED) → "Ответ на запрос восстановления не получен. Обновите состояние, чтобы проверить результат.". After every mutation: `refresh()` + `journal()`.

### 5.9 Other verbs in the shared pipeline (owned by servers/subscriptions areas)

`servers-check` / `subscription-refresh` bodies must be exactly `{"id","nonce"}` with nonce 32 lowercase hex, id 1..96 `[A-Za-z0-9_.-]` not starting with `.`/`-` (`common.sh:39-45`); 404 `SERVER_NOT_FOUND`/`SUBSCRIPTION_NOT_FOUND`; `queue-submit <action> <id> USER <context sha256> <nonce>`; success **202** with `accepted:true`. Errors from submit: `INVALID_QUEUE_ACTION`, `FAILOVER_UNCONFIRMED`, `REQUEST_MISMATCH`, `REQUEST_UNCONFIRMED`(404), `QUEUE_FULL`, `QUEUE_STATE_INVALID`.

---

## 6. Shared shell / client-only modules (no backend dependency)

- `app-shell.js`: sidebar collapse persisted in `localStorage["broray.sidebar.collapsed"]` (`:10`), mobile breakpoint 760 px, injects routes submenu (`/routes.html`, `/routes-custom.html`, `/routes-import.html`), active link by `body[data-page]`, support modal, logout (`:261-270`).
- `theme.js`/`theme-init.js`: `localStorage["broray.theme"]` ∈ `broray|night|day` (`js/theme.js:4-27`), mounts into `#theme-control-mount`.
- `dialogs.js`: `window.BROrayDialogs.{confirm,confirmPhrase,promptValue,inform,setLocked}` using `#confirm-root` markup present in each page; phrase check is exact string equality (`:117-124`).
- `common.js` toast: max 2 visible, 6 s, messages > 240 chars truncated, warnings/errors kept in a "Сообщения этой страницы" history panel (max 10) (`:185-327`); health helpers `normalizeHealth`, `severityMeta`, `firstHealthReason`, `healthIsConfirmedOk` (`:333-404`) define the canonical enums: severity `ok|warning|error|busy|unknown`, availability `available|partial|unavailable`, freshness `fresh|stale|expired|unknown`.
- `action-feedback.js` (dns/routes pages): maps `error.code` `ROUTES_OPERATION_BUSY|OPERATION_BUSY|DOMAIN_OPERATION_BUSY` → "другая конфликтующая операция", `ROUTES_PREFLIGHT_PLAN_FAILED|ROUTES_API_LOCK_FAILED` → preflight message; shows `error.details` (`js/action-feedback.js:10-34`).
- `operation-log.js` (xray page only): wraps `#operation-log` in a `<details>`; no requests.
- Note: `home.html` does NOT load `background-operations.js`; the operations panel exists only on `broray.html` (`web/broray.html:302-327, 445`).

---

## 7. Algorithms / constants summary (with sources)

| Item | Value | Source |
|---|---|---|
| Session TTL (sliding) | 1800 s | `lib/web-auth.sh:8,213` |
| Session token | 64 hex | `lib/web-auth.sh:43-48,83-95` |
| Login body limit / field limits | 8192 B / 128 / 512 | `api/login.cgi:21,62-63` |
| Invalid-credentials delay | 1 s | `api/login.cgi:111` |
| Keenetic auth hash | `sha256(challenge + md5(login:realm:password))` | `lib/web-auth.sh:356-366` |
| Home poll / timeout / max backoff | 30 s / 60 s / 120 s | `js/home.js:9-12` |
| Snapshot fresh / expire | 90 s / 600 s | `lib/home-snapshot.sh:8-19` |
| Snapshot daemon interval | 30 s (15..300) | `bin/broray-home-snapshotd:8,18` |
| Severity rank | error50 busy40 warning30 unknown20 ok10 | `api/home/summary.cgi:103-109` |
| Update check freshness | 3600 s | `lib/broray-page.sh:1583-1587`, `upd.sh:878` |
| Release relation | `X.Y.Z-rN` numeric compare | `upd.sh:130-174` |
| BROray page poll | 500 ms first, 2000 ms, fail backoff min(5000,1000+500n), stop after 6 fails / 15 unconfirmed | `js/broray.js:489,538,563-564,580` |
| Cleanup plan TTL | 120 s | `lib/broray-cleanup.sh:12` |
| Cleanup keep | 3 newest per group, >1 day; logs >6 days | `lib/broray-cleanup.sh:212-276` |
| Cleanup/uninstall/updater body limit | 4096 B | `api/broray/cleanup-plan.cgi:14`, `uninstall.cgi:17` |
| Operations POST body limit | 4096 B | `api/operations/common.sh:27` |
| Ops panel refresh | 10 s; fetch timeout 60 s | `js/background-operations.js:37,162` |
| Queue watch poll | 5 s; POST timeout 30 s | `js/common.js:66,168` |
| History list | page 10 | `js/background-operations.js:3,166-169` |
| Journal UI | last 20 events; server ≤500 | `js/background-operations.js:121`, `lib/operation-journal.sh:162-165` |
| Report max | 1 MiB | `lib/operation-report.sh:58`, `js/background-operations.js:152` |
| Coordinator guard wait | ≤15 attempts (~30 s) | `lib/operation-client.sh:68-72,118-122` |
| Uninstall phrases | `УДАЛИТЬ BROray` / `УДАЛИТЬ BROray ПОЛНОСТЬЮ` | `lib/broray-page.sh:2585-2596` |
| Updater operation id | `<update|reinstall>-YYYYmmddHHMMSS-<pid>` | `upd.sh:1090` |
| Uninstall operation id | `uninstall-YYYYmmddHHMMSS-<pid>` | `lib/broray-page.sh:2407` |

---

## 8. Dynamically built endpoints

- `"/api/operations/" + endpoint + ".cgi"` with endpoint ∈ `status, events, report, cancel, stop-background, automation, recover` (`js/background-operations.js:41, 111, 117, 151, 171-173, 73, 94`).
- `common.js` `submitQueued(url,id)`: url restricted to `/api/servers/check.cgi` | `/api/subscriptions/refresh.cgi` (`js/common.js:87`); sessionStorage key `"broray.queue.v1:"+url+":"+id`.
- `startAction(path, …)` in broray.js receives literal paths (`/api/broray/update-start.cgi`, `/api/broray/reinstall.cgi`, `/api/broray/restore.cgi`, `/api/broray/uninstall.cgi`); `operationKind` derived via `path.indexOf("uninstall")` (`js/broray.js:614`).
- Login redirect target taken from response `redirect` (`js/login.js:172-174`).
- Home card ids built as `prefix + "-status"`/`"-main"` (`js/home.js:126-129`).

## 9. Dependencies of the JS on server-produced wording / HTML

- No backend-produced HTML. All markup ids are static in the HTML files (home: `home-*` ids `web/home.html:115-357`; BROray: `web/broray.html:100-387`; ops panel `bg-*` `web/broray.html:302-327`).
- Server strings shown verbatim to the user: every `error.message` (and `details` when message empty), `+ " [updater rc=N]"`; updater/uninstall `message`, `error`, `logTail`; home `health.reasons[].message` (first 3), module `firstReason`, subscription `lastWarnings[0]`, DNS reason text patched by summary.cgi; component `name/version/path`; `capabilities[]`; plan `candidates[].path`; login `message`.
- Server enums the JS branches on: `state` (idle/queued/running/restoring/success/error/unconfirmed), `operation` (update/reinstall/restore/uninstall), `candidateRelation==="older"`, `universalUpdaterReady`, `platformHandoff.running`, `updateChannel` values, protocol ids, `health.severity`, `_snapshot.freshness`, `lastUpdateStatus`, `connectionState==="connected"`, `state==="up"`, coordinator `type/source/phase/state/reason/ownerStatus/cancelability/event/result` values, `errorCode` `REQUEST_UNCONFIRMED`/`RECOVERY_BLOCKED`, `result` recovery codes, `reportKind`.
- `#donate-link` is a `<details>`; `applyLink` sets a meaningless `href` (harmless).
- `#refresh-status` exists in `broray.html` header (`:100`) but broray.js never binds it (stays hidden).

---

## 10. 3.x updater-platform touch points (for mapping to the 3.5.0 installer)

| UI function | 3.x implementation | Updater-platform dependency | Suggested 3.5.0 mapping |
|---|---|---|---|
| Info card "Updater"/installation badge (`universalUpdaterReady`, `platformHandoff`, `reinstallSupported`, `components[updater]`, `components[opkg]`, `opkgRegistrationHealthy`, `versionsConsistent`) | `info.cgi` + `handoff.sh status` + `compat.sh` + `broray-updaterctl version` | Entirely tied to `/opt/libexec/broray-updater`, `S22broray-updater`, slot metadata `/opt/var/lib/broray-updater/slots/*.json`, OPKG control for `broray` | Installer status: report ready=true, handoff `UNIVERSAL_PLATFORM_READY` so UI shows "Универсальный"/"Установлено"; components list from the new layout |
| Check update | `update-check.cgi` → `updaterctl check` (minisign-signed release index, cache `release-index.json`, SOCKS fallback) | yes | installer `check` (keep response fields `updateAvailable, availableVersion(=releaseId), availablePackageVersion, candidateId, candidateRelation, checkedAt`) |
| Install update / "Завершить обновление" | `update-start.cgi` → handoff `schedule` or `updaterctl request update` (queue + daemon, app-slot switch, rollback) | yes | installer `update` (async, operationId + status file with same state/stage/progress/message/running/logTail contract) |
| Reinstall current | `reinstall.cgi` → `updaterctl request reinstall` (re-download current slot bundle) | yes | installer `reinstall` |
| Operation status | `update-status.cgi` → `updaterctl status` + handoff overlay; shared pointer `/opt/var/lib/broray/last-operation` | yes | installer `status` for last op (must also expose uninstall progress) |
| Restore from backup | `restore.cgi` → always 409 `RESTORE_UNAVAILABLE`; button disabled | legacy restore engine removed | installer `restore` (would require enabling the button = JS change; otherwise keep 409) |
| Backup | Only implicit: normal uninstall writes `/opt/broray-preserved/broray-user-data-*.tar.gz` + `LATEST` (format `BROray protected backup/2`); info `lastBackup/backupValid` from `/opt/var/lib/broray/last-backup` | uninstall worker stops/removes updater | installer `backup` (same archive format/location to keep `backupValid` semantics) |
| Uninstall normal/full | `uninstall.cgi` → `broray-system uninstall-start` (ops-guard scope, stops S22 updater, worker, `opkg remove broray`, removes updater platform dirs) | yes (stops/removes updater; OPKG) | installer `uninstall --mode normal|full` with the same phrases and stage table |
| Cleanup | `cleanup-plan.cgi`/`cleanup.cgi` (`broray-cleanup.sh`) — refuses while `/opt/var/lib/broray-updater/request.lock` exists; categories include old updater/installer backups `backup/*-before-*` | indirect (lock) | keep in daemon; replace lock check with installer busy check |
| Home BROray card | `broray-system info` snapshot (no handoff enrichment) | indirect | from installer status |
| Operations recover / report | `recover` blocks with `updater_pending` on updater `request.lock` or running `last-operation`; report `runtimeDetails.updater` from home broray snapshot `lastOperation` (ids `update-*|reinstall-*` only) | indirect | map to installer busy/last-op |
| Background op type `updater_maintenance` (`system:platform-preflight`), source `UPDATER` | coordinator platform preflight (`lib/operation-platform-*.sh`) | yes | likely obsolete; JS has no label for `updater_maintenance` (shows "Фоновая операция") |

---

## 11. Riskiest behaviours (summary)

1. Three incompatible envelopes coexist (flat auth, `success/data/error`, raw `ok/error{}`, coordinator `ok/errorCode`) and several endpoints return different shapes for success vs. pre-check failure (e.g. info/update/uninstall raw objects but B-envelope 401/403/405). `uninstall.cgi` returns HTTP 200 for backend errors and may return an empty body.
2. `update-status.cgi` handoff overlay rewrites ANY last operation to a failed "update" when `handoff.sh status` is not READY (including merely a stopped updater daemon); `info.cgi` likewise forces `updateAvailable:true`, `installationHealthy:false`, `reinstallSupported:false`. The Go daemon must decide whether to emulate this (behaviour-identical) or always report READY.
3. Operations POST origin validation (Origin/X-BROray-Origin equality, same-host http/https, KeenDNS `https://broray.<name>.<domain>` proof via ndmc) plus strict body schemas (exact key sets), 202/200/404/409/503 mapping, and status returning 503 with a full usable snapshot body.
4. Uninstall: ops-guard scope, updater stop proof, multi-stage worker with rollback, OPKG removal, preserved backup + `LATEST`, and terminal success never observable (status dir deleted) — the UI's 6-failure timeout is the expected end state.
5. Home summary freshness/severity escalation (90/600 s, rank max, reason ordering, DNS reason rewrite) and the BROray card's package-version-based "update available" rule, fed by a 30 s background snapshot daemon; sliding 1800 s sessions extended by every poll.
