(function () {
    "use strict";

    var STATUS_URL =
        "/api/servers/auto-switch-status.cgi";

    var SAVE_URL =
        "/api/servers/auto-switch-save.cgi";

    var SUMMARY_URL =
        "/api/servers/summary.cgi";

    var STATUS_REFRESH_INTERVAL_MS = 15000;
    var lastSummary = null;
    var pollTimer = null;
    var pollInFlight = false;
    var formDirty = false;
    var formReady = false;
    var formRevision = 0;
    var loadSequence = 0;
    var confirmedEnabled = false;
    var configRevision = null;
    var formConfigRevision = null;
    var writeInFlight = false;
    var toggleUnknown = false;

    function markFormDirty() {
        formDirty = true;
        formRevision += 1;
    }

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

        if (!value) {
            return "Не выполнялось";
        }

        date = new Date(value);

        if (isNaN(date.getTime())) {
            return String(value);
        }

        return date.toLocaleString("ru-RU");
    }

    function statusTitle(status) {
        var titles = {
            disabled: "Выключен",
            healthy: "Соединение работает",
            "manual-off": "VPN отключён",
            paused: "Приостановлен",
            "waiting-threshold": "Ожидание порога",
            "attempt-guard": "Ожидание проверки",
            cooldown: "Защитный интервал",
            "checking-candidates": "Проверка кандидатов",
            "no-candidate": "Кандидат не найден",
            recovered: "Связь восстановилась",
            cancelled: "Отменено",
            "dry-run": "Пробный расчёт",
            switching: "Переключение",
            switched: "Сервер переключён",
            error: "Ошибка"
        };

        return titles[status] || "Ожидание";
    }

    function ruleTitle(rule) {
        if (rule === "lowest-ping") {
            return "Минимальный пинг";
        }

        if (rule === "preferred") {
            return "Предпочтительный сервер";
        }

        return "Лучшее качество";
    }

    function createSection() {
        var section = element("server-auto-switch-section");
        if (!section) return null;

        if (section.getAttribute("data-auto-switch-bound") !== "true") {
            section.setAttribute("data-auto-switch-bound", "true");
            element("auto-switch-form").addEventListener("submit", saveSettings);
            element("auto-switch-form").addEventListener("input", markFormDirty);
            element("auto-switch-form").addEventListener("change", markFormDirty);
            element("auto-switch-rule").addEventListener("change", syncPreferredState);
            element("auto-switch-enabled").addEventListener("change", saveEnabled);
        }
        if (window.BROrayIcons) {
            window.BROrayIcons.scan(section);
        }
        return section;
    }

    function setInitialLoading(loading) {
        var form = element("auto-switch-form");
        form.setAttribute("aria-busy", loading ? "true" : "false");
        element("auto-switch-enabled").disabled = loading;
        Array.prototype.forEach.call(form.querySelectorAll("input, select, button"), function (control) {
            control.disabled = loading;
        });
    }

    function syncPreferredState() {
        var preferred = element("auto-switch-preferred");
        var rule = element("auto-switch-rule");

        if (preferred && rule) {
            preferred.disabled = rule.value !== "preferred";
        }
    }

    function fillServers(summary, selectedId) {
        var select = element("auto-switch-preferred");

        if (!select) {
            return;
        }

        select.innerHTML = '<option value="">Не задан</option>';

        if (!summary || !summary.servers) {
            return;
        }

        summary.servers.forEach(function (server) {
			if (server.connectionSupport && server.connectionSupport.supported === false) return;
            var option = document.createElement("option");

            option.value = server.id;
            option.textContent = server.name || server.id;
            option.selected = server.id === selectedId;
            select.appendChild(option);
        });
    }

    function render(payload, summary) {
        var config = payload.config || {};
        var state = payload.state || {};
        var service = payload.service || {};
        confirmEnabled({value: config.enabled, revision: payload.revision});
        var paused = confirmedEnabled && payload.automation && payload.automation.paused === true;
        var enabledLabel = paused ? "На паузе" : (confirmedEnabled ? "Включён" : "Выключен");

        if (!formDirty) {
            formConfigRevision = payload.revision;
            element("auto-switch-threshold").value =
                config.failureThreshold || 3;

            element("auto-switch-cooldown").value =
                config.cooldownMinutes || 10;

            element("auto-switch-minimum").value =
                config.minimumRating || "acceptable";

            element("auto-switch-rule").value =
                config.selectionRule || "best-quality";

            fillServers(summary, config.preferredServerId || "");
            syncPreferredState();
        }

        element("auto-switch-service").textContent =
            service.complete === false || typeof service.running !== "boolean"
                ? "Состояние не подтверждено"
                : service.state === "stopping"
                    ? "Останавливается"
                    : service.state === "starting"
                        ? "Запускается"
                        : service.running ? "Работает" : "Остановлен";

        element("auto-switch-status").textContent =
            paused ? "На паузе" : statusTitle(state.status);

        element("auto-switch-failures").textContent =
            String(state.consecutiveFailures || 0) +
            " из " +
            String(config.failureThreshold || 3);

        element("auto-switch-last-switch").textContent =
            state.lastSwitchAt
                ? formatDate(state.lastSwitchAt) +
                    (state.lastSwitchName
                        ? " — " + state.lastSwitchName
                        : "")
                : "Не выполнялось";

        element("auto-switch-reason").textContent =
            paused ? "Автовыбор приостановлен вместе с фоновой автоматикой." : (state.lastReason || "Ожидание");

        if (state.lastError) {
            element("auto-switch-error").hidden = false;
            element("auto-switch-error").textContent = state.lastError;
            if (element("server-reserve-section")) {
                element("server-reserve-section").open = true;
            }
        } else {
            element("auto-switch-error").hidden = true;
            element("auto-switch-error").textContent = "";
        }

        if (element("servers-auto-switch")) {
            element("servers-auto-switch").textContent =
                enabledLabel;
        }

        if (element("servers-selection-rule")) {
            element("servers-selection-rule").textContent =
                confirmedEnabled
                    ? ruleTitle(config.selectionRule)
                    : "Ручной выбор";
        }
    }

    function loadStatus(showError, refreshSummary, savedRevision) {
        if (writeInFlight) return Promise.resolve();
        var sequence = ++loadSequence;
        var summaryRequest =
            refreshSummary || !lastSummary
                ? request(SUMMARY_URL, { method: "GET" })
                : Promise.resolve(lastSummary);

        return Promise.all([
            request(STATUS_URL, { method: "GET" }),
            summaryRequest
        ]).then(function (values) {
            if (sequence !== loadSequence) return;
            if (savedRevision === formRevision) formDirty = false;
            lastSummary = values[1];
            render(values[0], values[1]);
            if (!formReady) {
                formReady = true;
                setInitialLoading(false);
                syncWriteState();
                syncPreferredState();
            }
        }).catch(function (error) {
            if (error.status === 401) {
                BROrayUI.redirectToLogin();
                return;
            }

            if (showError) {
                BROrayUI.toast(errorMessage(error), "error");
            }
        });
    }

    function setSaving(saving) {
        var button = element("auto-switch-save");

        button.disabled = saving;
        button.setAttribute("aria-busy", saving ? "true" : "false");
        button.textContent = saving ? "Сохранение…" : "Сохранить настройки";
        if (!saving) {
            button.removeAttribute("aria-busy");
        }
        if (window.BROrayIcons) {
            window.BROrayIcons.scan(button);
        }
        syncWriteState();
    }

    function syncWriteState() {
        var locked = writeInFlight || toggleUnknown || !formReady;
        element("auto-switch-enabled").disabled = locked;
        element("auto-switch-save").disabled = locked;
        element("auto-switch-enabled").setAttribute("aria-busy", writeInFlight ? "true" : "false");
    }

    function confirmEnabled(result) {
        var value = result && result.value;
        if (value === "true" || value === "false") value = value === "true";
        if (!result || typeof value !== "boolean" || typeof result.revision !== "string" || !result.revision) {
            throw new Error("Состояние переключателя не подтверждено.");
        }
        var changed = configRevision === null || confirmedEnabled !== value;
        confirmedEnabled = value;
        configRevision = result.revision;
        toggleUnknown = false;
        element("auto-switch-enabled").checked = confirmedEnabled;
        syncWriteState();
        if (changed) window.BROrayReserve?.refreshAutomationMode?.();
    }

    function saveEnabled() {
        var wanted = element("auto-switch-enabled").checked;
        var beforeRevision = configRevision;
        if (!formReady || writeInFlight || toggleUnknown) return;
        writeInFlight = true;
        ++loadSequence; // Invalidate a status request started before this write.
        syncWriteState();
        request("/api/servers/auto-switch-enabled.cgi", {
            method: "POST", body: {enabled: wanted, expectedRevision: configRevision}
        }).then(function (result) {
            confirmEnabled(result);
            if (formConfigRevision === beforeRevision) formConfigRevision = result.revision;
            BROrayUI.toast(wanted ? "Автовыбор сервера включён." : "Автовыбор сервера выключен.", "success");
        }).catch(function (error) {
            var explicit = error.status >= 400 && error.status < 500 && error.payload && error.payload.success === false;
            BROrayUI.toast(errorMessage(error), "error");
            if (explicit && error.status !== 409) {
                element("auto-switch-enabled").checked = confirmedEnabled;
                if (error.status === 401) BROrayUI.redirectToLogin();
                return;
            }
            // Network/5xx errors can follow a committed write; never replay it.
            return request(STATUS_URL, {method: "GET"}).then(function (payload) {
                confirmEnabled({value: payload.config && payload.config.enabled, revision: payload.revision});
            }).catch(function () {
                toggleUnknown = true;
                element("auto-switch-enabled").checked = confirmedEnabled;
                BROrayUI.toast("Состояние не подтверждено. Ожидаем обновления; повторная запись заблокирована.", "error");
            });
        }).then(function () {
            writeInFlight = false;
            syncWriteState();
            if (!toggleUnknown) loadStatus(false, false);
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
            failureThreshold: Number(element("auto-switch-threshold").value),
            cooldownMinutes: Number(element("auto-switch-cooldown").value),
            minimumRating: element("auto-switch-minimum").value,
            selectionRule: element("auto-switch-rule").value,
            preferredServerId:
                element("auto-switch-preferred").value || null
        };

        writeInFlight = true;
        ++loadSequence;
        setSaving(true);

        request(SAVE_URL, {
            method: "POST",
            body: payload
        }).then(function () {
            BROrayUI.toast(
                payload.enabled
                    ? "Автовыбор сервера включён."
                    : "Автовыбор сервера выключен.",
                "success"
            );

            if (element("refresh-servers")) {
                element("refresh-servers").click();
            }

            writeInFlight = false;
            return loadStatus(false, true, savedRevision);
        }).catch(function (error) {
            BROrayUI.toast(errorMessage(error), "error");
            writeInFlight = false;
            toggleUnknown = true;
            return loadStatus(true, false);
        }).then(function () {
            writeInFlight = false;
            setSaving(false);
        });
    }

    function stopPolling() {
        if (!pollTimer) {
            return;
        }
        window.clearTimeout(pollTimer);
        pollTimer = null;
    }

    function schedulePolling() {
        stopPolling();
        if (document.hidden) {
            return;
        }
        pollTimer = window.setTimeout(function () {
            pollTimer = null;
            if (pollInFlight) {
                schedulePolling();
                return;
            }
            pollInFlight = true;
            loadStatus(false, false).then(function () {
                pollInFlight = false;
                schedulePolling();
            });
        }, STATUS_REFRESH_INTERVAL_MS);
    }

    function onVisibilityChange() {
        if (document.hidden) {
            stopPolling();
            return;
        }
        if (pollInFlight) {
            return;
        }
        pollInFlight = true;
        loadStatus(false, false).then(function () {
            pollInFlight = false;
            schedulePolling();
        });
    }

    function initialize() {
        createSection();

        if (!element("server-auto-switch-section")) {
            return;
        }

        setInitialLoading(true);
        document.addEventListener("visibilitychange", onVisibilityChange);
        loadStatus(true, true).then(schedulePolling);
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize);
    } else {
        initialize();
    }
})();
