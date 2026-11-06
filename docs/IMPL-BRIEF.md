# Implementation brief: porting BROray 3.x WebUI areas to the Go daemon (3.5.0)

## Goal
BROray 3.5.0 is one Go binary (`broray daemon`) that serves the **unchanged 3.x WebUI**
(`webui/static/`, copied verbatim from 3.x `runtime/app/web-new/`). Every `/api/<area>/<name>.cgi`
the 3.x JavaScript calls must be served by Go with the **exact 3.x contract**: URL, method, query,
body schema, validation order, HTTP status, error codes, Russian messages (byte-exact),
success JSON shape and field semantics, side effects, background behaviour.
Function parity is the requirement: everything 3.x can do in your area, 3.5.0 must do.

## Sources of truth (read-only)
* Contract for your area: `/home/claude/broray-go/docs/contracts/<file>.md` (written from the 3.x code with file:line citations).
* 3.x code: `/home/claude/broadmin/broray` (commit f505928). `runtime/app/web-new/{api,assets/js,*.html}`,
  `runtime/app/lib/*.sh`, `runtime/app/bin/*`. When the contract is unclear or silent, read the 3.x code.
  **Never modify this repo.**
* The JS that will call you: `/home/claude/broray-go/webui/static/assets/js/*.js` (identical to 3.x).

## Where you write
* **Only** inside your own package directory `areas/<name>/` (Go files and `_test.go` files).
  Exceptions are stated in your task. Do not edit `internal/*`, `modules/*`, `cmd/*`, `webui/*`,
  `tools/*`, `go.mod`, other areas. Other agents are working in parallel on other areas.
  If you need something changed outside your directory, implement it locally in your package
  and mention it in your final report.
* Do not run git commands that change state (no commit, checkout, stash, reset). The lead commits.
* Each area package already has `areas/<name>/area.go` with
  `func init() { compat.Area("<name>", 50, setup) }` — keep that and fill `setup`.

## Framework (already in the repo — read these files first)
* `internal/cgi/cgi.go` — `cgi.Register(path, handler, opts...)` for envelope-B endpoints
  (`{"success":true,"data":…,"error":null}` / `{"success":false,"data":null,"error":{code,message,details}}`),
  `cgi.RegisterRaw` for endpoints with other shapes (raw objects, HTTP 202 queue receipts, downloads).
  Options: `cgi.POST()`, `cgi.Methods(...)`, `cgi.Limit(n)`, `cgi.TooLarge(err)`, `cgi.Status(202)`, `cgi.Anonymous()`.
  The framework checks method (405 with the 3.x text) → session (401 AUTH_REQUIRED) → body size.
  Everything else (body JSON schema, query parsing, CSRF headers with area-specific codes, Origin)
  is your handler's job, exactly as your contract specifies. Return `*cgi.APIError` (`cgi.E`, `cgi.Ed`)
  for contract errors. `c.RawQuery` is the raw QUERY_STRING (several 3.x CGIs require exact raw forms).
* `internal/compat/` — `Deps` (Root, App, Lock, Queue, Log, Ctx, Svc), path mapping, time formats
  (`compat.Stamp` = 3.x `date +%Y-%m-%dT%H:%M:%S%z`, `compat.StampUTC` = `…Z`), `Canonical` (jq -cS
  equivalent; Go is both producer and consumer of these hashes so only determinism matters),
  `WriteJSON`/`ReadJSON`/`ReadObject`, `GlobalLock` (3.x global operation fence; `JSON()` gives the
  3.x `globalOperation` object), `Coordinator` (queue + operations registry + journal: `Define`,
  `Submit`, `Enqueue`, `Lookup`, `StartOperation`, …), `RegisterSnapshot` (home page module
  snapshots), `Services` (the few cross-area function calls).
