/* BROray routes immediate operation window r1 */
(function () {
    "use strict";

    var MAX_TOTAL_BYTES = 2 * 1024 * 1024;
    var REQUEST_TIMEOUT_MS = 180000;
    var LONG_OPERATION_TIMEOUT_MS = 8 * 60 * 60 * 1000;
    var PROGRESS_POLL_INTERVAL_MS = 750;
    var PASSIVE_REFRESH_INTERVAL_MS = 30000;
    var OPERATION_EVENT = "broray:routes-operation-state";
    var CONTROL_EVENT = "broray:routes-operation-control";

    var bundles = [];
    var states = Object.create(null);
    var busyActions = Object.create(null);
    var progressWatchers = Object.create(null);
    var progressPollErrors = Object.create(null);
    var longRequests = Object.create(null);
    var peerOperation = null;
    var globalOperation = null;
    var lastPublishedOperationSignature = "";
    var lastPeerOperationSignature = "";
    var summaryObserver = null;
    var passiveTimer = null;
    var modal = null;
    var preview = null;
    var replaceBundleId = null;
    var uploadBusy = false;
    var latestSummary = null;
    var summaryRequest = null;
    var summaryController = null;

    function byId(id) {
        return document.getElementById(id);
    }

    function create(tag, className, text) {
        var node = document.createElement(tag);
        if (className) node.className = className;
        if (text !== undefined && text !== null) node.textContent = String(text);
        return node;
    }

    function unwrap(payload) {
        var error;
        if (payload && payload.success === false) {
            error = new Error(payload.error && payload.error.message
                ? payload.error.message
                : "Операция с пользовательскими маршрутами завершилась ошибкой.");
            error.code = payload.error ? payload.error.code : null;
            error.details = payload.error ? payload.error.details : null;
            error.status = payload.status || null;
            throw error;
        }
        return payload && Object.prototype.hasOwnProperty.call(payload, "data")
            ? payload.data
            : payload;
    }

    function request(url, options) {
        return window.BROrayUI.apiRequest(url, options || {}).then(unwrap);
    }

    function withTimeout(promise, milliseconds) {
        return new Promise(function (resolve, reject) {
            var done = false;
            var timer = window.setTimeout(function () {
                if (!done) {
                    done = true;
                    reject(new Error("Операция не завершилась за отведённое время."));
                }
            }, milliseconds);
            promise.then(function (value) {
                if (!done) {
                    done = true;
                    window.clearTimeout(timer);
                    resolve(value);
                }
            }, function (error) {
                if (!done) {
                    done = true;
                    window.clearTimeout(timer);
                    reject(error);
                }
            });
        });
    }

    function sameVersion(left, right) {
        return Boolean(left && right && (
            (left.contentSha256 && right.contentSha256 && left.contentSha256 === right.contentSha256) ||
            (!left.contentSha256 && !right.contentSha256 && left.sourceCommit === right.sourceCommit)
        ));
    }

    function presence(state) {
        return state && state.routerPresence ? state.routerPresence : null;
    }

    function operationProgress(state) {
        return state && state.operationProgress && typeof state.operationProgress === "object"
            ? state.operationProgress
            : null;
    }

    function isInstalled(state) {
        var value = presence(state);
        if (value && value.available === true && value.registered === true) {
            return value.actualInstalled === true;
        }
        return Boolean(state && state.installedVersion);
    }

    function hasDrift(state) {
        var value = presence(state);
        return Boolean(state && state.installedVersion && value &&
            value.available === true && value.registered === true && value.drift === true);
    }

    function needsExport(state) {
        return Boolean(state && state.downloadedVersion &&
            (!state.installedVersion || !sameVersion(state.downloadedVersion, state.installedVersion)));
    }

    function formatDate(value) {
        var date;
        if (!value) return "Не выполнялась";
        date = new Date(value);
        return isNaN(date.getTime()) ? String(value) : date.toLocaleString("ru-RU");
    }

    function formatBytes(value) {
        var size = Number(value || 0);
        if (size < 1024) return size + " Б";
        if (size < 1024 * 1024) return Math.round(size / 1024) + " КБ";
        return (size / (1024 * 1024)).toFixed(1).replace(".", ",") + " МБ";
    }

    function scanIcons(node) {
        if (window.BROrayIcons) window.BROrayIcons.scan(node || document);
    }

    function toast(message, kind) {
        window.BROrayUI.toast(message, kind || "success");
    }

    function bundleById(bundleId) {
        return bundles.filter(function (item) { return item.id === bundleId; })[0] || null;
    }

    function bundleName(bundleId) {
        var bundle = bundleById(bundleId);
        return bundle ? bundle.name : bundleId;
    }

    function currentCustomContext() {
        var found = null;
        bundles.some(function (bundle) {
            var progress = operationProgress(states[bundle.id]);
            if (progress && (progress.running || progress.resumable)) {
                found = {
                    source: "custom",
                    active: true,
                    pending: Boolean(progress.resumable && !progress.running),
                    resumable: Boolean(progress.resumable && !progress.running),
                    bundleId: bundle.id,
                    name: bundle.name,
                    action: progress.operation || "operation",
                    progress: progress
                };
                return true;
            }
            return false;
        });
        if (found) return found;
        if (globalOperation && globalOperation.active && bundleById(globalOperation.bundleId)) {
            return {
                source: "custom",
                active: true,
                pending: Boolean(globalOperation.pending),
                resumable: Boolean(globalOperation.resumable),
                bundleId: globalOperation.bundleId,
                name: bundleName(globalOperation.bundleId),
                action: globalOperation.action || "operation",
                progress: operationProgress(states[globalOperation.bundleId])
            };
        }
        return null;
    }

    function operationSignature(operation) {
        var progress = operation && operation.progress ? operation.progress : null;
        if (!operation || operation.active !== true) return "inactive";
        return [
            operation.source || "",
            operation.bundleId || "",
            operation.action || "",
            operation.pending ? "1" : "0",
            operation.resumable ? "1" : "0",
            progress ? String(progress.running === true) : "",
            progress ? String(progress.resumable === true) : "",
            progress ? String(progress.current || 0) : "",
            progress ? String(progress.total || 0) : "",
            progress ? String(progress.stopRequested === true) : "",
            progress ? String(progress.message || "") : ""
        ].join("|");
    }

    function publishOperationState() {
        var context = currentCustomContext();
        var detail = context || {source: "custom", active: false, pending: false, resumable: false, bundleId: null, progress: null};
        var signature = operationSignature(detail);
        if (signature === lastPublishedOperationSignature) return;
        lastPublishedOperationSignature = signature;
        document.dispatchEvent(new CustomEvent(OPERATION_EVENT, {detail: detail}));
    }

    function effectiveGlobalOperation() {
        if (peerOperation && peerOperation.active) return peerOperation;
        if (globalOperation && globalOperation.active) return globalOperation;
        return currentCustomContext();
    }

    function createPanel() {
        var button = byId("routes-custom-upload");
        if (button && button.dataset.bound !== "true") {
            button.dataset.bound = "true";
            button.addEventListener("click", function () { openUpload(null); });
        }
    }

    function statusPresentation(state, bundleId) {
        var progress = operationProgress(state);
        var global = effectiveGlobalOperation();
        if (progress && progress.running) {
            return {text: progress.stopRequested ? "Останавливается" : "Выполняется", className: "status-badge-warning", icon: "update"};
        }
        if (progress && progress.resumable) {
            return {text: "Можно продолжить", className: "status-badge-warning", icon: "restore"};
        }
        if (global && global.active && global.bundleId !== bundleId) {
            return {text: "Временно недоступно", className: "status-badge-neutral", icon: "access"};
        }
        if (!state) return {text: "Загрузка…", className: "status-loading", icon: "status"};
        if (state.lastError) return {text: "Ошибка", className: "status-badge-danger", icon: "status"};
        if (hasDrift(state)) return {text: "Требуется восстановление", className: "status-badge-warning", icon: "restore"};
        if (needsExport(state)) {
            return {text: state.installedVersion ? "Готово обновление" : "Готово к установке", className: "status-badge-warning", icon: "update"};
        }
        if (isInstalled(state)) return {text: "Установлено", className: "status-badge-success", icon: "status"};
        return {text: "Готово к установке", className: "status-badge-neutral", icon: "routes"};
    }

    function primaryPresentation(state) {
        if (hasDrift(state)) return {text: "Восстановить в Keenetic", icon: "restore"};
        if (needsExport(state)) {
            return {text: state.installedVersion ? "Обновить в Keenetic" : "Установить в Keenetic", icon: state.installedVersion ? "update" : "routes"};
        }
        return {text: "Установить в Keenetic", icon: "routes", hidden: isInstalled(state)};
    }

    function noticeMessage(state, bundle) {
        var report = state && state.customImport ? state.customImport : null;
        var progress = operationProgress(state);
        if (progress && progress.resumable) return progress.message || "Операция приостановлена. Нажмите «Продолжить».";
        if (state && state.lastError) {
            return typeof state.lastError === "string" ? state.lastError : (state.lastError.message || "Последняя операция завершилась ошибкой.");
        }
        if (hasDrift(state)) {
            var currentPresence = presence(state) || {};
            var expected = Number(currentPresence.expectedRouteCount || 0);
            var present = Number(currentPresence.presentRouteCount || 0);
            return "В Keenetic отсутствуют " + Math.max(0, expected - present) + " из " + expected + " маршрутов. Нажмите «Восстановить в Keenetic».";
        }
        if (needsExport(state)) return state.installedVersion
            ? "Новый BAT-файл проверен. Выполните обновление маршрутов в Keenetic."
            : "BAT-файлы проверены. Набор готов к установке в Keenetic.";
        if (report && report.warning) return report.warning;
        if (isInstalled(state)) return "Все зарегистрированные маршруты присутствуют в Keenetic.";
        return bundle.description;
    }

    function actionButton(bundleId, action, text, variant, icon) {
        var button = create("button", "button " + variant, text);
        button.type = "button";
        button.setAttribute("data-bundle-id", bundleId);
        button.setAttribute("data-custom-action", action);
        if (icon) button.setAttribute("data-icon", icon);
        button.addEventListener("click", onAction);
        return button;
    }

    function dataRow(label, value) {
        var row = create("div", "ui-data-row");
        row.append(create("span", "ui-data-row__label", label), create("strong", "ui-data-row__value", value));
        return row;
    }

    function operationTitle(progress) {
        if (!progress) return "Операция с маршрутами";
        if (progress.operation === "delete") return "Удаление маршрутов из Keenetic";
        if (progress.operation === "update") return "Обновление маршрутов в Keenetic";
        if (progress.operation === "restore") return "Восстановление маршрутов в Keenetic";
        return "Установка маршрутов в Keenetic";
    }

    function initialProgress(bundleId, operation) {
        return {
            schemaVersion: 2,
            kind: "routes",
            bundleId: bundleId,
            operation: operation || "install",
            phase: "preparing",
            current: 0,
            total: 0,
            percent: 0,
            currentRoute: null,
            message: "Подготовка безопасного плана операции.",
            running: true,
            success: null,
            rolledBack: false,
            resumable: false,
            stopRequested: false,
            stoppedByUser: false
        };
    }

    function createProgressBox() {
        var placeholder = create("span", "route-card-progress-placeholder");
        placeholder.hidden = true;
        return placeholder;
    }

    function renderProgress() {
        return;
    }

    function createCard(bundle, state) {
        var report = state && state.customImport ? state.customImport : {};
        var p = presence(state) || {};
        var progress = operationProgress(state);
        var global = effectiveGlobalOperation();
        var globalBusy = Boolean(global && global.active && global.bundleId !== bundle.id);
        var ownBusy = Boolean(busyActions[bundle.id]) || Boolean(progress && (progress.running || progress.resumable));
        var busy = globalBusy || ownBusy || uploadBusy;
        var status = statusPresentation(state, bundle.id);
        var primary = primaryPresentation(state);
        var card = create("article", "route-bundle-card route-custom-card ui-card");
        var summary = create("div", "route-card-summary");
        var service = create("div", "route-card-service");
        var logo = create("span", "route-card-logo route-custom-logo");
        var copy = create("div", "route-card-copy");
        var badge = create("span", "status-badge " + status.className, status.text);
        var metrics = create("div", "route-card-metrics");
        var notice = create("div", "route-card-notice " + (status.className.indexOf("danger") >= 0 ? "status-error" : status.className.indexOf("warning") >= 0 ? "status-warning" : status.className.indexOf("success") >= 0 ? "status-success" : "status-neutral"));
        var actions = create("div", "route-card-actions");
        var details = create("details", "route-custom-details");
        var detailsSummary = create("summary", "route-technical-summary", "Подробности импорта");
        var grid = create("div", "route-details-grid");
        var files = create("ul", "route-source-list");
        var fileItems = report.sourceFiles || [];
        var presentCount = p.registered ? Number(p.presentRouteCount || 0) + "/" + Number(p.expectedRouteCount || 0) : "—";
        var validateButton;
        var exportButton;
        var replaceButton;
        var removeButton;

        card.setAttribute("data-custom-route-card", bundle.id);
        logo.setAttribute("data-icon", "routes");
        logo.setAttribute("aria-hidden", "true");
        copy.append(create("span", "eyebrow route-card-eyebrow", "Пользовательский маршрут"), create("h2", "route-card-title", bundle.name), create("p", "route-card-description", bundle.description));
        service.append(logo, copy);
        badge.setAttribute("data-icon", status.icon);
        summary.append(service, badge);

        [[fileItems.length || bundle.sourceFileCount || 0, "файлов"], [report.exportRouteCount || bundle.exportRouteCount || state.routeCount || 0, "маршрутов"], [presentCount, "в Keenetic"]].forEach(function (item) {
            var metric = create("div", "route-card-metric");
            metric.append(create("strong", "", item[0]), create("span", "", item[1]));
            metrics.append(metric);
        });

        notice.append(create("span", "route-card-notice-icon"), create("p", "", noticeMessage(state, bundle)));
        notice.firstChild.setAttribute("data-icon", status.icon);

        validateButton = actionButton(bundle.id, "validate", busyActions[bundle.id] === "validate" ? "Проверка…" : "Проверить", "button-secondary", "search");
        exportButton = actionButton(bundle.id, "export", primary.text, "button-primary", primary.icon);
        replaceButton = actionButton(bundle.id, "replace", "Заменить файл", "button-secondary", "update");
        removeButton = actionButton(bundle.id, "remove", "Удалить", "button-danger-outline", "delete");
        if (primary.hidden) exportButton.hidden = true;
        actions.append(validateButton, exportButton, replaceButton, removeButton);

        validateButton.disabled = busy;
        exportButton.disabled = busy;
        replaceButton.disabled = busy;
        removeButton.disabled = busy;
        if (!primary.hidden && !busy && (hasDrift(state) || needsExport(state) || !isInstalled(state))) {
            exportButton.setAttribute("data-recommended-action", "true");
        } else if (!busy) {
            validateButton.classList.remove("button-secondary");
            validateButton.classList.add("button-primary");
            validateButton.setAttribute("data-recommended-action", "true");
        }

        grid.append(
            dataRow("Исходных файлов", fileItems.length || bundle.sourceFileCount || 0),
            dataRow("Канонических маршрутов", report.canonicalRouteCount || bundle.canonicalRouteCount || 0),
            dataRow("Маршрутов к установке", report.exportRouteCount || bundle.exportRouteCount || state.routeCount || 0),
            dataRow("Исходных строк", report.sourceRouteLineCount || "—"),
            dataRow("Исправлено адресов сети", report.normalizedNetworkCount || 0),
            dataRow("Удалено повторов", report.duplicateCount || 0),
            dataRow("Широких сетей /7–/8", report.broadRouteCount || 0),
            dataRow("Уникальных маршрутов", state.ownership ? Number(state.ownership.uniqueRouteCount || 0) : 0),
            dataRow("Общих с другими наборами", state.ownership ? Number(state.ownership.sharedRouteCount || 0) : 0),
            dataRow("Последняя проверка", formatDate(state.lastCheckedAt)),
            dataRow("Интерфейс", (state.exportBuild && state.exportBuild.targetInterfaceDisplay) ||
                (state.checkResult && state.checkResult.managedInterfaceDisplay) || "BROray"),
            dataRow("Метрика", (state.exportBuild && state.exportBuild.managedMetric) || 1200)
        );
        if (!fileItems.length) files.append(create("li", "route-source-empty", "Сведения об исходных файлах недоступны."));
        fileItems.forEach(function (file) {
            var item = create("li", "route-source-item");
            item.append(create("strong", "route-source-link", file.originalName || file.name || "BAT-файл"), create("span", "route-source-meta", Number(file.routeLineCount || file.routeCount || 0) + " строк · " + formatBytes(file.sizeBytes) + " · SHA " + String(file.sha256 || "—").slice(0, 12)));
            files.append(item);
        });
        details.append(detailsSummary, grid, create("h3", "route-source-title", "Исходные BAT-файлы"), files);
        card.append(summary, metrics, notice, actions, details);
        card.classList.toggle("is-paused", Boolean(progress && progress.resumable));
        scanIcons(card);
        return card;
    }

    function render() {
        var mount = byId("routes-bundles");
        var fragment = document.createDocumentFragment();
        var uploadButton = byId("routes-custom-upload");
        if (!mount) return;
        if (!bundles.length) {
            var empty = create("section", "routes-custom-empty ui-card");
            empty.append(create("span", "routes-custom-empty-icon"), create("div", "", "Пользовательские наборы пока не загружены."));
            empty.firstChild.setAttribute("data-icon", "routes");
            fragment.append(empty);
        } else {
            bundles.forEach(function (bundle) { fragment.append(createCard(bundle, states[bundle.id] || {})); });
        }
        mount.replaceChildren(fragment);
        if (uploadButton) uploadButton.disabled = uploadBusy || Boolean(effectiveGlobalOperation() && effectiveGlobalOperation().active);
        scanIcons(mount);
        updateSummary();
    }

    function adjustedNumber(node, customValue, totalBase) {
        var current;
        var previousCombined;
        var base;
        var combined;
        if (!node) return;
        current = parseInt(node.textContent, 10);
        previousCombined = parseInt(node.dataset.customCombined || "", 10);
        if (totalBase !== undefined) base = totalBase;
        else if (!isNaN(current) && current !== previousCombined) {
            node.dataset.customBase = String(current);
            base = current;
        } else base = parseInt(node.dataset.customBase || "0", 10);
        combined = String(base + customValue);
        node.dataset.customCombined = combined;
        if (node.textContent !== combined) node.textContent = combined;
    }

    function updateSummary() {
        var totals = latestSummary && latestSummary.totals ? latestSummary.totals : {};
        var status = byId("routes-page-status");
        var message = byId("routes-summary-message");
        var operation = latestSummary && latestSummary.operation ? latestSummary.operation : null;
        byId("routes-installed-count").textContent = String(Number(totals.installedCount || 0));
        byId("routes-attention-count").textContent = String(Number(totals.attentionCount || 0));
        byId("routes-total-count").textContent = String(Number(totals.bundleCount || 0));
        if (message) {
            message.textContent = operation && operation.active
                ? "Выполняется операция с набором «" + (operation.bundleName || operation.bundleId || "маршрутов") + "». Прогресс показан в общем окне."
                : Number(totals.attentionCount || 0) > 0
                    ? "Некоторые пользовательские наборы требуют действия."
                    : bundles.length
                        ? "Состояние пользовательских наборов получено."
                        : "Загрузите BAT-файлы, чтобы создать первый пользовательский набор.";
        }
        if (status) {
            if (operation && operation.active) {
                status.className = "status-badge status-badge-warning";
                status.textContent = operation.resumable ? "Можно продолжить" : "Операция выполняется";
            } else if (Number(totals.attentionCount || 0) > 0) {
                status.className = "status-badge status-badge-warning";
                status.textContent = "Требуется действие";
            } else {
                status.className = "status-badge status-badge-success";
                status.textContent = "Состояние получено";
            }
            status.setAttribute("data-icon", "status");
            scanIcons(status);
        }
    }

    function watchSummary() {
        return;
    }

    function updateGlobalFromState(state) {
        if (state && state.globalOperation && state.globalOperation.active) globalOperation = state.globalOperation;
        else if (!Object.keys(longRequests).length && !Object.keys(busyActions).length) globalOperation = null;
    }

    function onAuthoritativeOperation(event) {
        var detail = event && event.detail ? event.detail : {active:false};
        var authoritative = detail.globalOperation && detail.globalOperation.active
            ? detail.globalOperation
            : (detail.active ? detail : null);
        if (authoritative) {
            globalOperation = authoritative;
        } else if (!uploadBusy && !Object.keys(longRequests).length && !Object.keys(busyActions).length) {
            globalOperation = null;
            peerOperation = null;
        }
        render();
    }

    function loadState(bundleId) {
        if (states[bundleId]) return Promise.resolve(states[bundleId]);
        return load().then(function () { return states[bundleId] || null; });
    }

    function load() {
        if (summaryRequest) return summaryRequest;
        summaryController = window.AbortController ? new AbortController() : null;
        summaryRequest = request("/api/routes/custom-summary.cgi", {
            method: "GET",
            credentials: "same-origin",
            signal: summaryController ? summaryController.signal : undefined
        }).then(function (summary) {
            var nextStates = Object.create(null);
            latestSummary = summary || null;
            bundles = (summary && Array.isArray(summary.bundles) ? summary.bundles : []).map(function (state) {
                var meta = state.metadata || {};
                nextStates[state.id] = state;
                return {
                    id: state.id,
                    name: meta.name || state.id,
                    description: meta.description || "Пользовательский набор маршрутов.",
                    canonicalRouteCount: Number(meta.canonicalRouteCount || state.routeCount || 0),
                    exportRouteCount: Number(meta.exportRouteCount || state.routeCount || 0),
                    sourceFileCount: Number(meta.sourceFileCount || 0),
                    createdAt: meta.createdAt || null,
                    updatedAt: meta.updatedAt || null
                };
            });
            states = nextStates;
            globalOperation = summary && summary.globalOperation && summary.globalOperation.active
                ? summary.globalOperation
                : null;
            render();
            return summary;
        }).catch(function (error) {
            if (error && error.name === "AbortError") return null;
            throw error;
        }).then(function (value) {
            summaryRequest = null;
            summaryController = null;
            return value;
        }, function (error) {
            summaryRequest = null;
            summaryController = null;
            throw error;
        });
        return summaryRequest;
    }

    function loadProgress() {
        if (window.BROrayRoutesOperationUI) return window.BROrayRoutesOperationUI.refresh();
        return Promise.resolve(null);
    }

    function watchProgress(bundleId) {
        if (window.BROrayRoutesOperationUI) window.BROrayRoutesOperationUI.notifyStarted(bundleId);
    }

    function globalMessage(error) {
        return error && error.message ? error.message + (error.details ? " " + error.details : "") : "Операция не завершена.";
    }

    function runShort(bundleId, action, url) {
        busyActions[bundleId] = action;
        render();
        return withTimeout(request(url, {method: "POST", credentials: "same-origin", headers: {"Accept": "application/json"}, body: {}}), REQUEST_TIMEOUT_MS).then(function (state) {
            if (state && typeof state === "object") states[bundleId] = state;
            toast(action === "validate" ? "Пользовательский набор проверен." : "Операция завершена.", "success");
        }).catch(function (error) {
            if (error && error.status === 401) return window.BROrayUI.redirectToLogin();
            toast(globalMessage(error), "error");
        }).then(function () {
            delete busyActions[bundleId];
            return load();
        });
    }

    function formatKilobytes(value) {
        var number = Number(value || 0);
        if (number >= 1024) return (number / 1024).toFixed(number >= 10240 ? 0 : 1) + " МБ";
        return number + " КБ";
    }

    function preflightMessage(preflight) {
        var checks = preflight && preflight.checks ? preflight.checks : {};
        var summary = preflight && preflight.summary ? preflight.summary : {};
        var storage = checks.storage || {};
        var localSet = checks.localSet || {};
        var lines = [];
        lines.push(preflight.message || "Предварительная проверка завершена.");
        lines.push("");
        lines.push((checks.operationLock && checks.operationLock.ok ? "✓" : "✕") + " Конфликтующих операций нет");
        lines.push((checks.ndmc && checks.ndmc.ok ? "✓" : "✕") + " Команда ndmc доступна");
        lines.push((storage.ok ? "✓" : "✕") + " Свободное место: " + formatKilobytes(storage.freeKb) + "; требуется " + formatKilobytes(storage.requiredKb));
        lines.push((localSet.ok ? "✓" : "✕") + " Локальный набор: " + Number(localSet.routeCount || 0) + " маршрутов");
        lines.push("");
        lines.push("Всего: " + Number(summary.total || 0));
        if (Number(summary.toCreate || 0)) lines.push("Будет добавлено: " + Number(summary.toCreate || 0));
        if (Number(summary.toDelete || 0)) lines.push("Будет удалено: " + Number(summary.toDelete || 0));
        if (Number(summary.sharedKept || 0)) lines.push("Общие маршруты сохранятся: " + Number(summary.sharedKept || 0));
        if (Number(summary.externalKept || 0)) lines.push("Внешние маршруты не затрагиваются: " + Number(summary.externalKept || 0));
        if (preflight.requestedAction === "resume" && preflight.resume) lines.push("Продолжение: " + Number(preflight.resume.current || 0) + " из " + Number(preflight.resume.total || 0));
        return lines.join("\n");
    }

    function confirmPreflight(bundle, preflight) {
        var danger = preflight.operation === "delete";
        var title = danger ? "Удалить маршруты «" + bundle.name + "»" :
            preflight.requestedAction === "resume" ? "Продолжить «" + bundle.name + "»" :
                (preflight.operation === "update" ? "Обновить «" + bundle.name + "»" :
                    preflight.operation === "restore" ? "Восстановить «" + bundle.name + "»" : "Установить «" + bundle.name + "»");
        if (!preflight || preflight.ready !== true) return Promise.reject(new Error(preflightMessage(preflight || {})));
        if (!window.BROrayDialogs || typeof window.BROrayDialogs.confirm !== "function") return Promise.reject(new Error("Фирменное окно подтверждения недоступно."));
        return window.BROrayDialogs.confirm({
            eyebrow: "Предварительная проверка",
            title: title,
            message: preflightMessage(preflight),
            confirmText: preflight.requestedAction === "resume" ? "Продолжить" : danger ? "Удалить" : preflight.operation === "update" ? "Обновить" : preflight.operation === "restore" ? "Восстановить" : "Установить",
            cancelText: "Отмена",
            variant: danger ? "danger" : "primary",
            icon: danger ? "delete" : "security"
        });
    }

    function prepareLong(bundle, action) {
        busyActions[bundle.id] = "preflight";
        render();
        if (window.BROrayRoutesOperationUI) {
            window.BROrayRoutesOperationUI.showPending(
                bundle.id,
                bundle.name,
                "custom",
                "preflight",
                "Выполняется предварительная проверка набора и состояния Keenetic…",
                "preflight",
                false
            );
        }
        return withTimeout(request("/api/routes/preflight.cgi?bundleId=" + encodeURIComponent(bundle.id) + "&action=" + encodeURIComponent(action), {
            method: "POST", credentials: "same-origin", headers: {"Accept": "application/json"}, body: {}
        }), REQUEST_TIMEOUT_MS).then(function (preflight) {
            delete busyActions[bundle.id];
            render();
            if (window.BROrayRoutesOperationUI) {
                window.BROrayRoutesOperationUI.showPending(
                    bundle.id,
                    bundle.name,
                    "custom",
                    "preflight",
                    "Предварительная проверка завершена. Подтвердите действие.",
                    "confirmation",
                    false
                );
            }
            return confirmPreflight(bundle, preflight).then(function (confirmed) {
                if (confirmed) return executeLong(bundle, action, preflight.token);
                if (window.BROrayRoutesOperationUI) window.BROrayRoutesOperationUI.clearPending(false);
                return false;
            });
        }).catch(function (error) {
            delete busyActions[bundle.id];
            render();
            if (window.BROrayRoutesOperationUI) window.BROrayRoutesOperationUI.clearPending(false);
            if (error && error.status === 401) return window.BROrayUI.redirectToLogin();
            toast(globalMessage(error), "error");
            return false;
        });
    }

    function executeLong(bundle, action, token) {
        var url = "/api/routes/" + action + ".cgi?bundleId=" + encodeURIComponent(bundle.id) + "&preflightToken=" + encodeURIComponent(token || "");
        busyActions[bundle.id] = action;
        longRequests[bundle.id] = true;
        render();
        if (window.BROrayRoutesOperationUI) {
            window.BROrayRoutesOperationUI.notifyStarted(bundle.id, bundle.name, "custom", action);
        }
        var operationPromise = withTimeout(request(url, {
            method: "POST",
            credentials: "same-origin",
            headers: {"Accept": "application/json"},
            body: {}
        }), LONG_OPERATION_TIMEOUT_MS);
        return operationPromise.then(function (newState) {
            var progress = operationProgress(newState);
            if (progress && progress.resumable) toast(progress.message || "Операция приостановлена. Её можно продолжить.", "warning");
            else toast(action === "delete" ? "Маршруты удалены из Keenetic." : "Операция с маршрутами завершена.", "success");
            return !(progress && progress.resumable);
        }).catch(function (error) {
            if (error && error.status === 401) window.BROrayUI.redirectToLogin();
            else toast(globalMessage(error), "error");
            return false;
        }).then(function (completed) {
            delete longRequests[bundle.id];
            delete busyActions[bundle.id];
            if (window.BROrayRoutesOperationUI) window.BROrayRoutesOperationUI.clearPending(true);
            return load().catch(function () { return null; }).then(function () { return completed; });
        });
    }

    function requestStop() {
        if (window.BROrayRoutesOperationUI && typeof window.BROrayRoutesOperationUI.requestStop === "function") {
            return window.BROrayRoutesOperationUI.requestStop();
        }
        return Promise.reject(new Error("Модуль безопасной остановки недоступен."));
    }

    function confirmLocalRemove(bundle) {
        if (!window.BROrayDialogs || typeof window.BROrayDialogs.confirm !== "function") return Promise.reject(new Error("Фирменное окно подтверждения недоступно."));
        return window.BROrayDialogs.confirm({
            eyebrow: "Опасное действие",
            title: "Удалить пользовательский набор",
            message: "Локальная карточка «" + bundle.name + "» и её BAT-файлы будут удалены. Маршруты в Keenetic предварительно удаляются безопасной операцией.",
            confirmText: "Удалить",
            cancelText: "Отмена",
            variant: "danger",
            icon: "delete"
        });
    }

    function finalizeRemove(bundle) {
        busyActions[bundle.id] = "remove";
        render();
        return request("/api/routes/custom-remove.cgi?bundleId=" + encodeURIComponent(bundle.id), {method: "POST", credentials: "same-origin", headers: {"Accept": "application/json"}, body: {}}).then(function () {
            delete states[bundle.id];
            toast("Пользовательский набор удалён.", "success");
        }).catch(function (error) {
            toast(globalMessage(error), "error");
        }).then(function () {
            delete busyActions[bundle.id];
            return load();
        });
    }

    function removeBundle(bundle) {
        var state = states[bundle.id] || {};
        confirmLocalRemove(bundle).then(function (confirmed) {
            if (!confirmed) return null;
            if (isInstalled(state) || state.installedVersion || hasDrift(state)) {
                return prepareLong(bundle, "delete").then(function (completed) {
                    var refreshed = states[bundle.id] || {};
                    if (completed && !isInstalled(refreshed) && !refreshed.installedVersion && !(operationProgress(refreshed) || {}).resumable) return finalizeRemove(bundle);
                    return null;
                });
            }
            return finalizeRemove(bundle);
        }).catch(function (error) { toast(globalMessage(error), "error"); });
    }

    function onAction(event) {
        var button = event.currentTarget;
        var bundleId = button.getAttribute("data-bundle-id");
        var action = button.getAttribute("data-custom-action");
        var bundle = bundleById(bundleId);
        var progress = bundle ? operationProgress(states[bundle.id]) : null;
        if (!bundle) return;
        if (action === "stop") return requestStop(bundle);
        if (action === "resume") return prepareLong(bundle, "resume");
        if (effectiveGlobalOperation() && effectiveGlobalOperation().active && effectiveGlobalOperation().bundleId !== bundleId) return;
        if (busyActions[bundleId]) return;
        if (action === "replace") return openUpload(bundle);
        if (action === "validate") return runShort(bundleId, action, "/api/routes/custom-validate.cgi?bundleId=" + encodeURIComponent(bundleId));
        if (action === "export") return prepareLong(bundle, "export");
        if (action === "remove") return removeBundle(bundle);
        if (progress && progress.resumable) return prepareLong(bundle, "resume");
    }

    function bytesToBase64(buffer) {
        var bytes = new Uint8Array(buffer);
        var chunk = 0x8000;
        var binary = "";
        var index;
        for (index = 0; index < bytes.length; index += chunk) binary += String.fromCharCode.apply(null, bytes.subarray(index, Math.min(index + chunk, bytes.length)));
        return window.btoa(binary);
    }

    function readSelectedFiles(fileList) {
        var files = Array.prototype.slice.call(fileList || []);
        var total = files.reduce(function (sum, file) { return sum + Number(file.size || 0); }, 0);
        if (!files.length) return Promise.reject(new Error("Выберите хотя бы один BAT-файл."));
        if (files.length > 16) return Promise.reject(new Error("Разрешено не более 16 BAT-файлов."));
        if (total > MAX_TOTAL_BYTES) return Promise.reject(new Error("Общий размер файлов превышает 2 МБ."));
        if (files.some(function (file) { return !/\.bat$/i.test(file.name); })) return Promise.reject(new Error("Разрешены только файлы с расширением .bat."));
        return Promise.all(files.map(function (file) {
            return file.arrayBuffer().then(function (buffer) { return {name: file.name, contentBase64: bytesToBase64(buffer)}; });
        }));
    }

    function ensureModal() {
        var root;
        var form;
        var actions;
        if (modal) return modal;
        root = create("div", "routes-upload-root");
        root.hidden = true;
        root.innerHTML = '<button class="modal-backdrop routes-upload-backdrop" type="button" aria-label="Закрыть"></button>' +
            '<section class="modal routes-upload-modal" role="dialog" aria-modal="true" aria-labelledby="routes-upload-title">' +
            '<span class="eyebrow">Пользовательский маршрут</span><h2 id="routes-upload-title">Загрузить BAT-файлы</h2>' +
            '<p class="routes-upload-intro">Файлы разбираются как текст. Команды из них никогда не выполняются.</p>' +
            '<form id="routes-upload-form"><label class="routes-upload-field"><span>Название карточки</span><input id="routes-upload-name" type="text" maxlength="80" required autocomplete="off"></label>' +
            '<label class="routes-upload-field"><span>BAT-файлы</span><input id="routes-upload-files" type="file" accept=".bat,application/x-bat,text/plain" multiple required></label>' +
            '<p class="modal-input-hint">До 16 файлов, общий размер до 2 МБ.</p><div id="routes-upload-result" class="routes-upload-result" hidden></div>' +
            '<div class="modal-actions routes-upload-actions"><button class="button button-secondary" data-upload-action="preview" type="submit" data-icon="search">Проверить</button>' +
            '<button class="button button-primary" data-upload-action="commit" type="button" data-icon="routes" disabled hidden>Создать набор</button>' +
            '<button class="button button-secondary" data-upload-action="cancel" type="button">Отмена</button></div></form></section>';
        document.body.append(root);
        form = root.querySelector("#routes-upload-form");
        actions = root.querySelector(".routes-upload-actions");
        root.querySelector(".routes-upload-backdrop").addEventListener("click", closeUpload);
        actions.querySelector('[data-upload-action="cancel"]').addEventListener("click", closeUpload);
        actions.querySelector('[data-upload-action="commit"]').addEventListener("click", commitUpload);
        form.addEventListener("submit", previewUpload);
        root.querySelector("#routes-upload-files").addEventListener("change", function () { preview = null; showPreview(null); });
        root.querySelector("#routes-upload-name").addEventListener("input", function () { preview = null; showPreview(null); });
        modal = root;
        scanIcons(root);
        return modal;
    }

    function openUpload(bundle) {
        var root = ensureModal();
        if (effectiveGlobalOperation() && effectiveGlobalOperation().active) {
            toast("Завершите текущую операцию BROray или дождитесь обновления её состояния.", "warning");
            if (window.BROrayRoutesOperationUI) window.BROrayRoutesOperationUI.refresh();
            return;
        }
        replaceBundleId = bundle ? bundle.id : null;
        preview = null;
        root.querySelector("#routes-upload-title").textContent = bundle ? "Заменить BAT-файлы" : "Загрузить BAT-файлы";
        root.querySelector("#routes-upload-name").value = bundle ? bundle.name : "";
        root.querySelector("#routes-upload-files").value = "";
        root.querySelector('[data-upload-action="commit"]').textContent = bundle ? "Сохранить замену" : "Создать набор";
        showPreview(null);
        root.hidden = false;
        root.querySelector("#routes-upload-name").focus();
    }

    function closeUpload() {
        if (!modal || uploadBusy) return;
        modal.hidden = true;
        preview = null;
        replaceBundleId = null;
    }

    function showPreview(data, error) {
        var root = ensureModal();
        var box = root.querySelector("#routes-upload-result");
        var commit = root.querySelector('[data-upload-action="commit"]');
        box.textContent = "";
        commit.disabled = !data || uploadBusy;
        commit.hidden = !data;
        if (!data && !error) { box.hidden = true; return; }
        box.hidden = false;
        if (error) {
            box.className = "routes-upload-result status-error";
            box.append(create("strong", "", "Файл отклонён"), create("p", "", error));
            return;
        }
        box.className = "routes-upload-result " + (data.broadRouteCount > 0 ? "status-warning" : "status-success");
        box.append(create("strong", "", "Проверка завершена"));
        [["Строк маршрутов", data.sourceRouteLineCount], ["Канонических CIDR", data.canonicalRouteCount], ["К установке", data.exportRouteCount], ["Исправлено адресов сети", data.normalizedNetworkCount], ["Удалено повторов", data.duplicateCount], ["Широких сетей /7–/8", data.broadRouteCount]].forEach(function (item) {
            var row = create("div", "routes-upload-stat");
            row.append(create("span", "", item[0]), create("strong", "", item[1]));
            box.append(row);
        });
        if (data.warning) box.append(create("p", "routes-upload-warning", data.warning));
    }

    function previewUpload(event) {
        var root = ensureModal();
        var name = root.querySelector("#routes-upload-name").value.trim();
        var files = root.querySelector("#routes-upload-files").files;
        var previewButton = root.querySelector('[data-upload-action="preview"]');
        var previewError = null;
        event.preventDefault();
        if (!name) return showPreview(null, "Укажите название карточки.");
        uploadBusy = true;
        previewButton.disabled = true;
        previewButton.textContent = "Проверка…";
        Promise.resolve().then(function () { return readSelectedFiles(files); }).then(function (encoded) {
            return withTimeout(request("/api/routes/custom-preview.cgi", {
                method: "POST", credentials: "same-origin", headers: {"Content-Type": "application/json", "Accept": "application/json"}, body: JSON.stringify({name: name, files: encoded})
            }), REQUEST_TIMEOUT_MS);
        }).then(function (data) { preview = data; }).catch(function (error) {
            preview = null;
            previewError = globalMessage(error);
        }).then(function () {
            uploadBusy = false;
            previewButton.disabled = false;
            previewButton.textContent = "Проверить";
            showPreview(preview, previewError);
            render();
        });
    }

    function commitUpload() {
        var root = ensureModal();
        var name = root.querySelector("#routes-upload-name").value.trim();
        var commit = root.querySelector('[data-upload-action="commit"]');
        if (!preview || uploadBusy) return;
        uploadBusy = true;
        commit.disabled = true;
        commit.textContent = "Сохранение…";
        withTimeout(request("/api/routes/custom-commit.cgi", {
            method: "POST", credentials: "same-origin", headers: {"Content-Type": "application/json", "Accept": "application/json"}, body: JSON.stringify({token: preview.token, name: name, bundleId: replaceBundleId})
        }), REQUEST_TIMEOUT_MS).then(function () {
            toast(replaceBundleId ? "BAT-файлы заменены. Набор готов к обновлению." : "Пользовательский набор создан.", "success");
            modal.hidden = true;
            preview = null;
            replaceBundleId = null;
        }).catch(function (error) {
            commit.textContent = replaceBundleId ? "Сохранить замену" : "Создать набор";
            showPreview(preview, globalMessage(error));
        }).then(function () {
            uploadBusy = false;
            return load();
        });
    }

    function startPassiveRefresh() {
        function schedule() {
            if (passiveTimer) window.clearTimeout(passiveTimer);
            passiveTimer = null;
            if (document.hidden) return;
            passiveTimer = window.setTimeout(function () {
                passiveTimer = null;
                if (uploadBusy || Object.keys(longRequests).length || Object.keys(busyActions).length) {
                    schedule();
                    return;
                }
                load().catch(function () { return null; }).then(schedule);
            }, PASSIVE_REFRESH_INTERVAL_MS);
        }
        schedule();
    }

    function onPeerOperation() {
        return;
    }

    function onGlobalControl() {
        return;
    }

    function initialize() {
        var resumeId;
        if (!byId("routes-bundles")) return;
        createPanel();
        resumeId = new URLSearchParams(window.location.search).get("resume");

        document.addEventListener("broray:routes-operation-resume", function (event) {
            var detail = event && event.detail ? event.detail : {};
            var bundle = bundleById(detail.bundleId);
            if (!bundle || (detail.bundleType && detail.bundleType !== "custom")) return;
            event.preventDefault();
            prepareLong(bundle, "resume");
        });
        document.addEventListener("broray:routes-operation-finished", function () {
            load().catch(function () { return null; });
        });
        document.addEventListener("broray:routes-operation-authoritative", onAuthoritativeOperation);
        document.addEventListener("visibilitychange", function () {
            if (document.hidden) {
                if (passiveTimer) window.clearTimeout(passiveTimer);
                passiveTimer = null;
                if (summaryController) summaryController.abort();
            } else {
                load().catch(function () { return null; }).then(startPassiveRefresh);
            }
        });
        window.addEventListener("pagehide", function () {
            if (passiveTimer) window.clearTimeout(passiveTimer);
            if (summaryController) summaryController.abort();
        });

        request("/api/session.cgi", {method: "GET", credentials: "same-origin"}).then(function (session) {
            var app = byId("app");
            var loader = byId("page-loader");
            var user = byId("current-user");
            if (user) user.textContent = session && session.user ? session.user : "admin";
            if (loader) loader.hidden = true;
            if (app) app.hidden = false;
            return load();
        }).then(function () {
            startPassiveRefresh();
            if (resumeId) {
                var bundle = bundleById(resumeId);
                if (bundle) prepareLong(bundle, "resume");
            }
        }).catch(function (error) {
            if (error && error.status === 401) {
                window.BROrayUI.redirectToLogin();
                return;
            }
            var app = byId("app");
            var loader = byId("page-loader");
            if (loader) loader.hidden = true;
            if (app) app.hidden = false;
            toast(error.message || "Не удалось загрузить пользовательские маршруты.", "error");
        });
    }

    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", initialize, {once: true});
    else initialize();
})();
