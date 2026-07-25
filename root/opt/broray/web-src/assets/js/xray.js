(function () {
    "use strict";

    if (window.BROrayXrayInitialized) {
        return;
    }
    window.BROrayXrayInitialized = true;

    var app = document.getElementById("app");
    var loader = document.getElementById("page-loader");
    var currentUser = document.getElementById("current-user");
    var operationTimer = null;
    var controlsLocked = false;
    var latestStatus = null;

    function byId(id) {
        return document.getElementById(id);
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

    function request(url, options) {
        return window.BROrayUI.apiRequest(url, options).catch(function (error) {
            if (error.status === 401) {
                window.BROrayUI.redirectToLogin();
            }
            throw error;
        });
    }

    function ensureButtonParts(button) {
        var label;
        var spinner;
        var text = "";
        var nodes;

        if (!button) {
            return null;
        }

        label = button.querySelector(".button-label");
        if (!label) {
            nodes = Array.prototype.slice.call(button.childNodes);
            nodes.forEach(function (node) {
                if (node.nodeType === 3 && node.nodeValue.trim()) {
                    text += (text ? " " : "") + node.nodeValue.trim();
                    node.remove();
                }
            });
            label = document.createElement("span");
            label.className = "button-label";
            label.textContent = text || button.getAttribute("aria-label") || "Действие";
            button.appendChild(label);
        }

        spinner = button.querySelector(".button-spinner");
        if (!spinner) {
            spinner = document.createElement("span");
            spinner.className = "button-spinner";
            spinner.setAttribute("aria-hidden", "true");
            button.appendChild(spinner);
        }

        return {label: label, spinner: spinner};
    }

    function setButtonBusy(button, busy, label) {
        var parts = ensureButtonParts(button);

        if (!parts) {
            return;
        }

        if (busy) {
            if (!button.dataset.originalLabel) {
                button.dataset.originalLabel = parts.label.textContent;
            }
            button.dataset.disabledBeforeBusy = button.disabled ? "true" : "false";
            parts.label.textContent = label || "Выполнение…";
            button.classList.add("is-loading");
            button.disabled = true;
            button.setAttribute("aria-busy", "true");
        } else {
            parts.label.textContent = button.dataset.originalLabel || parts.label.textContent;
            button.classList.remove("is-loading");
            button.disabled = button.dataset.disabledBeforeBusy === "true";
            button.removeAttribute("aria-busy");
            delete button.dataset.originalLabel;
            delete button.dataset.disabledBeforeBusy;
        }
    }

    function formatBytes(bytes) {
        var value = Number(bytes || 0);
        if (value >= 1024 * 1024) {
            return (value / 1024 / 1024).toFixed(1) + " МБ";
        }
        if (value >= 1024) {
            return (value / 1024).toFixed(1) + " КБ";
        }
        return value + " Б";
    }

    function formatStorage(kilobytes) {
        var value = Number(kilobytes || 0);
        if (value >= 1024) {
            return (value / 1024).toFixed(1) + " МБ";
        }
        return value + " КБ";
    }

    function setStateText(element, text, tone) {
        if (!element) {
            return;
        }
        element.textContent = text;
        element.className = "state-text state-" + (tone || "neutral");
    }

    function applyControlState() {
        var running = latestStatus && latestStatus.running === true;
        var start = byId("xray-start");
        var stop = byId("xray-stop");
        var restart = byId("xray-restart");
        var reinstall = byId("xray-reinstall");

        if (controlsLocked) {
            [start, stop, restart, reinstall].forEach(function (control) {
                if (control) {
                    control.disabled = true;
                }
            });
            return;
        }

        start.disabled = running;
        stop.disabled = !running;
        restart.disabled = !running;
        reinstall.disabled = false;
    }

    function setXrayControlsLocked(locked) {
        controlsLocked = Boolean(locked);
        applyControlState();
    }

    function renderStatus(data) {
        var running = data.running === true;
        var live = byId("xray-live-status");
        var badge = byId("xray-state-badge");

        latestStatus = data;

        live.className = "xray-live-status " + (running ? "is-running" : "is-stopped");
        byId("xray-live-text").textContent = running ? "Xray работает" : "Xray остановлен";

        badge.textContent = running ? "Работает" : "Остановлен";
        badge.className = "status-badge " + (running ? "status-badge-success" : "status-badge-neutral");

        byId("xray-version").textContent = data.version ? "Xray " + data.version : "Xray";
        byId("xray-runtime-description").textContent = running
            ? "Процесс запущен и обслуживает локальный SOCKS-интерфейс."
            : "Процесс Xray сейчас не запущен.";
        byId("metric-pid").textContent = data.pid || "—";
        byId("metric-process").textContent = running ? "Процесс активен" : "Процесс отсутствует";
        byId("metric-socks").textContent = (data.socksAddress || "—") + ":" + (data.socksPort || "—");
        byId("metric-socks-status").textContent = running ? "Локальный интерфейс" : "Недоступно";
        byId("metric-architecture").textContent = data.architecture || "—";
        byId("metric-device-architecture").textContent = "Устройство: " + (data.deviceArchitecture || "—");
        byId("metric-storage").textContent = formatStorage(data.storageFreeKb);
        byId("config-path").textContent = data.configPath || "—";
        byId("config-size").textContent = formatBytes(data.configSizeBytes);
        byId("config-sha").textContent = data.configSha256 || "—";
        setStateText(
            byId("config-validity"),
            data.configValid ? "Настроено" : "Требуется исправление",
            data.configValid ? "success" : "warning"
        );
        byId("installed-version").textContent = data.version || "—";

        applyControlState();
    }

    async function loadStatus(showToast) {
        try {
            var payload = await request("/api/xray/status.cgi", {method: "GET"});
            renderStatus(payload.data || {});
            if (showToast) {
                window.BROrayUI.toast("Состояние обновлено.", "success");
            }
        } catch (error) {
            window.BROrayUI.toast(errorMessage(error), "error");
        } finally {
            applyControlState();
        }
    }

    function showConfirm(options) {
        if (!window.BROrayDialogs) {
            return Promise.reject(
                new Error("Окно подтверждения BROray недоступно.")
            );
        }

        return window.BROrayDialogs.confirm({
            eyebrow: "Управление Xray",
            title: options.title || "Подтвердите действие",
            message: options.message || "Продолжить операцию?",
            confirmText: options.acceptLabel || "Продолжить",
            variant: options.danger === false ? "primary" : "danger",
            icon: options.danger === false ? "update" : "security"
        });
    }

    async function runAction(endpoint, button, options) {
        var confirmed = options.confirm ? await showConfirm(options.confirm) : true;
        var keepLocked = false;

        if (!confirmed) {
            return;
        }

        setXrayControlsLocked(true);
        setButtonBusy(button, true, options.busyLabel);

        try {
            var payload = await request("/api/xray/" + endpoint, {method: "POST", body: {}});
            window.BROrayUI.toast(options.successMessage || "Операция выполнена.", "success");

            if (options.background === true) {
                keepLocked = true;
                showOperation();
                pollOperation(true);
            } else {
                await loadStatus(false);
            }
            return payload;
        } catch (error) {
            window.BROrayUI.toast(errorMessage(error), "error");
        } finally {
            setButtonBusy(button, false);
            if (!keepLocked) {
                setXrayControlsLocked(false);
                await loadStatus(false);
            }
        }
    }

    async function checkConfig() {
        var button = byId("xray-check-config");
        var output = byId("config-check-output");

        setButtonBusy(button, true, "Проверка…");
        try {
            var payload = await request("/api/xray/check-config.cgi", {method: "POST", body: {}});
            var data = payload.data || {};
            setStateText(
                byId("config-validity"),
                data.valid ? "Проверка пройдена" : "Требуется исправление",
                data.valid ? "success" : "error"
            );
            output.textContent = data.output || "";
            output.hidden = !data.output;
            window.BROrayUI.toast(
                data.valid ? "Проверка конфигурации завершена." : "В конфигурации обнаружена ошибка.",
                data.valid ? "success" : "error"
            );
        } catch (error) {
            window.BROrayUI.toast(errorMessage(error), "error");
        } finally {
            setButtonBusy(button, false);
        }
    }

    async function checkUpdate() {
        var button = byId("xray-update-check");

        setButtonBusy(button, true, "Проверка…");
        try {
            var payload = await request("/api/xray/update-check.cgi", {method: "POST", body: {}});
            var data = payload.data || {};
            byId("installed-version").textContent = data.currentVersion || "—";
            byId("available-version").textContent = data.latestVersion || "—";
            byId("release-channel").textContent = data.channel === "pre-release"
                ? "Предварительный"
                : (data.channel || "Стабильный");
            byId("update-message").textContent = data.message || (
                data.updateAvailable ? "Доступна новая версия." : "Установлена актуальная версия."
            );
            window.BROrayUI.toast(
                data.updateAvailable ? "Доступно обновление Xray." : "Установлена актуальная версия.",
                data.updateAvailable ? "success" : "success"
            );
        } catch (error) {
            window.BROrayUI.toast(errorMessage(error), "error");
        } finally {
            setButtonBusy(button, false);
        }
    }

    async function runDiagnostics() {
        var button = byId("xray-diagnostics");
        var list = byId("diagnostics-list");
        var summary = byId("diagnostics-summary");

        setButtonBusy(button, true, "Диагностика…");
        try {
            var payload = await request("/api/xray/diagnostics.cgi", {method: "GET"});
            var data = payload.data || {};
            var checks = Array.isArray(data.checks) ? data.checks : [];
            var totals = data.summary || {};

            summary.textContent = "Успешно: " + (totals.ok || 0) +
                " · Предупреждения: " + (totals.warning || 0) +
                " · Ошибки: " + (totals.error || 0);
            list.textContent = "";

            checks.forEach(function (check) {
                var item = document.createElement("div");
                var indicator = document.createElement("span");
                var copy = document.createElement("div");
                var title = document.createElement("strong");
                var details = document.createElement("small");

                item.className = "diagnostic-item";
                indicator.className = "diagnostic-indicator diagnostic-" + (check.status || "warning");
                indicator.setAttribute("aria-hidden", "true");
                title.textContent = check.title || check.id || "Проверка";
                details.textContent = check.details || "";
                copy.appendChild(title);
                copy.appendChild(details);
                item.appendChild(indicator);
                item.appendChild(copy);
                list.appendChild(item);
            });

            window.BROrayUI.toast(
                (totals.error || 0) === 0 ? "Диагностика завершена." : "Диагностика обнаружила ошибки.",
                (totals.error || 0) === 0 ? "success" : "error"
            );
        } catch (error) {
            window.BROrayUI.toast(errorMessage(error), "error");
        } finally {
            setButtonBusy(button, false);
        }
    }

    function showOperation() {
        byId("operation-section").hidden = false;
    }

    function hideOperation() {
        byId("operation-section").hidden = true;
    }

    async function pollOperation(notifyResult) {
        if (operationTimer) {
            window.clearTimeout(operationTimer);
        }

        try {
            var payload = await request("/api/xray/operation-status.cgi", {method: "GET"});
            var data = payload.data || {};
            var output = byId("operation-log");

            if (data.operationRunning) {
                showOperation();
                setXrayControlsLocked(true);
                byId("operation-title").textContent = "Переустановка…";
                byId("operation-message").textContent = "Не отключайте питание роутера.";
                if (data.logTail) {
                    output.textContent = data.logTail;
                    output.hidden = false;
                }
                operationTimer = window.setTimeout(function () {
                    pollOperation(true);
                }, 2000);
                return;
            }

            if (data.logTail) {
                output.textContent = data.logTail;
                output.hidden = false;
            }

            if (data.result && notifyResult === true) {
                var success = data.result.success === true;
                showOperation();
                byId("operation-title").textContent = success ? "Операция завершена" : "Операция завершилась ошибкой";
                byId("operation-message").textContent = success
                    ? "Xray установлен и запущен."
                    : (data.result.error || "Не удалось завершить операцию.");
                window.BROrayUI.toast(
                    success ? "Переустановка Xray завершена." : "Не удалось переустановить Xray.",
                    success ? "success" : "error"
                );
            } else if (!data.result) {
                hideOperation();
            } else if (notifyResult !== true) {
                hideOperation();
            }

            setXrayControlsLocked(false);
            await loadStatus(false);
        } catch (error) {
            showOperation();
            byId("operation-message").textContent = errorMessage(error);
            setXrayControlsLocked(false);
        }
    }

    function bindEvents() {
        byId("xray-check-config").addEventListener("click", checkConfig);
        byId("xray-update-check").addEventListener("click", checkUpdate);
        byId("xray-diagnostics").addEventListener("click", runDiagnostics);

        byId("xray-start").addEventListener("click", function (event) {
            runAction("start.cgi", event.currentTarget, {
                busyLabel: "Запуск…",
                successMessage: "Xray запущен."
            });
        });
        byId("xray-stop").addEventListener("click", function (event) {
            runAction("stop.cgi", event.currentTarget, {
                busyLabel: "Остановка…",
                successMessage: "Xray остановлен.",
                confirm: {
                    title: "Остановить Xray?",
                    message: "Прокси-соединение будет недоступно до повторного запуска.",
                    acceptLabel: "Остановить",
                    danger: true
                }
            });
        });
        byId("xray-restart").addEventListener("click", function (event) {
            runAction("restart.cgi", event.currentTarget, {
                busyLabel: "Перезапуск…",
                successMessage: "Xray перезапущен.",
                confirm: {
                    title: "Перезапустить Xray?",
                    message: "Прокси-соединение кратковременно прервётся.",
                    acceptLabel: "Перезапустить",
                    danger: false
                }
            });
        });
        byId("xray-reinstall").addEventListener("click", function (event) {
            runAction("reinstall.cgi", event.currentTarget, {
                busyLabel: "Запуск…",
                successMessage: "Переустановка запущена.",
                background: true,
                confirm: {
                    title: "Переустановить Xray?",
                    message: "Будет загружена и безопасно переустановлена текущая официальная версия. Соединение кратковременно прервётся.",
                    acceptLabel: "Переустановить",
                    danger: true
                }
            });
        });

    }

    async function initialize() {
        bindEvents();

        try {
            var session = await request("/api/session.cgi", {method: "GET"});
            currentUser.textContent = session.user || "admin";
            loader.hidden = true;
            app.hidden = false;
            await loadStatus(false);
            pollOperation(false);
        } catch (error) {
            window.BROrayUI.redirectToLogin();
        }
    }

    initialize();

    window.setInterval(function () {
        request("/api/session.cgi", {method: "GET"}).catch(function () {
            return;
        });
    }, 60000);
})();
