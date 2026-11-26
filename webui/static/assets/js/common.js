(function () {
    "use strict";

    // Authentication must release the form even if a cloud connection stalls
    // after headers. Do not apply this deadline to long route operations.
    async function apiRequest(url, options) {
        if (url !== "/api/login.cgi" && url !== "/api/session.cgi") {
            return requestJSON(url, options);
        }
        const controller = new AbortController();
        const upstream = options && options.signal;
        const cancel = function () { controller.abort(); };
        let expired = false;
        if (upstream) {
            if (upstream.aborted) controller.abort();
            else upstream.addEventListener("abort", cancel, {once: true});
        }
        const timer = setTimeout(function () {
            expired = true;
            controller.abort();
        }, 60000);
        try {
            return await requestJSON(url, Object.assign({}, options || {}, {signal: controller.signal}));
        } catch (error) {
            if (expired) {
                const timeout = new Error("Сервер не ответил вовремя. Проверьте доступ к роутеру и повторите попытку.");
                timeout.code = "REQUEST_TIMEOUT";
                throw timeout;
            }
            throw error;
        } finally {
            clearTimeout(timer);
            if (upstream) upstream.removeEventListener("abort", cancel);
        }
    }

    async function requestJSON(url, options) {
        const requestOptions = Object.assign(
            {
                credentials: "same-origin",
                cache: "no-store",
                headers: {}
            },
            options || {}
        );

        if (
            requestOptions.body &&
            typeof requestOptions.body !== "string"
        ) {
            requestOptions.headers["Content-Type"] =
                "application/json";
            requestOptions.body = JSON.stringify(
                requestOptions.body
            );
        }

        const response = await fetch(url, requestOptions);

        let payload = null;

        try {
            payload = await response.json();
        } catch (error) {
            payload = null;
        }

        if (!response.ok || !payload || typeof payload !== "object" || Array.isArray(payload)) {
            // Auth endpoints use {error: CODE, message}; operation endpoints
            // use {error: {code, message, details}}. Preserve both contracts.
            const errorPayload = payload && payload.error && typeof payload.error === "object"
                ? payload.error : payload;
            const requestError = new Error(
                errorPayload && typeof errorPayload.message === "string" && errorPayload.message
                    ? errorPayload.message
                    : (response.ok ? "Некорректный ответ сервера" : "Ошибка запроса") +
                        " (HTTP " + response.status + ")."
            );

            requestError.status = response.status;
            requestError.payload = payload;
            requestError.code = errorPayload && typeof errorPayload.code === "string"
                ? errorPayload.code : payload && typeof payload.error === "string"
                    ? payload.error : "HTTP_RESPONSE_INVALID";
            requestError.details = errorPayload && errorPayload.details
                ? errorPayload.details
                : null;

            throw requestError;
        }

        return payload;
    }

    // One request nonce survives a lost response and navigation in this tab.
    // Retrying observation never resubmits a mutation.
    var queueInFlight = new Map();
    async function queuePost(url, body, admission) {
        var controller=new AbortController(), timer=setTimeout(function () { controller.abort(); },30000);
        try {
            var result=await apiRequest(url,{method:"POST",signal:controller.signal,
                headers:{"X-BROray-Request":"operations","X-BROray-Origin":window.location.origin,
                    "X-BROray-Queue":admission ? "1" : "0"},body:body});
            if (!result || result.ok !== true || !/^q-[0-9a-f]{32}$/.test(result.requestId) ||
                !["queued","running","completed","failed","cancelled"].includes(result.state)) {
                throw new Error("Ответ на запрос задания не подтверждён.");
            }
            return result;
        } finally { clearTimeout(timer); }
    }
    function queueLabel(result) {
        if (result && result.state === "queued") {
            if (result.reason === "automation_paused") return "На паузе";
            if (result.reason === "active_connection") return "Отложено ради активного подключения";
        }
        return {queued:"В очереди",running:"Выполняется",completed:"Завершено",failed:"Ошибка",cancelled:"Отменено"}[result && result.state] ||
            "Результат запроса не подтверждён";
    }
    function submitQueued(url, id, selection) {
        var reserveSelection = url === "/api/servers/reserve-selection.cgi";
        var profileApply = url === "/api/subscriptions/apply-profile.cgi";
        if (!["/api/servers/check.cgi", "/api/servers/activate.cgi",
            "/api/subscriptions/refresh.cgi", "/api/subscriptions/delete.cgi", "/api/subscriptions/apply-profile.cgi",
            "/api/servers/reserve-selection.cgi"].includes(url) ||
            typeof id !== "string" || !/^[A-Za-z0-9_][A-Za-z0-9_.-]{0,95}$/.test(id) ||
            (reserveSelection && (!selection || !["promote", "ordinary"].includes(selection.mode) ||
                !/^[a-f0-9]{64}$/.test(selection.expectedRevision) || typeof selection.removeServerId !== "string" ||
                (selection.removeServerId !== "" && !/^[A-Za-z0-9_][A-Za-z0-9_.-]{0,95}$/.test(selection.removeServerId))))) {
            return Promise.reject(new Error("Некорректное задание."));
        }
        var key="broray.queue.v1:"+url+":"+id;
        if (queueInFlight.has(key)) return queueInFlight.get(key);
        var promise=Promise.resolve().then(async function () {
            var saved=sessionStorage.getItem(key),nonce;
            if (profileApply && !saved && (!selection || !/^[0-9a-f]{64}$/.test(selection.revision))) {
                throw new Error("Текущая ревизия ожидающих параметров не подтверждена.");
            }
            if (saved) {
                nonce=saved;
                if (!/^[0-9a-f]{32}$/.test(nonce)) throw new Error("Сохранённый запрос повреждён. Обновите состояние операций.");
            } else {
                var bytes=new Uint8Array(16);crypto.getRandomValues(bytes);
                nonce=Array.from(bytes,function (n) { return n.toString(16).padStart(2,"0"); }).join("");
                // Persist before sending. Without this receipt do not admit work.
                sessionStorage.setItem(key,nonce);
            }
            async function lookup() {
                try { return await queuePost("/api/operations/status.cgi",{nonce:nonce},false); }
                catch (error) {
                    if (error.status === 404 && error.payload && error.payload.errorCode === "REQUEST_UNCONFIRMED") {
                        sessionStorage.removeItem(key);
                        throw new Error("Задание не найдено в текущей очереди. Обновите данные перед повторным действием.");
                    }
                    throw error;
                }
            }
            var result;
            if (saved) result=await lookup();
            else {
                var body = {id:id, nonce:nonce};
                if (profileApply) body.revision = selection.revision;
                if (reserveSelection) {
                    body.mode = selection.mode;
                    body.expectedRevision = selection.expectedRevision;
                    body.removeServerId = selection.removeServerId;
                }
                try { result=await queuePost(url,body,true); }
                catch (error) {
                    if ([400,401,403,404,409,413,415,422].includes(error.status)) {
                        sessionStorage.removeItem(key);throw error;
                    }
                    result=await lookup();
                }
            }
            if (["completed","failed","cancelled"].includes(result.state)) sessionStorage.removeItem(key);
            return result;
        }).finally(function () { queueInFlight.delete(key); });
        queueInFlight.set(key,promise);
        return promise;
    }
    var queueWatches=new Map(), queueTimer=null;
    function hasQueuedRequest(url,id) {
        try { return sessionStorage.getItem("broray.queue.v1:"+url+":"+id) !== null; }
        catch (error) { return false; }
    }
    function queueNotify(key,watch,result) {
        watch.last=result;
        if (!["queued","running"].includes(result.state)) {
            queueWatches.delete(key);
            if (["completed","failed","cancelled"].includes(result.state)) {
                sessionStorage.removeItem(watch.storageKey || "broray.queue.v1:"+watch.url+":"+watch.id);
            }
        }
        watch.listeners.forEach(function (listener) { listener(result); });
    }
    function queueSchedule() {
        if (queueTimer || !queueWatches.size) return;
        queueTimer=setTimeout(async function () {
            queueTimer=null;
            if (document.hidden) { queueSchedule();return; }
            var controller=new AbortController(), timeout=setTimeout(function () { controller.abort(); },30000);
            try {
                var data=await apiRequest("/api/operations/status.cgi",{signal:controller.signal});
                if (!data || data.complete !== true || !Array.isArray(data.queue)) throw new Error("Состояние очереди недоступно.");
                queueWatches.forEach(function (watch,key) {
                    if (!watch.last) return;
                    var row=data.queue.find(function (item) { return item.requestId === watch.last.requestId; });
                    queueNotify(key,watch,row || {state:"unknown",message:"Задание не найдено в текущем снимке. Проверьте состояние повторно."});
                });
            } catch (error) {
                queueWatches.forEach(function (watch,key) {
                    // Admission owns its timeout/lost-response lookup. A
                    // status failure cannot settle a request without a reply.
                    if (!watch.last) return;
                    queueNotify(key,watch,{state:"unknown",message:"Не удалось обновить состояние задания. Проверьте состояние повторно."});
                });
            } finally { clearTimeout(timeout);queueSchedule(); }
        },5000);
    }
    function followQueued(url,id,listener,selection) {
        var key=url+":"+id,watch=queueWatches.get(key);
        if (watch) {
            watch.listeners.add(listener);
            if (watch.last) listener(watch.last);
            return watch.promise;
        }
        watch={url:url,id:id,listeners:new Set([listener]),last:null,promise:null};
        queueWatches.set(key,watch);
        watch.promise=submitQueued(url,id,selection).then(function (result) {
            queueNotify(key,watch,result);queueSchedule();return result;
        },function (error) { queueWatches.delete(key);throw error; });
        return watch.promise;
    }

    var reserveModeKey = "broray.reserve-mode.v1";
    var reserveModeURL = "/api/servers/reserve-mode.cgi";
    function readReserveModeRequest() {
        var raw = sessionStorage.getItem(reserveModeKey);
        if (raw === null) return null;
        var saved;
        try { saved = JSON.parse(raw); } catch (_) { /* Preserve unrecognised evidence. */ }
        if (!saved || saved.schemaVersion !== 1 || typeof saved.enabled !== "boolean" ||
            typeof saved.expectedRevision !== "string" || !/^[0-9a-f]{64}$/.test(saved.expectedRevision) ||
            typeof saved.nonce !== "string" || !/^[0-9a-f]{32}$/.test(saved.nonce) ||
            Object.keys(saved).sort().join(",") !== "enabled,expectedRevision,nonce,schemaVersion") {
            throw new Error("Сохранённый запрос резервирования повреждён. Проверьте состояние операций.");
        }
        return saved;
    }
    function followReserveMode(enabled, revision, listener) {
        var resume = enabled === null && revision === null, saved;
        if (!resume && (typeof enabled !== "boolean" || typeof revision !== "string" || !/^[0-9a-f]{64}$/.test(revision))) {
            return Promise.reject(new Error("Некорректный запрос резервирования."));
        }
        try {
            saved = readReserveModeRequest();
            if (saved && !resume && (saved.enabled !== enabled || saved.expectedRevision !== revision)) {
                throw new Error("Сначала проверьте результат предыдущего запроса резервирования.");
            }
            if (resume && !saved) throw new Error("Нет сохранённого запроса. Обновите состояние резервирования.");
        } catch (error) { return Promise.reject(error); }
        var key = reserveModeURL + ":reserve", watch = queueWatches.get(key);
        if (watch) {
            if (!resume && (watch.enabled !== enabled || watch.revision !== revision)) {
                return Promise.reject(new Error("Дождитесь завершения текущего запроса резервирования."));
            }
            watch.listeners.add(listener);
            if (watch.last) listener(watch.last);
            return watch.promise;
        }
        watch = {url:reserveModeURL,id:"reserve",storageKey:reserveModeKey,enabled:saved ? saved.enabled : enabled,
            revision:saved ? saved.expectedRevision : revision,listeners:new Set([listener]),last:null,promise:null};
        queueWatches.set(key, watch);
        watch.promise = Promise.resolve().then(async function () {
            var request = saved;
            if (!request) {
                var bytes = new Uint8Array(16); crypto.getRandomValues(bytes);
                request = {schemaVersion:1,enabled:enabled,expectedRevision:revision,
                    nonce:Array.from(bytes,function (n) { return n.toString(16).padStart(2,"0"); }).join("")};
                sessionStorage.setItem(reserveModeKey, JSON.stringify(request));
            }
            function bound(result) {
                if (result.requestId !== "q-" + request.nonce) throw new Error("Ответ не подтверждает сохранённый запрос резервирования.");
                return result;
            }
            async function lookup() {
                try { return bound(await queuePost("/api/operations/status.cgi", {nonce:request.nonce}, false)); }
                catch (error) {
                    if (error.status === 404 && error.payload && error.payload.errorCode === "REQUEST_UNCONFIRMED") {
                        sessionStorage.removeItem(reserveModeKey);
                    }
                    throw error;
                }
            }
            var result;
            if (saved) result = await lookup();
            else {
                try { result = bound(await queuePost(reserveModeURL, {enabled:request.enabled,expectedRevision:request.expectedRevision,nonce:request.nonce}, true)); }
                catch (error) {
                    if ([400,401,403,404,409,413,415,422].includes(error.status)) {
                        sessionStorage.removeItem(reserveModeKey); throw error;
                    }
                    result = await lookup();
                }
            }
            queueNotify(key, watch, result); queueSchedule(); return result;
        }).catch(function (error) { queueWatches.delete(key); throw error; });
        return watch.promise;
    }

    var dpiProfileKey = "broray.dpi-profile.v1";
    var dpiProfileURL = "/api/servers/dpi-profile.cgi";
    function readDPIProfileRequest() {
        var raw = sessionStorage.getItem(dpiProfileKey);
        if (raw === null) return null;
        var saved;
        try { saved = JSON.parse(raw); } catch (_) { /* Preserve unrecognised evidence. */ }
        if (!saved || saved.schemaVersion !== 1 || typeof saved.id !== "string" || !/^[A-Za-z0-9._-]{1,250}$/.test(saved.id) || typeof saved.enabled !== "boolean" ||
            typeof saved.expectedRevision !== "string" || !/^[0-9a-f]{64}$/.test(saved.expectedRevision) ||
            typeof saved.nonce !== "string" || !/^[0-9a-f]{32}$/.test(saved.nonce) ||
            Object.keys(saved).sort().join(",") !== "enabled,expectedRevision,id,nonce,schemaVersion") {
            throw new Error("Сохранённый запрос обхода DPI повреждён. Проверьте состояние операций.");
        }
        return saved;
    }
    function followDPIProfile(id, enabled, revision, listener) {
        var resume = id === null && enabled === null && revision === null, saved;
        if (!resume && (typeof id !== "string" || !/^[A-Za-z0-9._-]{1,250}$/.test(id) || typeof enabled !== "boolean" || typeof revision !== "string" || !/^[0-9a-f]{64}$/.test(revision))) {
            return Promise.reject(new Error("Некорректный запрос обхода DPI."));
        }
        try {
            saved = readDPIProfileRequest();
            if (saved && !resume && (saved.id !== id || saved.enabled !== enabled || saved.expectedRevision !== revision)) {
                throw new Error("Сначала проверьте результат предыдущего запроса обхода DPI.");
            }
            if (resume && !saved) throw new Error("Нет сохранённого запроса. Обновите состояние обхода DPI.");
        } catch (error) { return Promise.reject(error); }
        var key = dpiProfileURL + ":reserve", watch = queueWatches.get(key);
        if (watch) {
            if (!resume && (watch.serverID !== id || watch.enabled !== enabled || watch.revision !== revision)) {
                return Promise.reject(new Error("Дождитесь завершения текущего запроса обхода DPI."));
            }
            watch.listeners.add(listener);
            if (watch.last) listener(watch.last);
            return watch.promise;
        }
        watch = {url:dpiProfileURL,id:"reserve",storageKey:dpiProfileKey,serverID:saved ? saved.id : id,enabled:saved ? saved.enabled : enabled,
            revision:saved ? saved.expectedRevision : revision,listeners:new Set([listener]),last:null,promise:null};
        queueWatches.set(key, watch);
        watch.promise = Promise.resolve().then(async function () {
            var request = saved;
            if (!request) {
                var bytes = new Uint8Array(16); crypto.getRandomValues(bytes);
                request = {schemaVersion:1,id:id,enabled:enabled,expectedRevision:revision,
                    nonce:Array.from(bytes,function (n) { return n.toString(16).padStart(2,"0"); }).join("")};
                sessionStorage.setItem(dpiProfileKey, JSON.stringify(request));
            }
            function bound(result) {
                if (result.requestId !== "q-" + request.nonce) throw new Error("Ответ не подтверждает сохранённый запрос обхода DPI.");
                return result;
            }
            async function lookup() {
                try { return bound(await queuePost("/api/operations/status.cgi", {nonce:request.nonce}, false)); }
                catch (error) {
                    if (error.status === 404 && error.payload && error.payload.errorCode === "REQUEST_UNCONFIRMED") {
                        sessionStorage.removeItem(dpiProfileKey);
                    }
                    throw error;
                }
            }
            var result;
            if (saved) result = await lookup();
            else {
                try { result = bound(await queuePost(dpiProfileURL, {id:request.id,enabled:request.enabled,expectedRevision:request.expectedRevision,nonce:request.nonce}, true)); }
                catch (error) {
                    if ([400,401,403,404,409,413,415,422].includes(error.status)) {
                        sessionStorage.removeItem(dpiProfileKey); throw error;
                    }
                    result = await lookup();
                }
            }
            queueNotify(key, watch, result); queueSchedule(); return result;
        }).catch(function (error) { queueWatches.delete(key); throw error; });
        return watch.promise;
    }

    // UI-TOAST-01: the existing public toast(message, type) API stays intact.
    var toastRecords = [];
    var toastHistory = [];
    var toastEventsInstalled = false;
    var TOAST_VISIBLE_MS = 6000;
    var TOAST_MAX_VISIBLE = 2;
    var TOAST_HISTORY_LIMIT = 10;

    function toastKind(type) {
        if (!type) return "success";
        return ["success", "warning", "error", "info"].indexOf(type) >= 0 ? type : "info";
    }
    function toastLabel(kind) {
        return {success:"Успех", warning:"Предупреждение", error:"Ошибка", info:"Информация"}[kind];
    }
    function toastIcon(kind) {
        var svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
        svg.setAttribute("viewBox", "0 0 24 24");
        svg.setAttribute("class", "toast-icon");
        svg.setAttribute("aria-hidden", "true");
        svg.setAttribute("focusable", "false");
        var path = document.createElementNS("http://www.w3.org/2000/svg", "path");
        path.setAttribute("d", kind === "success" ? "M4 12l5 5L20 6" :
            kind === "info" ? "M12 10v9M12 5v.2" : "M12 4v10M12 19v.2");
        svg.appendChild(path);
        return svg;
    }
    function rememberToast(text, kind) {
        var main = document.querySelector("main.workspace-content") || document.querySelector("main");
        if (!main) return false;
        var panel = document.getElementById("page-notification-history");
        if (!panel) {
            panel = document.createElement("section");
            panel.id = "page-notification-history";
            panel.className = "page-notification-history ui-card";
            panel.setAttribute("aria-labelledby", "page-notification-title");
            var title = document.createElement("h2");
            title.id = "page-notification-title";
            title.textContent = "Сообщения этой страницы";
            var explanation = document.createElement("p");
            explanation.className = "page-notification-note";
            explanation.textContent = "Здесь остаются последние 10 предупреждений, ошибок и длинных сообщений. Это история уведомлений, не текущее состояние. Очищается при переходе со страницы.";
            var list = document.createElement("ol");
            list.className = "page-notification-list";
            list.setAttribute("aria-live", "off");
            panel.append(title, explanation, list);
            main.appendChild(panel);
        }
        var list = panel.querySelector(".page-notification-list");
        if (!list) return false;
        var existing = toastHistory.find(function (record) { return record.text === text && record.kind === kind; });
        if (existing) {
            list.prepend(existing.element);
            toastHistory = toastHistory.filter(function (record) { return record !== existing; });
            toastHistory.push(existing);
            return true;
        }
        var entry = document.createElement("li");
        entry.className = "page-notification-entry page-notification-" + kind;
        var label = document.createElement("strong");
        label.textContent = toastLabel(kind) + ": ";
        var body = document.createElement("span");
        body.textContent = text;
        entry.append(label, body);
        list.prepend(entry);
        toastHistory.push({text:text, kind:kind, element:entry});
        while (toastHistory.length > TOAST_HISTORY_LIMIT) toastHistory.shift().element.remove();
        return true;
    }
    function removeToast(record) {
        if (record.timer !== null) window.clearTimeout(record.timer);
        record.timer = null;
        record.element.remove();
        toastRecords = toastRecords.filter(function (item) { return item !== record; });
    }
    function scheduleToast(record) {
        if (document.hidden || record.timer !== null) return;
        record.started = performance.now();
        record.timer = window.setTimeout(function () { removeToast(record); }, record.remaining);
    }
    function retainOverflow(record) {
        if (record.preview || record.message.scrollHeight > record.message.clientHeight + 1) {
            if (rememberToast(record.text, record.kind)) record.note.hidden = false;
        }
    }
    function toastVisibilityChanged() {
        toastRecords.forEach(function (record) {
            if (document.hidden && record.timer !== null) {
                record.remaining = Math.max(0, record.remaining - (performance.now() - record.started));
                window.clearTimeout(record.timer);
                record.timer = null;
            } else if (!document.hidden) scheduleToast(record);
        });
    }
    function toast(message, type) {
        var root = document.getElementById("toast-root");
        var text = message == null ? "" : String(message);
        if (!root || !text.trim()) return;
        var kind = toastKind(type);
        if (kind === "error" || kind === "warning") rememberToast(text, kind);
        var duplicate = toastRecords.find(function (record) { return record.text === text && record.kind === kind; });
        if (duplicate && duplicate.element.isConnected) return duplicate.element;
        toastRecords.slice().forEach(function (record) { if (!record.element.isConnected) removeToast(record); });
        while (toastRecords.length >= TOAST_MAX_VISIBLE) removeToast(toastRecords[0]);
        var element = document.createElement("div");
        element.className = "toast toast-" + kind;
        var copy = document.createElement("div");
        copy.className = "toast-copy";
        var label = document.createElement("span");
        label.className = "toast-kind";
        label.textContent = toastLabel(kind) + ": ";
        var body = document.createElement("div");
        var characters = Array.from(text);
        body.className = "toast-message" + (characters.length > 160 ? " toast-message-long" : "");
        body.textContent = characters.length > 240 ? characters.slice(0, 240).join("") + "…" : text;
        var note = document.createElement("span");
        note.className = "toast-note";
        note.textContent = "Полный текст — в сообщениях страницы.";
        note.hidden = true;
        copy.append(label, body, note);
        element.append(toastIcon(kind), copy);
        root.setAttribute("aria-live", "polite");
        root.setAttribute("aria-atomic", "false");
        root.setAttribute("aria-relevant", "additions");
        root.appendChild(element);
        var record = {element:element, message:body, note:note, text:text, kind:kind,
            preview:characters.length > 240, timer:null, started:0, remaining:TOAST_VISIBLE_MS};
        toastRecords.push(record);
        retainOverflow(record);
        scheduleToast(record);
        if (!toastEventsInstalled) {
            toastEventsInstalled = true;
            document.addEventListener("visibilitychange", toastVisibilityChanged);
            window.addEventListener("resize", function () { toastRecords.forEach(retainOverflow); });
            window.addEventListener("pagehide", function () {
                toastRecords.slice().forEach(removeToast);
                toastHistory = [];
                var panel = document.getElementById("page-notification-history");
                if (panel) panel.remove();
            });
        }
        return element;
    }

    function redirectToLogin() {
        window.location.replace("/");
    }

    function normalizeHealth(value, moduleName) {
        var input = value && value.health && typeof value.health === "object"
            ? value.health
            : value;
        var severityValues = ["ok", "warning", "error", "busy", "unknown"];
        var availabilityValues = ["available", "partial", "unavailable"];
        var freshnessValues = ["fresh", "stale", "expired", "unknown"];
        var health = input && typeof input === "object" ? input : {};
        var severity = severityValues.indexOf(health.severity) >= 0
            ? health.severity
            : "unknown";
        var availability = availabilityValues.indexOf(health.availability) >= 0
            ? health.availability
            : "unavailable";
        var freshness = health.freshness && typeof health.freshness === "object"
            ? health.freshness
            : {};
        var freshnessState = freshnessValues.indexOf(freshness.state) >= 0
            ? freshness.state
            : "unknown";

        return {
            schemaVersion: Number(health.schemaVersion || 1),
            module: health.module || moduleName || "unknown",
            availability: availability,
            severity: severity,
            operational: health.operational === true,
            consistent: health.consistent === true,
            actionRequired: health.actionRequired !== false,
            freshness: {
                state: freshnessState,
                checkedAt: freshness.checkedAt || null
            },
            reasons: Array.isArray(health.reasons) ? health.reasons : [],
            facts: health.facts && typeof health.facts === "object" ? health.facts : {},
            lastOperation: health.lastOperation && typeof health.lastOperation === "object"
                ? health.lastOperation
                : null
        };
    }

    function severityMeta(value) {
        var health = typeof value === "string"
            ? {severity: value}
            : normalizeHealth(value);
        var map = {
            ok: {label: "Исправно", tone: "success", badgeClass: "status-success"},
            warning: {label: "Требуется внимание", tone: "warning", badgeClass: "status-warning"},
            error: {label: "Требуется исправление", tone: "error", badgeClass: "status-error"},
            busy: {label: "Выполняется", tone: "loading", badgeClass: "status-loading"},
            unknown: {label: "Не проверено", tone: "neutral", badgeClass: "status-neutral"}
        };
        return map[health.severity] || map.unknown;
    }

    function firstHealthReason(value, fallback) {
        var health = normalizeHealth(value);
        var reason = health.reasons.find(function (item) {
            return item && typeof item.message === "string" && item.message.trim();
        });
        return reason ? reason.message : (fallback || "Состояние не определено.");
    }

    function healthIsConfirmedOk(value) {
        var health = normalizeHealth(value);
        return health.availability === "available" &&
            health.severity === "ok" &&
            health.operational === true &&
            health.consistent === true &&
            health.actionRequired === false &&
            health.freshness.state === "fresh";
    }

    window.BROrayUI = {
        apiRequest: apiRequest,
        submitQueued: submitQueued,
        followQueued: followQueued,
        followReserveMode: followReserveMode,
        followDPIProfile: followDPIProfile,
        readDPIProfileRequest: readDPIProfileRequest,
        readReserveModeRequest: readReserveModeRequest,
        hasQueuedRequest: hasQueuedRequest,
        queueLabel: queueLabel,
        toast: toast,
        redirectToLogin: redirectToLogin,
        normalizeHealth: normalizeHealth,
        severityMeta: severityMeta,
        firstHealthReason: firstHealthReason,
        healthIsConfirmedOk: healthIsConfirmedOk
    };
})();
