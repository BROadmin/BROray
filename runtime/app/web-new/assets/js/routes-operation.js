/* BROray routes immediate operation window r1 */
(function () {
    "use strict";

    if (window.BROrayRoutesOperationUI) return;

    var POLL_INTERVAL_MS = 1000;
    var requestInFlight = null;
    var timer = null;
    var controller = null;
    var current = null;
    var optimistic = null;
    var previousActive = false;
    var previousBundleId = null;
    var minimized = false;

    function byId(id) { return document.getElementById(id); }

    function unwrap(payload) {
        var error;
        if (payload && payload.success === false) {
            error = new Error(payload.error && payload.error.message ? payload.error.message : "Не удалось получить состояние операции.");
            error.status = payload.status || null;
            error.code = payload.error ? payload.error.code : null;
            error.details = payload.error ? payload.error.details : null;
            throw error;
        }
        return payload && Object.prototype.hasOwnProperty.call(payload, "data") ? payload.data : payload;
    }

    function request(url, options) {
        return window.BROrayUI.apiRequest(url, options || {}).then(unwrap);
    }

    function operationTitle(data) {
        var action = data && data.progress ? data.progress.operation : (data ? data.action : null);
        if (action === "delete") return "Удаление маршрутов";
        if (action === "update") return "Обновление маршрутов";
        if (action === "restore") return "Восстановление маршрутов";
        if (action === "resume") return "Продолжение операции";
        if (action === "export" || action === "sync" || action === "install") return "Установка маршрутов";
        if (action === "preflight") return "Предварительная проверка";
        return "Операция с маршрутами";
    }

    function render(data) {
        var panel = byId("routes-operation-float");
        var compact = byId("routes-operation-compact");
        var progress = data && data.progress ? data.progress : {};
        var active = Boolean(data && data.active);
        var currentValue = Math.max(0, Number(progress.current || 0));
        var total = Math.max(0, Number(progress.total || 0));
        var counterText = total > 0 ? currentValue + " из " + total : (data && data.pending ? "Подготовка" : currentValue + " из " + total);
        var bar;
        var state;
        var stop;
        var resume;

        current = data || null;
        if (!panel || !compact) return;

        if (!active) {
            panel.hidden = true;
            compact.hidden = true;
            document.body.classList.remove("routes-operation-visible");
            return;
        }

        document.body.classList.add("routes-operation-visible");
        panel.hidden = minimized;
        compact.hidden = !minimized;

        byId("routes-operation-float-title").textContent = operationTitle(data) + (data.bundleName ? " · " + data.bundleName : "");
        byId("routes-operation-float-counter").textContent = counterText;
        byId("routes-operation-float-message").textContent = (progress.message || "Операция выполняется.") + (data.running ? " Остановка операций с маршрутами недоступна." : "");
        byId("routes-operation-float-current").textContent = progress.currentRoute ? "Текущий маршрут: " + progress.currentRoute : "";
        byId("routes-operation-float-current").hidden = !progress.currentRoute;

        bar = byId("routes-operation-float-bar");
        if (total > 0) {
            bar.max = total;
            bar.value = Math.min(currentValue, total);
        } else {
            bar.max = 1;
            bar.removeAttribute("value");
        }

        state = byId("routes-operation-float-state");
        state.className = "status-badge " + (data.running ? "status-loading" : "status-badge-warning");
        if (data.localPhase === "preflight") state.textContent = "Проверяется";
        else if (data.localPhase === "confirmation") state.textContent = "Ожидает подтверждения";
        else if (data.localPhase === "starting") state.textContent = "Запускается";
        else state.textContent = data.running ? (progress.stopRequested ? "Останавливается" : "Выполняется") : (data.resumable ? "Можно продолжить" : "Ожидание");
        state.setAttribute("data-icon", data.running ? "update" : "restore");

        stop = byId("routes-operation-float-stop");
        resume = byId("routes-operation-float-resume");
        stop.hidden = true;
        stop.disabled = true;
        resume.hidden = !data.resumable || data.running;
        resume.disabled = data.running;

        byId("routes-operation-compact-title").textContent = data.bundleName || "Маршруты";
        byId("routes-operation-compact-counter").textContent = counterText;
        if (window.BROrayIcons) {
            window.BROrayIcons.scan(panel);
            window.BROrayIcons.scan(compact);
        }
    }

    function stopTimer() {
        if (timer) window.clearTimeout(timer);
        timer = null;
    }

    function schedule() {
        stopTimer();
        if (!current || !current.active || document.hidden || current.localOnly === true) return;
        if (current.running || (current.globalOperation && current.globalOperation.active && !current.resumable)) {
            timer = window.setTimeout(refresh, POLL_INTERVAL_MS);
        }
    }

    function refresh() {
        if (requestInFlight) return requestInFlight;
        if (controller) controller.abort();
        controller = window.AbortController ? new AbortController() : null;
        requestInFlight = request("/api/routes/operation-status.cgi", {
            method: "GET",
            credentials: "same-origin",
            signal: controller ? controller.signal : undefined
        }).then(function (data) {
            var renderedData = data;
            var isActive;
            var completedBundle = previousBundleId;

            document.dispatchEvent(new CustomEvent("broray:routes-operation-authoritative", {
                detail: data && typeof data === "object" ? data : {active:false}
            }));

            if (optimistic) {
                if (data && data.active) optimistic = null;
                else renderedData = optimistic;
            }

            isActive = Boolean(renderedData && renderedData.active);
            render(renderedData);
            if (previousActive && !isActive) {
                document.dispatchEvent(new CustomEvent("broray:routes-operation-finished", {
                    detail: {bundleId: completedBundle}
                }));
            }
            previousActive = isActive;
            previousBundleId = renderedData && renderedData.bundleId ? renderedData.bundleId : previousBundleId;
            return renderedData;
        }).catch(function (error) {
            if (error && error.name === "AbortError") return null;
            if (error && error.status === 401) {
                window.BROrayUI.redirectToLogin();
                return null;
            }
            return null;
        }).then(function (value) {
            requestInFlight = null;
            controller = null;
            schedule();
            return value;
        });
        return requestInFlight;
    }

    function requestStop() {
        window.BROrayUI.toast("Остановка операций с маршрутами недоступна. Дождитесь завершения операции.", "warning");
    }

    function requestResume() {
        if (!current || !current.bundleId || !current.resumable) return;
        var event = new CustomEvent("broray:routes-operation-resume", {
            cancelable: true,
            detail: {
                bundleId: current.bundleId,
                bundleType: current.bundleType,
                bundleName: current.bundleName
            }
        });
        document.dispatchEvent(event);
        if (!event.defaultPrevented) {
            window.location.href = current.bundleType === "custom"
                ? "/routes-custom.html?v=WebUI-3.1.0-r09c02&resume=" + encodeURIComponent(current.bundleId)
                : "/routes-import.html?v=WebUI-3.1.0-r09c02&resume=" + encodeURIComponent(current.bundleId);
        }
    }

    function bind() {
        var panel = byId("routes-operation-float");
        var compact = byId("routes-operation-compact");
        if (!panel || !compact || panel.dataset.bound === "true") return;
        panel.dataset.bound = "true";
        byId("routes-operation-float-stop").addEventListener("click", requestStop);
        byId("routes-operation-float-resume").addEventListener("click", requestResume);
        byId("routes-operation-float-minimize").addEventListener("click", function () {
            minimized = true;
            render(current);
        });
        byId("routes-operation-compact-open").addEventListener("click", function () {
            minimized = false;
            render(current);
        });
        document.addEventListener("visibilitychange", function () {
            if (document.hidden) {
                stopTimer();
                if (controller) controller.abort();
            } else {
                refresh();
            }
        });
        window.addEventListener("pagehide", function () {
            stopTimer();
            if (controller) controller.abort();
        });
    }

    function pendingState(bundleId, bundleName, bundleType, action, message, localPhase, localOnly) {
        return {
            schemaVersion: 1,
            active: true,
            pending: true,
            resumable: false,
            running: true,
            bundleId: bundleId || null,
            bundleType: bundleType || null,
            bundleName: bundleName || bundleId || "Маршруты",
            action: action || "preflight",
            canStop: false,
            localPhase: localPhase || "starting",
            localOnly: localOnly === true,
            globalOperation: {
                active: true,
                pending: true,
                resumable: false,
                scope: "routes",
                action: action || "preflight",
                bundleId: bundleId || null
            },
            progress: {
                schemaVersion: 2,
                kind: "routes",
                bundleId: bundleId || "",
                operation: action || "preflight",
                phase: localPhase || "starting",
                current: 0,
                total: 0,
                percent: 0,
                currentRoute: null,
                message: message || "Подготовка операции с маршрутами.",
                running: true,
                success: null,
                rolledBack: false,
                resumable: false,
                stopRequested: false,
                stoppedByUser: false,
                resumed: action === "resume",
                errorRoute: null,
                pid: null,
                startedAt: null,
                updatedAt: null,
                completedAt: null
            }
        };
    }

    function showPending(bundleId, bundleName, bundleType, action, message, localPhase, poll) {
        previousBundleId = bundleId || previousBundleId;
        minimized = false;
        optimistic = pendingState(bundleId, bundleName, bundleType, action, message, localPhase, poll !== true);
        render(optimistic);
        stopTimer();
        return poll === true ? refresh() : Promise.resolve(optimistic);
    }

    function notifyStarted(bundleId, bundleName, bundleType, action) {
        return showPending(
            bundleId,
            bundleName,
            bundleType,
            action || "export",
            "Операция запускается. Получаем первый счётчик маршрутов…",
            "starting",
            true
        );
    }

    function clearPending(refreshAfter) {
        optimistic = null;
        stopTimer();
        if (refreshAfter === false) {
            render({active:false, progress:{}});
            previousActive = false;
            return Promise.resolve(null);
        }
        return refresh();
    }

    function initialize() {
        bind();
        refresh();
    }

    window.BROrayRoutesOperationUI = {
        refresh: refresh,
        showPending: showPending,
        notifyStarted: notifyStarted,
        clearPending: clearPending,
        requestStop: requestStop,
        requestResume: requestResume,
        getCurrent: function () { return current; }
    };

    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", initialize, {once:true});
    else initialize();
})();
