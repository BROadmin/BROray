(function () {
    "use strict";

    var REQUEST_TIMEOUT_MS = 180000;
    var states = Object.create(null);
    var operationRunning = false;
    var expandedBundles = Object.create(null);

    var ROUTE_LOGOS = {
        telegram: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#229ED9"/><path fill="#fff" d="M15.2 30.7 47 18.4c1.5-.6 2.8.4 2.3 2.7l-5.4 25.3c-.4 1.8-1.5 2.3-3 1.4l-8.2-6.1-4 3.9c-.4.4-.8.8-1.7.8l.6-8.4 15.3-13.8c.7-.6-.1-1-.9-.4L23.1 35.7 15 33.2c-1.8-.6-1.8-1.8.2-2.5Z"/></svg>',
        whatsapp: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#25D366"/><path fill="#fff" d="M18.4 47.3 20.5 40A21 21 0 1 1 28 47.5l-9.6-.2Zm10-3.6a16.8 16.8 0 1 0-4.2-4.1L23 43.9l5.4-1.4Z"/><path fill="#fff" d="M27 23.3c-.4-1-1-1-1.5-1h-1.3c-.5 0-1.2.2-1.8 1-.6.7-2.3 2.3-2.3 5.5s2.4 6.4 2.7 6.8c.3.4 4.6 7.1 11.4 9.7 5.7 2.2 6.8 1.8 8 1.7 1.2-.1 4-1.7 4.6-3.3.6-1.6.6-3 .4-3.3-.2-.3-.7-.5-1.5-.9l-4.8-2.3c-.7-.3-1.2-.5-1.7.3l-2.2 2.7c-.4.5-.9.6-1.6.2-4.5-2.2-7.4-4-10.4-9-.4-.7 0-1 .3-1.4l1.6-1.9c.3-.4.4-.8.6-1.3.1-.4.1-.9-.1-1.3L27 23.3Z"/></svg>',
        youtube: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect x="3" y="13" width="58" height="38" rx="12" fill="#FF0000"/><path fill="#fff" d="m27 23 16 9-16 9V23Z"/></svg>',
        chatgpt: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#10A37F"/><g fill="none" stroke="#fff" stroke-width="4.2" stroke-linecap="round" stroke-linejoin="round"><path d="M32 15a11 11 0 0 1 10.5 7.8A11 11 0 0 1 47 42a11 11 0 0 1-16.5 7.5A11 11 0 0 1 14.8 35 11 11 0 0 1 21.5 18.7 11 11 0 0 1 32 15Z"/><path d="m23 24 9-5 9 5v10l-9 5-9-5V24Z"/><path d="m23 34 9 5v10M41 34l-9 5M32 19v10l-9 5M41 24l-9 5"/></g></svg>',
        facebook: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#1877F2"/><path fill="#fff" d="M36.7 52V34.6h5.8l.9-6.8h-6.7v-4.3c0-2 .5-3.3 3.4-3.3h3.6v-6.1c-.6-.1-2.8-.3-5.3-.3-5.2 0-8.8 3.2-8.8 9v5h-5.9v6.8h5.9V52h7.1Z"/></svg>',
        instagram: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><defs><radialGradient id="route-instagram-gradient" cx="30%" cy="100%" r="115%"><stop offset="0" stop-color="#FEDA75"/><stop offset=".35" stop-color="#FA7E1E"/><stop offset=".62" stop-color="#D62976"/><stop offset=".82" stop-color="#962FBF"/><stop offset="1" stop-color="#4F5BD5"/></radialGradient></defs><rect x="3" y="3" width="58" height="58" rx="17" fill="url(#route-instagram-gradient)"/><rect x="16" y="16" width="32" height="32" rx="10" fill="none" stroke="#fff" stroke-width="4"/><circle cx="32" cy="32" r="8" fill="none" stroke="#fff" stroke-width="4"/><circle cx="43" cy="21" r="2.8" fill="#fff"/></svg>',
        meta: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><path fill="none" stroke="#0866FF" stroke-width="7" stroke-linecap="round" stroke-linejoin="round" d="M8 40c5-18 10-25 17-25 8 0 14 19 20 26 4 5 8 4 11-1 4-7-1-22-9-25-11-4-19 20-25 27-5 6-10 5-14-2Z"/></svg>',
        tiktok: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect x="3" y="3" width="58" height="58" rx="14" fill="#111"/><path fill="none" stroke="#25F4EE" stroke-width="7" stroke-linecap="round" stroke-linejoin="round" d="M34 16v27a10 10 0 1 1-8-9.8M34 16c2 8 7 12 14 12" transform="translate(-2 1)"/><path fill="none" stroke="#FE2C55" stroke-width="7" stroke-linecap="round" stroke-linejoin="round" d="M34 16v27a10 10 0 1 1-8-9.8M34 16c2 8 7 12 14 12" transform="translate(2 -1)"/><path fill="none" stroke="#fff" stroke-width="5" stroke-linecap="round" stroke-linejoin="round" d="M34 16v27a10 10 0 1 1-8-9.8M34 16c2 8 7 12 14 12"/></svg>',
        speedtest: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#1995E8"/><path fill="none" stroke="#fff" stroke-width="5" stroke-linecap="round" d="M14 41a20 20 0 1 1 36 0"/><path fill="#fff" d="M31 34.5 44.5 22 35 38a5 5 0 1 1-4-3.5Z"/><rect x="20" y="45" width="24" height="4" rx="2" fill="#fff"/></svg>'
    };

    var BUNDLES = [
        {
            id: "telegram",
            logo: ROUTE_LOGOS.telegram,
            name: "Telegram",
            description: "Маршруты для Telegram и его сервисов.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Telegram"
        },
        {
            id: "whatsapp",
            logo: ROUTE_LOGOS.whatsapp,
            name: "WhatsApp",
            description: "Маршруты для WhatsApp.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Whatsapp"
        },
        {
            id: "youtube",
            logo: ROUTE_LOGOS.youtube,
            name: "YouTube",
            description: "Все маршруты из .bat-файлов раздела YouTube.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Youtube"
        },
        {
            id: "chatgpt",
            logo: ROUTE_LOGOS.chatgpt,
            name: "ChatGPT",
            description: "Маршруты для ChatGPT и OpenAI.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/ChatGPT(OpenAI)"
        },
        {
            id: "facebook",
            logo: ROUTE_LOGOS.facebook,
            name: "Facebook",
            description: "Маршруты для Facebook.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Facebook"
        },
        {
            id: "instagram",
            logo: ROUTE_LOGOS.instagram,
            name: "Instagram",
            description: "Маршруты для Instagram.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Instagram"
        },
        {
            id: "meta",
            logo: ROUTE_LOGOS.meta,
            name: "Meta",
            description: "Маршруты для сервисов Meta.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Meta"
        },
        {
            id: "tiktok",
            logo: ROUTE_LOGOS.tiktok,
            name: "TikTok",
            description: "Маршруты для TikTok.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/TikTok"
        },
        {
            id: "speedtest",
            logo: ROUTE_LOGOS.speedtest,
            name: "Speedtest",
            description: "Маршруты для сервисов проверки скорости.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/speedtest"
        }
    ];

    window.BROrayRouteBundles = BUNDLES.slice();

    function byId(id) {
        return document.getElementById(id);
    }

    function create(tag, className, text) {
        var node = document.createElement(tag);

        if (className) {
            node.className = className;
        }
        if (text !== undefined && text !== null) {
            node.textContent = String(text);
        }
        return node;
    }

    function unwrap(payload) {
        var error;

        if (payload && payload.success === false) {
            error = new Error(
                payload.error && payload.error.message
                    ? payload.error.message
                    : "Операция с маршрутами завершилась ошибкой."
            );
            error.code = payload.error ? payload.error.code : null;
            error.details = payload.error ? payload.error.details : null;
            throw error;
        }

        if (payload && Object.prototype.hasOwnProperty.call(payload, "data")) {
            return payload.data;
        }

        return payload;
    }

    function request(url, options) {
        return window.BROrayUI.apiRequest(url, options || {}).then(unwrap);
    }

    function withTimeout(promise, milliseconds) {
        return new Promise(function (resolve, reject) {
            var finished = false;
            var timer = window.setTimeout(function () {
                if (!finished) {
                    finished = true;
                    reject(new Error("Операция не завершилась за отведённое время."));
                }
            }, milliseconds);

            promise.then(
                function (value) {
                    if (!finished) {
                        finished = true;
                        window.clearTimeout(timer);
                        resolve(value);
                    }
                },
                function (error) {
                    if (!finished) {
                        finished = true;
                        window.clearTimeout(timer);
                        reject(error);
                    }
                }
            );
        });
    }

    function sameVersion(left, right) {
        if (!left || !right) {
            return false;
        }
        if (left.contentSha256 && right.contentSha256) {
            return left.contentSha256 === right.contentSha256;
        }
        return String(left.sourceCommit || "") === String(right.sourceCommit || "") &&
            String(left.sourceDate || "") === String(right.sourceDate || "");
    }

    function formatDate(value) {
        var date;

        if (!value) {
            return "Не выполнялась";
        }
        date = new Date(value);
        return isNaN(date.getTime()) ? String(value) : date.toLocaleString("ru-RU");
    }

    function formatVersion(value, emptyValue) {
        var parts = [];

        if (!value) {
            return emptyValue;
        }
        if (typeof value === "string") {
            return value;
        }
        if (value.sourceDate) {
            parts.push(formatDate(value.sourceDate));
        }
        if (value.sourceCommit) {
            parts.push(String(value.sourceCommit).slice(0, 7));
        }
        return parts.length ? parts.join(" · ") : emptyValue;
    }

    function formatBytes(value) {
        var size = Number(value);

        if (!isFinite(size) || size <= 0) {
            return "—";
        }
        if (size < 1024) {
            return size + " Б";
        }
        if (size < 1024 * 1024) {
            return Math.round(size / 1024) + " КБ";
        }
        return (size / (1024 * 1024)).toFixed(1).replace(".", ",") + " МБ";
    }

    function sourceFiles(state) {
        if (
            state &&
            state.availableVersion &&
            Array.isArray(state.availableVersion.sourceFiles)
        ) {
            return state.availableVersion.sourceFiles;
        }
        if (
            state &&
            state.checkResult &&
            Array.isArray(state.checkResult.sourceFiles)
        ) {
            return state.checkResult.sourceFiles;
        }
        return [];
    }

    function routerPresence(state) {
        return state && state.routerPresence ? state.routerPresence : null;
    }

    function hasRouterDrift(state) {
        var presence = routerPresence(state);

        return Boolean(
            state &&
            state.installedVersion &&
            presence &&
            presence.available === true &&
            presence.registered === true &&
            presence.drift === true
        );
    }

    function isActuallyInstalled(state) {
        var presence = routerPresence(state);

        if (presence && presence.available === true && presence.registered === true) {
            return presence.actualInstalled === true;
        }
        return Boolean(state && state.installedVersion);
    }

    function statusPresentation(state) {
        if (state && state.lastError) {
            return {text: "Ошибка", className: "status-badge-danger", icon: "status"};
        }
        if (hasRouterDrift(state)) {
            return {text: "Требуется восстановление", className: "status-badge-warning", icon: "restore"};
        }
        if (
            state &&
            state.availableVersion &&
            state.installedVersion &&
            !sameVersion(state.availableVersion, state.installedVersion)
        ) {
            return {text: "Доступно обновление", className: "status-badge-warning", icon: "update"};
        }
        if (isActuallyInstalled(state)) {
            return {text: "Установлено", className: "status-badge-success", icon: "status"};
        }
        if (state && state.downloadedVersion) {
            return {text: "Готово к установке", className: "status-badge-neutral", icon: "routes"};
        }
        if (state && state.availableVersion) {
            return {text: "Доступно", className: "status-badge-neutral", icon: "backup"};
        }
        return {text: "Источник не проверен", className: "status-badge-neutral", icon: "search"};
    }

    function primaryPresentation(state) {
        if (hasRouterDrift(state) && state.downloadedVersion) {
            return {action: "export", text: "Восстановить", icon: "restore", disabled: false};
        }
        if (
            state &&
            state.availableVersion &&
            !sameVersion(state.availableVersion, state.downloadedVersion)
        ) {
            return {action: "download", text: "Скачать", icon: "backup", disabled: false};
        }
        if (
            state &&
            state.downloadedVersion &&
            !sameVersion(state.downloadedVersion, state.installedVersion)
        ) {
            return {
                action: "export",
                text: state.installedVersion ? "Обновить" : "Установить",
                icon: state.installedVersion ? "update" : "routes",
                disabled: false
            };
        }
        if (isActuallyInstalled(state)) {
            return {action: "export", text: "Установлено", icon: "status", disabled: true, hidden: true};
        }
        return {action: "export", text: "Сначала проверить", icon: "search", disabled: true};
    }

    function messageFor(state) {
        var presence;
        var missing;
        var expected;
        var lastError;

        if (state && state.lastError) {
            lastError = typeof state.lastError === "string"
                ? state.lastError
                : state.lastError.message;
            return lastError || "Последняя операция завершилась ошибкой.";
        }

        if (hasRouterDrift(state)) {
            presence = routerPresence(state) || {};
            missing = Number(presence.missingRouteCount || 0);
            expected = Number(presence.expectedRouteCount || 0);
            return "В Keenetic отсутствуют " + missing + " из " + expected +
                " маршрутов. Выполните восстановление.";
        }

        if (
            state &&
            state.availableVersion &&
            state.installedVersion &&
            !sameVersion(state.availableVersion, state.installedVersion)
        ) {
            return "Для набора доступно обновление источника.";
        }

        if (
            state &&
            state.downloadedVersion &&
            !sameVersion(state.downloadedVersion, state.installedVersion)
        ) {
            return "Маршруты готовы к установке в Keenetic.";
        }

        if (isActuallyInstalled(state)) {
            return "Все зарегистрированные маршруты присутствуют в Keenetic.";
        }

        if (state && state.availableVersion) {
            return "Источник проверен. Скачайте набор для установки.";
        }

        return "Выполните проверку источника, чтобы получить список всех .bat-файлов.";
    }

    function button(bundleId, action, text, variant, icon) {
        var node = create("button", "button " + variant, text);

        node.type = "button";
        node.setAttribute("data-bundle-id", bundleId);
        node.setAttribute("data-action", action);
        node.setAttribute("data-icon", icon);
        node.addEventListener("click", onAction);
        return node;
    }

    function dataRow(label, fieldName) {
        var row = create("div", "ui-data-row");
        var labelNode = create("span", "ui-data-row__label", label);
        var valueNode = create("strong", "ui-data-row__value", "—");

        valueNode.setAttribute("data-field", fieldName);
        row.append(labelNode, valueNode);
        return row;
    }

    function createCard(bundle) {
        var card = create("article", "route-bundle-card ui-card");
        var summary = create("div", "route-card-summary");
        var service = create("div", "route-card-service");
        var logoBox = create("span", "route-card-logo");
        var logo;
        var copy = create("div", "route-card-copy");
        var title = create("h2", "route-card-title", bundle.name);
        var status = create("span", "status-badge status-loading", "Загрузка…");
        var metrics = create("div", "route-card-metrics");
        var notice = create("div", "route-card-notice status-neutral");
        var actions = create("div", "route-card-actions");
        var details = create("div", "route-card-details");
        var detailsGrid = create("div", "route-details-grid");
        var sourceBlock = create("section", "route-source-block");
        var sourceHeading = create("div", "route-source-heading");
        var sourceList = create("ul", "route-source-list");
        var technical = create("details", "route-technical-details");
        var technicalSummary = create("summary", "route-technical-summary", "Последняя операция");
        var operationOutput = create("pre", "route-operation-output", "Нет данных.");
        var danger = create("div", "route-danger-zone");
        var detailsButton;

        card.setAttribute("data-route-card", "");
        card.setAttribute("data-bundle-id", bundle.id);
        title.id = "route-title-" + bundle.id;
        card.setAttribute("aria-labelledby", title.id);

        logoBox.setAttribute("aria-hidden", "true");
        logoBox.innerHTML = bundle.logo;
        logo = logoBox.firstElementChild;
        if (logo) {
            logo.classList.add("route-card-logo-image");
            logo.setAttribute("focusable", "false");
        } else {
            logoBox.setAttribute("data-icon", "routes");
        }

        copy.append(
            create("span", "eyebrow route-card-eyebrow", "Набор маршрутов"),
            title,
            create("p", "route-card-description", bundle.description)
        );
        service.append(logoBox, copy);

        status.setAttribute("data-field", "status");
        status.setAttribute("data-icon", "status");
        summary.append(service, status);

        [
            ["route-count", "маршрутов"],
            ["source-count", "исходных файлов"],
            ["presence-count", "в Keenetic"]
        ].forEach(function (item) {
            var metric = create("div", "route-card-metric");
            var value = create("strong", "", "—");
            value.setAttribute("data-field", item[0]);
            metric.append(value, create("span", "", item[1]));
            metrics.appendChild(metric);
        });

        notice.setAttribute("data-field", "notice-box");
        notice.setAttribute("role", "status");
        notice.append(
            create("span", "route-card-notice-icon", ""),
            create("p", "", "Загрузка состояния…")
        );
        notice.firstChild.setAttribute("data-icon", "status");
        notice.lastChild.setAttribute("data-field", "message");

        detailsButton = button(bundle.id, "toggle-details", "Подробнее", "button-secondary", "chevron");
        detailsButton.setAttribute("aria-expanded", "false");
        detailsButton.setAttribute("aria-controls", "route-details-" + bundle.id);

        actions.append(
            button(bundle.id, "check", "Проверить", "button-secondary", "search"),
            button(bundle.id, "primary", "Сначала проверить", "button-primary", "search"),
            detailsButton
        );

        details.id = "route-details-" + bundle.id;
        details.hidden = true;
        detailsGrid.append(
            dataRow("Установленная версия", "installed-version"),
            dataRow("Доступная версия", "available-version"),
            dataRow("Последняя проверка", "last-checked"),
            dataRow("Интерфейс", "target-interface"),
            dataRow("Метрика", "managed-metric")
        );

        sourceHeading.append(
            create("div", "", "Файлы источника"),
            create("a", "route-source-root", "Открыть раздел GitHub")
        );
        sourceHeading.firstChild.className = "route-source-title";
        sourceHeading.lastChild.href = bundle.source;
        sourceHeading.lastChild.target = "_blank";
        sourceHeading.lastChild.rel = "noopener noreferrer";
        sourceList.setAttribute("data-field", "source-files");
        sourceBlock.append(sourceHeading, sourceList);

        technical.append(technicalSummary, operationOutput);
        operationOutput.setAttribute("data-field", "operation-output");

        danger.append(
            create("div", "route-danger-copy"),
            button(bundle.id, "delete", "Удалить из Keenetic", "button-danger-outline", "delete")
        );
        danger.firstChild.append(
            create("strong", "", "Удаление набора"),
            create("p", "", "Общие маршруты, принадлежащие другим установленным наборам, будут сохранены.")
        );

        details.append(detailsGrid, sourceBlock, technical, danger);
        card.append(summary, metrics, notice, actions, details);
        return card;
    }

    function cardFor(bundleId) {
        return document.querySelector('[data-route-card][data-bundle-id="' + bundleId + '"]');
    }

    function setField(card, name, value) {
        var node = card.querySelector('[data-field="' + name + '"]');

        if (node) {
            node.textContent = value === null || value === undefined || value === ""
                ? "—"
                : String(value);
        }
    }

    function setButtonLabel(node, label, icon) {
        if (!node) {
            return;
        }
        node.textContent = label;
        if (icon) {
            node.setAttribute("data-icon", icon);
        }
        if (window.BROrayIcons) {
            window.BROrayIcons.scan(node);
        }
    }

    function setStatus(card, presentation) {
        var node = card.querySelector('[data-field="status"]');

        if (!node) {
            return;
        }
        node.className = "status-badge " + presentation.className;
        node.textContent = presentation.text;
        node.setAttribute("data-icon", presentation.icon);
        if (window.BROrayIcons) {
            window.BROrayIcons.scan(node);
        }
    }

    function setNotice(card, state) {
        var box = card.querySelector('[data-field="notice-box"]');
        var status = statusPresentation(state);

        if (!box) {
            return;
        }
        box.className = "route-card-notice ";
        if (status.className === "status-badge-danger") {
            box.className += "status-error";
        } else if (status.className === "status-badge-warning") {
            box.className += "status-warning";
        } else if (status.className === "status-badge-success") {
            box.className += "status-success";
        } else {
            box.className += "status-neutral";
        }
        setField(card, "message", messageFor(state));
    }

    function renderSourceFiles(card, state) {
        var list = card.querySelector('[data-field="source-files"]');
        var files = sourceFiles(state);

        if (!list) {
            return;
        }
        list.textContent = "";

        if (!files.length) {
            list.appendChild(create("li", "route-source-empty", "Список ещё не получен. Выполните проверку источника."));
            return;
        }

        files.forEach(function (file) {
            var item = create("li", "route-source-item");
            var link = create("a", "route-source-link", file.name || "Файл маршрутов");
            var meta = create("span", "route-source-meta");
            var hash = file.sha256 ? String(file.sha256).slice(0, 12) : "—";

            if (file.htmlUrl) {
                link.href = file.htmlUrl;
                link.target = "_blank";
                link.rel = "noopener noreferrer";
            } else {
                link.href = "#";
                link.addEventListener("click", function (event) {
                    event.preventDefault();
                });
            }

            meta.textContent = String(file.routeCount || 0) + " маршрутов · " +
                formatBytes(file.sizeBytes) + " · SHA " + hash;
            item.append(link, meta);
            list.appendChild(item);
        });
    }

    function operationOutput(state) {
        var output;

        if (state && state.operation && state.operation.output) {
            return state.operation.output;
        }
        if (state && state.lastError) {
            output = typeof state.lastError === "string"
                ? state.lastError
                : state.lastError.message;
            return output || "Последняя операция завершилась ошибкой.";
        }
        if (state && state.exportResult) {
            return "Последний экспорт: " + (state.exportResult.message || "завершён") + ".";
        }
        if (state && state.deleteResult) {
            return "Последнее удаление: " + (state.deleteResult.message || "завершено") + ".";
        }
        return "Нет данных о выполненных операциях.";
    }

    function renderCard(bundleId, state) {
        var card = cardFor(bundleId);
        var presence = routerPresence(state);
        var files = sourceFiles(state);
        var primary = primaryPresentation(state);
        var primaryButton;
        var checkButton;
        var deleteButton;
        var detailsButton;
        var expected;
        var present;
        var targetInterface;
        var metric;

        if (!card) {
            return;
        }

        states[bundleId] = state;
        expected = presence && presence.registered ? Number(presence.expectedRouteCount || 0) : 0;
        present = presence && presence.registered ? Number(presence.presentRouteCount || 0) : 0;
        targetInterface =
            (state.exportBuild && state.exportBuild.targetInterface) ||
            (state.checkResult && state.checkResult.managedInterface) ||
            (state.exportResult && state.exportResult.targetInterface) ||
            "ProxyN";
        metric =
            (state.exportBuild && state.exportBuild.managedMetric) ||
            (state.exportResult && state.exportResult.managedMetric) ||
            (state.deleteResult && state.deleteResult.managedMetric) ||
            "1200";

        setStatus(card, statusPresentation(state));
        setNotice(card, state);
        setField(card, "route-count", state.routeCount || 0);
        setField(card, "source-count", files.length);
        setField(card, "presence-count", presence && presence.registered ? present + "/" + expected : "—");
        setField(card, "installed-version", formatVersion(state.installedVersion, "Не установлено"));
        setField(card, "available-version", formatVersion(state.availableVersion, "Не проверялась"));
        setField(card, "last-checked", formatDate(state.lastCheckedAt));
        setField(card, "target-interface", targetInterface);
        setField(card, "managed-metric", metric);
        setField(card, "operation-output", operationOutput(state));
        renderSourceFiles(card, state);

        primaryButton = card.querySelector('[data-action="primary"], [data-action="download"], [data-action="export"]');
        checkButton = card.querySelector('[data-action="check"]');
        deleteButton = card.querySelector('[data-action="delete"]');
        detailsButton = card.querySelector('[data-action="toggle-details"]');

        if (primaryButton) {
            primaryButton.setAttribute("data-action", primary.action);
            setButtonLabel(primaryButton, primary.text, primary.icon);
            primaryButton.hidden = primary.hidden === true;
            primaryButton.disabled = operationRunning || primary.disabled;
            primaryButton.parentElement.classList.toggle(
                "route-card-actions-primary-hidden",
                primary.hidden === true
            );
        }
        if (checkButton) {
            setButtonLabel(checkButton, "Проверить", "search");
            checkButton.disabled = operationRunning;
        }
        if (deleteButton) {
            setButtonLabel(deleteButton, "Удалить из Keenetic", "delete");
            deleteButton.disabled = operationRunning || !state.installedVersion;
        }
        if (detailsButton) {
            detailsButton.disabled = operationRunning;
        }

        card.classList.toggle("has-warning", hasRouterDrift(state));
        card.classList.toggle("has-error", Boolean(state.lastError));
    }

    function renderLoadError(bundleId, error) {
        var card = cardFor(bundleId);

        if (!card) {
            return;
        }
        setStatus(card, {text: "Ошибка", className: "status-badge-danger", icon: "status"});
        setField(card, "message", error && error.message ? error.message : "Не удалось загрузить состояние набора.");
        card.classList.add("has-error");
        Array.prototype.forEach.call(card.querySelectorAll("button"), function (node) {
            if (node.getAttribute("data-action") !== "toggle-details") {
                node.disabled = true;
            }
        });
    }

    function renderSummary() {
        var installed = 0;
        var attention = 0;
        var loaded = 0;
        var status = byId("routes-page-status");
        var message = byId("routes-summary-message");

        BUNDLES.forEach(function (bundle) {
            var state = states[bundle.id];
            if (!state) {
                return;
            }
            loaded += 1;
            if (isActuallyInstalled(state)) {
                installed += 1;
            }
            if (
                state.lastError ||
                hasRouterDrift(state) ||
                (state.availableVersion && state.installedVersion && !sameVersion(state.availableVersion, state.installedVersion))
            ) {
                attention += 1;
            }
        });

        byId("routes-installed-count").textContent = String(installed);
        byId("routes-attention-count").textContent = String(attention);
        byId("routes-total-count").textContent = String(BUNDLES.length);

        if (message) {
            message.textContent = loaded < BUNDLES.length
                ? "Загружено " + loaded + " из " + BUNDLES.length + " наборов."
                : attention > 0
                    ? "Некоторые наборы требуют действия. Подробная причина показана в карточке."
                    : "Состояние всех наборов получено. Ошибок и обязательных действий нет.";
        }

        if (status) {
            if (loaded < BUNDLES.length) {
                status.className = "status-badge status-loading";
                status.textContent = "Загрузка…";
                status.setAttribute("data-icon", "status");
            } else if (attention > 0) {
                status.className = "status-badge status-badge-warning";
                status.textContent = "Требуется действие";
                status.setAttribute("data-icon", "status");
            } else {
                status.className = "status-badge status-badge-success";
                status.textContent = "Состояние получено";
                status.setAttribute("data-icon", "status");
            }
            if (window.BROrayIcons) {
                window.BROrayIcons.scan(status);
            }
        }
    }

    function renderAll() {
        BUNDLES.forEach(function (bundle) {
            if (states[bundle.id]) {
                renderCard(bundle.id, states[bundle.id]);
            }
        });
        renderSummary();
    }

    function loadState(bundleId) {
        return request(
            "/api/routes/status.cgi?bundleId=" + encodeURIComponent(bundleId),
            {method: "GET", credentials: "same-origin"}
        ).then(function (state) {
            renderCard(bundleId, state);
            renderSummary();
            return state;
        });
    }

    function loadAllStates() {
        return Promise.all(BUNDLES.map(function (bundle) {
            return loadState(bundle.id).catch(function (error) {
                if (error && error.status === 401) {
                    window.BROrayUI.redirectToLogin();
                    return null;
                }
                renderLoadError(bundle.id, error);
                renderSummary();
                return null;
            });
        }));
    }

    function operationText(action, bundleId) {
        if (action === "check") {
            return "Проверка…";
        }
        if (action === "download") {
            return "Загрузка…";
        }
        if (action === "export") {
            return hasRouterDrift(states[bundleId])
                ? "Восстановление…"
                : states[bundleId] && states[bundleId].installedVersion
                    ? "Обновление…"
                    : "Установка…";
        }
        if (action === "delete") {
            return "Удаление…";
        }
        return "Выполнение…";
    }

    function successMessage(action, bundle) {
        if (action === "check") {
            return "Проверка источника «" + bundle.name + "» завершена.";
        }
        if (action === "download") {
            return "Маршруты «" + bundle.name + "» загружены.";
        }
        if (action === "export") {
            return "Маршруты «" + bundle.name + "» установлены или восстановлены.";
        }
        if (action === "delete") {
            return "Маршруты «" + bundle.name + "» удалены.";
        }
        return "Операция завершена.";
    }

    function toggleDetails(bundleId, buttonNode) {
        var card = cardFor(bundleId);
        var details = card ? card.querySelector(".route-card-details") : null;
        var expanded;

        if (!details || !buttonNode) {
            return;
        }
        expanded = buttonNode.getAttribute("aria-expanded") === "true";
        expandedBundles[bundleId] = !expanded;
        details.hidden = expanded;
        buttonNode.setAttribute("aria-expanded", expanded ? "false" : "true");
        setButtonLabel(buttonNode, expanded ? "Подробнее" : "Скрыть", "chevron");
        card.classList.toggle("is-expanded", !expanded);
    }

    function confirmDelete(bundle) {
        if (!window.BROrayDialogs || typeof window.BROrayDialogs.confirm !== "function") {
            return Promise.reject(new Error("Фирменное окно подтверждения недоступно."));
        }
        return window.BROrayDialogs.confirm({
            eyebrow: "Опасное действие",
            title: "Удаление маршрутов",
            message: "Удалить набор «" + bundle.name + "» из Keenetic? Общие маршруты других установленных наборов будут сохранены.",
            confirmText: "Удалить",
            cancelText: "Отмена",
            variant: "danger"
        });
    }

    function executeOperation(bundle, action, buttonNode) {
        operationRunning = true;
        renderAll();
        setButtonLabel(buttonNode, operationText(action, bundle.id), buttonNode.getAttribute("data-icon"));
        buttonNode.setAttribute("aria-busy", "true");

        return withTimeout(
            request(
                "/api/routes/" + action + ".cgi?bundleId=" + encodeURIComponent(bundle.id),
                {
                    method: "POST",
                    credentials: "same-origin",
                    headers: {"Accept": "application/json"}
                }
            ),
            REQUEST_TIMEOUT_MS
        ).then(function (state) {
            states[bundle.id] = state;
            window.BROrayUI.toast(successMessage(action, bundle), "success");
        }).catch(function (error) {
            if (
                error &&
                (error.status === 401 || error.code === "AUTH_REQUIRED" || error.code === "SESSION_REQUIRED")
            ) {
                window.BROrayUI.redirectToLogin();
                return;
            }
            window.BROrayUI.toast(
                error && error.message ? error.message : "Операция с маршрутами завершилась ошибкой.",
                "error"
            );
        }).then(function () {
            operationRunning = false;
            buttonNode.removeAttribute("aria-busy");
            return loadAllStates();
        });
    }

    function onAction(event) {
        var buttonNode = event.currentTarget;
        var bundleId = buttonNode.getAttribute("data-bundle-id");
        var action = buttonNode.getAttribute("data-action");
        var bundle = BUNDLES.filter(function (item) {
            return item.id === bundleId;
        })[0];

        if (!bundle) {
            return;
        }
        if (action === "toggle-details") {
            toggleDetails(bundleId, buttonNode);
            return;
        }
        if (operationRunning) {
            return;
        }
        if (action === "delete") {
            confirmDelete(bundle).then(function (confirmed) {
                if (confirmed) {
                    executeOperation(bundle, action, buttonNode);
                }
            }).catch(function (error) {
                window.BROrayUI.toast(error.message, "error");
            });
            return;
        }
        executeOperation(bundle, action, buttonNode);
    }

    function revealApplication(session) {
        var app = byId("app");
        var loader = byId("page-loader");
        var user = byId("current-user");

        if (user) {
            user.textContent = session && session.user ? session.user : "admin";
        }
        if (loader) {
            loader.hidden = true;
        }
        if (app) {
            app.hidden = false;
        }
    }

    function initialize() {
        var mount = byId("routes-bundles");
        var fragment = document.createDocumentFragment();

        BUNDLES.forEach(function (bundle) {
            fragment.appendChild(createCard(bundle));
        });
        mount.replaceChildren(fragment);

        request("/api/session.cgi", {method: "GET", credentials: "same-origin"})
            .then(function (session) {
                revealApplication(session);
                return loadAllStates();
            })
            .catch(function (error) {
                if (error && error.status === 401) {
                    window.BROrayUI.redirectToLogin();
                    return;
                }
                revealApplication(null);
                window.BROrayUI.toast(
                    error && error.message ? error.message : "Не удалось открыть страницу маршрутов.",
                    "error"
                );
            });
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize, {once: true});
    } else {
        initialize();
    }
})();
