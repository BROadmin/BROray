(function () {
    "use strict";

    if (window.BROrayPageInitialized) {
        return;
    }
    window.BROrayPageInitialized = true;

    var state = {
        info: null,
        pollTimer: null,
        busy: false
    };
    var app = document.getElementById("app");
    var loader = document.getElementById("page-loader");

    function byId(id) {
        return document.getElementById(id);
    }

    function setText(id, value) {
        var element = byId(id);

        if (element) {
            element.textContent = value == null || value === ""
                ? "—"
                : String(value);
        }
    }

    function formatDate(value) {
        var date;

        if (!value) {
            return "—";
        }

        date = new Date(value);
        return Number.isNaN(date.getTime())
            ? String(value)
            : date.toLocaleString("ru-RU");
    }

    function errorMessage(error) {
        if (error && error.payload && error.payload.error) {
            return error.payload.error.message ||
                error.payload.error.details ||
                "Операция завершилась ошибкой.";
        }

        return error && error.message
            ? error.message
            : "Неизвестная ошибка.";
    }

    function toast(message, type) {
        if (window.BROrayUI && typeof window.BROrayUI.toast === "function") {
            window.BROrayUI.toast(message, type || "info");
        }
    }

    async function request(path, options) {
        var response = await fetch(path, Object.assign({
            credentials: "same-origin",
            cache: "no-store",
            headers: {
                "Accept": "application/json",
                "X-BROray-Request": "1"
            }
        }, options || {}));
        var text = await response.text();
        var data;
        var message;

        try {
            data = JSON.parse(text);
        } catch (error) {
            throw new Error("Backend вернул некорректный JSON.");
        }

        if (response.status === 401) {
            window.location.replace("/");
            throw new Error("Сессия завершена.");
        }

        if (!response.ok || data.ok === false || data.success === false) {
            message = data && data.error && (
                data.error.message || data.error.details
            );
            throw new Error(message || data.message || "Операция завершилась ошибкой.");
        }

        return data.data && data.success === true ? data.data : data;
    }

    function ensureButtonParts(button) {
        var label;
        var spinner;

        if (!button) {
            return null;
        }

        label = button.querySelector(".button-label");
        if (!label) {
            label = document.createElement("span");
            label.className = "button-label";
            label.textContent = button.textContent.trim() || "Действие";
            button.replaceChildren(label);
        }

        spinner = button.querySelector(".button-spinner");
        if (!spinner) {
            spinner = document.createElement("span");
            spinner.className = "button-spinner";
            spinner.setAttribute("aria-hidden", "true");
            button.appendChild(spinner);
        }

        return { label: label, spinner: spinner };
    }

    function setButtonBusy(button, busy, busyLabel) {
        var parts = ensureButtonParts(button);

        if (!parts) {
            return;
        }

        if (busy) {
            if (!button.dataset.idleLabel) {
                button.dataset.idleLabel = parts.label.textContent;
            }
            parts.label.textContent = busyLabel || "Выполнение…";
            button.classList.add("is-loading");
            button.setAttribute("aria-busy", "true");
            button.disabled = true;
            return;
        }

        if (button.dataset.idleLabel) {
            parts.label.textContent = button.dataset.idleLabel;
            delete button.dataset.idleLabel;
        }
        button.classList.remove("is-loading");
        button.removeAttribute("aria-busy");
    }

    function applyControlState() {
        var info = state.info;

        byId("check-update").disabled = state.busy;
        byId("install-update").disabled = state.busy || !info || !info.updateAvailable;
        byId("restore-backup").disabled = state.busy || !info || !info.lastBackup;
        byId("uninstall-normal").disabled = state.busy;
        byId("uninstall-full").disabled = state.busy;
    }

    function setBusy(busy) {
        state.busy = Boolean(busy);
        applyControlState();
    }

    function renderComponents(components) {
        var list = byId("component-list");

        list.replaceChildren();
        (components || []).forEach(function (component) {
            var row = document.createElement("div");
            var indicator = document.createElement("span");
            var copy = document.createElement("div");
            var heading = document.createElement("div");
            var name = document.createElement("strong");
            var version = document.createElement("span");
            var path = document.createElement("small");
            var healthy = component.healthy !== false;

            row.className = "broray-component-row";
            indicator.className = "broray-component-indicator " + (
                healthy ? "is-healthy" : "is-error"
            );
            indicator.title = healthy ? "Компонент установлен" : "Компонент недоступен";
            indicator.setAttribute("aria-label", indicator.title);

            copy.className = "broray-component-copy";
            heading.className = "broray-component-heading";
            name.textContent = component.name || component.id || "Компонент";
            version.className = "broray-component-version";
            version.textContent = component.version || "—";
            path.textContent = component.path || "Путь не указан";

            heading.append(name, version);
            copy.append(heading, path);
            row.append(indicator, copy);
            list.appendChild(row);
        });
    }

    function protocolName(id) {
        return {
            vless: "VLESS",
            vmess: "VMess",
            trojan: "Trojan",
            hysteria2: "Hysteria2"
        }[id] || id || "—";
    }

    function renderProtocols(protocols) {
        var list = byId("protocol-list");

        list.replaceChildren();
        (protocols || []).forEach(function (protocol) {
            var item = document.createElement("div");
            var name = document.createElement("strong");
            var status = document.createElement("span");
            var supported = protocol.supported !== false;

            item.className = "broray-protocol-item";
            name.textContent = protocolName(protocol.id);
            status.className = "status-badge " + (
                supported ? "status-success" : "status-error"
            );
            status.textContent = supported ? "Установлено" : "Недоступно";
            item.append(name, status);
            list.appendChild(item);
        });
    }

    function renderCapabilities(capabilities) {
        var list = byId("capability-list");

        list.replaceChildren();
        (capabilities || []).forEach(function (capability) {
            var item = document.createElement("li");
            var icon = document.createElement("span");
            var text = document.createElement("span");

            icon.className = "broray-capability-icon";
            icon.setAttribute("data-icon", "security");
            icon.setAttribute("aria-hidden", "true");
            text.textContent = capability;
            item.append(icon, text);
            list.appendChild(item);
        });
    }

    function applyLink(id, url) {
        var link = byId(id);

        if (link && url) {
            link.href = url;
        }
    }

    function renderInfo(info) {
        var installation = byId("installation-status");
        var buildId = document.documentElement.getAttribute("data-build-id") ||
            document.body.getAttribute("data-webui-build") || "—";

        state.info = info;
        setText("current-version", info.version);
        setText("build-description", "Сборка: " + (info.build || "не определена"));
        setText("architecture", info.architecture);
        setText("update-channel", info.updateChannel === "stable" ? "Стабильный" : info.updateChannel);
        setText(
            "available-version",
            info.updateAvailable ? (info.availableVersion || "Доступно") : "Не требуется"
        );
        setText("last-update-check", formatDate(info.lastCheckedAt));
        setText("webui-build", buildId);

        installation.textContent = info.installationHealthy
            ? "Установлено"
            : "Установка повреждена";
        installation.className = "status-badge " + (
            info.installationHealthy ? "status-success" : "status-error"
        );
        installation.setAttribute("data-icon", "status");

        renderComponents(info.components);
        renderProtocols(info.protocols);
        renderCapabilities(info.capabilities);
        applyLink("project-link", info.links && info.links.project);
        applyLink("github-link", info.links && info.links.github);
        applyLink("donate-link", info.links && info.links.donate);
        applyControlState();
    }

    function operationLabel(operation) {
        return {
            update: "Обновление",
            restore: "Восстановление",
            uninstall: "Удаление"
        }[operation] || "Операция";
    }

    function renderOperation(operation) {
        var badge = byId("operation-state");
        var progress = Math.max(0, Math.min(100, Number(operation.progress) || 0));
        var progressBox = byId("operation-progress");
        var progressBar = byId("operation-progress-bar");
        var error = byId("operation-error");
        var labels = {
            idle: "Нет операций",
            queued: "В очереди",
            running: operationLabel(operation.operation) + "…",
            restoring: "Восстановление…",
            success: "Завершено",
            error: "Ошибка"
        };
        var statusClass = "status-neutral";

        setText("operation-message", operation.message || "Операции ещё не выполнялись.");
        setText("operation-log", operation.logTail || "Журнал пока пуст.");
        setText("operation-progress-value", progress + "%");
        progressBar.value = progress;
        progressBar.textContent = progress + "%";
        progressBox.hidden = !operation.running && progress === 0;

        error.hidden = !operation.error;
        error.textContent = operation.error || "";

        if (operation.state === "success") {
            statusClass = "status-success";
        } else if (operation.state === "error") {
            statusClass = "status-error";
        } else if (operation.running || operation.state === "queued" || operation.state === "restoring") {
            statusClass = "status-loading";
        }

        badge.textContent = labels[operation.state] || operation.state || "Нет операций";
        badge.className = "status-badge " + statusClass;
        badge.setAttribute("data-icon", operation.state === "error" ? "close" : "logs");

        setBusy(Boolean(operation.running));
        if (operation.running) {
            startPolling();
        } else {
            stopPolling();
        }
    }

    function showPageError(message) {
        var error = byId("page-error");

        error.textContent = message;
        error.hidden = false;
    }

    function hidePageError() {
        byId("page-error").hidden = true;
    }

    async function loadInfo() {
        var info = await request("/api/broray/info.cgi", { method: "GET" });

        renderInfo(info);
        return info;
    }

    async function loadOperation() {
        var operation = await request("/api/broray/update-status.cgi", { method: "GET" });

        renderOperation(operation);
        return operation;
    }

    async function refresh() {
        hidePageError();
        try {
            await Promise.all([loadInfo(), loadOperation()]);
        } catch (error) {
            showPageError(errorMessage(error));
        }
    }

    function startPolling() {
        if (state.pollTimer) {
            return;
        }

        state.pollTimer = window.setInterval(async function () {
            try {
                var operation = await loadOperation();

                if (!operation.running) {
                    await loadInfo();
                    toast(
                        operation.message || "Операция завершена.",
                        operation.state === "success" ? "success" : "error"
                    );
                }
            } catch (error) {
                stopPolling();
                showPageError(errorMessage(error));
            }
        }, 2000);
    }

    function stopPolling() {
        if (state.pollTimer) {
            window.clearInterval(state.pollTimer);
            state.pollTimer = null;
        }
    }

    async function startAction(path, successMessage, body, button, busyLabel) {
        var options = { method: "POST" };

        if (state.busy) {
            return;
        }

        hidePageError();
        setBusy(true);
        setButtonBusy(button, true, busyLabel);

        if (body) {
            options.headers = {
                "Accept": "application/json",
                "Content-Type": "application/json",
                "X-BROray-Request": "1"
            };
            options.body = JSON.stringify(body);
        }

        try {
            await request(path, options);
            toast(successMessage, "success");
            await loadOperation();
        } catch (error) {
            setBusy(false);
            showPageError(errorMessage(error));
            toast(errorMessage(error), "error");
        } finally {
            setButtonBusy(button, false);
            applyControlState();
        }
    }

    async function checkUpdate(event) {
        var button = event.currentTarget;

        if (state.busy) {
            return;
        }

        hidePageError();
        setBusy(true);
        setButtonBusy(button, true, "Проверка…");
        try {
            var result = await request("/api/broray/update-check.cgi", { method: "POST" });

            await loadInfo();
            toast(
                result.updateAvailable
                    ? "Доступна версия " + result.availableVersion + "."
                    : "Установлена актуальная версия.",
                result.updateAvailable ? "info" : "success"
            );
        } catch (error) {
            showPageError(errorMessage(error));
            toast(errorMessage(error), "error");
        } finally {
            setBusy(false);
            setButtonBusy(button, false);
            applyControlState();
        }
    }

    async function confirmAction(options) {
        if (!window.BROrayDialogs) {
            throw new Error("Окно подтверждения BROray недоступно.");
        }

        return window.BROrayDialogs.confirm(options);
    }

    async function installUpdate(event) {
        var confirmed = await confirmAction({
            eyebrow: "Обновление",
            title: "Установить обновление BROray?",
            message: "Перед установкой будет создана резервная копия. WebUI может кратковременно стать недоступен.",
            confirmText: "Установить",
            variant: "primary",
            icon: "update"
        });

        if (confirmed) {
            startAction(
                "/api/broray/update-start.cgi",
                "Обновление запущено.",
                null,
                event.currentTarget,
                "Установка…"
            );
        }
    }

    async function restoreBackup(event) {
        var confirmed = await confirmAction({
            eyebrow: "Восстановление",
            title: "Восстановить резервную копию?",
            message: "Текущие файлы BROray будут заменены последней доступной резервной копией.",
            confirmText: "Восстановить",
            variant: "primary",
            icon: "restore"
        });

        if (confirmed) {
            startAction(
                "/api/broray/restore.cgi",
                "Восстановление запущено.",
                null,
                event.currentTarget,
                "Восстановление…"
            );
        }
    }

    async function uninstall(mode, button) {
        var full = mode === "full";
        var phrase = full ? "УДАЛИТЬ BROray ПОЛНОСТЬЮ" : "УДАЛИТЬ BROray";
        var confirmed;

        if (!window.BROrayDialogs) {
            throw new Error("Окно подтверждения BROray недоступно.");
        }

        confirmed = await window.BROrayDialogs.confirmPhrase({
            eyebrow: "Необратимое действие",
            title: full ? "Полностью удалить BROray?" : "Удалить BROray?",
            message: full
                ? "Будут безвозвратно удалены программа, настройки, подписки, серверы, маршруты, управляемый ProxyN, KeenDNS HTTP Proxy и служебные данные."
                : "Программа и созданные ею рабочие объекты будут удалены. Перед удалением будет создана резервная копия пользовательских данных.",
            phrase: phrase,
            inputLabel: "Введите контрольную фразу",
            inputHint: "Введите точно: " + phrase,
            mismatchText: "Контрольная фраза не совпадает.",
            confirmText: full ? "Удалить полностью" : "Удалить BROray",
            icon: "delete"
        });

        if (!confirmed) {
            return;
        }

        startAction(
            "/api/broray/uninstall.cgi",
            "Удаление запущено. WebUI станет недоступен.",
            { mode: mode, confirmation: phrase },
            button,
            "Удаление…"
        );
    }

    function bind() {
        byId("check-update").addEventListener("click", checkUpdate);
        byId("install-update").addEventListener("click", installUpdate);
        byId("restore-backup").addEventListener("click", restoreBackup);
        byId("uninstall-normal").addEventListener("click", function (event) {
            uninstall("normal", event.currentTarget).catch(function (error) {
                toast(errorMessage(error), "error");
            });
        });
        byId("uninstall-full").addEventListener("click", function (event) {
            uninstall("full", event.currentTarget).catch(function (error) {
                toast(errorMessage(error), "error");
            });
        });
    }

    async function initialize() {
        try {
            var session = await request("/api/session.cgi", { method: "GET" });

            setText("current-user", session.user || "admin");
            bind();
            loader.hidden = true;
            app.hidden = false;
            await refresh();
        } catch (error) {
            if (error.message !== "Сессия завершена.") {
                loader.hidden = true;
                app.hidden = false;
                showPageError(errorMessage(error));
            }
        }
    }

    window.addEventListener("pagehide", stopPolling);

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize, { once: true });
    } else {
        initialize();
    }
})();
