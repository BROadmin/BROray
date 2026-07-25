(function () {
    "use strict";

    var STATUS_URL =
        "/api/servers/auto-switch-status.cgi";

    var SAVE_URL =
        "/api/servers/auto-switch-save.cgi";

    var SUMMARY_URL =
        "/api/servers/summary.cgi";

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
        var overview;
        var importSection;
        var listSection;
        var section;

        section = element("server-auto-switch-section");
        if (!section) {
            overview = document.querySelector(".servers-overview");
            if (!overview || !overview.parentNode) {
                return null;
            }

            section = document.createElement("details");
            section.id = "server-auto-switch-section";
            section.className = "servers-section server-auto-switch-section ui-card";
            section.innerHTML =
                '<summary class="servers-disclosure-summary">' +
                    '<span class="servers-disclosure-icon" data-icon="settings" aria-hidden="true"></span>' +
                    '<span class="servers-disclosure-copy">' +
                        '<span class="eyebrow">Автовыбор</span>' +
                        '<strong>Автоматическое переключение</strong>' +
                        '<small>Настройки резервного выбора серверов</small>' +
                    '</span>' +
                    '<span id="auto-switch-badge" class="status-badge status-badge-neutral">Выключен</span>' +
                '</summary>' +
                '<form id="auto-switch-form" class="auto-switch-form">' +
                    '<div class="auto-switch-description">' +
                        '<p>При подтверждённой потере связи BROray проверит резервные серверы реальным HTTPS-запросом и выберет подходящий.</p>' +
                    '</div>' +
                    '<label class="auto-switch-master">' +
                        '<span>' +
                            '<strong>Автовыбор сервера</strong>' +
                            '<small>Ручное действие «Отключить VPN» выключает автовыбор. Остановленный вручную Xray автоматически не запускается.</small>' +
                        '</span>' +
                        '<input id="auto-switch-enabled" type="checkbox">' +
                        '<span class="auto-switch-toggle" aria-hidden="true"></span>' +
                    '</label>' +
                    '<div class="auto-switch-grid">' +
                        '<label class="auto-switch-field"><span>Правило выбора</span><select id="auto-switch-rule"><option value="best-quality">Лучшее качество</option><option value="lowest-ping">Минимальный пинг</option><option value="preferred">Предпочтительный сервер</option></select></label>' +
                        '<label class="auto-switch-field"><span>Предпочтительный сервер</span><select id="auto-switch-preferred"><option value="">Не задан</option></select></label>' +
                        '<label class="auto-switch-field"><span>Ошибок до переключения</span><input id="auto-switch-threshold" type="number" min="1" max="10" value="3"></label>' +
                        '<label class="auto-switch-field"><span>Защита после переключения</span><div class="auto-switch-number-unit"><input id="auto-switch-cooldown" type="number" min="1" max="120" value="10"><small>мин.</small></div></label>' +
                        '<label class="auto-switch-field"><span>Минимальное качество</span><select id="auto-switch-minimum"><option value="excellent">Отличное</option><option value="good">Хорошее</option><option value="acceptable">Приемлемое</option><option value="poor">Любое доступное</option></select></label>' +
                    '</div>' +
                    '<div class="auto-switch-state">' +
                        '<div><span>Сервис</span><strong id="auto-switch-service">—</strong></div>' +
                        '<div><span>Состояние</span><strong id="auto-switch-status">—</strong></div>' +
                        '<div><span>Ошибки соединения</span><strong id="auto-switch-failures">0</strong></div>' +
                        '<div><span>Последнее переключение</span><strong id="auto-switch-last-switch">Не выполнялось</strong></div>' +
                    '</div>' +
                    '<p id="auto-switch-reason" class="auto-switch-reason">Ожидание данных.</p>' +
                    '<p id="auto-switch-error" class="auto-switch-error" hidden></p>' +
                    '<div class="auto-switch-actions"><button id="auto-switch-save" class="button button-primary" data-icon="settings" type="submit">Сохранить настройки</button></div>' +
                '</form>';

            importSection = document.querySelector(".servers-import-section");
            listSection = document.querySelector(".servers-list-section");
            if (listSection && listSection.parentNode === overview.parentNode) {
                overview.parentNode.insertBefore(section, listSection);
            } else if (importSection && importSection.nextSibling) {
                overview.parentNode.insertBefore(section, importSection.nextSibling);
            } else {
                overview.parentNode.appendChild(section);
            }
        }

        if (section.getAttribute("data-auto-switch-bound") !== "true") {
            section.setAttribute("data-auto-switch-bound", "true");
            element("auto-switch-form").addEventListener("submit", saveSettings);
            element("auto-switch-rule").addEventListener("change", syncPreferredState);
        }
        if (window.BROrayIcons) {
            window.BROrayIcons.scan(section);
        }
        return section;
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

        element("auto-switch-enabled").checked =
            config.enabled === true;

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

        element("auto-switch-badge").textContent =
            config.enabled ? "Включён" : "Выключен";

        element("auto-switch-badge").className =
            config.enabled
                ? "status-badge auto-switch-badge-enabled"
                : "status-badge status-badge-neutral";

        element("auto-switch-service").textContent =
            service.running ? "Работает" : "Остановлен";

        element("auto-switch-status").textContent =
            statusTitle(state.status);

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
            state.lastReason || "Ожидание";

        if (state.lastError) {
            element("auto-switch-error").hidden = false;
            element("auto-switch-error").textContent = state.lastError;
            if (element("server-auto-switch-section")) {
                element("server-auto-switch-section").open = true;
            }
        } else {
            element("auto-switch-error").hidden = true;
            element("auto-switch-error").textContent = "";
        }

        if (element("servers-auto-switch")) {
            element("servers-auto-switch").textContent =
                config.enabled ? "Включён" : "Выключен";
        }

        if (element("servers-selection-rule")) {
            element("servers-selection-rule").textContent =
                config.enabled
                    ? ruleTitle(config.selectionRule)
                    : "Ручной выбор";
        }
    }

    function loadStatus(showError) {
        return Promise.all([
            request(STATUS_URL, { method: "GET" }),
            request(SUMMARY_URL, { method: "GET" })
        ]).then(function (values) {
            render(values[0], values[1]);
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
    }

    function saveSettings(event) {
        var payload;

        event.preventDefault();

        payload = {
            enabled: element("auto-switch-enabled").checked,
            failureThreshold: Number(element("auto-switch-threshold").value),
            cooldownMinutes: Number(element("auto-switch-cooldown").value),
            minimumRating: element("auto-switch-minimum").value,
            selectionRule: element("auto-switch-rule").value,
            preferredServerId:
                element("auto-switch-preferred").value || null
        };

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

            return loadStatus(false);
        }).catch(function (error) {
            BROrayUI.toast(errorMessage(error), "error");
        }).then(function () {
            setSaving(false);
        });
    }

    function initialize() {
        createSection();

        if (!element("server-auto-switch-section")) {
            return;
        }

        loadStatus(true);

        window.setInterval(function () {
            loadStatus(false);
        }, 15000);
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize);
    } else {
        initialize();
    }
})();
