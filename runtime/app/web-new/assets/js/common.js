(function () {
    "use strict";

    async function apiRequest(url, options) {
        const requestOptions = Object.assign(
            {
                credentials: "same-origin",
                cache: "no-store",
                headers: {}
            },
            options || {}
        );

        if (
            requestOptions.body &&
            typeof requestOptions.body !== "string"
        ) {
            requestOptions.headers["Content-Type"] =
                "application/json";
            requestOptions.body = JSON.stringify(
                requestOptions.body
            );
        }

        const response = await fetch(url, requestOptions);

        let payload = null;

        try {
            payload = await response.json();
        } catch (error) {
            payload = null;
        }

        if (!response.ok) {
            const errorPayload = payload && payload.error
                ? payload.error
                : payload;
            const requestError = new Error(
                errorPayload && errorPayload.message
                    ? errorPayload.message
                    : "Ошибка запроса."
            );

            requestError.status = response.status;
            requestError.payload = payload;
            requestError.code = errorPayload && errorPayload.code
                ? errorPayload.code
                : null;
            requestError.details = errorPayload && errorPayload.details
                ? errorPayload.details
                : null;

            throw requestError;
        }

        return payload;
    }

    function toast(message, type) {
        const root = document.getElementById("toast-root");

        if (!root) {
            return;
        }

        const element = document.createElement("div");

        element.className =
            "toast toast-" + (type || "success");
        element.textContent = message;

        root.appendChild(element);

        window.setTimeout(function () {
            element.remove();
        }, 4000);
    }

    function redirectToLogin() {
        window.location.replace("/");
    }

    function normalizeHealth(value, moduleName) {
        var input = value && value.health && typeof value.health === "object"
            ? value.health
            : value;
        var severityValues = ["ok", "warning", "error", "busy", "unknown"];
        var availabilityValues = ["available", "partial", "unavailable"];
        var freshnessValues = ["fresh", "stale", "expired", "unknown"];
        var health = input && typeof input === "object" ? input : {};
        var severity = severityValues.indexOf(health.severity) >= 0
            ? health.severity
            : "unknown";
        var availability = availabilityValues.indexOf(health.availability) >= 0
            ? health.availability
            : "unavailable";
        var freshness = health.freshness && typeof health.freshness === "object"
            ? health.freshness
            : {};
        var freshnessState = freshnessValues.indexOf(freshness.state) >= 0
            ? freshness.state
            : "unknown";

        return {
            schemaVersion: Number(health.schemaVersion || 1),
            module: health.module || moduleName || "unknown",
            availability: availability,
            severity: severity,
            operational: health.operational === true,
            consistent: health.consistent === true,
            actionRequired: health.actionRequired !== false,
            freshness: {
                state: freshnessState,
                checkedAt: freshness.checkedAt || null
            },
            reasons: Array.isArray(health.reasons) ? health.reasons : [],
            facts: health.facts && typeof health.facts === "object" ? health.facts : {},
            lastOperation: health.lastOperation && typeof health.lastOperation === "object"
                ? health.lastOperation
                : null
        };
    }

    function severityMeta(value) {
        var health = typeof value === "string"
            ? {severity: value}
            : normalizeHealth(value);
        var map = {
            ok: {label: "Исправно", tone: "success", badgeClass: "status-success"},
            warning: {label: "Требуется внимание", tone: "warning", badgeClass: "status-warning"},
            error: {label: "Требуется исправление", tone: "error", badgeClass: "status-error"},
            busy: {label: "Выполняется", tone: "loading", badgeClass: "status-loading"},
            unknown: {label: "Не проверено", tone: "neutral", badgeClass: "status-neutral"}
        };
        return map[health.severity] || map.unknown;
    }

    function firstHealthReason(value, fallback) {
        var health = normalizeHealth(value);
        var reason = health.reasons.find(function (item) {
            return item && typeof item.message === "string" && item.message.trim();
        });
        return reason ? reason.message : (fallback || "Состояние не определено.");
    }

    function healthIsConfirmedOk(value) {
        var health = normalizeHealth(value);
        return health.availability === "available" &&
            health.severity === "ok" &&
            health.operational === true &&
            health.consistent === true &&
            health.actionRequired === false &&
            health.freshness.state === "fresh";
    }

    window.BROrayUI = {
        apiRequest: apiRequest,
        toast: toast,
        redirectToLogin: redirectToLogin,
        normalizeHealth: normalizeHealth,
        severityMeta: severityMeta,
        firstHealthReason: firstHealthReason,
        healthIsConfirmedOk: healthIsConfirmedOk
    };
})();
