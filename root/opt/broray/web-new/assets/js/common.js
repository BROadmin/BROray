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

    window.BROrayUI = {
        apiRequest: apiRequest,
        toast: toast,
        redirectToLogin: redirectToLogin
    };
})();
