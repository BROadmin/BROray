(function (root) {
    "use strict";
    var POLICY = "/api/routes/policy.cgi", SCHEDULE = "/api/routes/geodata-schedule.cgi";
    var RECEIPT = "broray.policy.queue.v1";
    var hash = /^[0-9a-f]{64}$/, noncePattern = /^[0-9a-f]{32}$/;
    function copy(value) { return JSON.parse(JSON.stringify(value)); }
    function envelope(reply) {
        if (!reply || reply.success !== true || !reply.data || typeof reply.data !== "object") {
            throw new Error("Ответ BROray не подтверждён. Обновите данные.");
        }
        return reply.data;
    }
    function validPolicy(p) {
        return p && hash.test(p.revision) && typeof p.busy === "boolean" && typeof p.pending === "boolean" &&
            p.state && p.state.schemaVersion === 1 && Array.isArray(p.state.rules) && p.state.rules.every(function (r) {
                return r && typeof r.id === "string" && typeof r.name === "string" && typeof r.enabled === "boolean" &&
                    ["vpn", "direct", "deny"].includes(r.action);
            });
    }
    function validSchedule(s) { return s && typeof s.enabled === "boolean" && hash.test(s.revision) && s.attempts && typeof s.attempts === "object"; }
    function createClient(options) {
        var api = options.api, storage = options.storage || root.sessionStorage;
        var wait = options.wait || function (ms) { return new Promise(function (resolve) { setTimeout(resolve, ms); }); };
        var seq = 0, scheduleSeq = 0;
        var view = {policy: null, schedule: null, busy: false, scheduleBusy: false, uncertain: true, scheduleUncertain: true, message: ""};
        function snapshot() { return copy(view); }
        function emit() { if (options.onChange) options.onChange(snapshot()); }
        function headers(nonce) {
            var h = {"X-BROray-Request": "operations", "X-BROray-Origin": root.location ? root.location.origin : ""};
            if (nonce) h["X-BROray-Nonce"] = nonce;
            return h;
        }
        async function request(url, body, nonce) {
            var controller = new AbortController(), timer = setTimeout(function () { controller.abort(); }, 30000);
            try { return await api(url, {method: body ? "POST" : "GET", headers: headers(nonce), body: body, signal: controller.signal}); }
            finally { clearTimeout(timer); }
        }
        function record() {
            var raw = storage.getItem(RECEIPT);
            if (!raw) return null;
            var saved = JSON.parse(raw);
            if (!saved || !noncePattern.test(saved.nonce)) throw new Error("Сохранённый запрос повреждён. Проверьте операции.");
            return saved;
        }
        function newNonce() {
            if (options.nonce) return options.nonce();
            var b = new Uint8Array(16); root.crypto.getRandomValues(b);
            return Array.from(b, function (n) { return n.toString(16).padStart(2, "0"); }).join("");
        }
        function receipt(r) {
            if (!r || r.ok !== true || !/^q-[0-9a-f]{32}$/.test(r.requestId) ||
                !["queued", "running", "completed", "failed", "cancelled"].includes(r.state)) {
                throw new Error("Результат задания не подтверждён.");
            }
            return r;
        }
        async function observe(saved) { return receipt(await request("/api/operations/status.cgi", {nonce: saved.nonce})); }
        async function waitFor(saved, first) {
            var r = first || await observe(saved), deadline = Date.now() + 3620000;
            while (r.state === "queued" || r.state === "running") {
                if (Date.now() >= deadline) throw new Error("Операция ещё не подтверждена. Обновите данные позднее.");
                view.message = r.state === "queued" ? "Обновление в очереди…" : "Проверка и обновление геоданных…"; emit();
                await wait(1500); r = await observe(saved);
            }
            storage.removeItem(RECEIPT);
            if (r.state !== "completed") {
                var failure = new Error(r.message || "Обновление не завершено. Рабочие данные сохранены.");
                failure.confirmed = true; failure.code = r.errorCode; throw failure;
            }
            return r;
        }
        async function loadPolicy(token) {
            var p = envelope(await request(POLICY));
            if (!validPolicy(p)) throw new Error("Состояние правил не подтверждено.");
            if (token === seq) view.policy = p;
            return p;
        }
        async function refresh() {
            if (view.busy) return snapshot();
            var token = ++seq, saved, missing = false;
            try {
                saved = record();
                if (saved) {
                    try {
                        var r = await observe(saved);
                        if (r.state === "queued" || r.state === "running") {
                            view.busy = true; view.uncertain = true; emit();
                            try { await waitFor(saved, r); }
                            finally { view.busy = false; }
                        } else {
                            storage.removeItem(RECEIPT);
                            if (r.state !== "completed") view.message = r.message || "Предыдущее обновление не выполнено.";
                        }
                    } catch (error) {
                        if (error.status === 404) missing = true;
                        else if (error.confirmed) view.message = error.message;
                        else throw error;
                    }
                }
                var p = await loadPolicy(token);
                if (token === seq) {
                    if (missing && (p.busy || p.pending)) throw new Error("Предыдущая операция ещё не подтверждена.");
                    if (missing) { storage.removeItem(RECEIPT); view.message = "Состояние прочитано заново. Проверьте выбранные правила."; }
                    view.uncertain = false;
                }
                emit(); return snapshot();
            } catch (error) {
                if (token === seq) { view.uncertain = true; view.message = error.message; emit(); }
                throw error;
            }
        }
        async function change(fields) {
            if (view.busy || view.uncertain || !view.policy || view.policy.busy || view.policy.pending) {
                throw new Error("Сначала дождитесь завершения операции и обновите состояние правил.");
            }
            if (record()) throw new Error("Сначала проверьте результат предыдущего задания.");
            var token = ++seq, saved = null, result, failure = null, unknown = false;
            view.busy = true; view.message = "Сохранение…"; emit();
            try {
                var body = Object.assign({}, fields, {expectedRevision: view.policy.revision});
                if (fields.operation === "geo-upsert" || fields.operation === "geo-update") {
                    saved = {nonce: newNonce()};
                    if (!noncePattern.test(saved.nonce)) throw new Error("Не удалось создать номер задания.");
                    storage.setItem(RECEIPT, JSON.stringify(saved));
                    var accepted;
                    try { accepted = receipt(envelope(await request(POLICY, body, saved.nonce))); }
                    catch (error) {
                        if ([400, 401, 403, 404, 409, 413, 415, 422].includes(error.status)) { storage.removeItem(RECEIPT); error.confirmed = true; throw error; }
                        accepted = await observe(saved);
                    }
                    result = await waitFor(saved, accepted);
                } else { result = envelope(await request(POLICY, body)); }
            } catch (error) { failure = error; unknown = Boolean(saved && !error.confirmed); }
            try { await loadPolicy(token); }
            catch (error) { unknown = true; if (!failure) failure = error; }
            view.busy = false; view.uncertain = unknown;
            view.message = failure ? failure.message : "Сохранено"; emit();
            if (failure) throw failure;
            return result;
        }
        async function refreshSchedule() {
            if (view.scheduleBusy) return snapshot();
            var token = ++scheduleSeq;
            try {
                var s = envelope(await request(SCHEDULE));
                if (!validSchedule(s)) throw new Error("Настройка ежедневного обновления не подтверждена.");
                if (token === scheduleSeq) { view.schedule = s; view.scheduleUncertain = false; emit(); }
                return snapshot();
            } catch (error) { if (token === scheduleSeq) { view.scheduleUncertain = true; emit(); } throw error; }
        }
        async function setSchedule(enabled) {
            if (view.scheduleBusy || view.scheduleUncertain || !view.schedule || typeof enabled !== "boolean") {
                throw new Error("Сначала обновите настройку ежедневного обновления.");
            }
            ++scheduleSeq; view.scheduleBusy = true; emit();
            var failure;
            try { envelope(await request(SCHEDULE, {enabled: enabled, expectedRevision: view.schedule.revision})); }
            catch (error) { failure = error; }
            view.scheduleBusy = false;
            try { await refreshSchedule(); } catch (error) { failure = failure || error; }
            emit(); if (failure) throw failure;
            return snapshot();
        }
        return {snapshot: snapshot, refresh: refresh, change: change, refreshSchedule: refreshSchedule, setSchedule: setSchedule};
    }
    root.BROrayPolicy = {createClient: createClient};
})(typeof window === "undefined" ? globalThis : window);
