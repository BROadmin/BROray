(function () {
    "use strict";

    async function apiRequest(url, options) {
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

        if (!response.ok) {
            const errorPayload = payload && payload.error
                ? payload.error
                : payload;
            const requestError = new Error(
                errorPayload && errorPayload.message
                    ? errorPayload.message
                    : "Ошибка запроса."
            );

            requestError.status = response.status;
            requestError.payload = payload;
            requestError.code = errorPayload && errorPayload.code
                ? errorPayload.code
                : null;
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
    function submitQueued(url, id) {
        if (!["/api/servers/check.cgi","/api/subscriptions/refresh.cgi"].includes(url) ||
            typeof id !== "string" || !/^[A-Za-z0-9_][A-Za-z0-9_.-]{0,95}$/.test(id)) {
            return Promise.reject(new Error("Некорректное задание."));
        }
        var key="broray.queue.v1:"+url+":"+id;
        if (queueInFlight.has(key)) return queueInFlight.get(key);
        var promise=Promise.resolve().then(async function () {
            var saved=sessionStorage.getItem(key),nonce;
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
                try { result=await queuePost(url,{id:id,nonce:nonce},true); }
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
                sessionStorage.removeItem("broray.queue.v1:"+watch.url+":"+watch.id);
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
    function followQueued(url,id,listener) {
        var key=url+":"+id,watch=queueWatches.get(key);
        if (watch) {
            watch.listeners.add(listener);
            if (watch.last) listener(watch.last);
            return watch.promise;
        }
        watch={url:url,id:id,listeners:new Set([listener]),last:null,promise:null};
        queueWatches.set(key,watch);
        watch.promise=submitQueued(url,id).then(function (result) {
            queueNotify(key,watch,result);queueSchedule();return result;
        },function (error) { queueWatches.delete(key);throw error; });
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
