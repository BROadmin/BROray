(function () {
    "use strict";

    var status = null;
    var selected = Object.create(null);
    var initialized = false;
    var busy = false;
    var TEST_TTL_MS = 10 * 60 * 1000;

    function byId(id) { return document.getElementById(id); }
    function text(id, value) { var node = byId(id); if (node) node.textContent = value === null || value === undefined || value === "" ? "—" : String(value); }
    function formatDate(value) {
        if (!value) return "—";
        var date = new Date(value);
        return isNaN(date.getTime()) ? String(value) : date.toLocaleString("ru-RU");
    }
    function iconScan(node) { if (window.BROrayIcons) window.BROrayIcons.scan(node || document); }

    function selectedIds() {
        return Object.keys(selected).filter(function (id) { return selected[id] === true; });
    }

    function serverTestFresh(test, nowMs) {
        if (!test || !test.testedEpoch) return false;
        return (Number(nowMs) - Number(test.testedEpoch) * 1000) <= TEST_TTL_MS;
    }

    function testFresh(test) {
        return serverTestFresh(test, Date.now());
    }

    function recommendedAction(data, ids, nowMs) {
        var chosen;
        var testedOk;
        var matches;
        if (!data || data.runningConfigAvailable !== true) return "refresh";
        if (!ids || !ids.length) return "test";
        chosen = (data.servers || []).filter(function (server) { return ids.indexOf(server.id) !== -1; });
        testedOk = chosen.length === ids.length && chosen.every(function (server) {
            return server.test && server.test.ok === true && serverTestFresh(server.test, nowMs);
        });
        if (data.drift === true) return testedOk ? "apply" : "test";
        if (!testedOk) return "test";
        matches = chosen.every(function (server) { return server.present === true; }) &&
            (data.managed || []).every(function (managed) {
                return chosen.some(function (server) { return server.address === managed.address && server.sni === managed.sni; });
            });
        return matches ? "test" : "apply";
    }

    function testPresentation(server) {
        var test = server.test;
        if (!test) return { text: "Не проверен", className: "is-warning" };
        if (!testFresh(test)) return { text: "Проверка устарела", className: "is-warning" };
        if (test.ok === true) return { text: test.latencyMs !== null ? "Доступен · " + test.latencyMs + " мс" : "Доступен", className: "is-ok" };
        if (test.status === "unavailable") return { text: "Проверка недоступна", className: "is-warning" };
        return { text: "Ошибка TLS", className: "is-error" };
    }

    function createServer(server) {
        var label = document.createElement("label");
        var input = document.createElement("input");
        var copy = document.createElement("span");
        var name = document.createElement("strong");
        var provider = document.createElement("span");
        var endpoint = document.createElement("code");
        var state = document.createElement("span");
        var test = document.createElement("span");
        var installed = document.createElement("span");
        var presentation = testPresentation(server);

        label.className = "routes-dot-server" + (selected[server.id] ? " is-selected" : "");
        input.type = "checkbox";
        input.checked = selected[server.id] === true;
        input.dataset.serverId = server.id;
        input.setAttribute("aria-label", "Выбрать " + server.name);
        input.addEventListener("change", function () {
            selected[server.id] = input.checked;
            render();
        });

        copy.className = "routes-dot-server-copy";
        name.textContent = server.name;
        provider.textContent = server.provider;
        endpoint.textContent = server.address + " · " + server.sni;
        copy.append(name, provider, endpoint);

        state.className = "routes-dot-server-state";
        test.className = presentation.className;
        test.textContent = presentation.text;
        installed.className = server.present ? "is-ok" : "is-warning";
        installed.textContent = server.present ? (server.managed ? "Управляется BROray" : "Уже есть в Keenetic") : "Не установлен";
        state.append(test, installed);
        label.append(input, copy, state);
        return label;
    }

    function selectedServers() {
        if (!status) return [];
        return status.servers.filter(function (server) { return selected[server.id] === true; });
    }

    function desiredMatches() {
        var servers = selectedServers();
        if (!servers.length) return false;
        return servers.every(function (server) { return server.present === true; }) &&
            status.managed.every(function (managed) {
                return servers.some(function (server) { return server.address === managed.address && server.sni === managed.sni; });
            });
    }

    function allSelectedTestedOk() {
        var servers = selectedServers();
        return servers.length > 0 && servers.every(function (server) { return server.test && server.test.ok === true && testFresh(server.test); });
    }

    function setRecommended(id) {
        ["routes-dot-test", "routes-dot-apply", "routes-dot-refresh"].forEach(function (buttonId) {
            var button = byId(buttonId);
            if (!button) return;
            button.classList.remove("button-primary");
            button.classList.add("button-secondary");
            button.removeAttribute("data-recommended-action");
        });
        var target = byId(id);
        if (target) {
            target.classList.remove("button-secondary");
            target.classList.add("button-primary");
            target.setAttribute("data-recommended-action", "true");
        }
    }

    function setBadge(kind, message) {
        var badge = byId("routes-dot-status");
        var notice = byId("routes-dot-notice");
        if (badge) {
            badge.className = "status-badge " + (kind === "success" ? "status-badge-success" : kind === "warning" ? "status-badge-warning" : kind === "error" ? "status-badge-danger" : "status-badge-neutral");
            badge.textContent = kind === "success" ? "Настроено" : kind === "warning" ? "Требует действия" : kind === "error" ? "Ошибка" : "Не настроено";
            badge.setAttribute("data-icon", kind === "error" ? "warning" : "status");
        }
        if (notice) notice.className = "routes-dot-notice status-" + (kind === "neutral" ? "neutral" : kind);
        text("routes-dot-message", message);
        iconScan(badge);
    }

    function render() {
        var list = byId("routes-dot-servers");
        var ids = selectedIds();
        var servers;
        var tested;
        var installed;
        var external;
        var message;
        var kind;
        var recommended;

        if (!status || !list) return;
        list.textContent = "";
        status.servers.forEach(function (server) { list.appendChild(createServer(server)); });
        servers = selectedServers();
        tested = servers.filter(function (server) { return server.test && server.test.ok === true && testFresh(server.test); }).length;
        installed = servers.filter(function (server) { return server.present === true; }).length;
        external = Math.max(0, Number(status.actual.dot.length || 0) - Number(status.managed.length || 0));

        text("routes-dot-selected", ids.length);
        text("routes-dot-tested", tested + " из " + ids.length);
        text("routes-dot-installed", installed + " из " + ids.length);
        text("routes-dot-capacity", status.actual.totalSecure + " из " + status.maxServers);
        text("routes-dot-last-tested", formatDate(status.lastTestedAt));
        text("routes-dot-last-applied", formatDate(status.lastAppliedAt));
        text("routes-dot-last-deleted", formatDate(status.lastDeletedAt));
        text("routes-dot-external-dot", external);
        text("routes-dot-doh", status.actual.dohCount);

        if (status.runningConfigAvailable !== true) {
            kind = "error";
            message = "Не удалось прочитать конфигурацию Keenetic. Экспорт и удаление недоступны.";
            recommended = "routes-dot-refresh";
        } else if (!ids.length) {
            kind = "warning";
            message = "Выберите хотя бы один DNS-over-TLS сервер. Для устойчивости рекомендуется несколько провайдеров.";
            recommended = "routes-dot-test";
        } else if (status.drift === true) {
            kind = "warning";
            message = "Часть записей BROray отсутствует в Keenetic. Проверьте серверы и восстановите конфигурацию.";
            recommended = allSelectedTestedOk() ? "routes-dot-apply" : "routes-dot-test";
        } else if (!allSelectedTestedOk()) {
            kind = "warning";
            message = "Перед экспортом проверьте TLS-соединение и имя сертификата выбранных серверов.";
            recommended = "routes-dot-test";
        } else if (!desiredMatches()) {
            kind = "warning";
            message = "Проверка завершена. Выбранную конфигурацию можно экспортировать в Keenetic.";
            recommended = "routes-dot-apply";
        } else {
            kind = "success";
            message = "DNS-over-TLS настроен и соответствует выбранному списку. Чужие DoT/DoH записи не изменяются.";
            recommended = "routes-dot-test";
        }
        if (status.lastError && status.lastError.message) {
            kind = "error";
            message = status.lastError.message;
        }
        setBadge(kind, message);
        setRecommended(recommended);

        ["routes-dot-test", "routes-dot-apply", "routes-dot-refresh", "routes-dot-delete"].forEach(function (id) {
            var button = byId(id);
            if (button) button.disabled = busy || (id !== "routes-dot-refresh" && status.runningConfigAvailable !== true) || ((id === "routes-dot-test" || id === "routes-dot-apply") && !ids.length) || (id === "routes-dot-delete" && !status.managed.length);
        });
        iconScan(list);
    }

    function handleError(error) {
        if (error && error.status === 401 && window.BROrayUI) {
            window.BROrayUI.redirectToLogin();
            return;
        }
        setBadge("error", error && error.message ? error.message : "Операция DNS-over-TLS завершилась ошибкой.");
        if (window.BROrayUI) window.BROrayUI.toast(error && error.message ? error.message : "Ошибка DNS-over-TLS.", "error");
    }

    async function loadStatus(preserveSelection) {
        var payload = await window.BROrayUI.apiRequest("/api/routes/dot-status.cgi", { method: "GET" });
        status = payload.data;
        if (!initialized || preserveSelection !== true) {
            selected = Object.create(null);
            (status.selectedIds || []).forEach(function (id) { selected[id] = true; });
            initialized = true;
        }
        render();
    }

    async function runAction(action, body) {
        busy = true;
        render();
        try {
            var payload = await window.BROrayUI.apiRequest("/api/routes/dot-" + action + ".cgi", { method: "POST", body: body || {} });
            status = payload.data;
            if (window.BROrayUI) window.BROrayUI.toast(action === "test" ? "Проверка DNS-over-TLS завершена." : action === "apply" ? "DNS-over-TLS экспортирован в Keenetic." : "Записи DNS-over-TLS BROray удалены.", "success");
        } finally {
            busy = false;
            render();
        }
    }

    async function testServers() {
        try { await runAction("test", { serverIds: selectedIds(), allowUntested: false }); }
        catch (error) { handleError(error); }
    }

    async function applyServers() {
        var ids = selectedIds();
        var providers = Object.create(null);
        selectedServers().forEach(function (server) { providers[server.provider] = true; });
        if (Object.keys(providers).length < 2 && window.BROrayDialogs) {
            var oneProvider = await window.BROrayDialogs.confirm({
                eyebrow: "DNS-over-TLS",
                title: "Использовать одного провайдера?",
                message: "Для устойчивой работы рекомендуется выбрать серверы разных DNS-провайдеров. Продолжить с текущим выбором?",
                confirmText: "Продолжить",
                icon: "security"
            });
            if (!oneProvider) return;
        }
        try {
            await runAction("apply", { serverIds: ids, allowUntested: false });
        } catch (error) {
            if (error && error.code === "DOT_TEST_CONFIRMATION_REQUIRED" && window.BROrayDialogs) {
                var confirmed = await window.BROrayDialogs.confirm({
                    eyebrow: "Предупреждение",
                    title: "Экспортировать непроверенные серверы?",
                    message: "Один или несколько выбранных серверов не прошли свежую TLS-проверку. Экспорт может нарушить разрешение DNS. Продолжить осознанно?",
                    confirmText: "Экспортировать",
                    icon: "warning"
                });
                if (confirmed) {
                    try { await runAction("apply", { serverIds: ids, allowUntested: true }); }
                    catch (secondError) { handleError(secondError); }
                }
                return;
            }
            handleError(error);
        }
    }

    async function deleteServers() {
        var confirmed = true;
        if (window.BROrayDialogs) {
            confirmed = await window.BROrayDialogs.confirm({
                eyebrow: "Опасное действие",
                title: "Удалить DNS-over-TLS BROray?",
                message: "Будут удалены только записи, которые создал BROray. Другие DoT/DoH серверы Keenetic сохранятся.",
                confirmText: "Удалить",
                variant: "danger",
                icon: "delete"
            });
        }
        if (!confirmed) return;
        try { await runAction("delete", {}); }
        catch (error) { handleError(error); }
    }

    function bind() {
        var test = byId("routes-dot-test");
        var apply = byId("routes-dot-apply");
        var refresh = byId("routes-dot-refresh");
        var remove = byId("routes-dot-delete");
        if (test) test.addEventListener("click", testServers);
        if (apply) apply.addEventListener("click", applyServers);
        if (refresh) refresh.addEventListener("click", function () { busy = true; render(); loadStatus(true).catch(handleError).finally(function () { busy = false; render(); }); });
        if (remove) remove.addEventListener("click", deleteServers);
    }


    if (window.BROrayDotTestMode) {
        window.BROrayDotTestHooks = {
            serverTestFresh: serverTestFresh,
            recommendedAction: recommendedAction,
            testPresentation: testPresentation
        };
    }

    document.addEventListener("DOMContentLoaded", function () {
        if (!byId("routes-dot-card") || !window.BROrayUI) return;
        bind();
        loadStatus(false).catch(handleError);
    });
})();
