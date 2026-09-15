(function () {
    "use strict";

    var STATUS_URL = "/api/servers/quality-refresh-status.cgi";
    var SAVE_URL = "/api/servers/quality-refresh-save.cgi";
    var REFRESH_INTERVAL_MS = 15000;
    var pollTimer = null;
    var pollInFlight = false;
    var lastCompletedAt = null;

    function element(id) {
        return document.getElementById(id);
    }

    function unwrap(payload) {
        if (
            payload &&
            payload.success === true &&
            payload.data !== undefined
        ) {
            return payload.data;
        }
        return payload;
    }

    function request(url, options) {
        return BROrayUI.apiRequest(url, options).then(unwrap);
    }

    function errorMessage(error) {
        if (
            error &&
            error.payload &&
            error.payload.error &&
            error.payload.error.message
        ) {
            return error.payload.error.message;
        }
        return error && error.message
            ? error.message
            : "Операция завершилась ошибкой.";
    }

    function formatDate(value) {
        var date;
        if (!value) return "Не выполнялась";
        date = new Date(value);
        if (isNaN(date.getTime())) return String(value);
        return date.toLocaleString("ru-RU");
    }

    function formatEpoch(value) {
        var epoch = Number(value || 0);
        if (!epoch || epoch < 1) return "Не запланирована";
        return formatDate(new Date(epoch * 1000).toISOString());
    }

    function statusTitle(status) {
        var titles = {
            disabled: "Выключена",
            scheduled: "Запланирована",
            running: "Выполняется",
            success: "Завершена",
            partial: "Завершена с ошибками",
            paused: "Ожидает",
            error: "Ошибка"
        };
        return titles[status] || "Ожидание";
    }

    function syncIntervalState() {
        var enabled = element("quality-refresh-enabled");
        var interval = element("quality-refresh-interval");
        if (enabled && interval) interval.disabled = !enabled.checked;
    }

    function resultText(state) {
        var total = Number(state.totalCount || 0);
        var checked = Number(state.checkedCount || 0);
        var available = Number(state.availableCount || 0);
        var unavailable = Number(state.unavailableCount || 0);
        var errors = Number(state.errorCount || 0);

        if (state.status === "running") {
            return "Проверено " + checked + " из " + total + ". Серверы проверяются последовательно.";
        }
        if (state.lastResult) return state.lastResult;
        if (state.lastCompletedAt) {
            return "Доступно: " + available + ". Недоступно: " + unavailable + ". Ошибок выполнения: " + errors + ".";
        }
        return "Результатов автоматической проверки пока нет.";
    }

    function refreshServerListAfterCompletion(state) {
        var completed = state.lastCompletedAt || null;
        var refresh;

        if (lastCompletedAt !== null && completed && completed !== lastCompletedAt) {
            refresh = element("refresh-servers");
            if (refresh && !refresh.disabled) refresh.click();
        }
        lastCompletedAt = completed;
    }

    function render(payload) {
        var config = payload.config || {};
        var state = payload.state || {};
        var service = payload.service || {};
        var enabled = config.enabled === true;

        element("quality-refresh-enabled").checked = enabled;
        element("quality-refresh-interval").value = String(
            config.intervalMinutes || 60
        );
        syncIntervalState();

        element("quality-refresh-badge").textContent = enabled
            ? "Включена"
            : "Выключена";
        element("quality-refresh-badge").className = enabled
            ? "status-badge quality-refresh-badge-enabled"
            : "status-badge status-badge-neutral";
        element("quality-refresh-service").textContent = service.running
            ? "Работает"
            : "Остановлена";
        element("quality-refresh-status").textContent = statusTitle(
            state.status || (enabled ? "scheduled" : "disabled")
        );
        element("quality-refresh-last").textContent = formatDate(
            state.lastCompletedAt
        );
        element("quality-refresh-next").textContent = enabled
            ? formatEpoch(state.nextCheckEpoch)
            : "Не запланирована";
        element("quality-refresh-result").textContent = resultText(state);

        if (state.lastError) {
            element("quality-refresh-error").hidden = false;
            element("quality-refresh-error").textContent = state.lastError;
        } else {
            element("quality-refresh-error").hidden = true;
            element("quality-refresh-error").textContent = "";
        }

        refreshServerListAfterCompletion(state);
    }

    function loadStatus(showError) {
        return request(STATUS_URL, { method: "GET" }).then(render).catch(function (error) {
            if (error.status === 401) {
                BROrayUI.redirectToLogin();
                return;
            }
            if (showError) BROrayUI.toast(errorMessage(error), "error");
        });
    }

    function setSaving(saving) {
        var button = element("quality-refresh-save");
        button.disabled = saving;
        button.setAttribute("aria-busy", saving ? "true" : "false");
        button.textContent = saving ? "Сохранение…" : "Сохранить настройки";
        if (!saving) button.removeAttribute("aria-busy");
        if (window.BROrayIcons) window.BROrayIcons.scan(button);
    }

    function saveSettings(event) {
        var payload;
        event.preventDefault();
        payload = {
            enabled: element("quality-refresh-enabled").checked,
            intervalMinutes: Number(element("quality-refresh-interval").value)
        };
        setSaving(true);
        request(SAVE_URL, { method: "POST", body: payload }).then(function () {
            BROrayUI.toast(
                payload.enabled
                    ? "Автоматическая проверка качества включена."
                    : "Автоматическая проверка качества выключена.",
                "success"
            );
            return loadStatus(false);
        }).catch(function (error) {
            BROrayUI.toast(errorMessage(error), "error");
        }).then(function () {
            setSaving(false);
        });
    }

    function stopPolling() {
        if (!pollTimer) return;
        window.clearTimeout(pollTimer);
        pollTimer = null;
    }

    function schedulePolling() {
        stopPolling();
        if (document.hidden) return;
        pollTimer = window.setTimeout(function () {
            pollTimer = null;
            if (pollInFlight) {
                schedulePolling();
                return;
            }
            pollInFlight = true;
            loadStatus(false).then(function () {
                pollInFlight = false;
                schedulePolling();
            });
        }, REFRESH_INTERVAL_MS);
    }

    function onVisibilityChange() {
        if (document.hidden) {
            stopPolling();
            return;
        }
        if (pollInFlight) return;
        pollInFlight = true;
        loadStatus(false).then(function () {
            pollInFlight = false;
            schedulePolling();
        });
    }

    function initialize() {
        var form = element("server-quality-refresh-form");
        if (!form) return;
        form.addEventListener("submit", saveSettings);
        element("quality-refresh-enabled").addEventListener(
            "change",
            syncIntervalState
        );
        document.addEventListener("visibilitychange", onVisibilityChange);
        loadStatus(true).then(schedulePolling);
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize);
    } else {
        initialize();
    }
})();
