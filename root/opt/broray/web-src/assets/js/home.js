(function () {
    "use strict";

    if (window.BROrayHomeInitialized) {
        return;
    }
    window.BROrayHomeInitialized = true;

    var app = document.getElementById("app");
    var loader = document.getElementById("page-loader");

    function byId(id) {
        return document.getElementById(id);
    }

    function setText(id, value) {
        var element = byId(id);
        if (element) {
            element.textContent =
                value == null || value === ""
                    ? "—"
                    : String(value);
        }
    }

    function setStatus(id, text, tone) {
        var element = byId(id);
        if (!element) {
            return;
        }
        element.textContent = text;
        element.className =
            "home-card-status status-" + (tone || "neutral");
    }

    function formatDate(value) {
        if (!value) {
            return "—";
        }
        var date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return value;
        }
        return date.toLocaleString("ru-RU");
    }

    function formatPing(quality) {
        if (
            !quality ||
            quality.ping == null ||
            Number.isNaN(Number(quality.ping))
        ) {
            return "Не проверено";
        }
        return Math.round(Number(quality.ping)) + " мс";
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
            headers: {
                "Accept": "application/json"
            }
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
            throw new Error(
                payload &&
                payload.error &&
                payload.error.message
                    ? payload.error.message
                    : "Не удалось получить данные."
            );
        }

        return payload.success === true
            ? payload.data
            : payload;
    }

    function renderXray(data) {
        if (!data) {
            setStatus("home-xray-status", "Недоступно", "error");
            setText("home-xray-main", "Модуль Xray не вернул состояние.");
            return;
        }

        setStatus(
            "home-xray-status",
            data.running ? "Работает" : "Остановлен",
            data.running ? "success" : "warning"
        );
        setText(
            "home-xray-main",
            data.running
                ? "Процесс Xray запущен."
                : "Процесс Xray сейчас не работает."
        );
        setText("home-xray-version", data.version || "Не определено");
        setText(
            "home-xray-config",
            data.configValid ? "Настроено" : "Требуется исправление"
        );
    }

    function renderServers(data) {
        if (!data) {
            setStatus("home-servers-status", "Недоступно", "error");
            setText("home-server-name", "Сводка серверов недоступна.");
            return;
        }

        var active = data.activeServer;
        var connected = data.connectionState === "connected";

        setStatus(
            "home-servers-status",
            connected
                ? "Подключено"
                : active
                    ? "Ошибка"
                    : "Не выбрано",
            connected ? "success" : active ? "error" : "warning"
        );
        setText(
            "home-server-name",
            active
                ? active.name || active.id
                : "Активный сервер не выбран."
        );
        setText("home-servers-total", data.total);
        setText(
            "home-server-quality",
            active ? formatPing(active.quality) : "—"
        );
    }

    function renderSubscriptions(data) {
        if (!data) {
            setStatus(
                "home-subscriptions-status",
                "Недоступно",
                "error"
            );
            setText(
                "home-subscriptions-main",
                "Сводка подписок недоступна."
            );
            return;
        }

        var hasSubscriptions = Number(data.total) > 0;
        var updateError = data.lastUpdateStatus === "error";

        setStatus(
            "home-subscriptions-status",
            updateError
                ? "Ошибка"
                : hasSubscriptions
                    ? "Активны"
                    : "Не настроены",
            updateError ? "error" : hasSubscriptions ? "success" : "neutral"
        );
        var updateStatus = translateUpdateStatus(data.lastUpdateStatus);
        var updateTime = data.lastUpdatedAt
            ? formatDate(data.lastUpdatedAt)
            : "";

        setText(
            "home-subscriptions-main",
            "Последнее обновление: " +
                (updateTime
                    ? updateTime + " · " + updateStatus
                    : updateStatus) +
                "."
        );
        setText(
            "home-subscriptions-enabled",
            data.enabled + " из " + data.total
        );
        setText(
            "home-subscriptions-servers",
            data.serversReceived
        );
    }

    function renderKeenetic(data) {
        if (!data) {
            setStatus("home-keenetic-status", "Недоступно", "error");
            setText("home-keenetic-main", "Состояние управляемого ProxyN недоступно.");
            return;
        }

        setStatus(
            "home-keenetic-status",
            data.healthy
                ? "OK"
                : data.exists
                    ? "Требуется исправление"
                    : "Не настроено",
            data.healthy ? "success" : data.exists ? "warning" : "error"
        );
        setText(
            "home-keenetic-main",
            data.exists
                ? data.description || "Интерфейс " + (data.interfaceName || "ProxyN") + " создан."
                : "Интерфейс " + (data.interfaceName || "ProxyN") + " не создан."
        );
        setText("home-keenetic-link", data.link ? "Есть" : "Нет");
        setText(
            "home-keenetic-state",
            data.state === "up" ? "Подключено" : "Нет подключения"
        );
    }

    function renderRoutes(data) {
        if (!data) {
            setStatus("home-routes-status", "Недоступно", "error");
            setText("home-routes-main", "Сводка маршрутов недоступна.");
            return;
        }

        setStatus(
            "home-routes-status",
            data.installed
                ? data.updateAvailable
                    ? "Доступно обновление"
                    : "Установлено"
                : "Не установлено",
            data.installed
                ? data.updateAvailable
                    ? "warning"
                    : "success"
                : "neutral"
        );
        setText(
            "home-routes-main",
            "Установлено наборов: " +
                (data.installedBundles || 0) +
                " из " +
                (data.availableBundles || 9) +
                "."
        );
        setText("home-routes-count", data.installedBundles || 0);
        setText(
            "home-routes-update",
            data.updateAvailable ? "Доступно" : "Нет"
        );
    }

    function renderBroray(data) {
        if (!data) {
            setStatus("home-broray-status", "Недоступно", "error");
            setText("home-broray-main", "Сведения BROray недоступны.");
            return;
        }

        setStatus(
            "home-broray-status",
            data.installationHealthy
                ? data.updateAvailable
                    ? "Доступно обновление"
                    : "Установлено"
                : "Установка повреждена",
            data.installationHealthy
                ? data.updateAvailable
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
            data.updateAvailable
                ? data.availableVersion || "Доступно"
                : "Нет"
        );
    }

    function render(data) {
        var errors = Array.isArray(data.errors)
            ? data.errors
            : [];
        var health = byId("home-health");
        var warning = byId("home-warning");

        health.textContent = data.healthy
            ? "Система работает"
            : "Данные получены частично";
        health.className =
            "status-badge " +
            (data.healthy ? "status-success" : "status-warning");

        if (errors.length) {
            warning.textContent =
                "Не удалось получить сводку модулей: " +
                errors.join(", ") +
                ". Остальные данные показаны ниже.";
            warning.hidden = false;
        } else {
            warning.hidden = true;
        }

        renderXray(data.xray);
        renderServers(data.servers);
        renderSubscriptions(data.subscriptions);
        renderKeenetic(data.keenetic);
        renderRoutes(data.routes);
        renderBroray(data.broray);
        setText("home-updated-at", formatDate(data.updatedAt));
    }

    async function loadSummary(showToast) {
        try {
            var data = await request("/api/home/summary.cgi");
            render(data);
            if (showToast && window.BROrayUI) {
                window.BROrayUI.toast(
                    "Состояние обновлено.",
                    "success"
                );
            }
        } finally {
            // Главная обновляется при открытии; отдельная кнопка не требуется.
        }
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
