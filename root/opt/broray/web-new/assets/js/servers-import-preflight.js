(function () {
    "use strict";

    var SUMMARY_URL =
        "/api/servers/summary.cgi";

    var IMPORT_URL =
        "/api/servers/import.cgi";

    var IMPORT_KEY =
        "uri";

    var state = {
        busy: false,
        valid: false,
        readyValue: null,
        items: [],
        message: "",
        messageType: "neutral"
    };

    function trim(value) {
        return String(
            value === null || value === undefined
                ? ""
                : value
        ).replace(
            /^[\s\u00a0]+|[\s\u00a0]+$/g,
            ""
        );
    }

    function safeDecode(value) {
        try {
            return decodeURIComponent(value);
        } catch (error) {
            return value;
        }
    }

    function normalizeProtocol(value) {
        value = trim(value).toLowerCase();

        if (value === "hy2") {
            return "hysteria2";
        }

        return value;
    }

    function normalizeHost(value) {
        value = trim(value).toLowerCase();

        if (
            value.charAt(0) === "[" &&
            value.charAt(value.length - 1) === "]"
        ) {
            value = value.substring(
                1,
                value.length - 1
            );
        }

        return value;
    }

    function cleanName(value) {
        value = trim(value)
            .replace(/[\u0000-\u001f\u007f]/g, "")
            .replace(/\s+/g, " ");

        if (value.length > 96) {
            value = value.substring(0, 96);
        }

        return value;
    }

    function parsePort(value) {
        var port = Number(value);

        if (
            !isFinite(port) ||
            Math.floor(port) !== port ||
            port < 1 ||
            port > 65535
        ) {
            throw new Error(
                "Порт должен быть числом от 1 до 65535."
            );
        }

        return port;
    }

    function isUuid(value) {
        return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
            .test(value);
    }

    function canonical(
        protocol,
        host,
        port
    ) {
        return [
            normalizeProtocol(protocol),
            normalizeHost(host),
            String(port)
        ].join("|");
    }

    function decodeBase64(value) {
        var normalized = trim(value)
            .replace(/-/g, "+")
            .replace(/_/g, "/")
            .replace(/\s/g, "");

        var binary;
        var encoded = "";
        var index;
        var hex;

        while (normalized.length % 4) {
            normalized += "=";
        }

        try {
            binary = window.atob(normalized);
        } catch (error) {
            throw new Error(
                "VMess содержит некорректные данные Base64."
            );
        }

        for (index = 0; index < binary.length; index++) {
            hex = binary.charCodeAt(index)
                .toString(16);

            if (hex.length < 2) {
                hex = "0" + hex;
            }

            encoded += "%" + hex;
        }

        try {
            return decodeURIComponent(encoded);
        } catch (error) {
            return binary;
        }
    }

    function parseVmess(raw) {
        var encoded = raw.substring(
            raw.indexOf("://") + 3
        );

        var separator = encoded.search(/[?#]/);

        if (separator !== -1) {
            encoded = encoded.substring(0, separator);
        }

        var decoded = decodeBase64(encoded);
        var data;

        try {
            data = JSON.parse(decoded);
        } catch (error) {
            throw new Error(
                "VMess содержит некорректный JSON."
            );
        }

        var host = normalizeHost(
            data.add || data.host || ""
        );

        if (!host) {
            throw new Error(
                "В VMess не указан адрес сервера."
            );
        }

        var port = parsePort(data.port);

        var userId = trim(data.id);

        if (!userId) {
            throw new Error(
                "В VMess отсутствует идентификатор пользователя."
            );
        }

        if (!isUuid(userId)) {
            throw new Error(
                "VMess содержит некорректный UUID."
            );
        }

        var transport = trim(
            data.net || data.type || "tcp"
        ).toLowerCase();

        var security = trim(
            data.tls ||
            data.security ||
            "none"
        ).toLowerCase();

        if (
            security === "" ||
            security === "0"
        ) {
            security = "none";
        }

        var name = cleanName(
            data.ps ||
            data.name ||
            "VMess " + host + ":" + port
        );

        return {
            raw: raw,
            protocol: "vmess",
            name: name,
            host: host,
            port: port,
            transport: transport,
            security: security,
            canonical:
                canonical("vmess", host, port)
        };
    }

    function parseStandard(raw) {
        var parsed;

        try {
            parsed = new URL(raw);
        } catch (error) {
            throw new Error(
                "Некорректный формат URI."
            );
        }

        var protocol = normalizeProtocol(
            parsed.protocol.replace(/:$/, "")
        );

        if (
            protocol !== "vless" &&
            protocol !== "trojan" &&
            protocol !== "hysteria2"
        ) {
            throw new Error(
                "Неподдерживаемый протокол: " +
                protocol +
                "."
            );
        }

        var host = normalizeHost(parsed.hostname);

        if (!host) {
            throw new Error(
                "Не указан адрес сервера."
            );
        }

        var port = parsePort(parsed.port);

        var username = safeDecode(
            parsed.username || ""
        );

        var password = safeDecode(
            parsed.password || ""
        );

        var credential = username;

        if (password) {
            credential += ":" + password;
        }

        if (protocol === "vless") {
            if (!username) {
                throw new Error(
                    "В VLESS отсутствует UUID."
                );
            }

            if (!isUuid(username)) {
                throw new Error(
                    "VLESS содержит некорректный UUID."
                );
            }
        }

        if (
            protocol === "trojan" &&
            !credential
        ) {
            throw new Error(
                "В Trojan отсутствует пароль."
            );
        }

        if (
            protocol === "hysteria2" &&
            !credential
        ) {
            throw new Error(
                "В Hysteria2 отсутствует пароль."
            );
        }

        var query = parsed.searchParams;

        var transport;

        if (protocol === "hysteria2") {
            transport = "hysteria2";
        } else {
            transport = trim(
                query.get("type") ||
                query.get("network") ||
                "tcp"
            ).toLowerCase();
        }

        var security;

        if (protocol === "trojan") {
            security = trim(
                query.get("security") || "tls"
            ).toLowerCase();
        } else if (protocol === "hysteria2") {
            security = "tls";
        } else {
            security = trim(
                query.get("security") || "none"
            ).toLowerCase();
        }

        var fragment = parsed.hash
            ? parsed.hash.substring(1)
            : "";

        var name = cleanName(
            safeDecode(fragment) ||
            protocol.toUpperCase() +
                " " +
                host +
                ":" +
                port
        );

        return {
            raw: raw,
            protocol: protocol,
            name: name,
            host: host,
            port: port,
            transport: transport,
            security: security,
            canonical:
                canonical(protocol, host, port)
        };
    }

    function parseUri(raw) {
        var match = /^([A-Za-z][A-Za-z0-9+.-]*):\/\//.exec(
            raw
        );

        if (!match) {
            throw new Error(
                "Строка не является URI-конфигурацией."
            );
        }

        var protocol = normalizeProtocol(match[1]);

        if (protocol === "vmess") {
            return parseVmess(raw);
        }

        if (
            protocol === "vless" ||
            protocol === "trojan" ||
            protocol === "hysteria2"
        ) {
            return parseStandard(raw);
        }

        throw new Error(
            "Поддерживаются только vless://, vmess://, " +
            "trojan:// и hysteria2://."
        );
    }

    function parseInput(value) {
        if (value.length > 262144) {
            throw new Error(
                "Общий объём конфигураций слишком большой."
            );
        }

        var lines = value
            .split(/\r?\n/)
            .map(trim)
            .filter(function (line) {
                return line !== "";
            });

        if (!lines.length) {
            throw new Error(
                "Введите хотя бы одну URI-конфигурацию."
            );
        }

        if (lines.length > 50) {
            throw new Error(
                "За один раз можно проверить не более 50 серверов."
            );
        }

        return lines.map(function (line, index) {
            var item = {
                line: index + 1,
                raw: line,
                error: null,
                duplicate: null,
                importState: "ready"
            };

            if (line.length > 16384) {
                item.error =
                    "Строка конфигурации слишком длинная.";

                return item;
            }

            try {
                var parsed = parseUri(line);

                Object.keys(parsed).forEach(function (key) {
                    item[key] = parsed[key];
                });
            } catch (error) {
                item.error =
                    error && error.message
                        ? error.message
                        : "Не удалось разобрать конфигурацию.";
            }

            return item;
        });
    }

    function unwrap(payload) {
        if (
            payload &&
            payload.success === true &&
            payload.data !== undefined
        ) {
            return payload.data;
        }

        return payload;
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

    function request(url, options) {
        return BROrayUI
            .apiRequest(url, options)
            .then(unwrap);
    }

    function findCard() {
        return (
            document.getElementById(
                "server-import-card"
            ) ||
            document.querySelector(
                ".server-import-card"
            )
        );
    }

    function findForm() {
        var card = findCard();

        return (
            document.getElementById(
                "server-import-form"
            ) ||
            (
                card
                    ? card.querySelector("form")
                    : null
            )
        );
    }

    function findField() {
        var card = findCard();

        return (
            document.getElementById("server-uri") ||
            document.getElementById(
                "server-import-uri"
            ) ||
            document.getElementById(
                "server-config"
            ) ||
            document.getElementById(
                "server-import-config"
            ) ||
            (
                card
                    ? card.querySelector(
                        "textarea, " +
                        "input[name*='uri'], " +
                        "input[type='url'], " +
                        "input[type='text']"
                    )
                    : null
            )
        );
    }

    function findSubmit() {
        var form = findForm();

        if (!form) {
            return null;
        }

        return (
            form.querySelector(
                "button[type='submit']"
            ) ||
            form.querySelector(
                "input[type='submit']"
            ) ||
            Array.prototype.filter.call(
                form.querySelectorAll("button"),
                function (button) {
                    return /импорт|добав|провер/i.test(
                        trim(button.textContent)
                    );
                }
            )[0] ||
            null
        );
    }

    function closestButton(node) {
        while (
            node &&
            node !== document &&
            node.nodeType === 1
        ) {
            if (
                String(node.tagName).toLowerCase() ===
                "button"
            ) {
                return node;
            }

            node = node.parentNode;
        }

        return null;
    }

    function create(
        tag,
        className,
        content
    ) {
        var node = document.createElement(tag);

        if (className) {
            node.className = className;
        }

        if (
            content !== undefined &&
            content !== null
        ) {
            node.textContent = String(content);
        }

        return node;
    }

    function setButton(label, busy) {
        var button = findSubmit();

        if (!button) {
            return;
        }

        button.disabled = Boolean(busy);

        if (busy) {
            button.setAttribute("aria-busy", "true");
        } else {
            button.removeAttribute("aria-busy");
        }

        if (
            String(button.tagName).toLowerCase() ===
            "input"
        ) {
            button.value = label;
        } else {
            button.textContent = label;
        }
    }

    function previewRoot() {
        return document.getElementById(
            "server-import-preflight"
        );
    }

    function ensureInterface() {
        var card = findCard();
        var field = findField();

        if (!card || !field) {
            return false;
        }

        field.setAttribute("spellcheck", "false");
        field.setAttribute(
            "autocapitalize",
            "none"
        );

        if (!field.getAttribute("placeholder")) {
            field.setAttribute(
                "placeholder",
                [
                    "vless://...",
                    "vmess://...",
                    "trojan://...",
                    "hysteria2://..."
                ].join("\n")
            );
        }

        if (
            !document.getElementById(
                "server-import-preflight-hint"
            )
        ) {
            var hint = create(
                "p",
                "server-import-preflight-hint"
            );

            hint.id =
                "server-import-preflight-hint";

            hint.textContent =
                "Поддерживаются VLESS, VMess, Trojan и " +
                "Hysteria2. Несколько конфигураций можно " +
                "вставить по одной на строку.";

            var fieldContainer =
                field.closest
                    ? field.closest(".field")
                    : field.parentNode;

            if (!fieldContainer) {
                fieldContainer = field.parentNode;
            }

            if (fieldContainer) {
                fieldContainer.appendChild(hint);
            }
        }

        if (!previewRoot()) {
            var preview = create(
                "section",
                "server-import-preflight"
            );

            preview.id =
                "server-import-preflight";

            preview.hidden = true;
            preview.setAttribute(
                "aria-live",
                "polite"
            );

            var anchor =
                field.closest
                    ? field.closest(".field")
                    : field.parentNode;

            if (!anchor) {
                anchor = field;
            }

            if (anchor.parentNode) {
                anchor.parentNode.insertBefore(
                    preview,
                    anchor.nextSibling
                );
            } else {
                card.appendChild(preview);
            }
        }

        if (!state.busy && !state.valid) {
            setButton(
                "Проверить конфигурацию",
                false
            );
        }

        return true;
    }

    function existingServersMap(summary) {
        summary = unwrap(summary) || {};

        var map = Object.create(null);
        var servers = summary.servers || [];

        servers.forEach(function (server) {
            if (
                !server ||
                !server.protocol ||
                !(server.address || server.host) ||
                !server.port
            ) {
                return;
            }

            var key = canonical(
                server.protocol,
                server.address || server.host,
                server.port
            );

            map[key] = server;
        });

        return map;
    }

    function applyDuplicates(items, summary) {
        var existing =
            existingServersMap(summary);

        var inserted =
            Object.create(null);

        items.forEach(function (item) {
            if (item.error || !item.canonical) {
                return;
            }

            if (existing[item.canonical]) {
                item.duplicate =
                    "Такой сервер уже сохранён: " +
                    (
                        existing[item.canonical].name ||
                        existing[item.canonical].id ||
                        item.host
                    ) +
                    ".";

                return;
            }

            if (inserted[item.canonical]) {
                item.duplicate =
                    "Эта конфигурация повторяется " +
                    "во вставленном списке.";

                return;
            }

            inserted[item.canonical] = true;
        });
    }

    function statusFor(item) {
        if (item.importState === "importing") {
            return {
                className: "is-checking",
                title: "Импортируется"
            };
        }

        if (item.importState === "imported") {
            return {
                className: "is-imported",
                title: "Импортирован"
            };
        }

        if (item.importState === "failed") {
            return {
                className: "is-error",
                title: "Ошибка"
            };
        }

        if (item.error) {
            return {
                className: "is-error",
                title: "Ошибка"
            };
        }

        if (item.duplicate) {
            return {
                className: "is-duplicate",
                title: "Дубликат"
            };
        }

        return {
            className: "is-ready",
            title: "Готов"
        };
    }

    function appendDetail(
        root,
        label,
        value
    ) {
        var detail = create(
            "div",
            "server-import-preview-detail"
        );

        detail.appendChild(
            create("span", "", label)
        );

        detail.appendChild(
            create(
                "strong",
                "",
                value || "—"
            )
        );

        root.appendChild(detail);
    }

    function renderPreview() {
        var root = previewRoot();

        if (!root) {
            return;
        }

        root.innerHTML = "";
        root.hidden = false;

        var header = create(
            "div",
            "server-import-preflight-header"
        );

        var heading = create("div");

        heading.appendChild(
            create(
                "span",
                "eyebrow",
                "Предварительная проверка"
            )
        );

        heading.appendChild(
            create(
                "h3",
                "",
                "Результат разбора конфигураций"
            )
        );

        heading.appendChild(
            create(
                "p",
                "",
                state.message
            )
        );

        header.appendChild(heading);

        var totalBadge = create(
            "span",
            "server-import-preflight-summary " +
                "is-" +
                state.messageType,
            state.items.length
                ? String(state.items.length)
                : "0"
        );

        header.appendChild(totalBadge);
        root.appendChild(header);

        var list = create(
            "div",
            "server-import-preview-list"
        );

        state.items.forEach(function (item) {
            var status = statusFor(item);

            var article = create(
                "article",
                "server-import-preview-item " +
                    status.className
            );

            article.setAttribute(
                "data-import-line",
                String(item.line)
            );

            var top = create(
                "div",
                "server-import-preview-top"
            );

            top.appendChild(
                create(
                    "strong",
                    "server-import-preview-name",
                    item.name ||
                    "Строка " + item.line
                )
            );

            top.appendChild(
                create(
                    "span",
                    "server-import-preview-status " +
                        status.className,
                    status.title
                )
            );

            article.appendChild(top);

            if (item.error || item.duplicate) {
                article.appendChild(
                    create(
                        "p",
                        "server-import-preview-error",
                        item.error ||
                        item.duplicate
                    )
                );

                list.appendChild(article);
                return;
            }

            article.appendChild(
                create(
                    "p",
                    "server-import-preview-endpoint",
                    item.host + ":" + item.port
                )
            );

            var tags = create(
                "div",
                "server-import-preview-tags"
            );

            [
                item.protocol,
                item.transport,
                item.security
            ].forEach(function (value) {
                tags.appendChild(
                    create(
                        "span",
                        "server-tag",
                        value
                    )
                );
            });

            article.appendChild(tags);

            var details = create(
                "div",
                "server-import-preview-details"
            );

            appendDetail(
                details,
                "Адрес",
                item.host
            );

            appendDetail(
                details,
                "Порт",
                item.port
            );

            appendDetail(
                details,
                "Транспорт",
                item.transport
            );

            appendDetail(
                details,
                "Защита",
                item.security
            );

            article.appendChild(details);

            if (item.importState === "failed") {
                article.appendChild(
                    create(
                        "p",
                        "server-import-preview-error",
                        item.importError ||
                        "Импорт завершился ошибкой."
                    )
                );
            }

            list.appendChild(article);
        });

        root.appendChild(list);
    }

    function resetPreflight(hidePreview) {
        state.busy = false;
        state.valid = false;
        state.readyValue = null;
        state.items = [];
        state.message = "";
        state.messageType = "neutral";

        if (hidePreview) {
            var root = previewRoot();

            if (root) {
                root.hidden = true;
                root.innerHTML = "";
            }
        }

        setButton(
            "Проверить конфигурацию",
            false
        );
    }

    function validateInput() {
        if (!ensureInterface()) {
            BROrayUI.toast(
                "Форма импорта не найдена.",
                "error"
            );

            return;
        }

        var field = findField();
        var value = field ? field.value : "";

        var items;

        try {
            items = parseInput(value);
        } catch (error) {
            state.items = [
                {
                    line: 1,
                    error: error.message,
                    importState: "ready"
                }
            ];

            state.message =
                "Конфигурации не готовы к импорту.";

            state.messageType = "error";
            state.valid = false;
            state.readyValue = null;

            renderPreview();

            setButton(
                "Проверить повторно",
                false
            );

            return;
        }

        state.busy = true;
        state.valid = false;
        state.readyValue = null;
        state.items = items;
        state.message =
            "Проверяется список сохранённых серверов…";
        state.messageType = "checking";

        renderPreview();

        setButton(
            "Проверка…",
            true
        );

        var hasParseErrors =
            items.some(function (item) {
                return Boolean(item.error);
            });

        if (hasParseErrors) {
            state.busy = false;
            state.message =
                "Исправьте ошибки в конфигурациях.";
            state.messageType = "error";

            renderPreview();

            setButton(
                "Проверить повторно",
                false
            );

            return;
        }

        request(
            SUMMARY_URL,
            {
                method: "GET"
            }
        ).then(function (summary) {
            applyDuplicates(items, summary);

            var blocked =
                items.some(function (item) {
                    return Boolean(
                        item.error ||
                        item.duplicate
                    );
                });

            state.busy = false;

            if (blocked) {
                state.valid = false;
                state.readyValue = null;
                state.message =
                    "Обнаружены ошибки или дубликаты. " +
                    "Импорт заблокирован.";
                state.messageType = "error";

                setButton(
                    "Проверить повторно",
                    false
                );
            } else {
                state.valid = true;
                state.readyValue = value;
                state.message =
                    "Все обязательные параметры заполнены. " +
                    "Дубликаты не найдены.";
                state.messageType = "ready";

                setButton(
                    items.length === 1
                        ? "Импортировать сервер"
                        : "Импортировать серверы: " +
                            items.length,
                    false
                );
            }

            renderPreview();
        }).catch(function (error) {
            state.busy = false;
            state.valid = false;
            state.readyValue = null;
            state.message =
                "Не удалось проверить сохранённые серверы: " +
                errorMessage(error);
            state.messageType = "error";

            renderPreview();

            setButton(
                "Проверить повторно",
                false
            );

            if (error.status === 401) {
                BROrayUI.redirectToLogin();
            }
        });
    }

    function updateItemState(
        index,
        importState,
        importError
    ) {
        if (!state.items[index]) {
            return;
        }

        state.items[index].importState =
            importState;

        state.items[index].importError =
            importError || null;

        renderPreview();
    }

    function delay(milliseconds) {
        return new Promise(function (resolve) {
            window.setTimeout(
                resolve,
                milliseconds
            );
        });
    }

    function importNext(index) {
        if (index >= state.items.length) {
            return Promise.resolve();
        }

        var item = state.items[index];

        updateItemState(
            index,
            "importing"
        );

        state.message =
            "Импортируется " +
            (index + 1) +
            " из " +
            state.items.length +
            "…";

        state.messageType = "checking";

        renderPreview();

        setButton(
            "Импорт " +
                (index + 1) +
                " из " +
                state.items.length +
                "…",
            true
        );

        var field = findField();

        var requestBody = {};

        requestBody[
            (
                field &&
                trim(field.getAttribute("name"))
            ) ||
            IMPORT_KEY ||
            "uri"
        ] = item.raw;

        return request(
            IMPORT_URL,
            {
                method: "POST",
                body: requestBody
            }
        ).then(function () {
            updateItemState(
                index,
                "imported"
            );

            return delay(160);
        }).then(function () {
            return importNext(index + 1);
        }).catch(function (error) {
            updateItemState(
                index,
                "failed",
                errorMessage(error)
            );

            throw {
                index: index,
                original: error
            };
        });
    }

    function refreshServers() {
        var refresh =
            document.getElementById(
                "refresh-servers"
            );

        if (refresh) {
            refresh.click();
        }
    }

    function importValidated() {
        var field = findField();

        if (
            !field ||
            !state.valid ||
            state.readyValue !== field.value
        ) {
            resetPreflight(false);
            validateInput();
            return;
        }

        state.busy = true;

        importNext(0).then(function () {
            var imported = state.items.length;

            state.message =
                "Импорт успешно завершён.";
            state.messageType = "ready";

            renderPreview();

            BROrayUI.toast(
                imported === 1
                    ? "Сервер импортирован."
                    : "Импортировано серверов: " +
                        imported +
                        ".",
                "success"
            );

            refreshServers();

            field.value = "";

            window.setTimeout(function () {
                resetPreflight(true);

                var cancel =
                    document.getElementById(
                        "cancel-import"
                    );

                if (cancel) {
                    cancel.click();
                }
            }, 700);
        }).catch(function (failure) {
            state.busy = false;
            state.valid = false;
            state.readyValue = null;

            var imported = state.items.filter(
                function (item) {
                    return (
                        item.importState ===
                        "imported"
                    );
                }
            ).length;

            state.message =
                "Импорт остановлен. Успешно: " +
                imported +
                " из " +
                state.items.length +
                ".";

            state.messageType = "error";

            renderPreview();

            setButton(
                "Проверить повторно",
                false
            );

            refreshServers();

            BROrayUI.toast(
                errorMessage(
                    failure.original || failure
                ),
                "error"
            );
        });
    }

    function runPrimaryAction() {
        if (state.busy) {
            return;
        }

        var field = findField();

        if (
            state.valid &&
            field &&
            state.readyValue === field.value
        ) {
            importValidated();
        } else {
            validateInput();
        }
    }

    document.addEventListener(
        "submit",
        function (event) {
            var form = findForm();

            if (!form || event.target !== form) {
                return;
            }

            event.preventDefault();
            event.stopPropagation();
            event.stopImmediatePropagation();

            runPrimaryAction();
        },
        true
    );

    document.addEventListener(
        "click",
        function (event) {
            var button = closestButton(
                event.target
            );

            if (!button) {
                return;
            }

            var submit = findSubmit();

            if (
                submit &&
                button === submit
            ) {
                event.preventDefault();
                event.stopPropagation();
                event.stopImmediatePropagation();

                runPrimaryAction();
                return;
            }

            if (button.id === "cancel-import") {
                resetPreflight(true);
                return;
            }

            if (button.id === "toggle-import") {
                window.setTimeout(
                    ensureInterface,
                    0
                );
            }
        },
        true
    );

    document.addEventListener(
        "input",
        function (event) {
            var field = findField();

            if (
                field &&
                event.target === field &&
                !state.busy
            ) {
                resetPreflight(true);
            }
        },
        true
    );

    function initialize() {
        ensureInterface();

        var attempts = 0;

        var timer = window.setInterval(
            function () {
                attempts++;

                if (
                    ensureInterface() ||
                    attempts >= 20
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
