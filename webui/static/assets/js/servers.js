(function () {
    "use strict";

    var state = {
        summary: null,
        details: {},
        detailsPending: {},
        queuedChecks: {},
        groupSelection: new Map(),
        groupActive: new Map(),
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

    function qualitySourceLabel(quality) {
        var source = quality && quality.measurementSource;

        if (source === "manual") return "Ручная проверка";
        if (source === "auto-switch") return "Автовыбор после отказа";
        if (source === "scheduled") return "Автоматическая проверка";
        return "Источник не зафиксирован";
    }

    function qualityMethodLabel(quality) {
        var method = quality && quality.measurementMethod;

        if (method === "https-proxy-warm") return "HTTPS через прогретый туннель";

        if (method === "https-proxy+icmp") {
            return "HTTPS через Xray + ICMP ping";
        }

        return "Метод не зафиксирован";
    }

    function qualityLatencyLabel(quality) {
        return quality && quality.measurementMethod === "https-proxy-warm"
            ? "Задержка через туннель" : "Пинг";
    }

    function appendCloudEstablishment(root, quality) {
        if (!quality || quality.measurementMethod !== "https-proxy-warm") return;
        appendDefinition(root, "Установление туннеля",
            quality.establishmentMs !== null && quality.establishmentMs !== undefined
                ? quality.establishmentMs + " мс" : "Нет данных");
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
        var status = quality && quality.status ? quality.status : "unknown";
        var freshness = quality && quality.freshness ? quality.freshness : "unknown";
        if (status === "available" && freshness === "fresh") return "Доступен";
        if (status === "available" && freshness === "stale") return "Данные устаревают";
        if (status === "available" && freshness === "expired") return "Проверка устарела";
        if (status === "unavailable") return "Недоступен";
        return "Не проверялся";
    }

    function qualityClass(quality) {
        var status = quality && quality.status ? quality.status : "unknown";
        var freshness = quality && quality.freshness ? quality.freshness : "unknown";
        if (status === "available" && freshness === "fresh") return "status-badge-success";
        if (status === "unavailable") return "status-badge-danger";
        if (status === "available") return "status-badge-warning";
        return "status-badge-neutral";
    }

    function connectionLabel(value) {
        if (value === "connected") return "Подключено";
        if (value === "degraded") return "Подключение не подтверждено";
        if (value === "error") return "Ошибка";
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
            text(autoSwitch.selectionRuleLabel, autoSwitch.selectionRule === "manual" ? "Ручной выбор" : "Не задано");

        var live = element("servers-live-status");
        var moduleHealth = state.summary.health && typeof state.summary.health === "object" ? state.summary.health : null;
        var severity = moduleHealth && moduleHealth.severity ? moduleHealth.severity : connectionState === "connected" ? "ok" : "unknown";
        var socksActive = state.summary.socksActive === true;

        live.className = "servers-live-status " +
            (running && socksActive ? "is-running" : "is-stopped");

        element("servers-live-text").textContent = running
            ? socksActive ? "Xray и SOCKS работают" : "Xray запущен, SOCKS не подтверждён"
            : "Xray остановлен";

        var badge = element("connection-state-badge");

        badge.textContent = connectionLabel(connectionState);
        badge.className = "status-badge ";

        if (severity === "ok") {
            badge.className += "status-badge-success";
        } else if (severity === "error") {
            badge.className += "status-badge-danger";
        } else if (severity === "warning") {
            badge.className += "status-badge-warning";
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
                Boolean(active) && connectionState === "connected" && severity === "ok"
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
            if (quality.freshness === "stale") ratingText += " · данные устаревают";
            if (quality.freshness === "expired") ratingText += " · проверка устарела";
            if (quality.freshness === "unknown") ratingText += " · свежесть не определена";

            meta.innerHTML = "";

            appendDefinition(
                meta,
                qualityLatencyLabel(quality),
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

            appendDefinition(
                meta,
                "Проверено",
                formatDate(quality.lastCheckedAt)
            );

            appendDefinition(
                meta,
                "Источник проверки",
                qualitySourceLabel(quality)
            );

            appendCloudEstablishment(meta, quality);

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

    function sourceLabel(server) {
        return server.sourceType === "subscription"
            ? text(server.subscriptionName, server.subscriptionId || server.id)
            : "Ручной импорт";
    }

    function displayGroups(servers) {
        var groups = new Map();
        servers.forEach(function (server) {
            var id = server.displayGroupId || server.id;
            if (!groups.has(id)) groups.set(id, {id: id, members: []});
            groups.get(id).members.push(server);
        });
        groups.forEach(function (group) {
            group.members.sort(function (a, b) { return a.id < b.id ? -1 : a.id > b.id ? 1 : 0; });
            group.active = group.members.find(function (member) { return member.active; });
            var activeId = group.active ? group.active.id : null;
            var selected = group.members.find(function (member) { return member.id === state.groupSelection.get(group.id); });
            if (group.active && (!selected || state.groupActive.get(group.id) !== activeId)) selected = group.active;
            group.server = selected || group.members.find(function (member) { return member.sourceType !== "subscription"; }) || group.members[0];
            state.groupActive.set(group.id, activeId);
            state.groupSelection.set(group.id, group.server.id);
        });
        return Array.from(groups.values());
    }

    function renderServers(servers) {
        var list = element("servers-list");
        var groups = displayGroups(servers);
        var expanded = {};
        Array.prototype.forEach.call(list.querySelectorAll(".server-card"), function (card) {
            var toggle = card.querySelector(".server-details-toggle");
            if (toggle && (toggle.getAttribute("aria-expanded") === "true" ||
                toggle.getAttribute("aria-busy") === "true")) {
                expanded[card.getAttribute("data-server-id")] = true;
            }
        });
        list.innerHTML = "";
        element("servers-total").textContent = groups.length + " / " + servers.length;
        element("servers-list-description").textContent = "Уникальных карточек: " + groups.length + " · сохранённых вариантов: " + servers.length;

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

        groups.forEach(function (group) {
            var server = group.server;
            var card = renderServerCard(server, group);
            list.appendChild(card);
            if (expanded[server.id]) {
                toggleDetails(server, card, card.querySelector(".server-details-toggle"));
            }
        });
        servers.forEach(function (server) {
            if (!BROrayUI.hasQueuedRequest("/api/servers/check.cgi", server.id)) return;
            var button = Array.from(document.querySelectorAll("[data-check-server]")).find(function (item) { return item.dataset.checkServer === server.id; });
            if (server.active) button = element("check-active-server");
            if (button) checkServer(server.id, button);
        });
    }

    function renderServerCard(server, group) {
		var unsupported = server.connectionSupport && server.connectionSupport.supported === false;
        var card = create(
            "article",
            "server-card ui-card" + (group.active ? " is-active" : "")
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
        card.setAttribute("data-display-group-id", group.id);
        quality.setAttribute("data-icon", "status");
		if (unsupported) {
			quality.textContent = server.connectionSupport.mode === "requires-core" ? "Обновите Xray" : server.connectionSupport.mode === "core-unverified" ? "Проверьте Xray" : "Подключение не поддерживается";
			quality.className = "status-badge status-badge-warning";
		}

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
		if (server.connectionSupport && server.connectionSupport.message) {
			identity.appendChild(create("p", "server-card-address", server.connectionSupport.message));
		}
		if (server.connectionSupport && server.connectionSupport.notice) {
			identity.appendChild(create("p", "server-card-address", server.connectionSupport.notice));
		}
        if (group.members.length > 1) {
            var sourceControl = create("div", "form-field");
            var label = create("label", "", "Источник конфигурации");
            var select = create("select");
            select.id = "server-source-" + group.id;
            label.setAttribute("for", select.id);
            group.members.forEach(function (member) {
                var name = sourceLabel(member);
                if (group.members.filter(function (other) { return sourceLabel(other) === name; }).length > 1) name += " · " + member.id;
                var option = create("option", "", name + (member.active ? " · активен" : ""));
                option.value = member.id;
                select.appendChild(option);
            });
            select.value = server.id;
            select.addEventListener("change", function () {
                state.groupSelection.set(group.id, select.value);
                renderServers(state.summary.servers || []);
                var replacement = element(select.id);
                if (replacement) replacement.focus();
            });
            sourceControl.append(label, select);
            identity.appendChild(sourceControl);
        } else {
            identity.appendChild(create("p", "server-card-address", "Источник: " + sourceLabel(server)));
        }
        if (group.active) {
            identity.appendChild(create("p", "server-card-address", "Активный источник: " + sourceLabel(group.active)));
        }
        header.appendChild(identity);
        card.appendChild(header);

        appendDefinition(
            metrics,
            qualityLatencyLabel(server.quality),
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
        appendDefinition(
            metrics,
            "Источник проверки",
            qualitySourceLabel(server.quality)
        );
        appendCloudEstablishment(metrics, server.quality);
        card.appendChild(metrics);

        checkButton = create("button", "button button-secondary", "Проверить");
        checkButton.type = "button";
		checkButton.disabled = Boolean(unsupported);
        checkButton.dataset.checkServer=server.id;
        checkButton.setAttribute("data-icon", "speed");
        checkButton.addEventListener("click", function () {
            checkServer(server.id, checkButton);
        });
        actions.appendChild(checkButton);

        if (!server.active) {
            activateButton = create("button", "button button-primary", "Подключить");
            activateButton.type = "button";
			activateButton.disabled = Boolean(unsupported);
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
        if (!state.detailsPending[server.id]) {
            state.detailsPending[server.id] = request(
                "/api/servers/details.cgi?id=" + encodeURIComponent(server.id),
                { method: "GET" }
            );
        }
        var pending = state.detailsPending[server.id];
        pending.then(function (details) {
            if (state.detailsPending[server.id] === pending) {
                delete state.detailsPending[server.id];
            }
            state.details[server.id] = details;
            if (!document.documentElement.contains(card)) {
                return;
            }
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
            if (state.detailsPending[server.id] === pending) {
                delete state.detailsPending[server.id];
            }
            if (!document.documentElement.contains(card)) {
                return;
            }
            setBusy(button, false);
            handleError(error);
        });
    }

    function appendImportWarnings(root, warnings) {
        if (!Array.isArray(warnings) || !warnings.length) return;
        var disclosure = create("details", "server-import-warnings");
        disclosure.appendChild(create("summary", "", "Предупреждения конфигурации"));
        var list = create("ul");
        warnings.forEach(function (warning) { if (typeof warning === "string") list.appendChild(create("li", "", warning)); });
        disclosure.appendChild(list);
        root.appendChild(disclosure);
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
        appendImportWarnings(root, details.importWarnings);

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
        appendDefinition(grid, "Источник проверки", qualitySourceLabel(details.quality));
        appendDefinition(grid, "Метод проверки", qualityMethodLabel(details.quality));
        appendCloudEstablishment(grid, details.quality);
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
        if (state.queuedChecks[serverId]) {
            BROrayUI.followQueued("/api/servers/check.cgi",serverId,state.queuedChecks[serverId]).catch(function () {
                setBusy(button,false);
            });
            return;
        }
        var listener=function (result) {
            var pending=["queued","running"].includes(result.state);
            var buttons=new Set([button]);
            document.querySelectorAll("[data-check-server]").forEach(function (item) {
                if (item.dataset.checkServer === serverId) buttons.add(item);
            });
            var active=element("check-active-server");
            if (active && active.getAttribute("data-server-id") === serverId) buttons.add(active);
            buttons.forEach(function (item) {
                setBusy(item,false);item.textContent=pending ? BROrayUI.queueLabel(result) : "Проверить";item.disabled=pending;
            });
            if (!pending) {
                delete state.queuedChecks[serverId];
                BROrayUI.toast(result.state === "completed" ? "Проверка сервера завершена." :
                    result.message || BROrayUI.queueLabel(result),result.state === "completed" ? "success" : "warning");
                delete state.details[serverId];loadSummary(false).catch(function () {});
            }
        };
        state.queuedChecks[serverId]=listener;
        BROrayUI.followQueued("/api/servers/check.cgi",serverId,listener).catch(function (error) {
            delete state.queuedChecks[serverId];
            setBusy(button, false);
            BROrayUI.toast(error.message || "Результат запроса не подтверждён.","error");
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
            BROrayUI.followQueued("/api/servers/activate.cgi", server.id, function (result) {
                if (["queued", "running"].includes(result.state)) {
                    setBusy(button, true, BROrayUI.queueLabel(result));
                    return;
                }
                setBusy(button, false);
                state.details = {};
                BROrayUI.toast(result.state === "completed" ? "Сервер активирован." :
                    result.message || "Результат подключения не подтверждён.",
                    result.state === "completed" ? "success" : "warning");
                loadSummary(false).catch(handleError);
            }).catch(function (error) {
                setBusy(button, false);
                handleError(error);
                // An uncertain response does not prove that activation failed.
                // Observe real state; never replay the mutation blindly.
                loadSummary(false).catch(handleError);
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
            "Удалить «" + text(server.name, server.id) + "»? Отменить это действие нельзя." +
                // DEV5_SUBSCRIPTION_NODE_DELETE_CONFIRMATION_AMBIGUOUS
                (server.sourceType === "subscription"
                    ? " Сервер получен из подписки: если он останется в ней, при следующем обновлении подписки он будет добавлен снова. Чтобы убрать узлы подписки насовсем, выключите или удалите подписку."
                    : ""),
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

    function initialize() {
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
        }).catch(function (error) {
            if (error && error.status === 401) {
                handleError(error);
                return;
            }
            loader.hidden = false;
            app.hidden = true;
            loader.textContent = (error && error.message ? error.message : "Не удалось связаться с роутером.") + " ";
            var retry = document.createElement("button");
            retry.type = "button";
            retry.className = "button button-secondary";
            retry.textContent = "Повторить";
            retry.addEventListener("click", function () {
                retry.disabled = true;
                initialize();
            });
            loader.appendChild(retry);
        });
    }

    bindEvents();
    initialize();
})();
