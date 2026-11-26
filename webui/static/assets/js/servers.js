(function () {
    "use strict";

    var state = {
        summary: null,
        details: {},
        detailsPending: {},
        queuedChecks: {},
        checkButtons: null,
        serverCards: new Map(),
        groupSelection: new Map(),
        groupActive: new Map(),
        deletingOthers: false,
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
        return "Не проверен";
    }

    function qualityClass(quality) {
        var status = quality && quality.status ? quality.status : "unknown";
        var freshness = quality && quality.freshness ? quality.freshness : "unknown";
        if (status === "available" && freshness === "fresh") return "status-badge-success";
        if (status === "unavailable") return "status-badge-danger";
        if (status === "available") return "status-badge-warning";
        return "status-unknown";
    }

    function connectionLabel(value, connection) {
        if (value === "connected") return "Подключено";
        if (value === "degraded") return connection && connection.available === false
            ? "Ожидание проверки" : "Подключение не подтверждено";
        if (value === "error") return "Ошибка";
        return "Нет подключения";
    }

    function connectionTone(value) {
        return ({
            connected: "status-success",
            degraded: "status-warning",
            error: "status-error",
            disabled: "status-neutral"
        })[value] || "status-unknown";
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
        if (window.BROrayReserve) window.BROrayReserve.updateSummary(state.summary, sourceLabel);

        var active = state.summary.activeServer;
        var activeCheck = element("check-active-server");
        if (activeCheck.getAttribute("data-server-id") !== (active ? active.id : null)) {
            setBusy(activeCheck, false);
            setText(activeCheck, "Проверить");
        }
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

        live.className = "servers-live-status " + (running && socksActive ? "status-success" :
            severity === "busy" ? "status-loading" : running ? "status-warning" :
            severity === "error" ? "status-error" : severity === "unknown" ? "status-unknown" : "status-neutral");

        element("servers-live-text").textContent = running
            ? socksActive ? "Xray и SOCKS работают" : "Xray запущен, SOCKS не подтверждён"
            : "Xray остановлен";

        var badge = element("connection-state-badge");

        badge.textContent = connectionLabel(connectionState, state.summary.activeConnection);
        badge.className = "status-badge " + (severity === "busy" ? "status-loading" : connectionTone(connectionState));

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

            if (!state.queuedChecks[active.id]) activeCheck.disabled = false;

            activeCheck.setAttribute(
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

            activeCheck.disabled = true;

            activeCheck.removeAttribute(
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
        var source = server.sourceType === "subscription"
            ? text(server.subscriptionName, server.subscriptionId || server.id)
            : "Ручной импорт";
        var formats = new Map([["json", "JSON"], ["clash", "Clash"], ["uri", "URI"]]);
        var labels = (Array.isArray(server.sourceFormats) ? server.sourceFormats : [])
            .map(function (format) { return formats.get(format); }).filter(Boolean);
        return labels.length ? source + " · " + labels.join(" / ") : source;
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

    function sortServerGroups(groups, mode) {
        if (mode === "name") {
            groups.sort(function (a, b) {
                return text(a.server.name, a.server.id).localeCompare(text(b.server.name, b.server.id), "ru", {
                    numeric: true, sensitivity: "base"
                });
            });
        } else if (mode === "availability") {
            var ranks = new Map([["available", 0], ["unknown", 1], ["unavailable", 2], ["unsupported", 3]]);
            var rank = function (group) {
                var quality = group.server.quality;
                var value = ranks.get(quality && quality.status);
                return value === undefined ? 1 : value;
            };
            groups.sort(function (a, b) { return rank(a) - rank(b); });
        } else if (mode === "latency") {
            var latency = function (group) {
                var quality = group.server.quality;
                return quality && quality.freshness === "fresh" && Number.isFinite(quality.ping) && quality.ping >= 0
                    ? quality.ping : Infinity;
            };
            groups.sort(function (a, b) {
                var left = latency(a), right = latency(b);
                return left === right ? 0 : left < right ? -1 : 1;
            });
        }
    }

    function renderServers(servers) {
        updateDeleteOthersButton();
        var list = element("servers-list");
        var groups = displayGroups(servers);
        var sortOrder = element("servers-sort-order");
        if (sortOrder) sortServerGroups(groups, sortOrder.value);
        setText(element("servers-total"), groups.length + " / " + servers.length);
        setText(element("servers-list-description"), "Уникальных карточек: " + groups.length + " · сохранённых вариантов: " + servers.length);

        if (!groups.length) {
            state.serverCards.clear();
            state.checkButtons = null;
            if (!list.querySelector(".servers-empty-card")) {
                var empty = create("article", "servers-empty-card");
                empty.append(
                    create("h3", "", "Серверы не добавлены"),
                    create("p", "", "Импортируйте URI-конфигурацию или добавьте подписку.")
                );
                list.replaceChildren(empty);
            }
            return;
        }

        var focused = document.activeElement;
        var focusedCard = focused && list.contains(focused) ? focused.closest(".server-card") : null;
        var focusRole = focusedCard && focused.getAttribute("data-card-control");
        var focusProfile = focusedCard && focusedCard.getAttribute("data-server-id");
        var nextCards = new Map();
        var cursor = list.firstElementChild;
        groups.forEach(function (group) {
            var server = group.server;
            var key = JSON.stringify([
                server.id, server.name, server.address, server.port, server.protocol, server.transport, server.security,
                server.active, server.connectionSupport && [server.connectionSupport.supported, server.connectionSupport.message, server.connectionSupport.notice],
                group.active && [group.active.id, sourceLabel(group.active)],
                group.members.map(function (member) { return [member.id, sourceLabel(member), member.active]; })
            ]);
            var previous = state.serverCards.get(group.id);
            var card = previous && previous.node;
            if (!previous || previous.key !== key) {
                var sameProfile = card && card.getAttribute("data-server-id") === server.id;
                var toggle = sameProfile && card.querySelector(".server-details-toggle");
                var expanded = toggle && (toggle.getAttribute("aria-expanded") === "true" || toggle.getAttribute("aria-busy") === "true");
                card = renderServerCard(server, group, card);
                state.checkButtons = null;
                if (expanded) toggleDetails(server, card, card.querySelector(".server-details-toggle"));
            } else {
                updateServerMetrics(card.querySelector(".server-card-metrics"), server.quality);
                updateServerQuality(card.querySelector(".server-card-quality"), card.querySelector(".server-tag-quality"), server);
            }
            nextCards.set(group.id, {key:key,node:card});
            if (card !== cursor) list.insertBefore(card, cursor);
            cursor = card.nextElementSibling;
            var details = card.querySelector(".server-details-toggle");
            if (details.getAttribute("aria-expanded") === "true" && !state.details[server.id] && !state.detailsPending[server.id]) {
                details.setAttribute("aria-expanded", "false");
                toggleDetails(server, card, details);
            }
        });
        while (cursor) {
            var next = cursor.nextElementSibling;
            list.removeChild(cursor);
            cursor = next;
            state.checkButtons = null;
        }
        state.serverCards = nextCards;
        if (focusedCard && list.contains(focusedCard)) {
            if (!focusedCard.contains(focused)) {
                focused = focused.id ? element(focused.id) :
                    focusRole && focusedCard.getAttribute("data-server-id") === focusProfile ?
                        Array.prototype.find.call(focusedCard.querySelectorAll("[data-card-control]"), function (node) {
                            return node.getAttribute("data-card-control") === focusRole;
                        }) : null;
            }
            if (focused && focusedCard.contains(focused) && focused !== document.activeElement && !focused.disabled) {
                focused.focus({preventScroll:true});
            }
        }
        servers.forEach(function (server) {
            if (!BROrayUI.hasQueuedRequest("/api/servers/check.cgi", server.id)) return;
            var button = server.active ? element("check-active-server") : serverCheckButtons().get(server.id);
            if (button) checkServer(server.id, button);
        });
    }

    function serverCheckButtons() {
        if (!state.checkButtons) {
            state.checkButtons = new Map();
            element("servers-list").querySelectorAll("[data-check-server]").forEach(function (button) {
                state.checkButtons.set(button.dataset.checkServer, button);
            });
        }
        return state.checkButtons;
    }

    function setText(node, value) {
        if (node.textContent !== value) node.textContent = value;
    }

    function updateServerMetrics(root, quality) {
        var ping = quality && quality.ping;
        var jitter = quality && quality.jitter;
        var values = [
            [qualityLatencyLabel(quality), ping != null ? ping + " мс" : "—"],
            ["Джиттер", jitter != null ? jitter + " мс" : "—"],
            ["Последняя проверка", formatDate(quality && quality.lastCheckedAt)],
            ["Источник проверки", qualitySourceLabel(quality)]
        ];
        if (quality && quality.measurementMethod === "https-proxy-warm") {
            values.push(["Установление туннеля", quality.establishmentMs != null ? quality.establishmentMs + " мс" : "Нет данных"]);
        }
        values.forEach(function (pair, i) {
            var item = root.children[i];
            if (!item) appendDefinition(root, pair[0], pair[1]);
            else {
                setText(item.children[0], pair[0]);
                setText(item.children[1], text(pair[1]));
            }
        });
        while (root.children.length > values.length) root.removeChild(root.children[values.length]);
    }

    function updateServerQuality(badge, tag, server) {
        var label = qualityLabel(server.quality), color = qualityClass(server.quality);
        var support = server.connectionSupport;
        if (server.active) {
            setText(tag, label);
            label = connectionLabel(state.summary.connectionState || "disabled", state.summary.activeConnection);
            color = connectionTone(state.summary.connectionState || "disabled");
        } else if (support && support.supported === false) {
            color = "status-badge-warning";
            label = support.mode === "requires-core" ? "Обновите Xray" :
                support.mode === "core-unverified" ? "Проверьте Xray" : "Подключение не поддерживается";
        }
        if (state.queuedChecks[server.id]) {
            label = "Проверяется";
            color = "status-loading";
        }
        if (badge.getAttribute("data-quality-label") !== label) {
            badge.setAttribute("data-quality-label", label);
            badge.textContent = label;
        }
        var className = "status-badge server-card-quality " + color;
        if (badge.className !== className) badge.className = className;
    }

    function renderServerCard(server, group, card) {
        var unsupported = server.connectionSupport && server.connectionSupport.supported === false;
        var dpi = card && card.getAttribute("data-server-id") === server.id && card.querySelector(".server-dpi");
        if (card) card.replaceChildren();
        else card = create("article");
        card.className = "server-card ui-card" + (group.active ? " is-active" : "");
        var header = create("div", "server-card-header");
        var identity = create("div", "server-card-identity");
        var titleRow = create("div", "server-card-title-row");
        var title = create(
            "h3",
            "server-card-title",
            text(server.name, server.id)
        );
        var quality = create("span", "server-card-quality");
        var tags = create("div", "server-card-tags");
        var metrics = create("div", "server-card-metrics");
        var actions = create("div", "server-card-actions");
        var detailsRoot = create("div", "server-details");
        var checkButton;
        var activateButton;
        var toggleButton;
        var qualityTag;

        card.setAttribute("data-server-id", server.id);
        card.setAttribute("data-display-group-id", group.id);

        titleRow.appendChild(title);
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
            qualityTag = create("span", "server-tag server-tag-quality");
            tags.appendChild(qualityTag);
        }
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
                var sameSource = group.members.filter(function (other) { return sourceLabel(other) === name; });
                if (sameSource.length > 1) name += " · " + (sameSource.indexOf(member) + 1);
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
        identity.appendChild(tags);
        header.append(identity, quality);
        card.appendChild(header);

        updateServerMetrics(metrics, server.quality);
        updateServerQuality(quality, qualityTag, server);
        card.appendChild(metrics);
        if (dpi) card.appendChild(dpi);
        else if (window.BROrayDPI) card.appendChild(window.BROrayDPI.create(server));

        checkButton = create("button", "button button-secondary", "Проверить");
        checkButton.type = "button";
		checkButton.disabled = Boolean(unsupported);
        checkButton.dataset.checkServer=server.id;
        checkButton.setAttribute("data-card-control", "check");
        checkButton.setAttribute("data-icon", "speed");
        checkButton.addEventListener("click", function () {
            checkServer(server.id, checkButton);
        });
        actions.appendChild(checkButton);

        if (!server.active) {
            activateButton = create("button", "button button-primary", "Подключить");
            activateButton.type = "button";
            activateButton.setAttribute("data-card-control", "activate");
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
        toggleButton.setAttribute("data-card-control", "details");
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
            if (!document.documentElement.contains(card) || !card.contains(button)) {
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
            if (!document.documentElement.contains(card) || !card.contains(button)) {
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
        function stillTargetsServer() {
            return button.id === "check-active-server" ?
                button.getAttribute("data-server-id") === serverId :
                document.documentElement.contains(button);
        }
        function repaintBadge() {
            var row = serverCheckButtons().get(serverId);
            if (!row) return;
            var card = row.closest(".server-card");
            var server = (state.summary && state.summary.servers || []).find(function (item) { return item.id === serverId; });
            if (card && server) {
                updateServerQuality(card.querySelector(".server-card-quality"), card.querySelector(".server-tag-quality"), server);
            }
        }
        setBusy(button, true, "Проверка…");
        if (state.queuedChecks[serverId]) {
            BROrayUI.followQueued("/api/servers/check.cgi",serverId,state.queuedChecks[serverId]).catch(function () {
                if (stillTargetsServer()) setBusy(button, false);
            });
            return;
        }
        var listener=function (result) {
            var pending=["queued","running"].includes(result.state);
            var buttons=new Set();
            if (stillTargetsServer()) buttons.add(button);
            var rowButton=serverCheckButtons().get(serverId);
            if (rowButton) buttons.add(rowButton);
            var active=element("check-active-server");
            if (active && active.getAttribute("data-server-id") === serverId) buttons.add(active);
            buttons.forEach(function (item) {
                setBusy(item,false);item.textContent=pending ? BROrayUI.queueLabel(result) : "Проверить";item.disabled=pending;
            });
            if (!pending) {
                delete state.queuedChecks[serverId];
                BROrayUI.toast(result.state === "completed" ? "Проверка сервера завершена." :
                    result.message || BROrayUI.queueLabel(result),result.state === "completed" ? "success" : "warning");
                delete state.details[serverId];loadSummary(false).catch(repaintBadge);
            }
        };
        state.queuedChecks[serverId]=listener;
        repaintBadge();
        BROrayUI.followQueued("/api/servers/check.cgi",serverId,listener).catch(function (error) {
            delete state.queuedChecks[serverId];
            if (stillTargetsServer()) setBusy(button, false);
            repaintBadge();
            BROrayUI.toast(error.message || "Результат запроса не подтверждён.","error");
        });
    }

    function activateServer(server, button) {
        if (window.BROrayReserve && window.BROrayReserve.outsideRoster(server.id)) {
            window.BROrayReserve.openSelection(server.id);
            return;
        }
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

    function updateDeleteOthersButton() {
        var button = element("delete-inactive-servers");
        var summary = state.summary || {};
        var active = summary.activeServer;
        var others = (summary.servers || []).filter(function (server) { return !active || server.id !== active.id; });
        if (button) button.disabled = state.deletingOthers || !active || !active.id || !others.length;
    }

    function deleteOtherServers(button) {
        var summary = state.summary || {};
        var active = summary.activeServer;
        if (!active || !active.id || state.deletingOthers) return;
        var others = (summary.servers || []).filter(function (server) { return server.id !== active.id; });
        if (!others.length) return;
        var message = "Будет удалено серверов: " + others.length + ". Активный сервер «" + text(active.name, active.id) + "» сохранится. Отменить удаление нельзя.";
        if (others.some(function (server) { return server.sourceType === "subscription"; })) {
            message += " Серверы подписок могут появиться снова при следующем обновлении подписки.";
        }
        showConfirm("Удалить все, кроме активного?", message, "Удалить", true).then(function (confirmed) {
            if (!confirmed) return;
            state.deletingOthers = true;
            setBusy(button, true, "Удаление…");
            request("/api/servers/delete-inactive.cgi", { method: "POST", headers: { "X-BROray-Request": "operations", "X-BROray-Origin": window.location.origin }, body: { activeId: active.id } }).then(function (result) {
                Object.keys(state.details).forEach(function (id) { if (id !== active.id) delete state.details[id]; });
                BROrayUI.toast("Удалено серверов: " + result.removed + ".", "success");
                return loadSummary(false);
            }).catch(function (error) {
                handleError(error);
                return loadSummary(false).catch(handleError);
            }).then(function () {
                state.deletingOthers = false;
                setBusy(button, false);
                updateDeleteOthersButton();
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
        var deleteOthers = element("delete-inactive-servers");
        if (deleteOthers) deleteOthers.addEventListener("click", function () { deleteOtherServers(this); });
        var refresh = element("refresh-servers");
        var activeCheck = element("check-active-server");
        var deactivate = element("deactivate-servers");
        var toggle = element("toggle-import");
        var cancel = element("cancel-import");
        var submit = element("submit-import");
        var form = element("server-import-form");
        var sortOrder = element("servers-sort-order");

        if (sortOrder) {
            sortOrder.addEventListener("change", function () {
                if (state.summary) renderServers(state.summary.servers || []);
            });
        }
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
