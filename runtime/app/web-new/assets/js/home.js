(function () {
    "use strict";

    if (window.BROrayHomeInitialized) return;
    window.BROrayHomeInitialized = true;

    var app = document.getElementById("app");
    var loader = document.getElementById("page-loader");
    var REFRESH_INTERVAL_MS = 30000;
    // Ten concurrent pages can queue a read in the browser before it reaches CGI.
    var REQUEST_TIMEOUT_MS = 60000;
    var MAX_RETRY_MS = 120000;
    var refreshTimer = null;
    var activeRequest = null;
    var refreshPending = false;
    var authenticated = false;
    var stopped = false;
    var suspended = false;
    var retryDelay = REFRESH_INTERVAL_MS;
    var lastReceivedAt = null;
    var refreshButton = byId("refresh-status");
    var refreshFeedback = null;

    function byId(id) { return document.getElementById(id); }
    function setText(id, value) {
        var element = byId(id);
        if (element) element.textContent = value == null || value === "" ? "—" : String(value);
    }
    function setStatus(id, text, tone) {
        var element = byId(id);
        if (!element) return;
        element.textContent = text;
        element.className = "home-card-status status-" + (tone || "neutral");
    }
    function formatDate(value) {
        var date;
        if (!value) return "—";
        date = new Date(value);
        return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString("ru-RU");
    }
    function formatPing(quality) {
        if (!quality || quality.ping == null || Number.isNaN(Number(quality.ping))) return "Не проверено";
        return Math.round(Number(quality.ping)) + " мс";
    }
    function healthOf(data) {
        return data && data.health && typeof data.health === "object" ? data.health : null;
    }
    function severityOf(data) {
        var health = healthOf(data);
        return health && health.severity ? health.severity : "unknown";
    }
    function toneOf(severity) {
        return {
            ok: "success",
            warning: "warning",
            error: "error",
            busy: "loading",
            unknown: "neutral"
        }[severity] || "neutral";
    }
    function labelOf(severity) {
        return {
            ok: "Исправно",
            warning: "Требуется внимание",
            error: "Требуется исправление",
            busy: "Выполняется",
            unknown: "Не проверено"
        }[severity] || "Не проверено";
    }
    function firstReason(data, fallback) {
        var health = healthOf(data);
        var reasons = health && Array.isArray(health.reasons) ? health.reasons : [];
        return reasons.length && reasons[0].message ? reasons[0].message : fallback;
    }
    function translateUpdateStatus(value) {
        return {
            never: "ещё не выполнялось",
            success: "успешно",
            partial: "обновлено частично",
            error: "ошибка",
            running: "выполняется"
        }[value] || "состояние не определено";
    }
    function requestError(code, message) {
        var error = new Error(message);
        error.code = code;
        return error;
    }
    function ensureCurrent(task) {
        if (task !== activeRequest || task.cancelled || stopped || suspended || document.hidden) {
            throw requestError("INTERRUPTED", "Запрос отменён.");
        }
    }
    async function request(url, task) {
        // One deadline covers both headers and JSON, including initial authentication.
        var result = await Promise.race([(async function () {
            var response = await fetch(url, {
                credentials: "same-origin",
                cache: "no-store",
                headers: {"Accept": "application/json"},
                signal: task.controller.signal
            });
            // An expired session may have a non-JSON body. Handle the status first.
            if (response.status === 401) return {unauthorized: true};
            var payload;
            try { payload = await response.json(); }
            catch (error) { throw new Error("Сервер вернул некорректный ответ."); }
            if (!response.ok || !payload || typeof payload !== "object" || Array.isArray(payload) ||
                payload.success === false || payload.ok === false) {
                throw new Error(payload && payload.error && payload.error.message
                    ? payload.error.message : "Не удалось получить данные.");
            }
            return {data: payload.success === true ? payload.data : payload};
        })(), task.interrupted]);
        ensureCurrent(task);
        if (result.unauthorized) {
            authenticated = false;
            stopped = true;
            app.hidden = true;
            window.location.replace("/");
            throw requestError("SESSION_REQUIRED", "Сессия завершена.");
        }
        return result.data;
    }

    function renderUnavailable(prefix, message) {
        setStatus(prefix + "-status", "Недоступно", "error");
        setText(prefix + "-main", message);
    }

    function renderXray(data) {
        var severity;
        if (!data) {
            renderUnavailable("home-xray", "Модуль Xray не вернул состояние.");
            setText("home-xray-version", null);
            setText("home-xray-config", null);
            return;
        }
        severity = severityOf(data);
        setStatus("home-xray-status", severity === "ok" ? "Работает" : labelOf(severity), toneOf(severity));
        setText("home-xray-main", severity === "ok"
            ? "Процесс Xray запущен, конфигурация корректна, SOCKS принимает подключения."
            : firstReason(data, "Состояние Xray требует проверки."));
        setText("home-xray-version", data.version || "Не определено");
        setText("home-xray-config", data.configValid === true && data.socksActive === true ? "Конфигурация и SOCKS: OK" : "Требуется проверка");
    }

    function renderServers(data) {
        var active, severity, qualityText;
        if (!data) {
            renderUnavailable("home-servers", "Сводка серверов недоступна.");
            setText("home-server-name", "Сводка серверов недоступна.");
            setText("home-servers-total", null);
            setText("home-server-quality", null);
            return;
        }
        active = data.activeServer;
        severity = severityOf(data);
        setStatus("home-servers-status", data.connectionState === "connected" && severity === "ok" ? "Подключено" : labelOf(severity), toneOf(severity));
        setText("home-server-name", active ? active.name || active.id : firstReason(data, "Активный сервер не выбран."));
        setText("home-servers-total", data.total);
        qualityText = active ? formatPing(active.quality) : "—";
        if (active && active.quality && active.quality.freshness && active.quality.freshness !== "fresh") {
            qualityText += " · " + (active.quality.freshness === "expired" ? "устарело" : active.quality.freshness === "stale" ? "давно" : "не проверено");
        }
        setText("home-server-quality", qualityText);
    }

    function renderSubscriptions(data) {
        var severity, updateStatus, updateTime, warning;
        if (!data) {
            renderUnavailable("home-subscriptions", "Сводка подписок недоступна.");
            setText("home-subscriptions-enabled", null);
            setText("home-subscriptions-servers", null);
            return;
        }
        severity = severityOf(data);
        setStatus("home-subscriptions-status", data.lastUpdateStatus === "partial" ? "Обновлено частично" : labelOf(severity), toneOf(severity));
        updateStatus = translateUpdateStatus(data.lastUpdateStatus);
        updateTime = data.lastUpdatedAt ? formatDate(data.lastUpdatedAt) : "";
        warning = Array.isArray(data.lastWarnings) && data.lastWarnings.length ? data.lastWarnings[0] : null;
        setText("home-subscriptions-main", warning || ("Последнее обновление: " + (updateTime ? updateTime + " · " + updateStatus : updateStatus) + "."));
        setText("home-subscriptions-enabled", Number(data.enabled || 0) + " из " + Number(data.total || 0));
        setText("home-subscriptions-servers", Number(data.serversReceived || 0));
    }

    function renderDns(data) {
        var severity, requested, effective, present, max, observed;
        if (!data) {
            renderUnavailable("home-dns", "Сводка DNS-over-TLS недоступна.");
            setText("home-dns-selected", null);
            setText("home-dns-installed", null);
            return;
        }
        severity = severityOf(data);
        requested = Number(data.selectedCount != null ? data.selectedCount : (data.selectedIds || []).length);
        effective = data.selectedPresentCount != null ? requested :
            Number(data.effectiveCount != null ? data.effectiveCount : (data.managed || []).length);
        present = data.selectedPresentCount != null ? data.selectedPresentCount : data.managedPresentCount;
        max = Number(data.maxServers || 8);
        observed = data.runningConfigAvailable !== false && data.observationState !== "unknown" &&
            present != null;
        setStatus("home-dns-status", labelOf(severity), toneOf(severity));
        setText("home-dns-main", firstReason(data, severity === "ok"
            ? "DNS-over-TLS настроен в Keenetic."
            : "Проверьте состояние DNS-over-TLS."));
        setText("home-dns-selected", requested + " из " + max);
        setText("home-dns-installed", observed ? Number(present) + " из " + effective : null);
    }

    function renderRoutes(data) {
        var severity;
        var verified;
        var recorded;
        var available;
        var updates;
        var countsKnown;
        var countsInconsistent;
        var statusText;
        var statusTone;
        var mainText;

        if (!data) {
            setStatus("home-routes-status", "Недоступно", "error");
            setText("home-routes-main", "Сводка маршрутов недоступна.");
            setText("home-routes-count", "—");
            setText("home-routes-update", "—");
            return;
        }

        severity = severityOf(data);
        verified = Number(
            data.verifiedInstalledBundles != null
                ? data.verifiedInstalledBundles
                : data.installedBundles
        );
        recorded = Number(
            data.recordedInstalledBundles != null
                ? data.recordedInstalledBundles
                : verified
        );
        available = Number(data.availableBundles);
        updates = Number(data.updatesAvailableCount);

        countsKnown =
            Number.isFinite(verified) &&
            Number.isFinite(recorded) &&
            Number.isFinite(available) &&
            verified >= 0 &&
            recorded >= 0 &&
            available > 0;

        if (!Number.isFinite(updates) || updates < 0) {
            updates = 0;
        }

        countsInconsistent =
            countsKnown &&
            (verified > available || recorded > available);

        if (data.operationRunning === true || severity === "busy") {
            statusText = "Выполняется";
            statusTone = "loading";
        } else if (severity === "error" || data.error) {
            statusText = "Требуется исправление";
            statusTone = "error";
        } else if (!countsKnown) {
            statusText = "Не проверено";
            statusTone = "neutral";
        } else if (countsInconsistent) {
            statusText = "Требуется исправление";
            statusTone = "error";
        } else if (verified === 0) {
            statusText = "Не установлено";
            statusTone = "neutral";
        } else if (verified < available) {
            statusText = "Установлено частично";
            statusTone = "warning";
        } else if (updates > 0) {
            statusText = "Доступно обновление";
            statusTone = "warning";
        } else if (severity === "warning" && data.actionRequired === true) {
            statusText = "Требуется внимание";
            statusTone = "warning";
        } else {
            statusText = "Установлено";
            statusTone = "success";
        }

        if (severity === "error" || data.error) {
            mainText = firstReason(data, "Маршруты требуют исправления.");
        } else if (countsKnown) {
            mainText =
                "Установлено наборов: " +
                verified +
                " из " +
                available +
                ".";
        } else {
            mainText = "Количество установленных наборов не подтверждено.";
        }

        setStatus("home-routes-status", statusText, statusTone);
        setText("home-routes-main", mainText);
        setText(
            "home-routes-count",
            countsKnown
                ? verified +
                    (recorded !== verified
                        ? " · записано " + recorded
                        : "")
                : "—"
        );
        setText("home-routes-update", countsKnown ? updates : "—");
    }

    function renderKeenetic(data) {
        var severity, name;
        if (!data) {
            renderUnavailable("home-keenetic", "Состояние управляемого прокси-интерфейса недоступно.");
            setText("home-keenetic-link", null);
            setText("home-keenetic-state", null);
            return;
        }
        severity = severityOf(data);
        name = data.interfaceDisplayName ||
            (data.expected && data.expected.description) ||
            data.description ||
            "BROray";
        setStatus("home-keenetic-status", severity === "ok" ? "OK" : labelOf(severity), toneOf(severity));
        setText("home-keenetic-main", severity === "ok"
            ? name + " работает."
            : firstReason(data, "Состояние " + name + " требует проверки."));
        setText("home-keenetic-link", data.link === true ? "Есть" : "Нет");
        setText("home-keenetic-state", data.state === "up" && data.connected === true ? "Подключено" : "Нет подключения");
    }

    function renderBroray(data, installedRelease) {
        var packageChanged;
        var updateAvailable;
        var updateLabel;

        setText("home-broray-version", installedRelease &&
            typeof installedRelease.version === "string" ? installedRelease.version : null);
        if (!data) {
            setStatus("home-broray-status", "Недоступно", "error");
            setText("home-broray-main", "Сведения BROray недоступны.");
            setText("home-broray-update", null);
            return;
        }

        if (data._snapshot && data._snapshot.freshness !== "fresh") {
            setStatus("home-broray-status", "Данные устарели", "warning");
            setText("home-broray-main", "Состояние компонентов требует повторной проверки.");
            setText("home-broray-update", null);
            return;
        }

        packageChanged =
            typeof data.installedPackageVersion === "string" &&
            data.installedPackageVersion !== "" &&
            typeof data.availablePackageVersion === "string" &&
            data.availablePackageVersion !== "" &&
            data.installedPackageVersion !== data.availablePackageVersion;
        updateAvailable = data.updateAvailable === true && packageChanged;
        updateLabel = data.availableVersion;
        if (
            updateAvailable &&
            (!updateLabel || updateLabel === data.version)
        ) {
            updateLabel = data.availablePackageVersion;
        }

        setStatus(
            "home-broray-status",
            data.installationHealthy
                ? updateAvailable
                    ? "Доступно обновление"
                    : "Установлено"
                : "Установка повреждена",
            data.installationHealthy
                ? updateAvailable
                    ? "warning"
                    : "success"
                : "error"
        );
        setText(
            "home-broray-main",
            data.installationHealthy
                ? "Все обязательные компоненты установлены."
                : "Часть компонентов отсутствует или повреждена."
        );
        setText(
            "home-broray-update",
            updateAvailable
                ? updateLabel || "Доступно"
                : "Нет"
        );
    }

    function renderOverall(data) {
        var health = data && data.health ? data.health : null;
        var severity = health && health.severity ? health.severity : "unknown";
        var badge = byId("home-health");
        var warning = byId("home-warning");
        var reasons = health && Array.isArray(health.reasons) ? health.reasons : [];
        var errors = Array.isArray(data.errors) ? data.errors : [];
        var message;

        badge.textContent = {
            ok: "Система работает",
            warning: "Требуется внимание",
            error: "Требуется исправление",
            busy: "Выполняется операция",
            unknown: "Состояние не проверено"
        }[severity] || "Состояние не проверено";
        badge.className = "status-badge " + ({
            ok: "status-success",
            warning: "status-warning",
            error: "status-error",
            busy: "status-loading",
            unknown: "status-neutral"
        }[severity] || "status-neutral");

        if (severity !== "ok") {
            message = reasons.slice(0, 3).map(function (reason) { return reason.message; }).filter(Boolean).join(" ");
            if (!message && errors.length) message = "Не удалось получить сводку модулей: " + errors.join(", ") + ".";
            warning.textContent = message || "Один или несколько модулей требуют проверки.";
            warning.hidden = false;
        } else {
            warning.hidden = true;
        }
    }

    function render(data) {
        renderOverall(data);
        renderXray(data.xray);
        renderServers(data.servers);
        renderSubscriptions(data.subscriptions);
        renderDns(data.dns);
        renderRoutes(data.routes);
        renderKeenetic(data.keenetic);
        renderBroray(data.broray, data.installedRelease);
        setText("home-updated-at", formatDate(data.updatedAt));
    }

    function clearRefreshTimer() {
        if (refreshTimer !== null) window.clearTimeout(refreshTimer);
        refreshTimer = null;
    }
    function setRefreshBusy(busy) {
        if (!refreshButton) return;
        refreshButton.disabled = busy || stopped;
        refreshButton.setAttribute("aria-busy", busy ? "true" : "false");
        refreshButton.setAttribute("aria-label", busy ? "Обновление сводки" : "Обновить сводку");
        refreshButton.classList.toggle("is-loading", busy);
    }
    function interruptRequest(code) {
        var task = activeRequest;
        if (!task || task.cancelled) return;
        task.cancelled = true;
        task.reject(requestError(code, code === "REQUEST_TIMEOUT"
            ? "Роутер не ответил вовремя." : "Запрос отменён."));
        task.controller.abort();
    }
    function scheduleRefresh(delay) {
        clearRefreshTimer();
        if (stopped || suspended || document.hidden) return;
        refreshTimer = window.setTimeout(function () {
            refreshTimer = null;
            refresh(false);
        }, delay);
    }
    function validateSummary(data) {
        var modules = ["xray", "servers", "subscriptions", "dns", "routes", "keenetic", "broray"];
        if (!data || typeof data !== "object" || Array.isArray(data) ||
            !data.health || typeof data.health !== "object" || Array.isArray(data.health) ||
            !modules.every(function (name) {
                return Object.prototype.hasOwnProperty.call(data, name) &&
                    (data[name] === null || (typeof data[name] === "object" && !Array.isArray(data[name])));
            })) throw new Error("Сервер вернул неполную сводку.");
    }
    function showRefreshFailure(error) {
        if (lastReceivedAt === null) {
            render({health: {severity: "unknown"}, errors: [], updatedAt: null});
        }
        var badge = byId("home-health");
        badge.textContent = lastReceivedAt === null ? "Сводка недоступна" : "Данные не обновлены";
        badge.className = "status-badge status-warning";
        if (refreshFeedback) {
            refreshFeedback.textContent = "Не удалось обновить сводку. " + error.message +
                (lastReceivedAt === null ? " " : " Показаны последние полученные данные от " + formatDate(lastReceivedAt) + ". ") +
                "Нажмите «Обновить» или дождитесь повторной попытки.";
            refreshFeedback.hidden = false;
        }
    }
    async function refresh(showToast) {
        if (stopped || suspended || document.hidden) return;
        if (activeRequest) {
            // A return to the page must follow an aborted request, never run in parallel.
            if (activeRequest.cancelled) refreshPending = true;
            return;
        }
        clearRefreshTimer();
        if (typeof window.AbortController !== "function") {
            loader.hidden = true;
            app.hidden = false;
            showRefreshFailure(new Error("Для обновления сводки требуется современный браузер."));
            return;
        }
        var task = {controller: new window.AbortController(), cancelled: false};
        task.interrupted = new Promise(function (resolve, reject) { task.reject = reject; });
        activeRequest = task;
        refreshPending = false;
        setRefreshBusy(true);
        var deadline = window.setTimeout(function () {
            if (activeRequest === task) interruptRequest("REQUEST_TIMEOUT");
        }, REQUEST_TIMEOUT_MS);
        var failed = false;
        try {
            if (!authenticated) {
                var session = await request("/api/session.cgi", task);
                if (!session || session.authenticated !== true || typeof session.user !== "string" || !session.user) {
                    throw new Error("Не удалось подтвердить пользователя.");
                }
                setText("current-user", session.user);
                authenticated = true;
                loader.hidden = true;
                app.hidden = false;
            }
            var data = await request("/api/home/summary.cgi", task);
            validateSummary(data);
            render(data);
            lastReceivedAt = new Date().toISOString();
            if (refreshFeedback) refreshFeedback.hidden = true;
            // updatedAt is the assembled summary time, not proof that every module is fresh.
            if (byId("home-updated-at")) byId("home-updated-at").title =
                "Время формирования сводки. Актуальность каждого модуля указана в его состоянии.";
            retryDelay = REFRESH_INTERVAL_MS;
            if (showToast && window.BROrayUI) window.BROrayUI.toast("Сводка обновлена.", "success");
        } catch (error) {
            if (error.code !== "INTERRUPTED" && error.code !== "SESSION_REQUIRED" && !stopped) {
                failed = true;
                loader.hidden = true;
                app.hidden = false;
                showRefreshFailure(error);
            }
        } finally {
            window.clearTimeout(deadline);
            if (activeRequest === task) activeRequest = null;
            setRefreshBusy(false);
            var delay = refreshPending ? 0 : retryDelay;
            refreshPending = false;
            if (failed) retryDelay = Math.min(retryDelay * 2, MAX_RETRY_MS);
            scheduleRefresh(delay);
        }
    }
    function resumeRefresh() {
        if (!stopped && !suspended && !document.hidden) refresh(false);
    }
    function initialize() {
        var warning = byId("home-warning");
        refreshFeedback = document.createElement("div");
        refreshFeedback.id = "home-refresh-feedback";
        refreshFeedback.className = "home-warning";
        refreshFeedback.setAttribute("role", "status");
        refreshFeedback.hidden = true;
        if (warning && warning.parentNode) warning.parentNode.insertBefore(refreshFeedback, warning);
        if (refreshButton) {
            refreshButton.hidden = false;
            refreshButton.removeAttribute("aria-hidden");
            refreshButton.removeAttribute("tabindex");
            refreshButton.addEventListener("click", function () { refresh(true); });
        }
        document.addEventListener("visibilitychange", function () {
            if (document.hidden) {
                clearRefreshTimer();
                interruptRequest("INTERRUPTED");
            } else resumeRefresh();
        });
        window.addEventListener("pagehide", function () {
            suspended = true;
            clearRefreshTimer();
            interruptRequest("INTERRUPTED");
        });
        window.addEventListener("pageshow", function (event) {
            if (!event.persisted) return;
            suspended = false;
            authenticated = false;
            resumeRefresh();
        });
        window.addEventListener("online", resumeRefresh);
        var logout = byId("logout-button");
        if (logout) logout.addEventListener("click", function () {
            stopped = true;
            clearRefreshTimer();
            interruptRequest("INTERRUPTED");
        }, true);
        refresh(false);
    }

    initialize();
})();
