(function () {
    "use strict";

    var REFRESH_INTERVAL_MS = 60000;
    var timer = null;
    var inFlight = null;
    var controller = null;

    function byId(id) { return document.getElementById(id); }

    function unwrap(payload) {
        var error;
        if (payload && payload.success === false) {
            error = new Error(payload.error && payload.error.message ? payload.error.message : "Не удалось получить обзор маршрутов.");
            error.status = payload.status || null;
            throw error;
        }
        return payload && Object.prototype.hasOwnProperty.call(payload, "data") ? payload.data : payload;
    }

    function request(url, options) {
        return window.BROrayUI.apiRequest(url, options || {}).then(unwrap);
    }

    function setText(id, value) {
        var node = byId(id);
        if (node) node.textContent = value === null || value === undefined || value === "" ? "—" : String(value);
    }

    function operationText(operation) {
        if (!operation || !operation.active) return "Нет";
        if (operation.resumable) return "Приостановлена";
        if (operation.running) return "Выполняется";
        return "Подготовка";
    }

    function render(data) {
        var custom = data.custom || {};
        var catalog = data.catalog || {};
        var operation = data.operation || {};
        var message = byId("routes-overview-message");

        setText("routes-overview-interface", data.managedInterfaceDisplay || "BROray");
        setText("routes-overview-managed", Number(data.totalManagedRoutes || 0));
        setText("routes-overview-shared", Number(data.sharedRoutes || 0));
        setText("routes-overview-operation", operationText(operation));
        setText("routes-overview-custom-total", Number(custom.total || 0));
        setText("routes-overview-custom-installed", Number(custom.installed || 0));
        setText("routes-overview-custom-attention", Number(custom.attention || 0));
        setText("routes-overview-catalog-total", Number(catalog.total || 10));
        setText("routes-overview-catalog-installed", Number(catalog.installed || 0));
        setText("routes-overview-catalog-attention", Number(catalog.attention || 0));

        if (message) {
            if (operation.active) {
                message.textContent = (operation.bundleName || "Набор маршрутов") + ": " + operationText(operation).toLowerCase() + ". Прогресс показан в общем окне операции.";
            } else if (Number(custom.attention || 0) + Number(catalog.attention || 0) > 0) {
                message.textContent = "Некоторые наборы требуют внимания. Откройте соответствующий раздел для подробностей.";
            } else {
                message.textContent = "Сводка получена. Пользовательские и готовые маршруты используют единый реестр владельцев.";
            }
        }
        if (window.BROrayIcons) window.BROrayIcons.scan(document);
    }

    function stopSchedule() {
        if (timer) window.clearTimeout(timer);
        timer = null;
    }

    function schedule() {
        stopSchedule();
        if (!document.hidden) timer = window.setTimeout(load, REFRESH_INTERVAL_MS);
    }

    function load(showToast) {
        var button = byId("routes-overview-refresh");
        if (inFlight) return inFlight;
        if (controller) controller.abort();
        controller = window.AbortController ? new AbortController() : null;
        if (button) button.disabled = true;
        inFlight = request("/api/routes/overview.cgi", {
            method: "GET",
            credentials: "same-origin",
            signal: controller ? controller.signal : undefined
        }).then(function (data) {
            render(data || {});
            if (showToast) window.BROrayUI.toast("Состояние маршрутов обновлено.", "success");
            return data;
        }).catch(function (error) {
            if (error && error.name === "AbortError") return null;
            if (error && error.status === 401) {
                window.BROrayUI.redirectToLogin();
                return null;
            }
            window.BROrayUI.toast(error && error.message ? error.message : "Не удалось получить обзор маршрутов.", "error");
            return null;
        }).then(function (value) {
            inFlight = null;
            controller = null;
            if (button) button.disabled = false;
            schedule();
            return value;
        });
        return inFlight;
    }

    function reveal(session) {
        var app = byId("app");
        var loader = byId("page-loader");
        var user = byId("current-user");
        if (user) user.textContent = session && session.user ? session.user : "admin";
        if (loader) loader.hidden = true;
        if (app) app.hidden = false;
    }

    function initialize() {
        var refresh = byId("routes-overview-refresh");
        if (refresh) refresh.addEventListener("click", function () { load(true); });
        document.addEventListener("broray:routes-operation-finished", function () { load(false); });
        document.addEventListener("visibilitychange", function () {
            if (document.hidden) {
                stopSchedule();
                if (controller) controller.abort();
            } else {
                load(false);
            }
        });
        window.addEventListener("pagehide", function () {
            stopSchedule();
            if (controller) controller.abort();
        });
        request("/api/session.cgi", {method:"GET", credentials:"same-origin"}).then(function (session) {
            reveal(session);
            return load(false);
        }).catch(function (error) {
            if (error && error.status === 401) return window.BROrayUI.redirectToLogin();
            reveal(null);
            window.BROrayUI.toast("Не удалось открыть обзор маршрутов.", "error");
        });
    }

    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", initialize, {once:true});
    else initialize();
})();
