(function () {
    "use strict";

    var state = {
        summary: null,
        details: {},
        busy: false
    };

    var app = document.getElementById("app");
    var loader = document.getElementById("page-loader");
    var currentUser = document.getElementById("current-user");

    function element(id) {
        return document.getElementById(id);
    }

    function unwrap(payload) {
        if (!payload) {
            throw new Error("Сервер вернул пустой ответ.");
        }
        if (payload.success === false) {
            var message = "Операция завершилась ошибкой.";
            if (payload.error && payload.error.message) {
                message = payload.error.message;
            }
            if (payload.error && payload.error.details) {
                message += " " + payload.error.details;
            }
            throw new Error(message);
        }
        if (payload.success === true) {
            return payload.data;
        }
        return payload;
    }

    function request(url, options) {
        return BROrayUI.apiRequest(url, options).then(unwrap);
    }

    function setBusy(button, busy, label) {
        var original;

        if (!button) {
            return;
        }

        if (busy) {
            if (!button.hasAttribute("data-original-label")) {
                button.setAttribute("data-original-label", button.textContent.trim());
            }
            button.disabled = true;
            button.setAttribute("aria-busy", "true");
            button.textContent = label || "Выполнение…";
        } else {
            original = button.getAttribute("data-original-label");
            if (original) {
                button.textContent = original;
                button.removeAttribute("data-original-label");
            }
            button.disabled = false;
            button.removeAttribute("aria-busy");
        }

        if (window.BROrayIcons) {
            window.BROrayIcons.scan(button);
        }
    }

    function text(value, fallback) {
        if (value === null || value === undefined || value === "") {
            return fallback || "—";
        }
        return String(value);
    }

    function formatDate(value) {
        if (!value) {
            return "Нет данных";
        }
        var date = new Date(value);
        if (isNaN(date.getTime())) {
            return String(value);
        }
        return date.toLocaleString("ru-RU");
    }

    function qualityLabel(quality) {
        var status = quality && quality.status
            ? quality.status
            : "unknown";
        if (status === "available") {
            return "Доступен";
        }
        if (status === "unavailable") {
            return "Недоступен";
        }
        return "Не проверялся";
    }

    function qualityClass(quality) {
        var status = quality && quality.status
            ? quality.status
            : "unknown";
        if (status === "available") {
            return "status-badge-success";
        }
        if (status === "unavailable") {
            return "status-badge-danger";
        }
        return "status-badge-neutral";
    }

    function connectionLabel(value) {
        if (value === "connected") {
            return "Подключено";
        }
        if (value === "error") {
            return "Ошибка";
        }
        return "Нет подключения";
    }

    function create(tag, className, content) {
        var node = document.createElement(tag);
        if (className) {
            node.className = className;
        }
        if (content !== undefined && content !== null) {
            node.textContent = content;
        }
        return node;
    }

    function appendDefinition(container, label, value) {
        var item = create("div", "server-detail-item");
        item.appendChild(create("span", "server-detail-label", label));
        item.appendChild(
            create("strong", "server-detail-value", text(value))
        );
        container.appendChild(item);
    }

    function showConfirm(title, message, acceptLabel, danger) {
        if (
            !window.BROrayDialogs ||
            typeof window.BROrayDialogs.confirm !== "function"
        ) {
            return Promise.reject(
                new Error("Фирменное окно подтверждения недоступно.")
            );
        }

        return window.BROrayDialogs.confirm({
            eyebrow: danger ? "Опасное действие" : "Подтверждение",
            title: title,
            message: message,
            confirmText: acceptLabel || "Продолжить",
            cancelText: "Отмена",
            variant: danger ? "danger" : "primary"
        });
    }

    function renderSummary(summary) {
        state.summary = summary || {};

        var active = state.summary.activeServer;
        var connectionState =
            state.summary.connectionState || "disabled";
        var running = state.summary.xrayRunning === true;

        element("servers-total").textContent =
            text(state.summary.total, "0");

        element("servers-available").textContent =
            text(state.summary.available, "0");

        element("servers-unavailable").textContent =
            text(state.summary.unavailable, "0");

        var autoSwitch = state.summary.autoSwitch || {};

        element("servers-auto-switch").textContent =
            autoSwitch.enabled === true
                ? "Включён"
                : "Выключен";

        element("servers-selection-rule").textContent =
            autoSwitch.selectionRule === "manual"
                ? "Ручной выбор"
                : text(autoSwitch.selectionRule, "Не задано");

        var live = element("servers-live-status");

        live.className = "servers-live-status " +
            (running ? "is-running" : "is-stopped");

        element("servers-live-text").textContent = running
            ? "Xray работает"
            : "Xray остановлен";

        var badge = element("connection-state-badge");

        badge.textContent = connectionLabel(connectionState);
        badge.className = "status-badge ";

        if (connectionState === "connected") {
            badge.className += "status-badge-success";
        } else if (connectionState === "error") {
            badge.className += "status-badge-danger";
        } else {
            badge.className += "status-badge-neutral";
        }
        badge.setAttribute("data-icon", "connection");
        if (window.BROrayIcons) {
            window.BROrayIcons.scan(badge);
        }

        var activeCard =
            document.querySelector(".server-active-card");

        if (activeCard) {
            activeCard.classList.toggle(
                "is-connected",
                Boolean(active) && running
            );
        }

        if (active) {
            element("active-server-name").textContent =
                text(active.name, active.id);

            element("active-server-description").textContent =
                text(active.address) + ":" + text(active.port);

            var protocols =
                element("active-server-protocols");

            protocols.innerHTML = "";

            protocols.appendChild(
                create(
                    "span",
                    "server-tag",
                    text(active.protocol)
                )
            );

            protocols.appendChild(
                create(
                    "span",
                    "server-tag",
                    text(active.transport)
                )
            );

            protocols.appendChild(
                create(
                    "span",
                    "server-tag",
                    text(active.security)
                )
            );

            element("active-server-mark").textContent = "";

            var meta = element("active-server-meta");
            var quality = active.quality || {};
            var rating = quality.rating || "unknown";
            var ratingText = "Нет данных";

            if (rating === "excellent") {
                ratingText = "Отличное";
            } else if (rating === "good") {
                ratingText = "Хорошее";
            } else if (rating === "fair") {
                ratingText = "Среднее";
            } else if (rating === "poor") {
                ratingText = "Низкое";
            } else if (rating === "bad") {
                ratingText = "Плохое";
            } else if (quality.status === "available") {
                ratingText = "Доступен";
            } else if (quality.status === "unavailable") {
                ratingText = "Недоступен";
            }

            meta.innerHTML = "";

            appendDefinition(
                meta,
                "Пинг",
                quality.ping !== null &&
                    quality.ping !== undefined
                    ? quality.ping + " мс"
                    : "Нет данных"
            );

            appendDefinition(
                meta,
                "Джиттер",
                quality.jitter !== null &&
                    quality.jitter !== undefined
                    ? quality.jitter + " мс"
                    : "Нет данных"
            );

            appendDefinition(
                meta,
                "Качество",
                ratingText
            );

            element("check-active-server").disabled = false;

            element("check-active-server").setAttribute(
                "data-server-id",
                active.id
            );

            element("deactivate-servers").disabled = false;
        } else {
            element("active-server-name").textContent =
                "Активный сервер не выбран";

            element("active-server-description").textContent =
                "Выберите сервер из списка ниже.";

            element("active-server-protocols").textContent = "";

            element("active-server-mark").textContent = "";

            element("active-server-meta").innerHTML = "";

            element("check-active-server").disabled = true;

            element("check-active-server").removeAttribute(
                "data-server-id"
            );

            element("deactivate-servers").disabled = true;
        }

        var count = Array.isArray(state.summary.servers)
            ? state.summary.servers.length
            : 0;

        element("servers-list-description").textContent =
            count === 0
                ? "Сохранённых серверов нет."
                : "Сохранено серверов: " + count;

        renderServers(state.summary.servers || []);
    }
    function renderServers(servers) {
        var list = element("servers-list");
        list.innerHTML = "";

        if (!servers.length) {
            var empty = create("article", "servers-empty-card");
            empty.appendChild(
                create("h3", "", "Серверы не добавлены")
            );
            empty.appendChild(
                create(
                    "p",
                    "",
                    "Импортируйте URI-конфигурацию или добавьте подписку."
                )
            );
            list.appendChild(empty);
            return;
        }

        servers.forEach(function (server) {
            list.appendChild(renderServerCard(server));
        });
    }

    function renderServerCard(server) {
        var card = create(
            "article",
            "server-card ui-card" + (server.active ? " is-active" : "")
        );
        var header = create("div", "server-card-header");
        var identity = create("div", "server-card-identity");
        var titleRow = create("div", "server-card-title-row");
        var title = create(
            "h3",
            "server-card-title",
            text(server.name, server.id)
        );
        var quality = create(
            "span",
            "status-badge " + qualityClass(server.quality),
            qualityLabel(server.quality)
        );
        var tags = create("div", "server-card-tags");
        var metrics = create("div", "server-card-metrics");
        var actions = create("div", "server-card-actions");
        var detailsRoot = create("div", "server-details");
        var checkButton;
        var activateButton;
        var toggleButton;

        card.setAttribute("data-server-id", server.id);
        quality.setAttribute("data-icon", "status");

        titleRow.append(title, quality);
        identity.append(
            titleRow,
            create(
                "p",
                "server-card-address",
                text(server.address) + ":" + text(server.port)
            )
        );

        tags.append(
            create("span", "server-tag", text(server.protocol)),
            create("span", "server-tag", text(server.transport)),
            create("span", "server-tag", text(server.security))
        );
        if (server.active) {
            tags.appendChild(create("span", "server-tag server-tag-active", "Активен"));
        }
        identity.appendChild(tags);
        header.appendChild(identity);
        card.appendChild(header);

        appendDefinition(
            metrics,
            "Пинг",
            server.quality && server.quality.ping !== null && server.quality.ping !== undefined
                ? server.quality.ping + " мс"
                : "—"
        );
        appendDefinition(
            metrics,
            "Джиттер",
            server.quality && server.quality.jitter !== null && server.quality.jitter !== undefined
                ? server.quality.jitter + " мс"
                : "—"
        );
        appendDefinition(
            metrics,
            "Последняя проверка",
            formatDate(server.quality && server.quality.lastCheckedAt)
        );
        card.appendChild(metrics);

        checkButton = create("button", "button button-secondary", "Проверить");
        checkButton.type = "button";
        checkButton.setAttribute("data-icon", "speed");
        checkButton.addEventListener("click", function () {
            checkServer(server.id, checkButton);
        });
        actions.appendChild(checkButton);

        if (!server.active) {
            activateButton = create("button", "button button-primary", "Подключить");
            activateButton.type = "button";
            activateButton.setAttribute("data-icon", "connection");
            activateButton.addEventListener("click", function () {
                activateServer(server, activateButton);
            });
            actions.appendChild(activateButton);
        }

        toggleButton = create(
            "button",
            "button button-secondary server-details-toggle",
            "Подробнее"
        );
        toggleButton.type = "button";
        toggleButton.setAttribute("data-icon", "chevron");
        toggleButton.setAttribute("aria-expanded", "false");
        toggleButton.addEventListener("click", function () {
            toggleDetails(server, card, toggleButton);
        });
        actions.appendChild(toggleButton);
        card.appendChild(actions);

        detailsRoot.hidden = true;
        card.appendChild(detailsRoot);
        return card;
    }

    function toggleDetails(server, card, button) {
        var detailsRoot = card.querySelector(".server-details");
        var expanded = button.getAttribute("aria-expanded") === "true";

        if (expanded) {
            detailsRoot.hidden = true;
            card.classList.remove("is-expanded");
            button.setAttribute("aria-expanded", "false");
            button.textContent = "Подробнее";
            if (window.BROrayIcons) {
                window.BROrayIcons.scan(button);
            }
            return;
        }

        if (state.details[server.id]) {
            renderDetails(detailsRoot, state.details[server.id], server);
            detailsRoot.hidden = false;
            card.classList.add("is-expanded");
            button.setAttribute("aria-expanded", "true");
            button.textContent = "Скрыть";
            if (window.BROrayIcons) {
                window.BROrayIcons.scan(button);
            }
            return;
        }

        setBusy(button, true, "Загрузка…");
        request(
            "/api/servers/details.cgi?id=" + encodeURIComponent(server.id),
            { method: "GET" }
        ).then(function (details) {
            state.details[server.id] = details;
            renderDetails(detailsRoot, details, server);
            detailsRoot.hidden = false;
            card.classList.add("is-expanded");
            setBusy(button, false);
            button.setAttribute("aria-expanded", "true");
            button.textContent = "Скрыть";
            if (window.BROrayIcons) {
                window.BROrayIcons.scan(button);
            }
        }).catch(function (error) {
            setBusy(button, false);
            handleError(error);
        });
    }

    function renderDetails(root, details, server) {
        var heading;
        var grid;
        var errorBox;
        var danger;
        var dangerCopy;
        var deleteButton;

        root.innerHTML = "";
        heading = create("div", "server-details-heading");
        heading.append(
            create("h4", "", "Технические сведения"),
            create(
                "p",
                "",
                "Секретные параметры скрыты. Для изменения импортируйте сервер заново."
            )
        );
        root.appendChild(heading);

        grid = create("div", "server-details-grid");
        appendDefinition(grid, "ID", details.id);
        appendDefinition(grid, "Адрес", details.address);
        appendDefinition(grid, "Порт", details.port);
        appendDefinition(grid, "Протокол", details.protocol);
        appendDefinition(
            grid,
            "Транспорт",
            details.network || (details.transport && details.transport.type)
        );
        appendDefinition(grid, "Защита", details.security);
        appendDefinition(grid, "Источник", details.source && details.source.type);
        appendDefinition(grid, "Подписка", details.source && details.source.subscriptionId);
        appendDefinition(grid, "Номер узла", details.source && details.source.nodeIndex);
        appendDefinition(
            grid,
            "SNI",
            details.reality && details.reality.serverName
                ? details.reality.serverName
                : details.tls && details.tls.serverName
        );
        appendDefinition(grid, "Публичный ключ", details.reality && details.reality.publicKey);
        appendDefinition(grid, "Short ID", details.reality && details.reality.shortId);
        appendDefinition(
            grid,
            "Путь",
            details.xhttp && details.xhttp.path
                ? details.xhttp.path
                : details.transport && details.transport.path
        );
        appendDefinition(
            grid,
            "Service name",
            details.grpc && details.grpc.serviceName
                ? details.grpc.serviceName
                : details.transport && details.transport.serviceName
        );
        appendDefinition(grid, "Последняя проверка", formatDate(details.quality && details.quality.lastCheckedAt));
        appendDefinition(grid, "Успешных проверок", details.quality && details.quality.successfulChecks);
        appendDefinition(grid, "Неудачных проверок", details.quality && details.quality.failedChecks);
        root.appendChild(grid);

        if (details.quality && details.quality.error) {
            errorBox = create("div", "server-detail-error", details.quality.error);
            root.appendChild(errorBox);
        }

        danger = create("div", "server-details-danger");
        dangerCopy = create("div", "server-details-danger-copy");
        dangerCopy.append(
            create("strong", "", "Удаление сервера"),
            create(
                "p",
                "",
                server.active
                    ? "Сначала отключите сервер или выберите другое подключение."
                    : "Сервер будет удалён без возможности отмены."
            )
        );
        deleteButton = create("button", "button button-danger-outline", "Удалить сервер");
        deleteButton.type = "button";
        deleteButton.setAttribute("data-icon", "delete");
        deleteButton.disabled = Boolean(server.active);
        if (!server.active) {
            deleteButton.addEventListener("click", function () {
                deleteServer(server, deleteButton);
            });
        }
        danger.append(dangerCopy, deleteButton);
        root.appendChild(danger);
    }

    function loadSummary(showToast) {
        var refresh = element("refresh-servers");
        setBusy(refresh, true, "Обновление…");

        return request(
            "/api/servers/summary.cgi",
            { method: "GET" }
        ).then(function (summary) {
            renderSummary(summary);
            if (showToast) {
                BROrayUI.toast("Список серверов обновлён.", "success");
            }
        }).catch(function (error) {
            handleError(error);
            throw error;
        }).then(function () {
            setBusy(refresh, false);
        }, function (error) {
            setBusy(refresh, false);
            throw error;
        });
    }

    function checkServer(serverId, button) {
        setBusy(button, true, "Проверка…");

        request(
            "/api/servers/check.cgi",
            {
                method: "POST",
                body: { id: serverId }
            }
        ).then(function () {
            BROrayUI.toast("Проверка сервера завершена.", "success");
        }).catch(function (error) {
            BROrayUI.toast(
                error.message || "Сервер недоступен.",
                "error"
            );
        }).then(function () {
            delete state.details[serverId];
            return loadSummary(false);
        }).then(function () {
            setBusy(button, false);
        }).catch(function () {
            setBusy(button, false);
        });
    }

    function activateServer(server, button) {
        showConfirm(
            "Подключить сервер",
            "Активировать «" + text(server.name, server.id) + "»?",
            "Подключить",
            false
        ).then(function (confirmed) {
            if (!confirmed) {
                return;
            }

            setBusy(button, true, "Подключение…");
            request(
                "/api/servers/activate.cgi",
                {
                    method: "POST",
                    body: { id: server.id }
                }
            ).then(function () {
                state.details = {};
                BROrayUI.toast("Сервер активирован.", "success");
                return loadSummary(false);
            }).catch(handleError).then(function () {
                setBusy(button, false);
            });
        });
    }

    function deactivateServers(button) {
        showConfirm(
            "Отключить серверы",
            "Xray будет остановлен, а активное подключение отключено. Сохранённые серверы останутся.",
            "Отключить",
            true
        ).then(function (confirmed) {
            if (!confirmed) {
                return;
            }

            setBusy(button, true, "Отключение…");
            request(
                "/api/servers/deactivate.cgi",
                {
                    method: "POST",
                    body: {}
                }
            ).then(function () {
                state.details = {};
                BROrayUI.toast("Серверы отключены.", "success");
                return loadSummary(false);
            }).catch(handleError).then(function () {
                setBusy(button, false);
            });
        });
    }

    function deleteServer(server, button) {
        showConfirm(
            "Удалить сервер",
            "Удалить «" + text(server.name, server.id) + "»? Отменить это действие нельзя.",
            "Удалить",
            true
        ).then(function (confirmed) {
            if (!confirmed) {
                return;
            }

            setBusy(button, true, "Удаление…");
            request(
                "/api/servers/delete.cgi",
                {
                    method: "POST",
                    body: { id: server.id }
                }
            ).then(function () {
                delete state.details[server.id];
                BROrayUI.toast("Сервер удалён.", "success");
                return loadSummary(false);
            }).catch(handleError).then(function () {
                setBusy(button, false);
            });
        });
    }

    function toggleImport(show) {
        var card = element("server-import-card");
        var button = element("toggle-import");

        card.hidden = !show;
        button.setAttribute("aria-expanded", show ? "true" : "false");
        button.textContent = show ? "Скрыть форму" : "Добавить сервер";
        button.setAttribute("data-icon", show ? "close" : "add");

        if (window.BROrayIcons) {
            window.BROrayIcons.scan(button);
        }
        if (show) {
            element("server-import-uri").focus();
        }
    }

    function importServer(button) {
        var uriNode = element("server-import-uri");
        var uri = uriNode.value.replace(/^\s+|\s+$/g, "");

        if (!uri) {
            BROrayUI.toast(
                "Вставьте URI конфигурации сервера.",
                "error"
            );
            uriNode.focus();
            return;
        }

        setBusy(button, true, "Импорт…");
        request(
            "/api/servers/import.cgi",
            {
                method: "POST",
                body: { uri: uri }
            }
        ).then(function () {
            uriNode.value = "";
            toggleImport(false);
            state.details = {};
            BROrayUI.toast("Сервер импортирован.", "success");
            return loadSummary(false);
        }).catch(handleError).then(function () {
            setBusy(button, false);
        });
    }

    function handleError(error) {
        if (error && error.status === 401) {
            BROrayUI.redirectToLogin();
            return;
        }

        var message = error && error.message
            ? error.message
            : "Операция завершилась ошибкой.";

        if (
            error &&
            error.payload &&
            error.payload.error
        ) {
            if (error.payload.error.message) {
                message = error.payload.error.message;
            }
            if (error.payload.error.details) {
                message += " " + error.payload.error.details;
            }
        }

        BROrayUI.toast(message, "error");
    }

    function bindEvents() {
        var refresh = element("refresh-servers");
        var activeCheck = element("check-active-server");
        var deactivate = element("deactivate-servers");
        var toggle = element("toggle-import");
        var cancel = element("cancel-import");
        var submit = element("submit-import");
        var form = element("server-import-form");

        if (refresh) {
            refresh.addEventListener("click", function () {
                loadSummary(true).catch(function () { return; });
            });
        }
        if (activeCheck) {
            activeCheck.addEventListener("click", function () {
                var id = this.getAttribute("data-server-id");
                if (id) {
                    checkServer(id, this);
                }
            });
        }
        if (deactivate) {
            deactivate.addEventListener("click", function () {
                deactivateServers(this);
            });
        }
        if (toggle) {
            toggle.addEventListener("click", function () {
                toggleImport(element("server-import-card").hidden);
            });
        }
        if (cancel) {
            cancel.addEventListener("click", function () {
                element("server-import-uri").value = "";
                toggleImport(false);
            });
        }
        if (form) {
            form.addEventListener("submit", function (event) {
                event.preventDefault();
                importServer(submit);
            });
        }
    }

    function start() {
        bindEvents();

        request(
            "/api/session.cgi",
            { method: "GET" }
        ).then(function (session) {
            if (currentUser) {
                currentUser.textContent =
                    session && session.user ? session.user : "admin";
            }
            if (loader) {
                loader.hidden = true;
            }
            if (app) {
                app.hidden = false;
            }
            return loadSummary(false);
        }).catch(handleError);
    }

    start();
})();
