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
    var activeOperation = null;
    var releaseCatalog = [];
    var releaseContext = {};
    var catalogCurrentVersion = "";
    var selectedTag = "";
    var catalogLoading = false;
    var compatibilityLabels = {
        compatible: "Совместима с BROray",
        untested: "Не проверялась на совместимость с BROray",
        incompatible: "Несовместима с BROray"
    };
    var latestUpdate = {
        checked: false,
        updateAvailable: false,
        installedNewer: false,
        reinstallAllowed: false,
        storageOk: true,
        requiredBytes: 0,
        freeBytes: 0,
        shortfallBytes: 0,
        latestVersion: "",
        channel: ""
    };

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

    function operationCopy(mode) {
        if (mode === "install") {
            return {runningTitle: "Установка выбранной версии Xray…",
                runningMessage: "Проверяются архив и текущая конфигурация. Не отключайте питание роутера.",
                successMessage: "Выбранная версия Xray установлена. Проверки завершены.",
                successToast: "Установка Xray завершена.", errorToast: "Не удалось установить выбранную версию Xray."};
        }
        if (mode === "update") {
            return {
                runningTitle: "Обновление Xray…",
                runningMessage: "Загружается и проверяется новая официальная версия. Не отключайте питание роутера.",
                successMessage: "Xray обновлён и запущен.",
                successToast: "Обновление Xray завершено.",
                errorToast: "Не удалось обновить Xray."
            };
        }

        return {
            runningTitle: "Переустановка Xray…",
            runningMessage: "Текущая официальная версия переустанавливается. Не отключайте питание роутера.",
            successMessage: "Xray переустановлен и запущен.",
            successToast: "Переустановка Xray завершена.",
            errorToast: "Не удалось переустановить Xray."
        };
    }

    function renderUpdateState(data) {
        latestUpdate.checked = true;
        latestUpdate.updateAvailable = data.updateAvailable === true;
        latestUpdate.installedNewer = data.installedNewer === true;
        latestUpdate.latestVersion = data.latestVersion || "";
        latestUpdate.channel = data.channel || "";
        latestUpdate.reinstallAllowed = Boolean(
            data.temporaryStorage && data.temporaryStorage.reinstallAllowed === true
        );
        latestUpdate.storageOk = !(data.storage && data.storage.ok === false);
        latestUpdate.optFreeBytes = Number(data.storage && data.storage.freeBytes || 0);
        latestUpdate.requiredBytes = Number(data.temporaryStorage && data.temporaryStorage.requiredBytes || 0);
        latestUpdate.freeBytes = Number(data.temporaryStorage && data.temporaryStorage.freeBytes || 0);
        latestUpdate.shortfallBytes = Number(data.temporaryStorage && data.temporaryStorage.shortfallBytes || 0);

        byId("installed-version").textContent = data.currentVersion || "—";
        byId("available-version").textContent = data.latestVersion || "—";
        byId("release-channel").textContent = data.channel === "pre-release"
            ? "Предварительный"
            : "Стабильный";
        byId("update-message").textContent = !latestUpdate.storageOk
            ? "Недостаточно места в /opt: требуется " + formatBytes(Number(data.storage.requiredBytes || 0)) +
                ", доступно " + formatBytes(Number(data.storage.freeBytes || 0)) +
                ". Освободите ещё " + formatBytes(Number(data.storage.shortfallBytes || 0)) + "."
            : !latestUpdate.reinstallAllowed
            ? "Недостаточно временной памяти: требуется " + formatBytes(latestUpdate.requiredBytes) +
                ", доступно " + formatBytes(latestUpdate.freeBytes) +
                ". Освободите ещё " + formatBytes(latestUpdate.shortfallBytes) + "."
            : (data.message || (
                latestUpdate.updateAvailable
                ? "Доступна новая версия Xray."
                : "Установлена актуальная версия Xray."
            ));

        releaseCatalog = Array.isArray(data.releases) ? data.releases : [];
        releaseContext = data.context || {};
        catalogCurrentVersion = data.currentVersion || "";
        byId("update-message").textContent = data.catalogComplete === false
            ? "Список получен частично: показаны найденные официальные версии."
            : "Список обновлён. Совместимость указана для текущей сборки BROray";
        renderReleaseChoices();
    }

    function selectedRelease() {
        return releaseCatalog.find(function (row) { return row.tagName === selectedTag; });
    }

    function compatibilityStatus(row) {
        var status = row && row.compatibility && row.compatibility.status;
        return Object.prototype.hasOwnProperty.call(compatibilityLabels, status) ? status : "untested";
    }

    function installable(row) {
        return Boolean(row && row.available === true && /^[a-f0-9]{64}$/.test(row.archiveSha256 || "") &&
            compatibilityStatus(row) !== "incompatible" && catalogCurrentVersion &&
            spaceReady(row) &&
            (!latestStatus || latestStatus.version === catalogCurrentVersion));
    }

    function spaceReady(row) {
        return latestUpdate.freeBytes >= latestUpdate.requiredBytes &&
            latestUpdate.optFreeBytes >= Math.max(latestUpdate.requiredBytes, Number(row.asset && row.asset.size || 0) + 1048576);
    }

    function isDowngrade(row) {
        function key(version) {
            var parts = String(version).split(/[.-]/).slice(0, 3);
            return Number(parts[0]) * 10000 + Number(parts[1]) * 100 + Number(parts[2]);
        }
        return key(row.version) < key(catalogCurrentVersion);
    }

    function releaseWarnings(row) {
        var warnings = [];
        if (!row.available || !/^[a-f0-9]{64}$/.test(row.archiveSha256 || "")) {
            warnings.push("Официальный архив этой версии не подтверждён. Установка недоступна.");
        }
        if (compatibilityStatus(row) === "incompatible") {
            warnings.push("Установка заблокирована: выявлена несовместимость с BROray.");
        } else if (compatibilityStatus(row) === "untested") {
            warnings.push("Мы не подтверждали совместимость этой версии с вашей сборкой BROray.");
        }
        if (row.prerelease === true) warnings.push("Это предварительный релиз Xray: возможны ошибки.");
        if (isDowngrade(row)) warnings.push("Будет выполнено понижение версии. Новые возможности текущего Xray могут отсутствовать.");
        if (!spaceReady(row)) warnings.push("Недостаточно свободного места в /opt или временной памяти для безопасной установки. Освободите место и повторите проверку версий.");
        return warnings.join(" ");
    }

    function renderSelectedRelease() {
        applyControlState();
    }

    function renderReleaseChoices() {
        var select = byId("xray-version-select");
        var showPre = byId("xray-show-prereleases").checked;
        var lastCompatible = releaseCatalog.reduce(function (latest, row) {
            if (compatibilityStatus(row) !== "compatible") return latest;
            return !latest || row.compatibility.testedAt > latest.compatibility.testedAt ? row : latest;
        }, null);
        var visible = releaseCatalog.filter(function (row) {
            return row.prerelease !== true || showPre || row.installed || row === lastCompatible;
        });
        select.textContent = "";
        if (!visible.some(function (row) { return row.tagName === selectedTag; })) {
            var preferred = visible.find(function (row) { return row.prerelease === false && installable(row); }) || visible[0];
            selectedTag = preferred ? preferred.tagName : "";
        }
        visible.forEach(function (row) {
            var option = document.createElement("option");
            option.value = row.tagName;
            option.textContent = row.version + (row.installed ? " · Установлена" : "") +
                (row.prerelease === true ? " · Предварительная" : row.prerelease === false ? " · Стабильная" : "") +
                " · " + compatibilityLabels[compatibilityStatus(row)];
            select.appendChild(option);
        });
        if (!visible.length) {
            var empty = document.createElement("option");
            empty.textContent = "Сначала проверьте версии";
            empty.value = "";
            select.appendChild(empty);
        }
        select.value = selectedTag;
        renderSelectedRelease();
    }

    function installRelease(row, button) {
        if (controlsLocked || catalogLoading || !installable(row)) return;
        var body = {tag: row.tagName, currentVersion: catalogCurrentVersion, archiveSha256: row.archiveSha256,
            allowUntested: compatibilityStatus(row) === "untested", allowPrerelease: row.prerelease === true,
            allowDowngrade: isDowngrade(row)};
        runAction("install.cgi", button, {
            body: body, busyLabel: "Запуск…", background: true, operation: "install",
            successMessage: "Установка выбранной версии запущена.",
            confirm: {title: (row.installed ? "Переустановить Xray " : "Установить Xray ") + row.version + "?",
                message: releaseWarnings(row) + " Архив и действующая конфигурация будут проверены. Соединение кратковременно прервётся, если Xray запущен. При ошибке замены будет выполнен откат.",
                acceptLabel: body.allowDowngrade ? "Подтвердить понижение" : "Подтвердить установку",
                danger: body.allowDowngrade || body.allowPrerelease || body.allowUntested}
        }).catch(function (error) { window.BROrayUI.toast(errorMessage(error), "error"); });
    }

    function markUpdateInstalled(result) {
        var version = result && result.version
            ? result.version
            : (latestUpdate.latestVersion || (latestStatus && latestStatus.version) || "");

        latestUpdate.checked = true;
        latestUpdate.updateAvailable = false;
        latestUpdate.installedNewer = false;
        latestUpdate.reinstallAllowed = true;
        latestUpdate.latestVersion = version;

        if (version) {
            byId("installed-version").textContent = version;
            byId("available-version").textContent = version;
        }
        byId("update-message").textContent = result && result.message
            ? result.message
            : "Установлена актуальная версия Xray.";

        applyControlState();
    }

    function applyControlState() {
        var running = latestStatus && latestStatus.running === true;
        var start = byId("xray-start");
        var stop = byId("xray-stop");
        var restart = byId("xray-restart");
        var update = byId("xray-update-install");
        var reinstall = byId("xray-reinstall");
        var configCheck = byId("xray-check-config");
        var updateCheck = byId("xray-update-check");
        var diagnostics = byId("xray-diagnostics");
        var updateParts;
        var updateEnabled = !catalogLoading && installable(selectedRelease());
        var installed = releaseCatalog.find(function (row) { return row.installed; });
        byId("xray-version-select").disabled = controlsLocked || catalogLoading || !releaseCatalog.length;
        byId("xray-show-prereleases").disabled = controlsLocked || catalogLoading || !releaseCatalog.length;

        if (controlsLocked) {
            [
                start,
                stop,
                restart,
                update,
                reinstall,
                configCheck,
                updateCheck,
                diagnostics
            ].forEach(function (control) {
                if (control) {
                    control.disabled = true;
                }
            });
            return;
        }

        start.disabled = running;
        stop.disabled = !running;
        restart.disabled = !running;

        if (update) {
            update.disabled = !updateEnabled;
            update.classList.toggle("button-primary", updateEnabled);
            update.classList.toggle("button-secondary", !updateEnabled);
            updateParts = ensureButtonParts(update);
            if (updateParts && !update.classList.contains("is-loading")) {
                updateParts.label.textContent = selectedRelease()
                    ? (selectedRelease().installed ? "Переустановить " : "Установить ") + selectedRelease().version
                    : "Установить выбранную версию";
            }
        }

        if (reinstall) {
            reinstall.disabled = catalogLoading || !installable(installed);
        }

        configCheck.disabled = false;
        updateCheck.disabled = catalogLoading;
        diagnostics.disabled = false;
    }

    function setXrayControlsLocked(locked) {
        controlsLocked = Boolean(locked);
        applyControlState();
    }

    function renderStatus(data) {
        var running = data.running === true;
        var socksActive = data.socksActive === true || (data.socks && data.socks.active === true);
        var health = data.health && typeof data.health === "object" ? data.health : null;
        var severity = health && health.severity ? health.severity : running && data.configValid && socksActive ? "ok" : "error";
        var reason = health && Array.isArray(health.reasons) && health.reasons.length ? health.reasons[0].message : null;
        var live = byId("xray-live-status");
        var badge = byId("xray-state-badge");
        var badgeClass = {
            ok: "status-badge-success",
            warning: "status-badge-warning",
            error: "status-badge-danger",
            busy: "status-loading",
            unknown: "status-neutral"
        }[severity] || "status-neutral";

        latestStatus = data;

        live.className = "xray-live-status " + (severity === "ok" ? "is-running" : "is-stopped");
        byId("xray-live-text").textContent = severity === "ok" ? "Xray работает" : running ? "Xray требует проверки" : "Xray остановлен";

        badge.textContent = severity === "ok" ? "Работает" : severity === "warning" ? "Требуется внимание" : severity === "busy" ? "Выполняется" : severity === "error" ? "Требуется исправление" : "Не проверено";
        badge.className = "status-badge " + badgeClass;

        byId("xray-version").textContent = data.version ? "Xray " + data.version : "Xray";
        byId("xray-runtime-description").textContent = severity === "ok"
            ? "Процесс запущен, конфигурация корректна, локальный SOCKS-интерфейс принимает подключения."
            : reason || (running ? "Процесс запущен, но работоспособность SOCKS не подтверждена." : "Процесс Xray сейчас не запущен.");
        byId("metric-pid").textContent = data.pid || "—";
        byId("metric-process").textContent = running ? "Процесс активен" : "Процесс отсутствует";
        byId("metric-socks").textContent = (data.socksAddress || "—") + ":" + (data.socksPort || "—");
        byId("metric-socks-status").textContent = socksActive ? "Принимает подключения" : running ? "Не отвечает" : "Недоступно";
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
            var payload = await request("/api/xray/" + endpoint, {method: "POST", body: options.body || {}});
            window.BROrayUI.toast(options.successMessage || "Операция выполнена.", "success");

            if (options.background === true) {
                keepLocked = true;
                activeOperation = options.operation || activeOperation || "reinstall";
                showOperation();
                byId("operation-title").textContent = operationCopy(activeOperation).runningTitle;
                byId("operation-message").textContent = operationCopy(activeOperation).runningMessage;
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
        if (catalogLoading || controlsLocked) return;
        catalogLoading = true;
        applyControlState();
        setButtonBusy(button, true, "Проверка…");
        try {
            var payload = await request("/api/xray/update-check.cgi", {method: "POST", body: {}});
            var data = payload.data || {};

            renderUpdateState(data);
            window.BROrayUI.toast("Список версий Xray обновлён.", "success");
        } catch (error) {
            releaseCatalog = [];
            latestUpdate.checked = false;
            renderReleaseChoices();
            byId("update-message").textContent = "Не удалось подтвердить список версий. Установка недоступна до повторной проверки. " + errorMessage(error);
            window.BROrayUI.toast(errorMessage(error), "error");
        } finally {
            catalogLoading = false;
            setButtonBusy(button, false);
            applyControlState();
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
            var mode = data.operation || (data.result && data.result.operation) || activeOperation || "reinstall";
            var copy = operationCopy(mode);

            activeOperation = mode;

            if (data.operationRunning) {
                showOperation();
                setXrayControlsLocked(true);
                byId("operation-title").textContent = copy.runningTitle;
                byId("operation-message").textContent = copy.runningMessage;
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
                byId("operation-title").textContent = success
                    ? "Операция завершена"
                    : "Операция завершилась ошибкой";
                byId("operation-message").textContent = success
                    ? (data.result.message || copy.successMessage)
                    : (data.result.error || "Не удалось завершить операцию.");

                if (success) {
                    markUpdateInstalled(data.result.result || data.result);
                }
                releaseCatalog = [];
                renderReleaseChoices();

                window.BROrayUI.toast(
                    success ? copy.successToast : copy.errorToast,
                    success ? "success" : "error"
                );
            } else if (!data.result) {
                hideOperation();
            } else if (notifyResult !== true) {
                hideOperation();
            }

            activeOperation = null;
            setXrayControlsLocked(false);
            await loadStatus(false);
            if (notifyResult === true && data.result) await checkUpdate();
        } catch (error) {
            showOperation();
            byId("operation-message").textContent = errorMessage(error);
            activeOperation = null;
            setXrayControlsLocked(false);
        }
    }

    function bindEvents() {
        byId("xray-version-select").addEventListener("change", function () {
            selectedTag = this.value;
            renderSelectedRelease();
        });
        byId("xray-show-prereleases").addEventListener("change", renderReleaseChoices);
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
        byId("xray-update-install").addEventListener("click", function (event) {
            installRelease(selectedRelease(), event.currentTarget);
        });

        byId("xray-reinstall").addEventListener("click", function (event) {
            installRelease(releaseCatalog.find(function (row) { return row.installed; }), event.currentTarget);
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
