(function () {
    "use strict";

    var MAX_TOTAL_BYTES = 2 * 1024 * 1024;
    var REQUEST_TIMEOUT_MS = 180000;
    var bundles = [];
    var states = Object.create(null);
    var busy = false;
    var summaryObserver = null;
    var modal = null;
    var preview = null;
    var replaceBundleId = null;

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

    function isAttention(state) {
        return Boolean(state && (state.lastError || hasDrift(state) || needsExport(state)));
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

    function createPanel() {
        var summary = document.querySelector(".routes-summary");
        var panel;
        var copy;
        var button;
        var list;
        var logo;

        if (!summary || byId("routes-custom-panel")) return;

        panel = create("section", "routes-custom-panel ui-card");
        panel.id = "routes-custom-panel";
        logo = create("span", "route-card-logo routes-custom-panel-logo");
        logo.setAttribute("data-icon", "security");
        logo.setAttribute("aria-hidden", "true");
        copy = create("div", "routes-custom-panel-copy");
        copy.append(
            create("span", "eyebrow", "Локальные файлы"),
            create("h2", "", "Свои маршруты"),
            create("p", "", "Загрузите один или несколько BAT-файлов. BROray проверит их как текст и никогда не будет выполнять команды.")
        );
        button = create("button", "button button-primary", "Загрузить BAT");
        button.type = "button";
        button.setAttribute("data-icon", "backup");
        button.addEventListener("click", function () { openUpload(null); });
        panel.append(logo, copy, button);

        list = create("div", "routes-bundle-list routes-custom-list");
        list.id = "routes-custom-bundles";
        list.setAttribute("aria-live", "polite");

        summary.insertAdjacentElement("afterend", panel);
        panel.insertAdjacentElement("afterend", list);
        scanIcons(panel);
    }

    function statusPresentation(state) {
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
        if (state && state.lastError) {
            return typeof state.lastError === "string" ? state.lastError : (state.lastError.message || "Последняя операция завершилась ошибкой.");
        }
        if (hasDrift(state)) {
            var currentPresence = presence(state) || {};
            var expected = Number(currentPresence.expectedRouteCount || 0);
            var present = Number(currentPresence.presentRouteCount || 0);
            var missing = Math.max(0, expected - present);
            return "Маршруты были установлены ранее, но сейчас в Keenetic отсутствуют " +
                missing + " из " + expected + ". Нажмите «Восстановить в Keenetic».";
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
        button.setAttribute("data-icon", icon);
        button.addEventListener("click", onAction);
        return button;
    }

    function dataRow(label, value) {
        var row = create("div", "ui-data-row");
        row.append(create("span", "ui-data-row__label", label), create("strong", "ui-data-row__value", value));
        return row;
    }

    function createCard(bundle, state) {
        var report = state && state.customImport ? state.customImport : {};
        var p = presence(state) || {};
        var status = statusPresentation(state);
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

        card.setAttribute("data-custom-route-card", bundle.id);
        logo.setAttribute("data-icon", "routes");
        logo.setAttribute("aria-hidden", "true");
        copy.append(create("span", "eyebrow route-card-eyebrow", "Пользовательский маршрут"), create("h2", "route-card-title", bundle.name), create("p", "route-card-description", bundle.description));
        service.append(logo, copy);
        badge.setAttribute("data-icon", status.icon);
        summary.append(service, badge);

        [[report.canonicalRouteCount || bundle.canonicalRouteCount || 0, "канонических"], [report.exportRouteCount || bundle.exportRouteCount || state.routeCount || 0, "к установке"], [presentCount, "в Keenetic"]].forEach(function (item) {
            var metric = create("div", "route-card-metric");
            metric.append(create("strong", "", item[0]), create("span", "", item[1]));
            metrics.append(metric);
        });

        notice.append(create("span", "route-card-notice-icon"), create("p", "", noticeMessage(state, bundle)));
        notice.firstChild.setAttribute("data-icon", status.icon);

        actions.append(
            actionButton(bundle.id, "validate", "Проверить", "button-secondary", "search"),
            actionButton(bundle.id, "export", primary.text, "button-primary", primary.icon),
            actionButton(bundle.id, "replace", "Заменить файл", "button-secondary", "update"),
            actionButton(bundle.id, "remove", "Удалить", "button-danger-outline", "delete")
        );
        if (primary.hidden) actions.children[1].hidden = true;

        grid.append(
            dataRow("Исходных строк", report.sourceRouteLineCount || "—"),
            dataRow("Исправлено адресов сети", report.normalizedNetworkCount || 0),
            dataRow("Удалено повторов", report.duplicateCount || 0),
            dataRow("Широких сетей /7–/8", report.broadRouteCount || 0),
            dataRow("Последняя проверка", formatDate(state.lastCheckedAt)),
            dataRow("Интерфейс", (state.exportBuild && state.exportBuild.targetInterface) || "ProxyN"),
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
        Array.prototype.forEach.call(card.querySelectorAll("button"), function (button) { button.disabled = busy; });
        scanIcons(card);
        return card;
    }

    function render() {
        var mount = byId("routes-custom-bundles");
        var fragment = document.createDocumentFragment();
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
        if (totalBase !== undefined) {
            base = totalBase;
        } else if (!isNaN(current) && current !== previousCombined) {
            node.dataset.customBase = String(current);
            base = current;
        } else {
            base = parseInt(node.dataset.customBase || "0", 10);
        }
        combined = String(base + customValue);
        node.dataset.customCombined = combined;
        if (node.textContent !== combined) {
            node.textContent = combined;
        }
    }

    function updateSummary() {
        var installed = 0;
        bundles.forEach(function (bundle) {
            var state = states[bundle.id];
            if (isInstalled(state)) installed += 1;
        });
        adjustedNumber(byId("routes-installed-count"), installed);
        // Локальные наборы не участвуют в счётчике обновлений GitHub.
        adjustedNumber(byId("routes-total-count"), bundles.length, 9);
    }

    function watchSummary() {
        var nodes = [byId("routes-installed-count")].filter(Boolean);
        if (!nodes.length || !window.MutationObserver) return;
        summaryObserver = new MutationObserver(function () { window.setTimeout(updateSummary, 0); });
        nodes.forEach(function (node) { summaryObserver.observe(node, {childList: true, characterData: true, subtree: true}); });
    }

    function load() {
        return request("/api/routes/custom-list.cgi", {method: "GET", credentials: "same-origin"}).then(function (data) {
            bundles = data && Array.isArray(data.bundles) ? data.bundles : [];
            return Promise.all(bundles.map(function (bundle) {
                return request("/api/routes/custom-status.cgi?bundleId=" + encodeURIComponent(bundle.id), {method: "GET", credentials: "same-origin"}).then(function (state) {
                    states[bundle.id] = state;
                }).catch(function (error) {
                    states[bundle.id] = {lastError: {message: error.message}};
                });
            }));
        }).then(render);
    }

    function setBusy(value) {
        busy = value;
        render();
    }

    function runAction(bundleId, action, url) {
        setBusy(true);
        return withTimeout(request(url, {method: "POST", credentials: "same-origin", headers: {"Accept": "application/json"}}), REQUEST_TIMEOUT_MS).then(function () {
            toast(action === "validate" ? "Пользовательский набор проверен." : "Операция с маршрутами завершена.", "success");
        }).catch(function (error) {
            if (error && error.status === 401) return window.BROrayUI.redirectToLogin();
            toast(error.message + (error.details ? " " + error.details : ""), "error");
        }).then(function () {
            busy = false;
            return load();
        });
    }

    function syncPlanMessage(plan) {
        var summary = plan.summary || {};
        if (plan.mode === "update") {
            return "Новая версия: добавлено " + Number(summary.addedRoutes || 0) +
                ", удалено " + Number(summary.removedRoutes || 0) +
                ", без изменений " + Number(summary.unchangedRoutes || 0) +
                ". Из Keenetic будут удалены " + Number(summary.toDelete || 0) +
                " маршрутов. Ещё " + Number(summary.sharedKept || 0) +
                " используются другими наборами и сохранятся.";
        }
        if (plan.mode === "restore") {
            return "В Keenetic будут восстановлены " +
                Number(summary.toCreate || 0) + " маршрутов.";
        }
        return "В Keenetic будут установлены " + Number(summary.total || 0) + " маршрутов.";
    }

    function confirmSync(bundle, plan) {
        var primary = primaryPresentation(states[bundle.id] || {});
        if (!plan.canApply) return Promise.reject(new Error(plan.message || "План содержит конфликты."));
        if (plan.mode === "none") return Promise.resolve(false);
        if (!window.BROrayDialogs || typeof window.BROrayDialogs.confirm !== "function") {
            return Promise.reject(new Error("Фирменное окно подтверждения недоступно."));
        }
        return window.BROrayDialogs.confirm({
            eyebrow: "Маршруты Keenetic",
            title: primary.text,
            message: syncPlanMessage(plan),
            confirmText: primary.text,
            cancelText: "Отмена"
        });
    }

    function prepareSync(bundle) {
        setBusy(true);
        return withTimeout(request("/api/routes/plan.cgi?bundleId=" + encodeURIComponent(bundle.id), {
            method: "POST",
            credentials: "same-origin",
            headers: {"Accept": "application/json"}
        }), REQUEST_TIMEOUT_MS).then(function (plan) {
            busy = false;
            render();
            if (plan.mode === "none") {
                toast("Изменения в Keenetic не требуются.", "success");
                return false;
            }
            return confirmSync(bundle, plan);
        }).then(function (confirmed) {
            if (confirmed) {
                return runAction(
                    bundle.id,
                    "export",
                    "/api/routes/export.cgi?bundleId=" + encodeURIComponent(bundle.id)
                );
            }
            return null;
        }).catch(function (error) {
            busy = false;
            render();
            if (error && error.status === 401) return window.BROrayUI.redirectToLogin();
            toast(error && error.message ? error.message : "Не удалось подготовить план установки.", "error");
            return null;
        });
    }

    function confirmRemove(bundle) {
        if (!window.BROrayDialogs || typeof window.BROrayDialogs.confirm !== "function") {
            return Promise.reject(new Error("Фирменное окно подтверждения недоступно."));
        }
        return window.BROrayDialogs.confirm({
            eyebrow: "Опасное действие",
            title: "Удаление пользовательского набора",
            message: "Удалить «" + bundle.name + "»? Маршруты будут безопасно удалены из Keenetic, а общие маршруты других наборов сохранятся.",
            confirmText: "Удалить",
            cancelText: "Отмена",
            variant: "danger"
        });
    }

    function onAction(event) {
        var button = event.currentTarget;
        var bundleId = button.getAttribute("data-bundle-id");
        var action = button.getAttribute("data-custom-action");
        var bundle = bundles.filter(function (item) { return item.id === bundleId; })[0];
        if (!bundle || busy) return;
        if (action === "replace") return openUpload(bundle);
        if (action === "validate") return runAction(bundleId, action, "/api/routes/custom-validate.cgi?bundleId=" + encodeURIComponent(bundleId));
        if (action === "export") return prepareSync(bundle);
        if (action === "remove") {
            confirmRemove(bundle).then(function (confirmed) {
                if (confirmed) runAction(bundleId, action, "/api/routes/custom-remove.cgi?bundleId=" + encodeURIComponent(bundleId));
            });
        }
    }

    function bytesToBase64(buffer) {
        var bytes = new Uint8Array(buffer);
        var chunk = 0x8000;
        var binary = "";
        var index;
        for (index = 0; index < bytes.length; index += chunk) {
            binary += String.fromCharCode.apply(null, bytes.subarray(index, Math.min(index + chunk, bytes.length)));
        }
        return window.btoa(binary);
    }

    function readSelectedFiles(fileList) {
        var files = Array.prototype.slice.call(fileList || []);
        var total = files.reduce(function (sum, file) { return sum + file.size; }, 0);
        if (!files.length) return Promise.reject(new Error("Выберите хотя бы один BAT-файл."));
        if (files.length > 16) return Promise.reject(new Error("Разрешено не более 16 BAT-файлов."));
        if (total > MAX_TOTAL_BYTES) return Promise.reject(new Error("Общий размер файлов превышает 2 МБ."));
        if (files.some(function (file) { return !/\.bat$/i.test(file.name); })) {
            return Promise.reject(new Error("Разрешены только файлы с расширением .bat."));
        }
        return Promise.all(files.map(function (file) {
            return file.arrayBuffer().then(function (buffer) {
                return {name: file.name, contentBase64: bytesToBase64(buffer)};
            });
        }));
    }

    function ensureModal() {
        var root;
        var dialog;
        var form;
        var actions;
        if (modal) return modal;
        root = create("div", "routes-upload-root");
        root.hidden = true;
        root.innerHTML = '<button class="modal-backdrop routes-upload-backdrop" type="button" aria-label="Закрыть"></button>' +
            '<section class="modal routes-upload-modal" role="dialog" aria-modal="true" aria-labelledby="routes-upload-title">' +
            '<span class="eyebrow">Пользовательский маршрут</span>' +
            '<h2 id="routes-upload-title">Загрузить BAT-файлы</h2>' +
            '<p class="routes-upload-intro">Файлы разбираются как текст. Команды из них никогда не выполняются.</p>' +
            '<form id="routes-upload-form">' +
            '<label class="routes-upload-field"><span>Название карточки</span><input id="routes-upload-name" type="text" maxlength="80" required autocomplete="off"></label>' +
            '<label class="routes-upload-field"><span>BAT-файлы</span><input id="routes-upload-files" type="file" accept=".bat,application/x-bat,text/plain" multiple required></label>' +
            '<p class="modal-input-hint">До 16 файлов, общий размер до 2 МБ.</p>' +
            '<div id="routes-upload-result" class="routes-upload-result" hidden></div>' +
            '<div class="modal-actions routes-upload-actions"><button class="button button-secondary" data-upload-action="preview" type="submit" data-icon="search">Проверить</button><button class="button button-primary" data-upload-action="commit" type="button" data-icon="routes" disabled hidden>Создать набор</button><button class="button button-secondary" data-upload-action="cancel" type="button">Отмена</button></div>' +
            '</form></section>';
        document.body.append(root);
        dialog = root.querySelector(".routes-upload-modal");
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
        if (!modal || busy) return;
        modal.hidden = true;
        preview = null;
        replaceBundleId = null;
    }

    function showPreview(data, error) {
        var root = ensureModal();
        var box = root.querySelector("#routes-upload-result");
        var commit = root.querySelector('[data-upload-action="commit"]');
        box.textContent = "";
        commit.disabled = !data || busy;
        commit.hidden = !data;
        if (!data && !error) {
            box.hidden = true;
            return;
        }
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
        busy = true;
        previewButton.disabled = true;
        previewButton.textContent = "Проверка…";
        Promise.resolve().then(function () {
            return readSelectedFiles(files);
        }).then(function (encoded) {
            return withTimeout(request("/api/routes/custom-preview.cgi", {
                method: "POST",
                credentials: "same-origin",
                headers: {"Content-Type": "application/json", "Accept": "application/json"},
                body: JSON.stringify({name: name, files: encoded})
            }), REQUEST_TIMEOUT_MS);
        }).then(function (data) {
            preview = data;
        }).catch(function (error) {
            preview = null;
            previewError = error.message + (error.details ? " " + error.details : "");
        }).then(function () {
            busy = false;
            previewButton.disabled = false;
            previewButton.textContent = "Проверить";
            showPreview(preview, previewError);
            scanIcons(previewButton);
        });
    }

    function commitUpload() {
        var root = ensureModal();
        var name = root.querySelector("#routes-upload-name").value.trim();
        var commit = root.querySelector('[data-upload-action="commit"]');
        if (!preview || busy) return;
        busy = true;
        commit.disabled = true;
        commit.textContent = "Сохранение…";
        withTimeout(request("/api/routes/custom-commit.cgi", {
            method: "POST",
            credentials: "same-origin",
            headers: {"Content-Type": "application/json", "Accept": "application/json"},
            body: JSON.stringify({token: preview.token, name: name, bundleId: replaceBundleId})
        }), REQUEST_TIMEOUT_MS).then(function () {
            toast(replaceBundleId ? "BAT-файлы заменены. Набор готов к обновлению." : "Пользовательский набор создан.", "success");
            busy = false;
            modal.hidden = true;
            preview = null;
            replaceBundleId = null;
            return load();
        }).catch(function (error) {
            busy = false;
            commit.textContent = replaceBundleId ? "Сохранить замену" : "Создать набор";
            commit.disabled = false;
            showPreview(preview, error.message + (error.details ? " " + error.details : ""));
        });
    }

    function initialize() {
        if (!byId("routes-bundles")) return;
        createPanel();
        watchSummary();
        load().catch(function (error) {
            toast(error.message || "Не удалось загрузить пользовательские маршруты.", "error");
        });
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize, {once: true});
    } else {
        initialize();
    }
})();
