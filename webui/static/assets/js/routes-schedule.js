/* BROray 3.5.0: scheduled update check of the built-in route sets. */
(function () {
    "use strict";

    var SCHEDULE_URL = "/api/routes/catalog-schedule.cgi";
    var CHECK_URL = "/api/routes/catalog-check.cgi";
    var IDLE_REFRESH_MS = 60000;
    var RUNNING_REFRESH_MS = 2000;
    var DUE_REFRESH_MS = 5000;
    var view = null;
    var saving = false;
    var timer = null;
    var wasRunning = false;

    var RESULT_TEXT = {
        no_changes: "Новых версий нет",
        updates_found: "Найдены обновления",
        failed: "Ошибка проверки"
    };

    function byId(id) { return document.getElementById(id); }

    function unwrap(payload) {
        var error;
        if (payload && payload.success === false) {
            error = new Error(payload.error && payload.error.message ? payload.error.message : "Запрос завершился ошибкой.");
            error.code = payload.error ? payload.error.code : null;
            throw error;
        }
        return payload && Object.prototype.hasOwnProperty.call(payload, "data") ? payload.data : payload;
    }

    function request(url, options) {
        return window.BROrayUI.apiRequest(url, options || {}).then(unwrap);
    }

    function post(url, body) {
        return request(url, {method: "POST", credentials: "same-origin",
            headers: {"Accept": "application/json", "Content-Type": "application/json",
                "X-BROray-Request": "operations", "X-BROray-Origin": window.location.origin},
            body: JSON.stringify(body || {})});
    }

    function formatDate(value, empty) {
        var date;
        if (!value) return empty;
        date = new Date(value);
        return isNaN(date.getTime()) ? String(value) : date.toLocaleString("ru-RU");
    }

    function intervalText(hours) {
        if (hours === 6) return "каждые 6 часов";
        if (hours === 12) return "каждые 12 часов";
        if (hours === 24) return "раз в сутки";
        if (hours === 168) return "раз в неделю";
        return "каждые " + hours + " ч";
    }

    function setBadge(text, kind) {
        var node = byId("routes-schedule-badge");
        if (!node) return;
        node.className = "status-badge " + kind;
        node.textContent = text;
        if (window.BROrayIcons) window.BROrayIcons.scan(node);
    }

    function render() {
        var enabled;
        var result;
        var resultNode = byId("routes-schedule-result");
        var busy = saving || !view || view.running === true;
        if (!view) return;
        enabled = Number(view.intervalHours || 0) > 0;
        if (!saving) { // keep the owner's choice visible while it is being saved
            byId("routes-schedule-interval").value = String(view.intervalHours || 0);
            byId("routes-schedule-removal").value = String(view.maxRemovalPercent || 50);
            byId("routes-schedule-auto").checked = view.autoApply === true;
        }
        byId("routes-schedule-interval").disabled = saving;
        byId("routes-schedule-removal").disabled = saving;
        byId("routes-schedule-auto").disabled = saving;
        byId("routes-schedule-check").disabled = busy;
        byId("routes-schedule-check").textContent = view.running ? "Проверка…" : "Проверить сейчас";

        byId("routes-schedule-last").textContent = view.lastCheckAt
            ? formatDate(view.lastCheckAt, "") + (RESULT_TEXT[view.lastResult] ? " · " + RESULT_TEXT[view.lastResult] : "")
            : "Не выполнялась";
        if (view.automationPaused && enabled) byId("routes-schedule-next").textContent = "Фоновые операции остановлены";
        else byId("routes-schedule-next").textContent = enabled ? formatDate(view.nextCheckAt, "Скоро") : "Не запланирована";
        byId("routes-schedule-head").textContent = view.headCommit
            ? String(view.headCommit).slice(0, 7) + (view.headDate ? " · " + formatDate(view.headDate, "") : "")
            : "Закреплённая " + String((view.source && view.source.pinnedCommit) || "").slice(0, 7);

        result = view.lastMessage || "Результатов проверки пока нет.";
        if (view.running) result = "Проверка обновлений выполняется. Карточки наборов обновятся после её завершения.";
        resultNode.textContent = result;
        resultNode.classList.toggle("is-error", !view.running && view.lastResult === "failed");

        if (view.running) setBadge("Проверка…", "status-badge-warning");
        else if (!enabled) setBadge("Выключена", "status-badge-neutral");
        else if (view.automationPaused) setBadge("Приостановлена", "status-badge-warning");
        else setBadge(view.autoApply ? "Автоустановка " + intervalText(view.intervalHours) : "Проверка " + intervalText(view.intervalHours), "status-badge-success");

        window.BROrayRoutesSchedule = view;
        document.dispatchEvent(new CustomEvent("broray:routes-schedule", {detail: view}));
    }

    function handleError(error) {
        if (error && (error.status === 401 || error.code === "AUTH_REQUIRED" || error.code === "SESSION_REQUIRED")) {
            window.BROrayUI.redirectToLogin();
            return;
        }
        if (window.BROrayUI && window.BROrayUI.toast) {
            window.BROrayUI.toast(error && error.message ? error.message : "Не удалось выполнить запрос.", "error");
        }
    }

    // A check is running or due (just enabled, or its time has passed): the
    // daemon starts it within a minute, so look again soon.
    function checkImminent() {
        var next;
        if (!view) return false;
        if (view.running) return true;
        if (!(Number(view.intervalHours || 0) > 0) || view.automationPaused) return false;
        next = view.nextCheckAt ? new Date(view.nextCheckAt).getTime() : NaN;
        return !view.lastCheckAt || (isFinite(next) && next <= Date.now());
    }

    function schedule() {
        if (timer) window.clearTimeout(timer);
        timer = window.setTimeout(load, view && view.running ? RUNNING_REFRESH_MS
            : checkImminent() ? DUE_REFRESH_MS : IDLE_REFRESH_MS);
    }

    function apply(data) {
        var finished = data && data.running !== true &&
            (wasRunning || Boolean(view && view.lastCheckAt !== data.lastCheckAt && view.lastCheckAt !== undefined));
        view = data;
        wasRunning = Boolean(data && data.running);
        render();
        if (finished) document.dispatchEvent(new CustomEvent("broray:routes-catalog-refresh"));
    }

    function load() {
        if (document.hidden) { schedule(); return Promise.resolve(); }
        return request(SCHEDULE_URL, {method: "GET", credentials: "same-origin"})
            .then(apply)
            .catch(function (error) {
                if (!view) setBadge("Недоступна", "status-badge-danger");
                if (error && (error.status === 401 || error.code === "AUTH_REQUIRED")) handleError(error);
            })
            .then(schedule);
    }

    function save() {
        var body;
        if (saving || !view) return;
        // read the controls before render() puts the saved values back
        body = {
            intervalHours: Number(byId("routes-schedule-interval").value),
            autoApply: byId("routes-schedule-auto").checked,
            maxRemovalPercent: Number(byId("routes-schedule-removal").value)
        };
        saving = true;
        render();
        post(SCHEDULE_URL, body).then(function (data) {
            view = data;
            if (window.BROrayUI && window.BROrayUI.toast) window.BROrayUI.toast("Настройки проверки обновлений сохранены.", "success");
        }).catch(handleError).then(function () {
            saving = false;
            render();
            schedule();
        });
    }

    function checkNow() {
        if (!view || view.running) return;
        view.running = true;
        render();
        post(CHECK_URL, {}).then(function (data) {
            apply(data);
            wasRunning = true;
        }).catch(function (error) {
            view.running = false;
            render();
            handleError(error);
        }).then(schedule);
    }

    function initialize() {
        if (!byId("routes-schedule")) return;
        byId("routes-schedule-interval").addEventListener("change", save);
        byId("routes-schedule-removal").addEventListener("change", save);
        byId("routes-schedule-auto").addEventListener("change", save);
        byId("routes-schedule-check").addEventListener("click", checkNow);
        document.addEventListener("visibilitychange", function () { if (!document.hidden) load(); });
        load();
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize, {once: true});
    } else {
        initialize();
    }
}());
