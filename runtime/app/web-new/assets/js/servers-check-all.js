(function () {
    "use strict";

    var SUMMARY_URL =
        "/api/servers/summary.cgi";

    var CHECK_URL =
        "/api/servers/check.cgi";

    var BATCH_COMPLETE_URL =
        "/api/servers/quality-batch-complete.cgi";

    var IDLE_NOTE =
        "«Проверить все серверы» запускает изолированный " +
        "Xray и выполняет HTTPS-запрос через каждый " +
        "сохранённый сервер. После завершения обновятся " +
        "пинг, джиттер, качество и время проверки.";

    function text(value) {
        if (
            value === null ||
            value === undefined
        ) {
            return "";
        }

        return String(value);
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

        if (
            error &&
            error.payload &&
            error.payload.message
        ) {
            return error.payload.message;
        }

        return error && error.message
            ? error.message
            : "Операция завершилась ошибкой.";
    }

    function unwrap(payload) {
        var error;

        if (
            payload &&
            payload.success === false
        ) {
            error = new Error(
                payload.error &&
                payload.error.message
                    ? payload.error.message
                    : "API сообщил об ошибке."
            );

            error.payload = payload;
            throw error;
        }

        if (
            payload &&
            payload.success === true &&
            payload.data !== undefined
        ) {
            return payload.data;
        }

        return payload;
    }

    function request(url, options) {
        return BROrayUI
            .apiRequest(url, options)
            .then(unwrap);
    }

    function serverId(server) {
        if (!server || typeof server !== "object") {
            return null;
        }

        if (
            server.id !== null &&
            server.id !== undefined &&
            text(server.id) !== ""
        ) {
            return server.id;
        }

        if (
            server.serverId !== null &&
            server.serverId !== undefined &&
            text(server.serverId) !== ""
        ) {
            return server.serverId;
        }

        return null;
    }

    function looksLikeServer(server) {
        if (
            !server ||
            typeof server !== "object" ||
            serverId(server) === null
        ) {
            return false;
        }

        return Boolean(
            server.name ||
            server.address ||
            server.host ||
            server.port ||
            server.uri ||
            server.protocol ||
            server.transport ||
            server.security
        );
    }

    function extractServerIds(payload) {
        var data = unwrap(payload);
        var result = [];
        var seen = {};

        function add(server) {
            var id = serverId(server);
            var key;

            if (id === null) {
                return;
            }

            key = text(id);

            if (!key || seen[key]) {
                return;
            }

            seen[key] = true;
            result.push(id);
        }

        function addArray(array) {
            if (
                Object.prototype.toString.call(array) !==
                "[object Array]"
            ) {
                return;
            }

            array.forEach(function (server) {
                add(server);
            });
        }

        function walk(value, key, depth) {
            var property;

            if (
                depth > 8 ||
                value === null ||
                value === undefined
            ) {
                return;
            }

            if (
                Object.prototype.toString.call(value) ===
                "[object Array]"
            ) {
                value.forEach(function (item) {
                    if (
                        /server/i.test(key || "") ||
                        looksLikeServer(item)
                    ) {
                        add(item);
                    }

                    walk(
                        item,
                        key,
                        depth + 1
                    );
                });

                return;
            }

            if (typeof value !== "object") {
                return;
            }

            if (looksLikeServer(value)) {
                add(value);
            }

            for (property in value) {
                if (
                    Object.prototype.hasOwnProperty.call(
                        value,
                        property
                    )
                ) {
                    walk(
                        value[property],
                        property,
                        depth + 1
                    );
                }
            }
        }

        if (data) {
            addArray(data.servers);
            addArray(data.items);
            addArray(data.savedServers);

            if (
                data.summary &&
                typeof data.summary === "object"
            ) {
                addArray(data.summary.servers);
            }
        }

        if (!result.length) {
            walk(data, "servers", 0);
        }

        return result;
    }

    function delay(milliseconds) {
        return new Promise(function (resolve) {
            window.setTimeout(
                resolve,
                milliseconds
            );
        });
    }

    function setNote(message) {
        var note = document.querySelector(
            ".servers-refresh-note"
        );

        if (note) {
            note.textContent = message;
        }
    }

    function setButton(button, busy, label) {
        if (!button) {
            return;
        }

        button.disabled = busy;

        if (busy) {
            button.setAttribute(
                "aria-busy",
                "true"
            );
        } else {
            button.removeAttribute(
                "aria-busy"
            );
        }

        button.textContent =
            label || "Проверить все серверы";
    }

    function refreshSummary() {
        var refresh = document.getElementById(
            "refresh-servers"
        );

        if (refresh) {
            refresh.click();
        }
    }

    function checkNext(
        ids,
        index,
        state,
        button
    ) {
        if (index >= ids.length) {
            return Promise.resolve(state);
        }

        setButton(
            button,
            true,
            "Проверка " +
                (index + 1) +
                " из " +
                ids.length +
                "…"
        );

        setNote(
            "Выполняется последовательная проверка: " +
                (index + 1) +
                " из " +
                ids.length +
                "."
        );

        return request(
            CHECK_URL,
            {
                method: "POST",
                body: {
                    id: ids[index]
                }
            }
        ).then(
            function () {
                state.success += 1;
            },
            function (error) {
                if (
                    error &&
                    error.status === 401
                ) {
                    BROrayUI.redirectToLogin();
                    throw error;
                }

                state.failed += 1;
                state.errors.push(
                    errorMessage(error)
                );
            }
        ).then(function () {
            return delay(180);
        }).then(function () {
            return checkNext(
                ids,
                index + 1,
                state,
                button
            );
        });
    }

    function runCheckAll(button) {
        setButton(button,true,"Подготовка…");
        BROrayUI.followQueued(CHECK_URL,"all",function (result) {
            var pending=["queued","running"].includes(result.state);
            setButton(button,false,pending ? BROrayUI.queueLabel(result) : "Проверить все серверы");
            button.disabled=pending;
            setNote(pending ? BROrayUI.queueLabel(result)+". Серверы проверяются по одному, уступая активному подключению." : IDLE_NOTE);
            if (!pending) {
                refreshSummary();
                BROrayUI.toast(result.state === "completed" ? "Проверка всех серверов завершена." :
                    result.message || BROrayUI.queueLabel(result),result.state === "completed" ? "success" : "warning");
            }
        }).catch(function (error) {
            setButton(button,false,"Проверить все серверы");
            setNote(error.message || "Результат запроса не подтверждён.");
            BROrayUI.toast(error.message || "Результат запроса не подтверждён.","error");
        });
    }

    function updateInterface() {
        var button = document.getElementById(
            "check-all-servers"
        );

        var note = document.querySelector(
            ".servers-refresh-note"
        );

        if (note) {
            note.textContent = IDLE_NOTE;
        }

        if (
            button &&
            button.getAttribute(
                "data-check-all-bound"
            ) !== "true"
        ) {
            button.setAttribute(
                "data-check-all-bound",
                "true"
            );

            button.addEventListener(
                "click",
                function () {
                    runCheckAll(button);
                }
            );
            if (BROrayUI.hasQueuedRequest(CHECK_URL,"all")) runCheckAll(button);
        }
    }

    function initialize() {
        var attempts = 0;

        updateInterface();

        var timer = window.setInterval(
            function () {
                attempts += 1;
                updateInterface();

                if (
                    attempts >= 20 ||
                    (
                        document.querySelector(
                            ".servers-refresh-note"
                        ) &&
                        document.getElementById(
                            "check-all-servers"
                        )
                    )
                ) {
                    window.clearInterval(timer);
                }
            },
            250
        );
    }

    if (document.readyState === "loading") {
        document.addEventListener(
            "DOMContentLoaded",
            initialize
        );
    } else {
        initialize();
    }
})();
