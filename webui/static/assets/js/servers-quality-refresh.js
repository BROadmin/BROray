(function () {
    "use strict";

    var STATUS_URL = "/api/servers/quality-refresh-status.cgi";
    var SAVE_URL = "/api/servers/quality-refresh-save.cgi";
    var REFRESH_INTERVAL_MS = 15000;
    var pollTimer = null;
    var pollInFlight = false;
    var formDirty = false;
    var formRevision = 0;
    var loadSequence = 0;
    var formReady = false;
    var confirmedEnabled = false;
    var configRevision = null;
    var formConfigRevision = null;
    var writeInFlight = false;
    var toggleUnknown = false;

    function markFormDirty(event) {
        if (event && event.target === element("quality-refresh-enabled")) return;
        formDirty = true;
        formRevision += 1;
    }
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
            paused: "На паузе",
            unavailable: "Состояние не подтверждено",
            starting: "Запускается",
            stopping: "Останавливается",
            stopped: "Служба остановлена",
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
        var paused = payload.automation && payload.automation.paused === true;
        var serviceKnown = service.complete === true && typeof service.running === "boolean";
        var operational = serviceKnown && service.running === true && service.ready === true;
        var transitioning = serviceKnown && (service.state === "starting" || service.state === "stopping");
        var effectiveStatus = !enabled ? "disabled" : paused ? "paused"
            : !serviceKnown ? "unavailable" : transitioning ? service.state
            : !operational ? "stopped" : state.status || "scheduled";

        confirmEnabled({value: config.enabled, revision: payload.revision});
        if (!formDirty) {
            formConfigRevision = payload.revision;
            element("quality-refresh-interval").value = String(
                config.intervalMinutes || 60
            );
            syncIntervalState();
        }

        var tone = {
            disabled: "neutral", scheduled: "success", success: "success",
            partial: "warning", paused: "warning", unavailable: "unknown",
            running: "loading", starting: "loading", stopping: "loading",
            stopped: "error", error: "error"
        }[effectiveStatus] || "unknown";
        element("quality-refresh-badge").textContent = tone === "success" ? "Включена" : statusTitle(effectiveStatus);
        element("quality-refresh-badge").className = "status-badge status-" + tone;
        element("quality-refresh-service").textContent =
            service.complete === false || typeof service.running !== "boolean"
                ? "Состояние не подтверждено"
                : service.state === "stopping"
                    ? "Останавливается"
                    : service.state === "starting"
                        ? "Запускается"
                        : service.running ? "Работает" : "Остановлена";
        element("quality-refresh-status").textContent = statusTitle(
            effectiveStatus
        );
        element("quality-refresh-last").textContent = formatDate(
            state.lastCompletedAt
        );
        element("quality-refresh-next").textContent = enabled && !paused && operational
            ? formatEpoch(state.nextCheckEpoch)
            : "Не запланирована";
        element("quality-refresh-result").textContent = paused
            ? "Автоматика BROray на паузе. Возобновите её на странице BROray; расписание сохранено."
            : enabled && !operational
                ? "Проверки не запускаются: служба автоматического выбора не готова. Откройте диагностику на странице BROray."
                : resultText(state);

        var recoveryError = service.errorCode === "SERVICE_RECOVERY_REBOOT_REQUIRED"
            ? "Для восстановления службы требуется штатная перезагрузка роутера. Исходные записи сохранены; после перезагрузки BROray проверит их и повторит запуск."
            : "";
        if (state.lastError || recoveryError) {
            element("quality-refresh-error").hidden = false;
            element("quality-refresh-error").textContent = state.lastError || recoveryError;
        } else {
            element("quality-refresh-error").hidden = true;
            element("quality-refresh-error").textContent = "";
        }

        refreshServerListAfterCompletion(state);
    }

    function loadStatus(showError, savedRevision) {
        if (writeInFlight) return Promise.resolve();
        var sequence = ++loadSequence;
        return request(STATUS_URL, { method: "GET" }).then(function (payload) {
            if (sequence !== loadSequence) return;
            if (savedRevision === formRevision) formDirty = false;
            render(payload);
            formReady = true;
            syncWriteState();
        }).catch(function (error) {
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

    function syncWriteState() {
        var locked = writeInFlight || toggleUnknown || !formReady;
        element("quality-refresh-enabled").disabled = locked;
        element("quality-refresh-save").disabled = locked;
        element("quality-refresh-enabled").setAttribute("aria-busy", writeInFlight ? "true" : "false");
    }

    function confirmEnabled(result) {
        if (!result || typeof result.value !== "boolean" || typeof result.revision !== "string" || !result.revision) {
            throw new Error("Состояние переключателя не подтверждено.");
        }
        confirmedEnabled = result.value;
        configRevision = result.revision;
        toggleUnknown = false;
        element("quality-refresh-enabled").checked = confirmedEnabled;
        syncWriteState();
        syncIntervalState();
    }

    function saveEnabled() {
        var wanted = element("quality-refresh-enabled").checked;
        var beforeRevision = configRevision;
        if (!formReady || writeInFlight || toggleUnknown) return;
        writeInFlight = true;
        ++loadSequence; // Invalidate a status request started before this write.
        syncWriteState();
        request("/api/servers/quality-refresh-enabled.cgi", {
            method: "POST", body: {enabled: wanted, expectedRevision: configRevision}
        }).then(function (result) {
            confirmEnabled(result);
            if (formConfigRevision === beforeRevision) formConfigRevision = result.revision;
            BROrayUI.toast(wanted ? "Автопроверка качества включён." : "Автопроверка качества выключен.", "success");
        }).catch(function (error) {
            var explicit = error.status >= 400 && error.status < 500 && error.payload && error.payload.success === false;
            BROrayUI.toast(errorMessage(error), "error");
            if (explicit && error.status !== 409) {
                element("quality-refresh-enabled").checked = confirmedEnabled;
                if (error.status === 401) BROrayUI.redirectToLogin();
                return;
            }
            // Network/5xx errors can follow a committed write; never replay it.
            return request(STATUS_URL, {method: "GET"}).then(function (payload) {
                confirmEnabled({value: payload.config && payload.config.enabled, revision: payload.revision});
            }).catch(function () {
                toggleUnknown = true;
                element("quality-refresh-enabled").checked = confirmedEnabled;
                BROrayUI.toast("Состояние не подтверждено. Ожидаем обновления; повторная запись заблокирована.", "error");
            });
        }).then(function () {
            writeInFlight = false;
            syncWriteState();
            if (!toggleUnknown) loadStatus(false);
        });
    }

    function saveSettings(event) {
        var payload;
        var savedRevision = formRevision;
        event.preventDefault();
        if (!formReady || writeInFlight || toggleUnknown) return;
        payload = {
            enabled: confirmedEnabled,
            expectedRevision: formConfigRevision,
            intervalMinutes: Number(element("quality-refresh-interval").value)
        };
        writeInFlight = true;
        ++loadSequence;
        setSaving(true);
        syncWriteState();
        request(SAVE_URL, { method: "POST", body: payload }).then(function () {
            BROrayUI.toast(
                payload.enabled
                    ? "Расписание автоматической проверки сохранено."
                    : "Автоматическая проверка качества выключена.",
                "success"
            );
            writeInFlight = false;
            return loadStatus(false, savedRevision);
        }).catch(function (error) {
            BROrayUI.toast(errorMessage(error), "error");
            writeInFlight = false;
            toggleUnknown = true;
            return loadStatus(true);
        }).then(function () {
            writeInFlight = false;
            setSaving(false);
            syncWriteState();
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
        form.addEventListener("input", markFormDirty);
        form.addEventListener("change", markFormDirty);
        element("quality-refresh-enabled").addEventListener(
            "change",
            saveEnabled
        );
        syncWriteState();
        document.addEventListener("visibilitychange", onVisibilityChange);
        loadStatus(true).then(schedulePolling);
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize);
    } else {
        initialize();
    }
})();
