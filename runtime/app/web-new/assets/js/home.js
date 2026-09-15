(function () {
    "use strict";

    if (window.BROrayHomeInitialized) return;
    window.BROrayHomeInitialized = true;

    var app = document.getElementById("app");
    var loader = document.getElementById("page-loader");

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
    async function request(url) {
        var response = await fetch(url, {
            credentials: "same-origin",
            cache: "no-store",
            headers: {"Accept": "application/json"}
        });
        var payload;
        try {
            payload = await response.json();
        } catch (error) {
            throw new Error("Backend вернул некорректный JSON.");
        }
        if (response.status === 401) {
            window.location.replace("/");
            throw new Error("Сессия завершена.");
        }
        if (!response.ok || payload.success === false) {
            throw new Error(payload && payload.error && payload.error.message ? payload.error.message : "Не удалось получить данные.");
        }
        return payload.success === true ? payload.data : payload;
    }

    function renderUnavailable(prefix, message) {
        setStatus(prefix + "-status", "Недоступно", "error");
        setText(prefix + "-main", message);
    }

    function renderXray(data) {
        var severity;
        if (!data) {
            renderUnavailable("home-xray", "Модуль Xray не вернул состояние.");
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
        var severity, requested, effective, max;
        if (!data) {
            setText("home-routes-dns", "Сводка DNS-over-TLS недоступна.");
            return;
        }
        severity = severityOf(data);
        requested = Number(data.selectedCount != null ? data.selectedCount : (data.selectedIds || []).length);
        effective = Number(data.effectiveCount != null ? data.effectiveCount : (data.managed || []).length);
        max = Number(data.maxServers || 8);
        setText("home-routes-dns", severity === "ok"
            ? "DNS-over-TLS: " + Number(data.managedPresentCount || 0) + " из " + effective
            : "DNS-over-TLS: " + requested + " из " + max + " · требуется проверка");
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

    function renderBroray(data) {
        var packageChanged;
        var updateAvailable;
        var updateLabel;

        if (!data) {
            setStatus("home-broray-status", "Недоступно", "error");
            setText("home-broray-main", "Сведения BROray недоступны.");
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
        setText("home-broray-version", data.version);
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
        renderBroray(data.broray);
        setText("home-updated-at", formatDate(data.updatedAt));
    }

    async function loadSummary(showToast) {
        var data = await request("/api/home/summary.cgi");
        render(data);
        if (showToast && window.BROrayUI) window.BROrayUI.toast("Состояние обновлено.", "success");
    }

    async function initialize() {
        try {
            var session = await request("/api/session.cgi");
            setText("current-user", session.user || "admin");
            loader.hidden = true;
            app.hidden = false;
            await loadSummary(false);
        } catch (error) {
            if (error.message !== "Сессия завершена.") {
                loader.hidden = true;
                app.hidden = false;
                byId("home-warning").textContent = error.message;
                byId("home-warning").hidden = false;
            }
        }
    }

    initialize();
})();
