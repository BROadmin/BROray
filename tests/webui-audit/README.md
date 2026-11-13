# Isolated WebUI audit

Run `node tests/webui-audit/server.cjs <sanitized-fixtures.json>` from the repository.
The server binds to127.0.0.1:28458, serves the current production HTML/CSS/JS,
and terminates every API request locally. It has no upstream transport.
Fixture values must contain only synthetic addresses and credentials.

`GET /__audit/state` identifies the fixture and records API calls.
`POST /__audit/response` accepts `{path,body,status}` for controlled scenarios.
`GET /__audit/host` provides a390px iframe for a real narrow rendering viewport.
Verify the fixture ID before `POST /__audit/shutdown`.

These responses validate UI handling, not backend effects. In particular,
successful fake update/uninstall/import endpoints prove neither actual router
changes nor persisted data. Keep cancelled, disabled, unimplemented501 and
completed scenarios separate. Authentication uses synthetic credentials only;
login request bodies are omitted from the call log.

2026-11-13 evidence and matrix:
`G:/GPT-Codex/BROray-webui-dns-audit-20261113/CODEX-REPORT-DNS-WEBUI.md`.
Route operation geometry uses the existing `tests/webui-layout` fixture.
