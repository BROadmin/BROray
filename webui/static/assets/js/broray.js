(function () {
    "use strict";

    if (window.BROrayPageInitialized) {
        return;
    }
    window.BROrayPageInitialized = true;

    var state = {
        info: null,
        pollTimer: null,
        pollInFlight: false,
        pollFailures: 0,
        operationId: null,
        operationKind: null,
        unconfirmedPolls: 0,
        busy: false,
        cleanupPlan: null,
        backupPlan: null,
        backupCopy: null
    };
    var app = document.getElementById("app");
    var loader = document.getElementById("page-loader");

    function byId(id) {
        return document.getElementById(id);
    }

    var uninstallAccessMessage = "Удаление из WebUI доступно только по локальному IPv4-адресу роутера. Через внешний адрес KeenDNS оно заблокировано: при потере связи нельзя проверить состояние роутера без локального доступа.";

    function isLocalUninstallHost() {
        var parts = String(window.location.hostname || "").split(".");
        var octets = [];
        var i;

        if (parts.length !== 4) {
            return false;
        }
        for (i = 0; i < parts.length; i += 1) {
            if (!/^(0|[1-9][0-9]{0,2})$/.test(parts[i]) || Number(parts[i]) > 255) {
                return false;
            }
            octets.push(Number(parts[i]));
        }
        return octets[0] === 10 ||
            (octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31) ||
            (octets[0] === 192 && octets[1] === 168);
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

    function formatBytes(value) {
        var bytes = Math.max(0, Number(value) || 0);
        var units = ["Б", "КБ", "МБ", "ГБ"];
        var index = 0;

        while (bytes >= 1024 && index < units.length - 1) {
            bytes /= 1024;
            index += 1;
        }

        return new Intl.NumberFormat("ru-RU", {
            maximumFractionDigits: index === 0 ? 0 : 1
        }).format(bytes) + " " + units[index];
    }

    function errorMessage(error) {
        var payloadError = error && error.payload && error.payload.error;
        var message;

        if (payloadError) {
            message = payloadError.message ||
                payloadError.details ||
                "Операция завершилась ошибкой.";
            if (typeof payloadError.updaterRc === "number" && isFinite(payloadError.updaterRc)) {
                message += " [updater rc=" + payloadError.updaterRc + "]";
            }
            return message;
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
        if (path === "/api/session.cgi") {
            try {
                return await window.BROrayUI.apiRequest(path, options);
            } catch (error) {
                if (error.status === 401) {
                    window.location.replace("/");
                    throw new Error("Сессия завершена.");
                }
                throw error;
            }
        }
        var settings = Object.assign({
            credentials: "same-origin",
            cache: "no-store"
        }, options || {});
        settings.headers = Object.assign({
            "Accept": "application/json",
            "X-BROray-Request": "1"
        }, settings.headers || {});
        settings.headers["X-BROray-Origin"] = window.location.origin;
        var response = await fetch(path, settings);
        var text = await response.text();
        var data;
        var message;

        if (!text.trim()) {
            throw new Error("Backend вернул пустой ответ (HTTP " + response.status + ").");
        }

        try {
            data = JSON.parse(text);
        } catch (error) {
            if (response.status === 502 || response.status === 503 || response.status === 504) {
                throw new Error("Шлюз не получил ответ от роутера (HTTP " + response.status + "). Результат операции не подтверждён. Обновите состояние перед повтором.");
            }
            throw new Error("Backend вернул некорректный JSON (HTTP " + response.status + ").");
        }

        if (response.status === 401) {
            window.location.replace("/");
            throw new Error("Сессия завершена.");
        }

        if (!response.ok || data.ok === false || data.success === false) {
            message = data && data.error && (
                data.error.message || data.error.details
            );
            var failure = new Error(message || data.message || "Операция завершилась ошибкой.");
            failure.payload = data;
            failure.status = response.status;
            if (data && data.error) {
                failure.code = data.error.code || "";
                if (typeof data.error.updaterRc === "number" && isFinite(data.error.updaterRc)) {
                    failure.updaterRc = data.error.updaterRc;
                }
            }
            throw failure;
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
        var localUninstall = isLocalUninstallHost();
        var uninstallAccessNote = byId("uninstall-access-note");

        uninstallAccessNote.hidden = localUninstall;
        uninstallAccessNote.textContent = localUninstall ? "" : uninstallAccessMessage;

        byId("check-update").disabled = state.busy || !info;
        var installUpdateButton = byId("install-update");
        installUpdateButton.disabled = state.busy || !info || !info.updateAvailable;
        var installUpdateLabel = ensureButtonParts(installUpdateButton);
        if (installUpdateLabel && !state.busy) {
            installUpdateLabel.label.textContent = info && info.universalUpdaterReady === false
                ? "Завершить обновление"
                : "Установить обновление";
        }
        installUpdateButton.classList.toggle("button-primary", !installUpdateButton.disabled && Boolean(info && info.updateAvailable));
        installUpdateButton.classList.toggle("button-secondary", installUpdateButton.disabled || !info || !info.updateAvailable);
        if (!installUpdateButton.disabled && info && info.updateAvailable) {
            installUpdateButton.setAttribute("data-recommended-action", "true");
        } else {
            installUpdateButton.removeAttribute("data-recommended-action");
        }
        byId("reinstall-current").disabled = state.busy || !info || !info.reinstallSupported;
        var recoveryButton = byId("restore-backup");
        var recoveryLabel = ensureButtonParts(recoveryButton);
        var recoveryAvailable = Boolean(info && info.recoveryAvailable);
        recoveryButton.disabled = state.busy || !recoveryAvailable;
        recoveryButton.setAttribute("aria-disabled", recoveryButton.disabled ? "true" : "false");
        recoveryButton.title = recoveryAvailable
            ? "Завершить или откатить прерванное обновление BROray."
            : "Доступно при прерванном обновлении BROray.";
        if (recoveryLabel && !state.busy) {
            recoveryLabel.label.textContent = recoveryAvailable
                ? "Завершить прерванное обновление"
                : "Восстановление недоступно";
        }
        byId("uninstall-normal").disabled = state.busy || !localUninstall;
        byId("uninstall-full").disabled = state.busy || !localUninstall;
        byId("cleanup-plan").disabled = state.busy;
        byId("cleanup-run").disabled = state.busy || !state.cleanupPlan || state.cleanupPlan.candidateCount < 1;
        ["cleanup-temp", "cleanup-backups", "cleanup-route-backups", "cleanup-logs"].forEach(function (id) {
            byId(id).disabled = state.busy;
        });
        ["backup-export", "backup-preview", "backup-file", "backup-import-password", "backup-unencrypted",
            "backup-cancel", "backup-undo"].forEach(function (id) {
            byId(id).disabled = state.busy;
        });
        var unencrypted = byId("backup-unencrypted").checked;
        byId("backup-password").disabled = state.busy || unencrypted;
        byId("backup-password-confirm").disabled = state.busy || unencrypted;
        byId("backup-apply").disabled = state.busy || !state.backupPlan || !state.backupPlan.hasChanges;
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
            hysteria2: "Hysteria2",
            shadowsocks: "Shadowsocks"
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
        var channelLabels = {
            stable: "Стабильный",
            staging: "Тестовый (staging)",
            test: "Тестовый",
            development: "Разработка"
        };
        var installationHealthy = info.installationHealthy === true && info.versionsConsistent === true && info.universalUpdaterReady === true;
        var handoff = info.platformHandoff || {};
        var availableLabel;

        state.info = info;
        setText("current-version", info.version);
        setText("build-description", "Кандидат: " + (info.candidateId || info.webUIBuild || info.releaseId || info.build || "не определён") + " · релиз: " + (info.releaseId || "не определён") + " · источник: " + (info.releaseSource || "не определён"));
        setText("architecture", info.architecture);
        setText("update-channel", channelLabels[info.updateChannel] || info.updateChannel || "Не определён");
        setText("installed-package-version", info.installedPackageVersion);

        if (info.updateAvailable) availableLabel = info.availableVersion || "Доступно";
        else if (info.lastCheckedAt) availableLabel = info.availableVersion || info.version || "Обновлений нет";
        else availableLabel = "Не проверено";
        setText("available-version", availableLabel);
        setText("last-update-check", formatDate(info.lastCheckedAt));
        setText("webui-build", info.webUIBuild || "—");
        setText(
            "updater-platform",
            info.universalUpdaterReady === true
                ? "Универсальный"
                : (handoff.running === true ? "Переход выполняется" : "Переход не завершён")
        );

        installation.textContent = installationHealthy
            ? "Установлено"
            : (handoff.running === true ? "Завершение обновления" : "Обновление не завершено");
        installation.className = "status-badge " + (
            installationHealthy ? "status-success" : (handoff.running === true ? "status-loading" : "status-error")
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

    function closestCard(element) {
        if (!element) {
            return null;
        }

        return element.closest("section, article, [data-card], .card, .panel, .panel-card, .content-card, .surface-card");
    }

    function findHeading(text) {
        var headings = document.querySelectorAll("h1, h2, h3, h4, [data-card-title], .card-title, .section-title");
        var found = null;

        Array.prototype.some.call(headings, function (heading) {
            if (heading.textContent.trim() === text) {
                found = heading;
                return true;
            }
            return false;
        });

        return found;
    }

    function placeOperationCard() {
        var operationCard = closestCard(byId("operation-state"));
        var storageCard = closestCard(byId("cleanup-state"));

        if (!operationCard || !storageCard || operationCard === storageCard) {
            return;
        }
        if (!storageCard.parentNode || operationCard.parentNode !== storageCard.parentNode) {
            return;
        }

        storageCard.parentNode.insertBefore(operationCard, storageCard);
    }

    function operationLabel(operation) {
        return {
            update: "Обновление",
            reinstall: "Переустановка",
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

        // An accepted request can precede publication of its status. A stale
        // idle/terminal response for another operation is not our completion.
        if (state.operationId && operation.operationId !== state.operationId) {
            state.unconfirmedPolls += 1;
            if (state.unconfirmedPolls >= 15) {
                stopPolling();
                setBusy(false);
                showPageError("Результат операции " + state.operationId + " не подтверждён. Обновите состояние; не запускайте удаление повторно до проверки.");
                return { running: false, state: "unconfirmed" };
            }
            operation = { operationId: state.operationId, operation: state.operationKind,
                state: "queued", running: true, message: "Запрос принят. Ожидается состояние операции." };
        } else {
            state.unconfirmedPolls = 0;
        }
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

    function renderPollingInterruption() {
        var badge = byId("operation-state");

        badge.textContent = "Ожидание WebUI…";
        badge.className = "status-badge status-loading";
        badge.setAttribute("data-icon", "logs");
        setBusy(true);
        showPageError(
            "WebUI временно перезапускается. Проверка результата продолжается автоматически; обновлять страницу не требуется."
        );
    }

    function schedulePolling(delay) {
        if (state.pollTimer || state.pollInFlight) {
            return;
        }

        state.pollTimer = window.setTimeout(function () {
            state.pollTimer = null;
            pollOperation();
        }, delay);
    }

    async function pollOperation() {
        var operation;
        var delay = 2000;

        if (state.pollInFlight) {
            return;
        }

        state.pollInFlight = true;
        try {
            operation = await loadOperation();
            state.pollFailures = 0;
            hidePageError();

            if (!operation.running) {
                if (operation.state === "unconfirmed") return;
                state.operationId = null;
                state.operationKind = null;
                await loadInfo();
                toast(
                    operation.message || "Операция завершена.",
                    operation.state === "success" ? "success" : "error"
                );
                return;
            }
        } catch (error) {
            state.pollFailures += 1;
            delay = Math.min(5000, 1000 + state.pollFailures * 500);
            if (state.pollFailures >= 6) {
                stopPolling();
                setBusy(false);
                showPageError("Результат операции " + (state.operationId || "") + " не подтверждён: WebUI недоступен. При удалении интерфейс может исчезнуть до получения ответа. Проверьте состояние установки перед повторным действием.");
            } else {
                renderPollingInterruption();
            }
        } finally {
            state.pollInFlight = false;
            if (state.busy || state.pollFailures > 0) {
                schedulePolling(delay);
            }
        }
    }

    function startPolling() {
        schedulePolling(500);
    }

    function stopPolling() {
        if (state.pollTimer) {
            window.clearTimeout(state.pollTimer);
            state.pollTimer = null;
        }
        state.pollInFlight = false;
        state.pollFailures = 0;
    }

    async function startAction(path, successMessage, body, button, busyLabel) {
        var options = {
            method: "POST",
            headers: {
                "Accept": "application/json",
                "Content-Type": "application/json",
                "X-BROray-Request": "1"
            },
            body: JSON.stringify(body || {})
        };

        if (state.busy) {
            return;
        }

        hidePageError();
        setBusy(true);
        setButtonBusy(button, true, busyLabel);

        try {
            var accepted = await request(path, options);
            state.operationId = accepted.operationId || null;
            state.operationKind = accepted.operation || (path.indexOf("uninstall") >= 0 ? "uninstall" : null);
            state.unconfirmedPolls = 0;
            state.pollFailures = 0;
            toast(successMessage, "success");
            // Poll after acceptance, so losing the first status reply cannot
            // turn a confirmed request into a failed submission.
            startPolling();
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

        if (state.busy || !state.info) {
            return;
        }

        hidePageError();
        setBusy(true);
        setButtonBusy(button, true, "Проверка…");
        try {
            var result = await request("/api/broray/update-check.cgi", {
                method: "POST",
                headers: {
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "X-BROray-Request": "1"
                },
                body: "{}"
            });

            var platformPending = state.info && state.info.universalUpdaterReady === false;
            state.info = Object.assign({}, state.info || {}, {
                updateAvailable: platformPending || result.updateAvailable === true,
                availableVersion: result.availableVersion || null,
                availablePackageVersion: result.availablePackageVersion || null,
                availableCandidateId: result.candidateId || null,
                candidateRelation: result.candidateRelation || "uncomparable",
                lastCheckedAt: result.checkedAt || new Date().toISOString(),
                updateCheckFresh: true,
                // Capability belongs to the newly checked channel, not the
                // previous page snapshot. Stay disabled until confirmed.
                reinstallSupported: false
            });
            renderInfo(state.info);
            await loadInfo();
            toast(
                platformPending
                    ? "Требуется завершить обновление системы установки BROray."
                    : result.updateAvailable
                    ? "Доступна версия " + result.availableVersion + "."
                    : (result.candidateRelation === "older"
                        ? "В канале находится более старый кандидат; установка отключена."
                        : "Установлена актуальная версия."),
                platformPending || result.updateAvailable ? "info" : (result.candidateRelation === "older" ? "warning" : "success")
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

    function cleanupOptions() {
        return {
            temp: byId("cleanup-temp").checked,
            backups: byId("cleanup-backups").checked,
            routeBackups: byId("cleanup-route-backups").checked,
            logs: byId("cleanup-logs").checked
        };
    }

    function invalidateCleanupPlan() {
        state.cleanupPlan = null;
        byId("cleanup-result").hidden = true;
        byId("cleanup-state").textContent = "Не проверено";
        byId("cleanup-state").className = "status-badge status-neutral";
        byId("cleanup-state").setAttribute("data-icon", "storage");
        applyControlState();
    }

    function renderCleanupPlan(plan) {
        var list = byId("cleanup-candidates");
        var badge = byId("cleanup-state");

        state.cleanupPlan = plan;
        setText("cleanup-count", plan.candidateCount || 0);
        setText("cleanup-bytes", formatBytes(plan.estimatedBytes));
        setText("cleanup-expiry", "действует " + (plan.ttlSeconds || 120) + " секунд");
        list.replaceChildren();
        (plan.candidates || []).forEach(function (candidate) {
            var item = document.createElement("li");
            var path = document.createElement("code");
            var size = document.createElement("span");

            path.textContent = candidate.path;
            size.textContent = formatBytes(candidate.sizeBytes);
            item.append(path, size);
            list.appendChild(item);
        });
        if (!(plan.candidates || []).length) {
            var empty = document.createElement("li");
            empty.textContent = "Подходящих файлов не найдено.";
            empty.className = "is-empty";
            list.appendChild(empty);
        }
        badge.textContent = plan.candidateCount > 0 ? "Готово к очистке" : "Очистка не требуется";
        badge.className = "status-badge " + (plan.candidateCount > 0 ? "status-warning" : "status-success");
        badge.setAttribute("data-icon", plan.candidateCount > 0 ? "storage" : "status");
        byId("cleanup-result").hidden = false;
        applyControlState();
    }

    async function planCleanup(event) {
        var button = event.currentTarget;

        if (state.busy) {
            return;
        }
        hidePageError();
        setBusy(true);
        setButtonBusy(button, true, "Проверка…");
        try {
            var plan = await request("/api/broray/cleanup-plan.cgi", {
                method: "POST",
                headers: {
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "X-BROray-Request": "1"
                },
                body: JSON.stringify(cleanupOptions())
            });
            renderCleanupPlan(plan);
            toast(
                plan.candidateCount > 0
                    ? "План очистки сформирован."
                    : "Очистка не требуется.",
                plan.candidateCount > 0 ? "info" : "success"
            );
        } catch (error) {
            invalidateCleanupPlan();
            showPageError(errorMessage(error));
            toast(errorMessage(error), "error");
        } finally {
            setBusy(false);
            setButtonBusy(button, false);
            applyControlState();
        }
    }

    async function runCleanup(event) {
        var button = event.currentTarget;
        var plan = state.cleanupPlan;
        var confirmed;

        if (!plan || state.busy) {
            return;
        }
        confirmed = await confirmAction({
            eyebrow: "Безопасная очистка",
            title: "Удалить найденные служебные файлы?",
            message: "Будет удалено объектов: " + plan.candidateCount + ". Ожидаемое освобождение: " + formatBytes(plan.estimatedBytes) + ". Неизвестные файлы и последние резервные копии не затрагиваются.",
            confirmText: "Очистить",
            variant: "primary",
            icon: "delete"
        });
        if (!confirmed) {
            return;
        }

        hidePageError();
        setBusy(true);
        setButtonBusy(button, true, "Очистка…");
        try {
            var result = await request("/api/broray/cleanup.cgi", {
                method: "POST",
                headers: {
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "X-BROray-Request": "1"
                },
                body: JSON.stringify({ token: plan.token })
            });
            state.cleanupPlan = null;
            byId("cleanup-state").textContent = "Очищено";
            byId("cleanup-state").className = "status-badge status-success";
            byId("cleanup-state").setAttribute("data-icon", "status");
            setText("cleanup-count", result.deletedCount || 0);
            setText("cleanup-bytes", formatBytes(result.freedBytes));
            setText("cleanup-expiry", "завершено");
            byId("cleanup-candidates").replaceChildren();
            toast("Очистка завершена: освобождено " + formatBytes(result.freedBytes) + ".", "success");
        } catch (error) {
            invalidateCleanupPlan();
            showPageError(errorMessage(error));
            toast(errorMessage(error), "error");
        } finally {
            setBusy(false);
            setButtonBusy(button, false);
            applyControlState();
        }
    }

    async function installUpdate(event) {
        var platformPending = state.info && state.info.universalUpdaterReady === false;
        var confirmed = await confirmAction({
            eyebrow: "Обновление",
            title: platformPending ? "Завершить обновление BROray?" : "Установить обновление BROray?",
            message: platformPending
                ? "Будет обновлена система установки BROray. Xray и пользовательские данные сохраняются."
                : "Настройки и пользовательские данные сохраняются. Во время переключения версии WebUI кратковременно перезапустится.",
            confirmText: platformPending ? "Завершить" : "Установить",
            variant: "primary",
            icon: "update"
        });

        if (confirmed) {
            startAction(
                "/api/broray/update-start.cgi",
                platformPending ? "Завершение обновления запущено." : "Обновление запущено.",
                null,
                event.currentTarget,
                "Установка…"
            );
        }
    }

    async function reinstallCurrent(event) {
        var version = state.info && (state.info.candidateId || state.info.installedPackageVersion || state.info.version);
        var confirmed = await confirmAction({
            eyebrow: "Восстановительная переустановка",
            title: "Переустановить текущую версию BROray?",
            message: "Будет заново загружен и проверен компактный архив приложения " + (version || "текущей версии") + ". Общий Xray и пользовательские данные сохраняются. При ошибке будет восстановлена предыдущая версия BROray.",
            confirmText: "Переустановить",
            variant: "primary",
            icon: "restore"
        });

        if (confirmed) {
            startAction(
                "/api/broray/reinstall.cgi",
                "Восстановительная переустановка запущена.",
                null,
                event.currentTarget,
                "Переустановка…"
            );
        }
    }

    async function recoverInterruptedUpdate(event) {
        if (state.busy || !state.info || !state.info.recoveryAvailable) {
            return;
        }
        var confirmed = await confirmAction({
            eyebrow: "Восстановление после сбоя",
            title: "Завершить прерванное обновление?",
            message: "Установщик проверит журнал и завершит или откатит прерванное обновление. BROray может перезапуститься; текущая сессия завершится.",
            confirmText: "Продолжить",
            variant: "primary",
            icon: "restore"
        });

        if (confirmed) {
            startAction(
                "/api/broray/restore.cgi",
                "Восстановление после сбоя запущено.",
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

        if (!isLocalUninstallHost()) {
            throw new Error(uninstallAccessMessage);
        }

        if (!window.BROrayDialogs) {
            throw new Error("Окно подтверждения BROray недоступно.");
        }

        confirmed = await window.BROrayDialogs.confirmPhrase({
            eyebrow: "Необратимое действие",
            title: full ? "Полностью удалить BROray?" : "Удалить BROray?",
            message: full
                ? "Будут безвозвратно удалены программа, настройки, подписки, серверы, маршруты, принадлежащий BROray управляемый ProxyN, KeenDNS HTTP Proxy и служебные данные."
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

    // ---------- settings backup (card «Резервная копия настроек») ----------

    var backupActionLabels = {
        add: "новых",
        replace: "заменить",
        remove: "удалить",
        unchanged: "без изменений",
        keep: "останутся"
    };
    var backupAppliedLabels = {
        add: "добавлено",
        replace: "заменено",
        remove: "удалено",
        unchanged: "без изменений",
        keep: "сохранены"
    };
    var backupItemLabels = {
        add: "добавить",
        replace: "заменить",
        remove: "удалить"
    };
    var backupItemAppliedLabels = {
        add: "добавлено",
        replace: "заменено",
        remove: "удалено"
    };
    var backupKindLabels = {
        server: "Сервер",
        subscription: "Подписка",
        customRoutes: "Свой набор маршрутов",
        setting: ""
    };

    function setBackupBadge(text, kind) {
        var badge = byId("backup-state");

        badge.textContent = text;
        badge.className = "status-badge status-" + (kind || "neutral");
        badge.setAttribute("data-icon", kind === "success" ? "status" : "backup");
    }

    function backupCounts(counts, applied) {
        var labels = applied ? backupAppliedLabels : backupActionLabels;
        var parts = [];

        ["add", "replace", "remove", "unchanged", "keep"].forEach(function (action) {
            var value = counts && Number(counts[action]) || 0;

            if (value > 0) {
                parts.push(labels[action] + ": " + value);
            }
        });
        return parts.length ? parts.join(" · ") : "нет";
    }

    function renderBackupPlan(view, applied) {
        var summary = view.summary || {};
        var list = byId("backup-changes");
        var follow = byId("backup-followups");
        var labels = applied ? backupItemAppliedLabels : backupItemLabels;
        var shown = 0;
        var created = view.file && view.file.createdAt ? formatDate(view.file.createdAt) : "—";

        byId("backup-result-title").textContent = applied
            ? "Восстановление выполнено. Копия от " + created + "."
            : "Резервная копия от " + created + " (BROray " + ((view.file && view.file.brorayVersion) || "—") + ", " +
                (view.file && view.file.encrypted ? "зашифрована" : "без шифрования") + ").";
        setText("backup-sum-servers", backupCounts(summary.servers, applied));
        setText("backup-sum-subscriptions", backupCounts(summary.subscriptions, applied));
        setText("backup-sum-routes", backupCounts(summary.customRoutes, applied));
        setText("backup-sum-settings", backupCounts(summary.settings, applied));
        list.replaceChildren();
        (view.changes || []).forEach(function (change) {
            var item;
            var name;
            var action;
            var prefix = backupKindLabels[change.kind] || "";

            if (change.action === "keep" || change.action === "unchanged") {
                return;
            }
            item = document.createElement("li");
            name = document.createElement("code");
            action = document.createElement("span");
            name.textContent = (prefix ? prefix + " «" + change.name + "»" : change.name) +
                (change.detail ? " — " + change.detail : "");
            action.textContent = labels[change.action] || change.action;
            item.append(name, action);
            list.appendChild(item);
            shown += 1;
        });
        if (!shown) {
            var empty = document.createElement("li");
            empty.className = "is-empty";
            empty.textContent = "Текущие настройки совпадают с резервной копией.";
            list.appendChild(empty);
        }
        follow.replaceChildren();
        (view.notes || []).concat(view.followUps || []).forEach(function (text) {
            var item = document.createElement("li");

            item.textContent = text;
            follow.appendChild(item);
        });
        follow.hidden = !follow.children.length;
        byId("backup-apply").hidden = applied || !view.hasChanges;
        byId("backup-cancel").hidden = applied;
        byId("backup-result").hidden = false;
        applyControlState();
    }

    function resetBackupPlan() {
        state.backupPlan = null;
        byId("backup-result").hidden = true;
        setBackupBadge("Не проверено", "neutral");
        applyControlState();
    }

    function backupFileName(response) {
        var header = response.headers.get("Content-Disposition") || "";
        var match = /filename="([A-Za-z0-9._-]+)"/.exec(header);

        return match ? match[1] : "broray-settings.json";
    }

    async function exportBackup(event) {
        var button = event.currentTarget;
        var unencrypted = byId("backup-unencrypted").checked;
        var password = byId("backup-password").value;
        var body;
        var response;
        var blob;
        var link;
        var url;

        if (state.busy) {
            return;
        }
        if (unencrypted) {
            if (!await confirmAction({
                eyebrow: "Резервная копия без шифрования",
                title: "Сохранить копию без пароля?",
                message: "Файл будет содержать ссылки серверов с ключами и адреса подписок открытым текстом. Любой, кто получит файл, сможет подключиться к вашим серверам. Храните его в надёжном месте.",
                confirmText: "Скачать без шифрования",
                variant: "danger",
                icon: "backup"
            })) {
                return;
            }
            body = { encrypt: false, acknowledgeUnencrypted: true };
        } else {
            if (password.length < 8) {
                toast("Пароль должен содержать не менее 8 символов.", "error");
                byId("backup-password").focus();
                return;
            }
            if (password !== byId("backup-password-confirm").value) {
                toast("Пароли не совпадают.", "error");
                byId("backup-password-confirm").focus();
                return;
            }
            body = { encrypt: true, password: password };
        }
        hidePageError();
        setBusy(true);
        setButtonBusy(button, true, "Подготовка…");
        try {
            response = await fetch("/api/broray/backup-export.cgi", {
                method: "POST",
                credentials: "same-origin",
                cache: "no-store",
                headers: {
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "X-BROray-Request": "1",
                    "X-BROray-Origin": window.location.origin
                },
                body: JSON.stringify(body)
            });
            if (response.status === 401) {
                window.location.replace("/");
                return;
            }
            if (!response.ok || !/attachment/.test(response.headers.get("Content-Disposition") || "")) {
                var failure = null;
                try {
                    failure = JSON.parse(await response.text());
                } catch (parseError) {
                    failure = null;
                }
                throw new Error(failure && failure.error && failure.error.message ||
                    "Не удалось подготовить резервную копию (HTTP " + response.status + ").");
            }
            blob = await response.blob();
            url = URL.createObjectURL(blob);
            link = document.createElement("a");
            link.href = url;
            link.download = backupFileName(response);
            link.hidden = true;
            document.body.appendChild(link);
            link.click();
            link.remove();
            window.setTimeout(function () {
                URL.revokeObjectURL(url);
            }, 30000);
            byId("backup-password").value = "";
            byId("backup-password-confirm").value = "";
            setBackupBadge(unencrypted ? "Копия без шифрования" : "Копия сохранена", unencrypted ? "warning" : "success");
            toast("Резервная копия сохранена: " + link.download + ".", "success");
        } catch (error) {
            showPageError(errorMessage(error));
            toast(errorMessage(error), "error");
        } finally {
            setBusy(false);
            setButtonBusy(button, false);
            applyControlState();
        }
    }

    async function readBackupFile() {
        var input = byId("backup-file");
        var file = input.files && input.files[0];

        if (!file) {
            throw new Error("Выберите файл резервной копии.");
        }
        if (file.size > 4 * 1024 * 1024) {
            throw new Error("Файл резервной копии слишком большой (больше 4 МБ).");
        }
        return file.text();
    }

    async function previewBackup(event) {
        var button = event.currentTarget;
        var text;
        var password = byId("backup-import-password").value;
        var view;

        if (state.busy) {
            return;
        }
        hidePageError();
        setBusy(true);
        setButtonBusy(button, true, "Проверка файла…");
        try {
            text = await readBackupFile();
            view = await request("/api/broray/backup-preview.cgi", {
                method: "POST",
                headers: {
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "X-BROray-Request": "1"
                },
                body: JSON.stringify({ file: text, password: password })
            });
            state.backupPlan = { file: text, password: password, view: view, hasChanges: Boolean(view.hasChanges) };
            setBackupBadge(view.hasChanges ? "Готово к восстановлению" : "Изменений нет", view.hasChanges ? "warning" : "success");
            renderBackupPlan(view, false);
            toast(view.hasChanges ? "Файл проверен. Проверьте изменения и подтвердите восстановление." : "Текущие настройки совпадают с резервной копией.",
                view.hasChanges ? "info" : "success");
        } catch (error) {
            resetBackupPlan();
            showPageError(errorMessage(error));
            toast(errorMessage(error), "error");
        } finally {
            setBusy(false);
            setButtonBusy(button, false);
            applyControlState();
        }
    }

    async function applyBackup(event) {
        var button = event.currentTarget;
        var plan = state.backupPlan;
        var result;

        if (!plan || state.busy) {
            return;
        }
        if (!await confirmAction({
            eyebrow: "Восстановление настроек",
            title: "Восстановить настройки из резервной копии?",
            message: "Серверы, подписки и настройки из файла заменят текущие с теми же идентификаторами. Перед заменой BROray сохранит копию текущего состояния на роутере, восстановление можно будет отменить.",
            confirmText: "Восстановить",
            variant: "primary",
            icon: "restore"
        })) {
            return;
        }
        hidePageError();
        setBusy(true);
        setButtonBusy(button, true, "Восстановление…");
        try {
            result = await request("/api/broray/backup-import.cgi", {
                method: "POST",
                headers: {
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "X-BROray-Request": "1"
                },
                body: JSON.stringify({ file: plan.file, password: plan.password, confirmToken: plan.view.confirmToken })
            });
            state.backupPlan = null;
            byId("backup-file").value = "";
            byId("backup-import-password").value = "";
            setBackupBadge("Восстановлено", "success");
            renderBackupPlan(result, true);
            toast("Настройки восстановлены из резервной копии.", "success");
            loadBackupStatus();
        } catch (error) {
            if (error.code === "BACKUP_PLAN_CHANGED") {
                resetBackupPlan();
            }
            showPageError(errorMessage(error));
            toast(errorMessage(error), "error");
        } finally {
            setBusy(false);
            setButtonBusy(button, false);
            applyControlState();
        }
    }

    async function loadBackupStatus() {
        var copies;

        try {
            copies = (await request("/api/broray/backup-status.cgi", { method: "GET" })).preImportCopies || [];
        } catch (error) {
            copies = [];
        }
        state.backupCopy = copies[0] || null;
        byId("backup-undo-row").hidden = !state.backupCopy;
        if (state.backupCopy) {
            byId("backup-undo-text").textContent = "Перед восстановлением " + formatDate(state.backupCopy.createdAt) +
                " BROray сохранил копию прежних настроек.";
        }
    }

    async function undoBackup(event) {
        var button = event.currentTarget;
        var copy = state.backupCopy;
        var result;

        if (!copy || state.busy) {
            return;
        }
        if (!await confirmAction({
            eyebrow: "Отмена восстановления",
            title: "Вернуть настройки, которые были до восстановления?",
            message: "Серверы, подписки, свои маршруты и настройки вернутся к состоянию на " + formatDate(copy.createdAt) +
                ". Изменения, сделанные после восстановления в этих разделах, будут заменены.",
            confirmText: "Вернуть",
            variant: "primary",
            icon: "restore"
        })) {
            return;
        }
        hidePageError();
        setBusy(true);
        setButtonBusy(button, true, "Возврат…");
        try {
            result = await request("/api/broray/backup-undo.cgi", {
                method: "POST",
                headers: {
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "X-BROray-Request": "1"
                },
                body: JSON.stringify({ copyId: copy.id })
            });
            byId("backup-result").hidden = true;
            setBackupBadge("Восстановление отменено", "success");
            (result.followUps || []).forEach(function (text) {
                toast(text, "info");
            });
            toast("Прежние настройки возвращены.", "success");
        } catch (error) {
            showPageError(errorMessage(error));
            toast(errorMessage(error), "error");
        } finally {
            setBusy(false);
            setButtonBusy(button, false);
            applyControlState();
            loadBackupStatus();
        }
    }

    function bind() {
        byId("check-update").addEventListener("click", checkUpdate);
        byId("install-update").addEventListener("click", installUpdate);
        byId("reinstall-current").addEventListener("click", reinstallCurrent);
        byId("restore-backup").addEventListener("click", recoverInterruptedUpdate);
        byId("cleanup-plan").addEventListener("click", planCleanup);
        byId("cleanup-run").addEventListener("click", function (event) {
            runCleanup(event).catch(function (error) {
                toast(errorMessage(error), "error");
            });
        });
        ["cleanup-temp", "cleanup-backups", "cleanup-route-backups", "cleanup-logs"].forEach(function (id) {
            byId(id).addEventListener("change", invalidateCleanupPlan);
        });
        byId("backup-export").addEventListener("click", exportBackup);
        byId("backup-preview").addEventListener("click", previewBackup);
        byId("backup-apply").addEventListener("click", function (event) {
            applyBackup(event).catch(function (error) {
                toast(errorMessage(error), "error");
            });
        });
        byId("backup-cancel").addEventListener("click", resetBackupPlan);
        byId("backup-undo").addEventListener("click", function (event) {
            undoBackup(event).catch(function (error) {
                toast(errorMessage(error), "error");
            });
        });
        byId("backup-unencrypted").addEventListener("change", applyControlState);
        ["backup-file", "backup-import-password"].forEach(function (id) {
            byId(id).addEventListener("change", function () {
                if (state.backupPlan) {
                    resetBackupPlan();
                }
            });
        });
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
            applyControlState();
            loader.hidden = true;
            app.hidden = false;
            placeOperationCard();
            loadBackupStatus();
            await refresh();
        } catch (error) {
            if (error.message !== "Сессия завершена.") {
                loader.hidden = false;
                app.hidden = true;
                loader.textContent = errorMessage(error) + " ";
                var retry = document.createElement("button");
                retry.type = "button";
                retry.className = "button button-secondary";
                retry.textContent = "Повторить";
                retry.addEventListener("click", function () {
                    retry.disabled = true;
                    initialize();
                });
                loader.appendChild(retry);
            }
        }
    }

    window.addEventListener("pagehide", stopPolling);
    window.addEventListener("online", function () {
        if (state.busy || state.pollFailures > 0) {
            startPolling();
        }
    });
    document.addEventListener("visibilitychange", function () {
        if (!document.hidden && (state.busy || state.pollFailures > 0)) {
            startPolling();
        }
    });

    function start() {
        bind();
        initialize();
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", start, { once: true });
    } else {
        start();
    }
})();
