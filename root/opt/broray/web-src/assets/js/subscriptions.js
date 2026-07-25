(function () {
    "use strict";

    if (window.BROraySubscriptionsInitialized) {
        return;
    }
    window.BROraySubscriptionsInitialized = true;

    var paths = {
        list: "/api/subscriptions/list.cgi",
        details: "/api/subscriptions/details.cgi",
        create: "/api/subscriptions/create.cgi",
        update: "/api/subscriptions/update.cgi",
        refresh: "/api/subscriptions/refresh.cgi",
        remove: "/api/subscriptions/delete.cgi",
        servers: "/api/subscriptions/servers.cgi",
        summary: "/api/subscriptions/summary.cgi"
    };

    var state = {
        subscriptions: [],
        editingId: null,
        openServersId: null,
        serverCache: new Map(),
        polling: null
    };

    function byId(id) {
        return document.getElementById(id);
    }

    var app = byId("app");
    var loader = byId("page-loader");
    var currentUser = byId("current-user");
    var listElement = byId("subscriptions-list");
    var formPanel = byId("subscription-form-panel");
    var form = byId("subscription-form");

    function escapeHtml(value) {
        return String(value == null ? "" : value)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;")
            .replace(/'/g, "&#039;");
    }

    function toast(message, type) {
        window.BROrayUI.toast(message, type || "success");
    }

    async function api(url, options) {
        var request = Object.assign({
            credentials: "same-origin",
            cache: "no-store",
            headers: {"Accept": "application/json"}
        }, options || {});
        var response;
        var payload;
        var apiError;
        var error;

        if (request.body && typeof request.body !== "string") {
            request.headers = Object.assign({}, request.headers, {
                "Content-Type": "application/json"
            });
            request.body = JSON.stringify(request.body);
        }

        response = await fetch(url, request);

        try {
            payload = await response.json();
        } catch (parseError) {
            throw new Error("Сервер вернул некорректный ответ.");
        }

        if (response.status === 401) {
            window.BROrayUI.redirectToLogin();
            throw new Error("Сессия завершена.");
        }

        if (!response.ok || payload.success === false) {
            apiError = payload && payload.error ? payload.error : null;
            error = new Error(
                apiError && apiError.message
                    ? apiError.message
                    : "Операция завершилась ошибкой."
            );
            error.code = apiError && apiError.code ? apiError.code : "HTTP_ERROR";
            error.status = response.status;
            throw error;
        }

        return payload && Object.prototype.hasOwnProperty.call(payload, "data")
            ? payload.data
            : payload;
    }

    function formatDate(value) {
        var date;

        if (!value) {
            return "—";
        }
        date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return String(value);
        }
        return new Intl.DateTimeFormat("ru-RU", {
            dateStyle: "short",
            timeStyle: "medium"
        }).format(date);
    }

    function statusPresentation(status) {
        return ({
            never: {label: "Не обновлено", tone: "neutral"},
            running: {label: "Обновляется", tone: "loading"},
            success: {label: "Обновлено", tone: "success"},
            partial: {label: "Обновлено частично", tone: "warning"},
            error: {label: "Ошибка", tone: "error"}
        })[status] || {label: "Не определено", tone: "neutral"};
    }

    function setButtonBusy(button, busy, busyLabel) {
        var label;

        if (!button) {
            return;
        }

        label = button.querySelector(".button-label");
        if (!button.hasAttribute("data-default-label")) {
            button.setAttribute(
                "data-default-label",
                label ? label.textContent : button.textContent.trim()
            );
        }

        button.disabled = Boolean(busy);
        button.classList.toggle("is-loading", Boolean(busy));
        button.setAttribute("aria-busy", busy ? "true" : "false");

        if (label) {
            label.textContent = busy
                ? busyLabel
                : button.getAttribute("data-default-label");
        }
    }

    function renderSummary(summary) {
        var presentation = statusPresentation(summary.lastUpdateStatus);

        byId("subscriptions-total").textContent = summary.total == null ? "—" : summary.total;
        byId("subscriptions-enabled").textContent = summary.enabled == null ? "—" : summary.enabled;
        byId("subscriptions-servers").textContent = summary.serversReceived == null ? "—" : summary.serversReceived;
        byId("subscriptions-last-status").textContent = presentation.label;
        byId("subscriptions-last-time").textContent = formatDate(summary.lastUpdatedAt);
    }

    function renderResult(item) {
        var result = item.lastUpdateResult;

        if (!result) {
            return "Обновление ещё не выполнялось.";
        }
        if (result.errorCode) {
            return "Код: " + escapeHtml(result.errorCode) +
                " · длительность: " + escapeHtml(result.durationMs || 0) + " мс";
        }
        return [
            "получено " + (result.received || 0),
            "принято " + (result.accepted || 0),
            "добавлено " + (result.added || 0),
            "обновлено " + (result.updated || 0),
            "удалено " + (result.removed || 0),
            "отклонено " + (result.rejected || 0)
        ].join(" · ");
    }

    function renderNodes(subscriptionId) {
        var key = String(subscriptionId);
        var cached;
        var items;

        if (String(state.openServersId) !== key) {
            return "";
        }

        cached = state.serverCache.get(key);
        if (!cached) {
            return '<div class="subscription-nodes subscription-loading"><span class="loader-spinner" aria-hidden="true"></span><span>Загрузка серверов…</span></div>';
        }
        if (!cached.items || !cached.items.length) {
            return '<div class="subscription-nodes subscription-empty-compact">Серверы отсутствуют.</div>';
        }

        items = cached.items.map(function (server) {
            var details = [
                server.protocol,
                server.address && server.port ? server.address + ":" + server.port : server.address,
                server.network
            ].filter(Boolean).join(" · ");

            return '<div class="subscription-node">' +
                '<div class="subscription-node-copy">' +
                    '<strong>' + escapeHtml(server.name) + '</strong>' +
                    '<small>' + escapeHtml(details) + '</small>' +
                '</div>' +
                '<div class="subscription-badges">' +
                    (server.active ? '<span class="status-badge status-success">Активен</span>' : '') +
                    (!server.enabled ? '<span class="status-badge status-neutral">Отключён</span>' : '') +
                '</div>' +
            '</div>';
        }).join("");

        return '<div class="subscription-nodes">' +
            '<div class="subscription-nodes-heading"><strong>Серверы</strong><span>' + escapeHtml(cached.total) + '</span></div>' +
            '<div class="subscription-node-list">' + items + '</div>' +
        '</div>';
    }

    function actionButton(className, action, icon, label, disabled, loading) {
        return '<button class="button ' + className + (loading ? ' is-loading' : '') + '" type="button" data-action="' + action + '"' +
            (icon ? ' data-icon="' + icon + '"' : '') +
            (disabled ? ' disabled' : '') +
            '><span class="button-label">' + escapeHtml(label) + '</span>' +
            '<span class="button-spinner" aria-hidden="true"></span></button>';
    }

    function renderSubscriptions() {
        if (!state.subscriptions.length) {
            listElement.innerHTML = '<div class="subscription-empty">' +
                '<span class="subscription-empty-icon" data-icon="subscriptions" aria-hidden="true"></span>' +
                '<h2>Подписок пока нет</h2>' +
                '<p>Добавьте URL провайдера, чтобы импортировать серверы.</p>' +
            '</div>';
            listElement.setAttribute("aria-busy", "false");
            return;
        }

        listElement.innerHTML = state.subscriptions.map(function (item) {
            var running = item.lastUpdateStatus === "running";
            var update = statusPresentation(item.lastUpdateStatus);
            var enabled = item.enabled
                ? {label: "Активна", tone: "success"}
                : {label: "Отключена", tone: "neutral"};
            var auto = item.autoUpdateEnabled
                ? {label: "Автообновление включено", tone: "success"}
                : {label: "Автообновление отключено", tone: "neutral"};
            var id = escapeHtml(item.id);
            var serversOpen = String(state.openServersId) === String(item.id);

            return '<article class="subscription-card' + (running ? ' subscription-card-running' : '') + '" data-id="' + id + '">' +
                '<div class="subscription-card-header">' +
                    '<div class="subscription-card-title">' +
                        '<span class="subscription-card-icon" data-icon="subscriptions" aria-hidden="true"></span>' +
                        '<div><h2>' + escapeHtml(item.name) + '</h2><p class="subscription-url">' + escapeHtml(item.displayUrl) + '</p></div>' +
                    '</div>' +
                    '<div class="subscription-badges">' +
                        '<span class="status-badge status-' + update.tone + '">' + escapeHtml(update.label) + '</span>' +
                        '<span class="status-badge status-' + enabled.tone + '">' + enabled.label + '</span>' +
                        '<span class="status-badge status-' + auto.tone + '">' + auto.label + '</span>' +
                    '</div>' +
                '</div>' +
                '<dl class="subscription-card-meta">' +
                    '<div><dt>Серверы</dt><dd>' + escapeHtml(item.serversCount) + '</dd></div>' +
                    '<div><dt>Интервал</dt><dd>' + escapeHtml(item.updateIntervalMinutes) + ' мин.</dd></div>' +
                    '<div><dt>Последнее обновление</dt><dd>' + escapeHtml(formatDate(item.lastUpdatedAt)) + '</dd></div>' +
                    '<div><dt>Следующее обновление</dt><dd>' + escapeHtml(formatDate(item.nextUpdateAt)) + '</dd></div>' +
                '</dl>' +
                '<div class="subscription-result">' + renderResult(item) + '</div>' +
                (item.lastError ? '<div class="subscription-error"><strong>Ошибка обновления</strong><span>' + escapeHtml(item.lastError) + '</span></div>' : '') +
                '<div class="subscription-card-actions">' +
                    actionButton('button-primary', 'refresh', 'update', running ? 'Обновляется' : 'Обновить сейчас', running, running) +
                    actionButton('button-secondary', 'servers', 'servers', serversOpen ? 'Скрыть серверы' : 'Показать серверы', false, false) +
                    actionButton('button-secondary', 'edit', 'edit', 'Изменить', running, false) +
                    actionButton('button-secondary', 'toggle-enabled', 'settings', item.enabled ? 'Отключить' : 'Включить', running, false) +
                    actionButton('button-secondary', 'toggle-auto', 'update', item.autoUpdateEnabled ? 'Отключить автообновление' : 'Включить автообновление', running, false) +
                    actionButton('button-danger-outline', 'delete', 'delete', 'Удалить', running, false) +
                '</div>' +
                renderNodes(item.id) +
            '</article>';
        }).join("");

        listElement.setAttribute("aria-busy", "false");
    }

    async function loadAll(silent) {
        var result;
        var hasRunning;

        if (!silent) {
            listElement.setAttribute("aria-busy", "true");
            listElement.innerHTML = '<div class="subscription-loading"><span class="loader-spinner" aria-hidden="true"></span><span>Загрузка подписок…</span></div>';
        }

        try {
            result = await Promise.all([api(paths.list), api(paths.summary)]);
            state.subscriptions = Array.isArray(result[0]) ? result[0] : [];
            renderSummary(result[1] || {});
            renderSubscriptions();
            hasRunning = state.subscriptions.some(function (item) {
                return item.lastUpdateStatus === "running";
            });
            window.clearTimeout(state.polling);
            if (hasRunning) {
                state.polling = window.setTimeout(function () {
                    loadAll(true);
                }, 3000);
            }
        } catch (error) {
            listElement.setAttribute("aria-busy", "false");
            listElement.innerHTML = '<div class="subscription-error-panel"><h2>Не удалось загрузить подписки</h2><p>' + escapeHtml(error.message) + '</p></div>';
        }
    }

    function resetForm() {
        state.editingId = null;
        form.reset();
        byId("subscription-form-title").textContent = "Добавить подписку";
        byId("subscription-enabled").checked = true;
        byId("subscription-auto").checked = true;
        byId("subscription-immediate").checked = true;
        byId("subscription-interval").value = "360";
        byId("subscription-immediate-row").hidden = false;
        formPanel.hidden = true;
    }

    async function openEdit(id) {
        var item;

        try {
            item = await api(paths.details + "?id=" + encodeURIComponent(id));
            state.editingId = String(id);
            byId("subscription-form-title").textContent = "Изменить подписку";
            byId("subscription-name").value = item.name || "";
            byId("subscription-url").value = item.url || "";
            byId("subscription-interval").value = item.updateIntervalMinutes || 360;
            byId("subscription-enabled").checked = item.enabled === true;
            byId("subscription-auto").checked = item.autoUpdateEnabled === true;
            byId("subscription-immediate-row").hidden = true;
            formPanel.hidden = false;
            byId("subscription-name").focus();
            formPanel.scrollIntoView({behavior: "smooth", block: "start"});
        } catch (error) {
            toast(error.message, "error");
        }
    }

    async function saveForm(event) {
        var submit = byId("subscription-submit");
        var body;

        event.preventDefault();
        body = {
            name: byId("subscription-name").value.trim(),
            url: byId("subscription-url").value.trim(),
            updateIntervalMinutes: Number(byId("subscription-interval").value),
            enabled: byId("subscription-enabled").checked,
            autoUpdateEnabled: byId("subscription-auto").checked
        };
        if (!state.editingId) {
            body.updateImmediately = byId("subscription-immediate").checked;
        }

        setButtonBusy(submit, true, "Сохранение…");
        try {
            if (state.editingId) {
                await api(paths.update + "?id=" + encodeURIComponent(state.editingId), {
                    method: "POST",
                    body: body
                });
                toast("Настройки подписки сохранены.");
            } else {
                await api(paths.create, {method: "POST", body: body});
                toast("Подписка добавлена.");
            }
            resetForm();
            await loadAll(false);
        } catch (error) {
            toast(error.message, "error");
        } finally {
            setButtonBusy(submit, false, "Сохранение…");
        }
    }

    async function patchSubscription(id, patch, successMessage) {
        try {
            await api(paths.update + "?id=" + encodeURIComponent(id), {
                method: "POST",
                body: patch
            });
            toast(successMessage);
            await loadAll(true);
        } catch (error) {
            toast(error.message, "error");
        }
    }

    async function refreshSubscription(id, button) {
        setButtonBusy(button, true, "Обновление…");
        try {
            await api(paths.refresh + "?id=" + encodeURIComponent(id), {
                method: "POST",
                body: {}
            });
            toast("Обновление подписки запущено.");
        } catch (error) {
            toast(error.message, "error");
        } finally {
            await loadAll(true);
        }
    }

    async function toggleServers(id) {
        var key = String(id);
        var servers;

        if (String(state.openServersId) === key) {
            state.openServersId = null;
            renderSubscriptions();
            return;
        }

        state.openServersId = key;
        renderSubscriptions();
        try {
            servers = await api(paths.servers + "?id=" + encodeURIComponent(id));
            state.serverCache.set(key, servers || {items: [], total: 0});
        } catch (error) {
            state.serverCache.set(key, {items: [], total: 0});
            toast(error.message, "error");
        }
        renderSubscriptions();
    }

    async function removeSubscription(item) {
        var serverText = Number(item.serversCount) > 0
            ? " Вместе с ней будут удалены серверы этого источника: " + item.serversCount + "."
            : "";
        var confirmed;

        try {
            confirmed = await window.BROrayDialogs.confirm({
                eyebrow: "Опасное действие",
                title: "Удалить подписку?",
                message: "Подписка «" + item.name + "» будет удалена." + serverText,
                confirmText: "Удалить",
                variant: "danger"
            });
        } catch (error) {
            toast(error.message, "error");
            return;
        }

        if (!confirmed) {
            return;
        }

        try {
            await api(paths.remove + "?id=" + encodeURIComponent(item.id), {
                method: "POST",
                body: {}
            });
            state.serverCache.delete(String(item.id));
            toast("Подписка удалена.");
            await loadAll(false);
        } catch (error) {
            toast(error.message, "error");
        }
    }

    function bindEvents() {
        listElement.addEventListener("click", function (event) {
            var button = event.target.closest("button[data-action]");
            var card;
            var item;

            if (!button) {
                return;
            }
            card = button.closest("[data-id]");
            if (!card) {
                return;
            }
            item = state.subscriptions.find(function (entry) {
                return String(entry.id) === String(card.dataset.id);
            });
            if (!item) {
                return;
            }

            switch (button.dataset.action) {
                case "refresh":
                    refreshSubscription(item.id, button);
                    break;
                case "servers":
                    toggleServers(item.id);
                    break;
                case "edit":
                    openEdit(item.id);
                    break;
                case "toggle-enabled":
                    patchSubscription(
                        item.id,
                        {enabled: !item.enabled},
                        item.enabled ? "Подписка отключена." : "Подписка включена."
                    );
                    break;
                case "toggle-auto":
                    patchSubscription(
                        item.id,
                        {autoUpdateEnabled: !item.autoUpdateEnabled},
                        item.autoUpdateEnabled
                            ? "Автообновление отключено."
                            : "Автообновление включено."
                    );
                    break;
                case "delete":
                    removeSubscription(item);
                    break;
            }
        });

        form.addEventListener("submit", saveForm);
        byId("subscription-cancel").addEventListener("click", resetForm);
        byId("add-subscription").addEventListener("click", function () {
            resetForm();
            formPanel.hidden = false;
            byId("subscription-name").focus();
        });
    }

    async function initialize() {
        var session;

        bindEvents();
        try {
            session = await api("/api/session.cgi", {method: "GET"});
            if (!session || session.ok !== true) {
                throw new Error("AUTH_REQUIRED");
            }
            currentUser.textContent = session.user || "admin";
            loader.hidden = true;
            app.hidden = false;
            resetForm();
            await loadAll(false);
        } catch (error) {
            window.BROrayUI.redirectToLogin();
        }
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize, {once: true});
    } else {
        initialize();
    }
})();
