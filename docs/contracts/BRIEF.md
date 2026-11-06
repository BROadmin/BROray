# Brief for contract extraction (BROray 3.x → 3.5.0 Go backend)

Repository (read-only): /home/claude/broadmin/broray at commit f505928 ("Republish BROray 3.2.0-r12").
- WebUI: runtime/app/web-new/*.html, runtime/app/web-new/assets/js/*.js
- CGI API: runtime/app/web-new/api/**/*.cgi (+ *-common.sh helpers)
- Shell libraries: runtime/app/lib/*.sh, binaries: runtime/app/bin/*
- State seed / defaults: runtime/state-seed/, runtime/app/share/

Goal: BROray 3.5.0 replaces the shell/CGI backend with one Go daemon, but the
3.x pages and their JavaScript must run UNCHANGED (pixel- and behaviour-identical
UI). So the Go daemon must serve every `/api/.../*.cgi` URL the JS uses with the
exact same request/response contract and the same user-visible behaviour.

Write ONE markdown file per area at /home/claude/contracts/<area>.md containing:

1. Endpoint table, then one section per endpoint:
   - URL, HTTP method(s); query params / JSON body fields (names, types,
     required/optional, limits and validation rules, error codes on violation)
   - the success response JSON exactly as produced (full field list with types;
     paste one realistic example), and which fields the JS actually reads
     (function names + file:line)
   - error responses: HTTP status, envelope shape, error code strings and
     message texts the JS checks or shows
   - side effects: files read/written (paths under /opt/broray...), Keenetic
     commands, processes started/stopped, background operations
   - async model: does it return an operation id / job? how does the JS poll,
     which status values/phases/progress fields does it expect, how is cancel
     handled
2. Algorithms and rules needed to reproduce behaviour (formulas, thresholds,
   defaults, enum values, sorting, limits, timers), each with file:line.
3. Endpoints the JS builds dynamically (string concatenation) — list them.
4. Anything in the JS that depends on HTML ids/classes produced by the backend
   or on response wording.

Be exact and cite file:line. Do not modify the repository. Do not guess:
mark anything unconfirmed as UNCONFIRMED. The common response envelope and
auth/CSRF rules live in runtime/app/web-new/api/auth-common.sh and
runtime/app/web-new/assets/js/common.js — read them first.

Return to the caller only a short summary (< 250 words): the file path,
number of endpoints documented, and the 5 riskiest/most complex behaviours.