* `internal/keenetic` — `Transport` (RCI http://127.0.0.1:79 with ndmc fallback): `Parse(cmds)`,
  `RunningConfig`, `StartupConfig`, `Show(path)`; helpers `ParseBlocks`, `FindBlock`, `Save` (system
  configuration save + running==startup confirmation), `Apply(area, cmds)` (allowlist check + parse).
  You may call `d.App.T.Parse` directly after validating commands against your own exact allowlist
  (as 3.x did per area). `internal/keenetic/allow.go` has the existing shapes.
* `modules/*` — dev1 code (share-link parser, Xray config builder, Xray installer with pinned
  digests, route catalog downloader with bogon filter, DoT probe). Reuse by import if it fits the
  3.x contract; otherwise copy into your package and adapt. Do not edit `modules/*` (except where
  your task says so).
* `internal/app.App` (`d.App`) — `Cfg`, `T`, `K`, `Xray` (*xray.Process: Start/Stop/Restart/Status),
  `HTTP`, `Log`. Its old methods and the old `/api/v1/*` API are dev1 leftovers; do not build on them
  and do not edit app.go.

## Data layout
3.x path `/opt/broray/<X>` → `d.Path("<X>")` (= `<root>/data/<X>`), with these exceptions:
* `/opt/broray/config/config.json` (Xray config) → `d.Root.XrayConfig()`
* `/opt/broray/runtime/xray` (Xray binary) → `d.Root.XrayBinary()`
* `/opt/broray/run/<X>` → `d.RunPath("<X>")`; `/opt/broray/logs/<X>` → `filepath.Join(d.Root.Logs(), X)`;
  `/opt/broray/tmp` → `d.Root.Tmp()`; 3.x RAM paths (`/tmp/broray-*`, `/opt/var/run/broray`) → `d.RunPath(...)`.
* `/opt/var/lock/broray/global-operation.lock` → `d.Lock` (in-process fence).
Keep the 3.x JSON file formats so areas can read each other's files the way 3.x did
(e.g. `config/interface.json`, `run/keenetic-status.json`, `servers/<id>.json`, `run/server-quality/<id>.json`).
Create directories you need with `os.MkdirAll(…, 0o700)`. Atomic writes only (`compat.WriteJSON`,
`platform.WriteFileAtomic`).

## Processes / scripts
There is no shell layer in 3.5.0: no lighttpd, no CGI scripts, no `jq`, no `broray-*` helpers,
no init scripts per service, no cron. What 3.x did with daemons/schedulers you implement as
goroutines started in `setup` on `d.Ctx`; with flock-guarded helpers → mutexes; with
`broray_job_*`/route jobs → goroutines + `d.Queue.StartOperation` (so they show in the operations
panel). External tools available on the router: Xray binary only (and BusyBox). Use Go for HTTP
(d.App.HTTP or your own client with timeouts), DNS, TLS, hashing, zip.
Where 3.x shows a behaviour the contract marks as a bug (e.g. an endpoint that always fails), implement
the evidently intended working behaviour and list it in your report as a deliberate deviation.

## Product decisions already taken (do not change)
* Keenetic: write through RCI in batches, ndmc fallback (Transport does this). Never touch objects
  BROray does not own (owner receipts). No reboot in any scenario.
* Xray: one copy at `runtime/xray`, version pinned per architecture in `modules/xray` (no backup copy).
* Built-in route catalogs: ONE alphabetical list, no "recommended/featured" subset.
* Remote support: postponed (not part of 3.5.0).
* Architectures: aarch64, armv7, mipsel, mips. Keep memory use modest (routers with 256–512 MB).

## Tooling
```
cd /home/claude/broray-go && source .goenv     # Go 1.24.7, offline (GOPROXY=off); stdlib + golang.org/x/{crypto,sys} only
go build ./... && go vet ./areas/<name>/... && go test ./areas/<name>/...
```
If `go build ./...` fails only because of another area that is mid-work, build/test your own packages.
No network access to a router. Test against a fake `keenetic.Transport` implemented in your
`_test.go` files; test HTTP handlers with `httptest` through `cgi.Serve` (pass `nil` authenticator).
Write table tests for every validation branch and every error code of your contract.

## Final report (keep it compact)
1. Endpoint list with status (done / partial / not done).
2. Background loops and queue actions implemented.
3. Deliberate deviations from 3.x and why.
4. Anything you need from outside your package (exact function/signature/file).
5. Test summary (`go test` output line).
