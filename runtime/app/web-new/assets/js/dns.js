(function () {
    "use strict";

    // Hotfix r2: account for the real Keenetic DoT/DoH capacity before apply.
    var TEST_TTL_MS = 10 * 60 * 1000;
    var STATUS_REFRESH_INTERVAL_MS = 30000;
    var status = null;
    var selected = Object.create(null);
    var busy = false;
    var selectionInitialized = false;
    var pollTimer = null;

    function byId(id) { return document.getElementById(id); }
    function create(tag, className, text) {
        var node = document.createElement(tag);
        if (className) node.className = className;
        if (text !== undefined && text !== null) node.textContent = String(text);
        return node;
    }
    function setText(id, value) {
        var node = byId(id);
        if (node) node.textContent = value === null || value === undefined || value === "" ? "—" : String(value);
    }
    function unwrap(payload) {
        var error;
        if (payload && payload.success === false) {
            error = new Error(payload.error && payload.error.message ? payload.error.message : "Операция DNS-over-TLS завершилась ошибкой.");
            error.code = payload.error ? payload.error.code : null;
            error.details = payload.error ? payload.error.details : null;
            error.status = payload.status || null;
            throw error;
        }
        return payload && Object.prototype.hasOwnProperty.call(payload, "data") ? payload.data : payload;
    }
    function request(url, options) {
        return window.BROrayUI.apiRequest(url, options || {}).then(unwrap);
    }
    function formatDate(value) {
        var date;
        if (!value) return "Не выполнялась";
        date = new Date(value);
        return isNaN(date.getTime()) ? String(value) : date.toLocaleString("ru-RU");
    }
    function testFresh(test) {
        if (!test || test.ok !== true || !test.testedEpoch) return false;
        return (Date.now() - Number(test.testedEpoch) * 1000) <= TEST_TTL_MS;
    }
    function selectedIds() {
        return Object.keys(selected).filter(function (id) { return selected[id] === true; });
    }
    function selectedServers() {
        var ids = selectedIds();
        return status ? status.servers.filter(function (server) { return ids.indexOf(server.id) >= 0; }) : [];
    }
    function testedSelectedCount() {
        return selectedServers().filter(function (server) { return testFresh(server.test); }).length;
    }
    function presentSelectedCount() {
        return selectedServers().filter(function (server) { return server.present === true; }).length;
    }
    function allSelectedTested() {
        var servers = selectedServers();
        return servers.length > 0 && servers.every(function (server) { return testFresh(server.test); });
    }
    function effectivePort(entry) {
        var port;
        if (!entry) return null;
        port = Number(entry.effectivePort !== undefined && entry.effectivePort !== null ? entry.effectivePort : entry.port);
        return isFinite(port) && port > 0 ? port : null;
    }
    function sameEntry(first, second) {
        return first && second &&
            first.address === second.address &&
            effectivePort(first) === effectivePort(second) &&
            first.sni === second.sni;
    }
    function projectedTotal() {
        var actual = status && status.actual && Array.isArray(status.actual.dot) ? status.actual.dot : [];
        var managed = status && Array.isArray(status.managed) ? status.managed : [];
        var external = actual.filter(function (entry) {
            return !managed.some(function (owned) { return sameEntry(entry, owned); });
        });
        var desiredNotExternal = selectedServers().filter(function (server) {
            return !external.some(function (entry) { return sameEntry(server, entry); });
        });
        var doh = status && status.actual ? Number(status.actual.dohCount || 0) : 0;
        if (status && isFinite(Number(status.projectedTotal))) return Number(status.projectedTotal);
        return doh + external.length + desiredNotExternal.length;
    }
    function selectionOverLimit() {
        return selectedIds().length > Number(status && status.maxServers || 8);
    }
    function capacityExceeded() {
        if (status && typeof status.capacityExceeded === "boolean") return status.capacityExceeded;
        return Boolean(status) && projectedTotal() > Number(status.maxServers || 8);
    }
    function selectionMatches() {
        var servers = selectedServers();
        var managed = status && Array.isArray(status.managed) ? status.managed : [];
        if (status && typeof status.matchesSelection === "boolean") return status.matchesSelection;
        if (!servers.length) return false;
        if (!servers.every(function (server) { return server.present === true; })) return false;
        return managed.every(function (entry) {
            return servers.some(function (server) {
                return sameEntry(server, entry);
            });
        });
    }
    function observationDeterminate() {
        return Boolean(status && status.runningConfigAvailable === true && status.actual && status.actual.determinate === true);
    }
    function installationState() {
        if (!status || !observationDeterminate()) return "unknown";
        if (status.installationState === "installed" || status.installationState === "not-installed") return status.installationState;
        if (status.installed === true) return "installed";
        if (status.installed === false) return "not-installed";
        return "unknown";
    }
    function mutationAvailable() {
        return Boolean(status && observationDeterminate() && status.writeProtocolEnabled === true && status.mutationAvailable === true);
    }
    function mutationBlockText() {
        var reason = status && status.mutationBlockedReason;
        if (status && status.writeProtocolEnabled !== true) return "протокол записи Keenetic CLI не включён в эти bytes";
        if (reason === "recovery-required") return "нужно завершить восстановление предыдущей транзакции";
        if (reason === "keenetic-unavailable") return "Keenetic CLI недоступен";
        if (reason === "dot-observation-underdetermined") return "фактические DoT-записи нельзя однозначно сверить";
        return valueOrDash(reason || "изменение недоступно");
    }
    function quarantinedReceipts() {
        return status && Array.isArray(status.quarantinedReceipts) ? status.quarantinedReceipts : [];
    }
    function deleteAvailable() {
        return mutationAvailable() && status.deleteEligible === true &&
            status.runningConfigAvailable === true && selectedIds().length > 0;
    }
    function recommendedAction() {
        if (!status || busy || !observationDeterminate()) return null;
        if (!selectedIds().length) return "test";
        if (selectionOverLimit() || capacityExceeded()) return null;
        if (!allSelectedTested()) return "test";
        if (!selectionMatches() && mutationAvailable()) return "apply";
        return null;
    }
    function buttonVariant(button, primary) {
        if (!button) return;
        button.classList.remove("button-primary", "button-secondary");
        button.classList.add(primary ? "button-primary" : "button-secondary");
        if (primary) button.setAttribute("data-recommended-action", "true");
        else button.removeAttribute("data-recommended-action");
    }
    function setPageStatus(text, kind, icon) {
        var badge = byId("dns-page-status");
        if (!badge) return;
        badge.className = "status-badge " + (kind === "success" ? "status-badge-success" : kind === "warning" ? "status-badge-warning" : kind === "error" ? "status-badge-danger" : kind === "loading" ? "status-loading" : "status-neutral");
        badge.textContent = text;
        badge.setAttribute("data-icon", icon || "dns");
    }
    function testPresentation(server) {
        if (!server.test) return {text: "Не проверен", className: "status-neutral"};
        if (!testFresh(server.test)) return {text: "Проверка устарела", className: "status-warning"};
        if (server.test.ok === true) {
            return {text: server.test.latencyMs !== null ? "TLS/SNI: OK · " + server.test.latencyMs + " мс" : "TLS/SNI: OK", className: "status-success"};
        }
        if (server.test.status === "unavailable") return {text: "OpenSSL недоступен", className: "status-warning"};
        return {text: "TLS/SNI: ошибка", className: "status-error"};
    }
    function valueOrDash(value) {
        return value === null || value === undefined || value === "" ? "—" : String(value);
    }
    function portPresentation(entry) {
        var port = effectivePort(entry);
        if (entry && entry.portState === "omitted") {
            return "Порт в конфигурации: не указан · эффективный: " + valueOrDash(port);
        }
        return "Порт в конфигурации: " + valueOrDash(entry && entry.portRaw) + " · эффективный: " + valueOrDash(port);
    }
    function classificationPresentation(entry) {
        var value = entry && entry.classification ? String(entry.classification) : "unknown";
        if (value.indexOf("catalog:") === 0) {
            return {text:"В каталоге BROray: " + value.slice(8), className:"status-success"};
        }
        if (value === "catalog-endpoint-with-extra-attributes") {
            return {text:"Каталожный endpoint с дополнительными атрибутами", className:"status-warning"};
        }
        if (value === "not-in-broray-catalog") {
            return {text:"NOT_IN_CATALOG", className:"status-warning"};
        }
        if (value === "ambiguous") {
            return {text:"Неоднозначное совпадение каталога", className:"status-error"};
        }
        if (value === "selector-collision") {
            return {text:"Коллизия селектора адрес + порт", className:"status-error"};
        }
        return {text:"Ошибка разбора записи", className:"status-error"};
    }
    function ownershipPresentation(entry) {
        if (entry && entry.deleteEligible === true) {
            return {text:"Точная запись · удаляется при выборе", className:"status-success"};
        }
        return {text:"Неоднозначная запись · изменение запрещено", className:"status-warning"};
    }
    function actualAttribute(label, value) {
        return create("span", "dns-server-address", label + ": " + valueOrDash(value));
    }
    function parseErrorPresentation(entry) {
        if (!entry || entry.parseError === false || entry.parseError === null || entry.parseError === "") return "нет";
        if (entry.parseError === true) return "строгий разбор не пройден";
        return String(entry.parseError);
    }
    function createActualCard(entry, position) {
        var card = create("div", "dns-server-card");
        var marker = create("span", "status-badge status-neutral", "#" + valueOrDash(entry.index !== undefined ? entry.index : position + 1));
        var header = create("div", "dns-server-header");
        var copy = create("div", "dns-server-copy");
        var badges = create("div", "dns-server-badges");
        var classification = classificationPresentation(entry);
        var ownership = ownershipPresentation(entry);
        var matches = Array.isArray(entry.catalogMatchIds) && entry.catalogMatchIds.length ? entry.catalogMatchIds.join(", ") : "нет";
        var classificationBadge = create("span", "status-badge " + classification.className, classification.text);
        var membershipBadge = create("span", "status-badge " + (entry.inCatalog === true ? "status-success" : "status-warning"), entry.inCatalog === true ? "Член каталога: да" : "Член каталога: нет");
        var ownershipBadge = create("span", "status-badge " + ownership.className, ownership.text);

        marker.setAttribute("data-icon", entry.valid === true ? "dns" : "warning");
        copy.append(
            create("span", "eyebrow", "Установленная DoT-запись"),
            create("strong", "dns-server-name", valueOrDash(entry.address) + ":" + valueOrDash(effectivePort(entry))),
            create("span", "dns-server-address", portPresentation(entry)),
            actualAttribute("SNI", entry.sni),
            actualAttribute("SPKI", entry.spki),
            actualAttribute("Интерфейс (on)", entry.interface !== undefined && entry.interface !== null ? entry.interface : entry.on),
            actualAttribute("Domain", entry.domain),
            actualAttribute("Совпадения каталога", matches),
            actualAttribute("Строгий разбор", entry.valid === true ? "корректен" : "ошибка"),
            actualAttribute("Неизвестных токенов", Number(entry.unknownTokenCount || 0)),
            actualAttribute("Ошибка разбора", parseErrorPresentation(entry))
        );
        badges.append(classificationBadge, membershipBadge, ownershipBadge);
        header.append(copy, badges);
        card.append(marker, header);
        card.classList.toggle("is-installed", true);
        return card;
    }
    function renderActualEntries() {
        var list = byId("dns-actual-list");
        var empty = byId("dns-actual-empty");
        var actual = status && status.actual && Array.isArray(status.actual.dot) ? status.actual.dot : [];
        if (!list || !empty) return;
        list.replaceChildren();
        empty.hidden = actual.length > 0;
        actual.forEach(function (entry, index) { list.appendChild(createActualCard(entry, index)); });
    }
    function createServerCard(server) {
        var label = create("label", "dns-server-card");
        var checkbox = create("input");
        var header = create("div", "dns-server-header");
        var copy = create("div", "dns-server-copy");
        var badges = create("div", "dns-server-badges");
        var test = testPresentation(server);
        var known = observationDeterminate() && installationState() !== "unknown";
        var testBadge = create("span", "status-badge " + test.className, test.text);
        var ownership = create("span", "dns-server-ownership", !known ? "Состояние не определено" : server.present ? "Установлен в Keenetic" : "Не добавлен");

        checkbox.type = "checkbox";
        checkbox.checked = selected[server.id] === true;
        checkbox.disabled = busy;
        checkbox.setAttribute("aria-label", "Выбрать " + server.name);
        checkbox.addEventListener("change", function () {
            selected[server.id] = checkbox.checked;
            render();
        });

        copy.append(
            create("span", "eyebrow", server.provider),
            create("strong", "dns-server-name", server.name),
            create("span", "dns-server-address", "Адрес: " + server.address + ":" + valueOrDash(effectivePort(server))),
            create("span", "dns-server-sni", "TLS-домен: " + server.sni)
        );
        testBadge.setAttribute("data-icon", test.className === "status-success" ? "status" : "dns");
        badges.append(testBadge, ownership);
        header.append(copy, badges);
        label.append(checkbox, header);
        label.classList.toggle("is-selected", checkbox.checked);
        label.classList.toggle("is-installed", known && server.present === true);
        return label;
    }
    function stateMessage() {
        var ids = selectedIds();
        var tested = testedSelectedCount();
        var present = presentSelectedCount();
        if (!status) return {text:"Получение состояния…", kind:"neutral", badge:"Загрузка…"};
        if (status.runningConfigAvailable !== true) return {text:"Не удалось прочитать фактическую конфигурацию Keenetic. Состояние установки неизвестно; установка и удаление заблокированы.", kind:"error", badge:"Состояние неизвестно"};
        if (!observationDeterminate() || installationState() === "unknown") return {text:"Фактические DoT-записи прочитаны не полностью или не удалось однозначно разобрать и сверить их с runtime. Состояние установки неизвестно; установка и удаление заблокированы.", kind:"error", badge:"Состояние неизвестно"};
        if (!ids.length) return {text:"Выберите один или несколько серверов. Для устойчивой работы рекомендуется несколько серверов разных провайдеров.", kind:"neutral", badge:"Требуется выбор"};
        if (selectionOverLimit()) return {text:"Сохранено " + ids.length + " выбранных серверов, но Keenetic поддерживает максимум " + Number(status.maxServers || 8) + ". Фактически BROray управляет " + Number((status.effectiveIds || []).length || (status.managed || []).length) + ". Снимите выбор минимум с " + (ids.length - Number(status.maxServers || 8)) + " сервера. Текущая конфигурация Keenetic не изменяется.", kind:"warning", badge:"Выбор превышает лимит"};
        if (capacityExceeded()) return {text:"После экспорта будет " + projectedTotal() + " из " + Number(status.maxServers || 8) + " DoT/DoH-серверов. Снимите выбор минимум с " + (projectedTotal() - Number(status.maxServers || 8)) + " сервера или удалите лишнюю внешнюю запись в Keenetic.", kind:"error", badge:"Превышен лимит Keenetic"};
        if (selectionMatches()) {
            if (tested === ids.length) return {text:"Установлено " + present + " из " + ids.length + ". Фактическая конфигурация Keenetic соответствует выбранной.", kind:"success", badge:"Установлено"};
            if (status.testAvailable !== true) return {text:"Установлено " + present + " из " + ids.length + ". Фактическая конфигурация Keenetic соответствует выбранной, но OpenSSL недоступен для повторной TLS/SNI-проверки.", kind:"warning", badge:"Установлено"};
            return {text:"Установлено " + present + " из " + ids.length + ". Фактическая конфигурация Keenetic соответствует выбранной. TLS/SNI-проверка актуальна для " + tested + " из " + ids.length + ".", kind:"warning", badge:"Установлено"};
        }
        if (status.testAvailable !== true) return {text:"OpenSSL недоступен. Установите зависимость openssl-util, чтобы выполнить TLS/SNI-проверку.", kind:"error", badge:"Нет OpenSSL"};
        if (tested !== ids.length) return {text:"Проверено " + tested + " из " + ids.length + ". Перед установкой каждый выбранный сервер должен пройти TLS/SNI-проверку.", kind:"warning", badge:"Требуется проверка"};
        if (!mutationAvailable()) return {text:"Изменение DoT заблокировано: " + mutationBlockText() + ".", kind:"error", badge:status.writeProtocolEnabled === true ? "Изменения заблокированы" : "Протокол записи не включён"};
        return {text:"Все выбранные серверы проверены. Следующее действие — установить конфигурацию в Keenetic.", kind:"warning", badge:"Готово к установке"};
    }
    function renderOperation() {
        var panel = byId("dns-operation");
        if (!panel) return;
        panel.hidden = !busy;
        if (busy) {
            byId("dns-operation-progress").removeAttribute("value");
        }
    }
    function render() {
        var list = byId("dns-server-list");
        var ids = selectedIds();
        var tested = testedSelectedCount();
        var present = presentSelectedCount();
        var message = stateMessage();
        var recommended = recommendedAction();
        var notice = byId("dns-notice");
        var buttons;
        var external;

        if (!status || !list) return;
        list.replaceChildren();
        status.servers.forEach(function (server) { list.appendChild(createServerCard(server)); });
        renderActualEntries();

        setText("dns-support", status.runningConfigAvailable === true ? "Доступна" : "Недоступна");
        setText("dns-selected", ids.length);
        setText("dns-tested", tested + " из " + ids.length);
        setText("dns-installed", installationState() === "unknown" ? "Неизвестно" : present + " из " + ids.length);
        setText("dns-detail-selected", ids.length);
        setText("dns-detail-managed", Number(status.managedPresentCount || 0) + " из " + Number((status.managed || []).length));
        external = status.externalDotCount !== undefined ? status.externalDotCount : Math.max(0, Number(status.actual.dot.length || 0) - Number((status.managed || []).length));
        setText("dns-detail-external-dot", external);
        setText("dns-detail-doh", Number(status.actual.dohCount || 0));
        setText("dns-detail-total", Number(status.actual.totalSecure || 0) + " из " + Number(status.maxServers || 8));
        setText("dns-detail-capacity", Number(status.maxServers || 8));
        setText("dns-detail-write-protocol", status.writeProtocolEnabled === true ? "Включён в R14C01 bytes" : "Не включён — изменения заблокированы");
        setText("dns-detail-delete-gate", deleteAvailable() ? "Разрешено для точных выбранных записей" : valueOrDash(status.deleteBlockedReason || "Удаление заблокировано"));
        setText("dns-detail-quarantined", quarantinedReceipts().length);
        setText("dns-detail-tested-at", formatDate(status.lastTestedAt));
        setText("dns-detail-applied-at", formatDate(status.lastAppliedAt));
        setText("dns-summary-message", message.text);
        setText("dns-notice-message", message.text);
        setText("dns-selection-hint", ids.length ? "Выбрано: " + ids.length + " · максимум: " + Number(status.maxServers || 8) : "Выберите серверы");
        setText("dns-action-state", message.badge);

        notice.className = "dns-notice status-" + message.kind;
        setPageStatus(message.badge, message.kind, message.kind === "error" ? "warning" : "dns");

        buttons = {
            test: byId("dns-test"),
            apply: byId("dns-apply"),
            refresh: byId("dns-refresh"),
            delete: byId("dns-delete")
        };
        buttons.test.disabled = busy || !ids.length || selectionOverLimit() || status.testAvailable !== true;
        buttons.apply.disabled = busy || !ids.length || selectionOverLimit() || capacityExceeded() || !allSelectedTested() || status.runningConfigAvailable !== true || !mutationAvailable() || selectionMatches();
        buttons.refresh.disabled = busy;
        buttons.delete.disabled = busy || !deleteAvailable();
        buttonVariant(buttons.test, recommended === "test");
        buttonVariant(buttons.apply, recommended === "apply");
        buttonVariant(buttons.refresh, false);
        renderOperation();
        if (window.BROrayIcons) window.BROrayIcons.scan(document);
    }
    function initializeSelection(data) {
        if (selectionInitialized) return;
        (data.selectedIds || []).forEach(function (id) { selected[id] = true; });
        selectionInitialized = true;
    }
    function handleError(error) {
        if (error && (error.status === 401 || error.code === "AUTH_REQUIRED" || error.code === "SESSION_REQUIRED")) {
            window.BROrayUI.redirectToLogin();
            return;
        }
        window.BROrayUI.toast(error && error.message ? error.message : "Операция DNS-over-TLS завершилась ошибкой.", "error");
    }
    function loadStatus(silent, force) {
        return request("/api/routes/dot-status.cgi" + (force ? "?force=1" : ""), {method:"GET", credentials:"same-origin"}).then(function (data) {
            status = data;
            initializeSelection(data);
            render();
            return data;
        }).catch(function (error) {
            if (!silent) handleError(error);
            throw error;
        });
    }
    function setBusy(value, title, message) {
        busy = value;
        if (title) setText("dns-operation-title", title);
        if (message) setText("dns-operation-message", message);
        render();
    }
    function runAction(action, payload) {
        var endpoint = action === "test" ? "dot-test.cgi" : action === "apply" ? "dot-apply.cgi" : "dot-delete.cgi";
        var title = action === "test" ? "Проверка TLS/SNI" : action === "apply" ? "Установка в Keenetic" : "Удаление выбранных записей";
        setBusy(true, title, "Подождите. После завершения состояние обновится автоматически.");
        return request("/api/routes/" + endpoint, {
            method:"POST",
            credentials:"same-origin",
            headers:{"Accept":"application/json", "Content-Type":"application/json"},
            body: payload ? JSON.stringify(payload) : undefined
        }).then(function (data) {
            status = data;
            if (action === "test") window.BROrayUI.toast("TLS/SNI-проверка выбранных серверов завершена.", "success");
            if (action === "apply") window.BROrayUI.toast("DNS-over-TLS установлен и фактически проверен в Keenetic.", "success");
            if (action === "delete") window.BROrayUI.toast("Точные выбранные DNS-over-TLS записи удалены. Остальные DoT/DoH сохранены.", "success");
        }).catch(handleError).then(function () {
            busy = false;
            return loadStatus(true, false).catch(function () { render(); });
        });
    }
    function confirmAction(options) {
        if (!window.BROrayDialogs || typeof window.BROrayDialogs.confirm !== "function") {
            return Promise.reject(new Error("Окно подтверждения недоступно."));
        }
        return window.BROrayDialogs.confirm(options);
    }
    function onTest() {
        var ids = selectedIds();
        if (!ids.length || selectionOverLimit() || busy) return;
        runAction("test", {serverIds:ids, allowUntested:false});
    }
    function onApply() {
        var ids = selectedIds();
        if (!ids.length || selectionOverLimit() || capacityExceeded() || !allSelectedTested() || !mutationAvailable() || busy) return;
        confirmAction({
            eyebrow:"DNS-over-TLS",
            title:"Установить подтверждённую конфигурацию",
            message:"После операции в Keenetic будет " + projectedTotal() + " из " + Number(status.maxServers || 8) + " DoT/DoH-серверов. Сторонние записи не удаляются и не переходят под управление BROray.",
            confirmText:"Установить",
            cancelText:"Отмена",
            variant:"primary",
            icon:"dns"
        }).then(function (confirmed) {
            if (confirmed) return runAction("apply", {serverIds:ids, allowUntested:false});
            return null;
        }).catch(handleError);
    }
    function onDelete() {
        if (busy || !status || !deleteAvailable()) return;
        confirmAction({
            eyebrow:"Опасное действие",
            title:"Удалить выбранные записи DNS-over-TLS",
            message:"Будут удалены точные выбранные записи независимо от их происхождения. Не выбранные DoT/DoH-записи сохранятся.",
            confirmText:"Удалить",
            cancelText:"Отмена",
            variant:"danger",
            icon:"delete"
        }).then(function (confirmed) {
            if (confirmed) return runAction("delete", null);
            return null;
        }).catch(handleError);
    }
    function reveal(session) {
        var app = byId("app");
        var loader = byId("page-loader");
        var user = byId("current-user");
        if (user) user.textContent = session && session.user ? session.user : "admin";
        if (loader) loader.hidden = true;
        if (app) app.hidden = false;
    }
    function startBackgroundRefresh() {
        if (pollTimer || document.hidden) return;
        pollTimer = window.setTimeout(function () {
            pollTimer = null;
            if (busy) {
                startBackgroundRefresh();
                return;
            }
            loadStatus(true, false).catch(function () { return null; }).then(startBackgroundRefresh);
        }, STATUS_REFRESH_INTERVAL_MS);
    }
    function stopBackgroundRefresh() {
        if (!pollTimer) return;
        window.clearTimeout(pollTimer);
        pollTimer = null;
    }
    function onVisibilityChange() {
        if (document.hidden) {
            stopBackgroundRefresh();
            return;
        }
        if (busy) {
            startBackgroundRefresh();
            return;
        }
        loadStatus(true, false).catch(function () { return null; }).then(startBackgroundRefresh);
    }
    function initialize() {
        byId("dns-test").addEventListener("click", onTest);
        byId("dns-apply").addEventListener("click", onApply);
        byId("dns-refresh").addEventListener("click", function () {
            if (busy) return;
            setBusy(true, "Обновление состояния", "Чтение фактической конфигурации Keenetic…");
            loadStatus(false, true).then(function () { busy = false; render(); }).catch(function () { busy = false; render(); });
        });
        byId("dns-delete").addEventListener("click", onDelete);
        document.addEventListener("visibilitychange", onVisibilityChange);
        request("/api/session.cgi", {method:"GET", credentials:"same-origin"}).then(function (session) {
            reveal(session);
            return loadStatus(false, false);
        }).then(startBackgroundRefresh).catch(function (error) {
            reveal(null);
            handleError(error);
        });
    }

    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", initialize, {once:true});
    else initialize();
})();
