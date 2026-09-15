(function () {
    "use strict";

    var state = {
        data: null,
        webAccess: null,
        busy: false,
        webAccessBusy: false
    };

    var ids = {
        badge: "keenetic-status-badge",
        title: "keenetic-status-title",
        description: "keenetic-status-description",
        expectedTag: "keenetic-expected-tag",
        upstreamTag: "keenetic-upstream-tag",
        interfaceName: "keenetic-interface-name",
        protocol: "keenetic-protocol",
        upstream: "keenetic-upstream",
        link: "keenetic-link",
        connected: "keenetic-connected",
        statusState: "keenetic-state",
        expectedDescription: "keenetic-expected-description",
        actualDescription: "keenetic-actual-description",
        descriptionCheck: "keenetic-description-check",
        expectedProtocol: "keenetic-expected-protocol",
        actualProtocol: "keenetic-actual-protocol",
        protocolCheck: "keenetic-protocol-check",
        expectedUpstream: "keenetic-expected-upstream",
        actualUpstream: "keenetic-actual-upstream",
        upstreamCheck: "keenetic-upstream-check",
        checks: "keenetic-checks",
        type: "keenetic-type",
        mtu: "keenetic-mtu",
        via: "keenetic-via",
        localAddress: "keenetic-local-address",
        remoteAddress: "keenetic-remote-address",
        updatedAt: "keenetic-updated-at",
        problemsPanel: "keenetic-problems-panel",
        problems: "keenetic-problems",
        refresh: "keenetic-refresh",
        checkUpstream: "keenetic-check-upstream",
        create: "keenetic-create",
        repair: "keenetic-repair",
        syncDescription: "keenetic-sync-description",
        deleteInterface: "keenetic-delete",
        webAccessBadge: "keenetic-web-access-badge",
        webAccessDescription: "keenetic-web-access-description",
        webAccessAddress: "keenetic-web-access-address",
        webAccessLocalAddress: "keenetic-web-access-local-address",
        webAccessMode: "keenetic-web-access-mode",
        webAccessEnable: "keenetic-web-access-enable",
        webAccessDisable: "keenetic-web-access-disable",
        webAccessOpen: "keenetic-web-access-open",
        webAccessCopy: "keenetic-web-access-copy"
    };

    function element(name) {
        return document.getElementById(ids[name]);
    }

    function text(name, value) {
        var node = element(name);
        var normalized = value;

        if (name === "link") {
            if (
                value === true ||
                value === "up" ||
                value === "yes" ||
                value === "Да"
            ) {
                normalized = "Установлена";
            } else if (
                value === false ||
                value === "down" ||
                value === "no" ||
                value === "Нет"
            ) {
                normalized = "Не установлена";
            }
        } else if (name === "connected") {
            if (
                value === true ||
                value === "up" ||
                value === "yes" ||
                value === "Да"
            ) {
                normalized = "Подключено";
            } else if (
                value === false ||
                value === "down" ||
                value === "no" ||
                value === "Нет"
            ) {
                normalized = "Нет подключения";
            }
        } else if (name === "statusState") {
            if (
                value === "up" ||
                value === "running"
            ) {
                normalized = "Работает";
            } else if (
                value === "down" ||
                value === "stopped"
            ) {
                normalized = "Остановлен";
            }
        } else if (
            name === "protocol" ||
            name === "expectedProtocol" ||
            name === "actualProtocol"
        ) {
            if (
                String(value || "").toLowerCase() ===
                    "socks5"
            ) {
                normalized = "SOCKS5";
            }
        }

        if (node) {
            node.textContent =
                normalized === null ||
                normalized === undefined ||
                normalized === ""
                    ? "—"
                    : String(normalized);
        }
    }

    function unwrap(payload) {
        if (payload && payload.success === false) {
            throw new Error(
                payload.error && payload.error.message
                    ? payload.error.message
                    : "Операция завершилась ошибкой."
            );
        }

        if (payload && Object.prototype.hasOwnProperty.call(payload, "data")) {
            return payload.data;
        }

        return payload;
    }

    function request(url, options) {
        return BROrayUI.apiRequest(url, options || {}).then(unwrap);
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
        if (error && error.message) {
            return error.message;
        }
        return "Не удалось выполнить операцию.";
    }

    function handleError(error) {
        if (
            error &&
            (error.status === 401 ||
                error.code === "AUTH_REQUIRED" ||
                error.code === "SESSION_REQUIRED")
        ) {
            BROrayUI.redirectToLogin();
            return;
        }
        BROrayUI.toast(errorMessage(error), "error");
    }

    function yesNo(value) {
        return value ? "Да" : "Нет";
    }

    function upDown(value) {
        return value ? "up" : "down";
    }

    function displayDate(value) {
        if (!value) {
            return "—";
        }

        var date = new Date(value);
        if (isNaN(date.getTime())) {
            return value;
        }

        return date.toLocaleString("ru-RU");
    }

    function normalizeDescription(value) {
        return String(value == null ? "" : value)
            .replace(/\s+/g, " ")
            .trim()
            .replace(/^BROray\s*[\u2010-\u2015\u2212-]\s*/, "BROray — ");
    }

    function effectiveChecks(data) {
        var checks = Object.assign({}, data.checks || {});
        var actual = data.actual || {};
        var expected = data.expected || {};

        if (
            data.expectedReady === true &&
            expected.description != null &&
            actual.description != null
        ) {
            checks.description =
                normalizeDescription(actual.description) ===
                normalizeDescription(expected.description);
        }
        return checks;
    }

    function effectiveMatchesExpected(data) {
        var checks = effectiveChecks(data);
        return checks.description === true &&
            checks.protocol === true &&
            checks.upstream === true;
    }

    function setBadge(label, className) {
        var badge = element("badge");
        badge.textContent = label;
        badge.className = "keenetic-status-badge " + className;
    }

    function setCheckValue(name, value) {
        var node = element(name);
        if (!node) {
            return;
        }

        node.textContent = value ? "Совпадает" : "Не совпадает";
        node.className = value ? "keenetic-value-ok" : "keenetic-value-error";
    }

    function createCheckRow(label, detail, value) {
        var row = document.createElement("div");
        var indicator = document.createElement("span");
        var copy = document.createElement("span");
        var copyTitle = document.createElement("strong");
        var copyDetail = document.createElement("span");
        var result = document.createElement("span");

        row.className = "keenetic-check-row " +
            (value ? "keenetic-check-ok" : "keenetic-check-error");
        indicator.className = "keenetic-check-indicator";
        copy.className = "keenetic-check-copy";
        copyTitle.textContent = label;
        copyDetail.textContent = detail;
        result.className = "keenetic-check-value " +
            (value ? "keenetic-check-value-ok" : "keenetic-check-value-error");
        result.textContent = value ? "OK" : "Ошибка";

        copy.appendChild(copyTitle);
        copy.appendChild(copyDetail);
        row.appendChild(indicator);
        row.appendChild(copy);
        row.appendChild(result);
        return row;
    }

    function renderChecks(data) {
        var container = element("checks");
        var checks = effectiveChecks(data);
        var interfaceName = currentInterfaceName(data);
        var rows = [
            ["Интерфейс существует", interfaceName + " найден в KeeneticOS", checks.exists],
            ["Принадлежность", checks.ownership !== false ? "Интерфейс подтверждён как созданный BROray" : "Принадлежность интерфейса BROray не подтверждена", checks.ownership !== false],
            ["Протокол", "Протокол SOCKS5 настроен правильно", checks.protocol],
            ["Адрес Xray", "Адрес Xray настроен правильно", checks.upstream],
            ["Описание", "Имя соответствует активному серверу", checks.description],
            ["Прокси Xray", "Прокси Xray принимает подключения", checks.upstreamReachable],
            ["Связь", "Связь установлена", checks.link],
            ["Подключение", "Интерфейс подключён", checks.connected],
            ["Состояние", "Интерфейс работает", checks.state]
        ];

        container.textContent = "";
        rows.forEach(function (item) {
            container.appendChild(createCheckRow(item[0], item[1], item[2] === true));
        });
    }

    function renderProblems(data) {
        var panel = element("problemsPanel");
        var list = element("problems");
        var problems = Array.isArray(data.problems) ? data.problems.slice() : [];
        var checks = effectiveChecks(data);

        if (checks.description === true) {
            problems = problems.filter(function (problem) {
                return !/описан/i.test(String(problem));
            });
        }

        list.textContent = "";
        problems.forEach(function (problem) {
            var item = document.createElement("li");
            item.textContent = problem;
            list.appendChild(item);
        });
        panel.hidden = problems.length === 0;
    }

    function setButtonVisibility(data) {
        var actual = data.actual || {};
        var expected = data.expected || {};
        var exists = data.exists === true;
        var ready = data.expectedReady === true;
        var ownershipReceiptPresent = data.ownershipReceiptPresent === true;
        var descriptionMismatch =
            exists &&
            ready &&
            normalizeDescription(actual.description) !==
                normalizeDescription(expected.description);

        element("create").hidden = exists || !ready || ownershipReceiptPresent;
        element("repair").hidden =
            (!exists && (!ready || !ownershipReceiptPresent)) ||
            (exists && data.healthy && effectiveMatchesExpected(data));
        element("syncDescription").hidden = !descriptionMismatch;
        element("deleteInterface").hidden = !exists;
    }

    function currentInterfaceName(data) {
        var current = data || state.data || {};
        return current.interfaceDisplayName ||
            (current.expected && current.expected.description) ||
            current.description ||
            "BROray";
    }

    function renderStatus(data) {
        var actual = data.actual || {};
        var expected = data.expected || {};
        var technical = data.technical || {};
        var checks = effectiveChecks(data);
        var matchesExpected = effectiveMatchesExpected(data);
        var interfaceName = currentInterfaceName(data);
        var actualDescription = normalizeDescription(actual.description);
        var expectedDescription = normalizeDescription(expected.description);

        state.data = data;

        if (!data.expectedReady) {
            setBadge("Настройка недоступна", "status-warning");
            text("title", interfaceName + " не может быть настроен");
            text(
                "description",
                "Нужны работающий SOCKS-модуль Xray и выбранный активный сервер."
            );
        } else if (!data.exists) {
            setBadge("Не настроено", "status-neutral");
            text("title", interfaceName + " отсутствует");
            text(
                "description",
                "Интерфейс можно создать из текущих параметров Xray и активного сервера."
            );
        } else if (data.healthy && matchesExpected) {
            setBadge("Настроено", "status-success");
            text("title", interfaceName);
            text(
                "description",
                "Управляемый SOCKS-прокси KeeneticOS исправен и синхронизирован."
            );
        } else if (data.healthy) {
            setBadge("Настройка неполная", "status-warning");
            text("title", interfaceName);
            text(
                "description",
                "Интерфейс работает, но параметры отличаются от ожидаемой конфигурации."
            );
        } else {
            setBadge("Требуется восстановление", "status-error");
            text("title", interfaceName);
            text(
                "description",
                "Одна или несколько обязательных проверок " + interfaceName + " завершились ошибкой."
            );
        }

        text(
            "expectedTag",
            data.expectedReady
                ? "Ожидается: " + expectedDescription
                : "Ожидаемая конфигурация недоступна"
        );
        text(
            "upstreamTag",
            "Прокси Xray: " +
                (data.upstreamReachable ? "доступен" : "недоступен")
        );

        text("interfaceName", interfaceName);
        text("protocol", data.protocol);
        text("upstream", data.upstream);
        text("link", data.link);
        text("connected", data.connected);
        text("statusState", data.state);

        text("expectedDescription", expectedDescription);
        text("actualDescription", actualDescription);
        setCheckValue("descriptionCheck", checks.description);

        text("expectedProtocol", expected.protocol);
        text("actualProtocol", actual.protocol);
        setCheckValue("protocolCheck", checks.protocol);

        text("expectedUpstream", expected.upstream);
        text("actualUpstream", actual.upstream);
        setCheckValue("upstreamCheck", checks.upstream);

        text("type", technical.type);
        text("mtu", technical.mtu || "—");
        text("via", technical.via);
        text("localAddress", technical.localAddress);
        text("remoteAddress", technical.remoteAddress);
        text("updatedAt", displayDate(data.updatedAt));

        renderChecks(data);
        renderProblems(data);
        setButtonVisibility(data);
    }

    function webAccessModeLabel(value) {
        switch (String(value || "").toLowerCase()) {
        case "cloud":
            return "Через облако";
        case "direct":
            return "Прямой доступ";
        case "auto":
            return "Автоматически";
        default:
            return value || "—";
        }
    }

    function setWebAccessBadge(label, className) {
        var badge = element("webAccessBadge");
        if (!badge) {
            return;
        }
        badge.textContent = label;
        badge.className = "keenetic-status-badge " + className;
    }

    function setWebAccessButtonLabel(button, label) {
        var node;
        if (!button) {
            return;
        }
        node = button.querySelector(".button-label");
        if (node) {
            node.textContent = label;
        } else {
            button.textContent = label;
        }
        button.setAttribute("data-default-label", label);
    }

    function renderWebAccess(data) {
        var reason = data.reason || {};
        var enabled = data.enabled === true;
        var address = data.address || "";
        var enableButton = element("webAccessEnable");
        var disableButton = element("webAccessDisable");
        var openButton = element("webAccessOpen");
        var copyButton = element("webAccessCopy");

        state.webAccess = data;

        switch (data.state) {
        case "enabled":
            if (data.consistent === true) {
                setWebAccessBadge("Включён", "status-success");
            } else {
                setWebAccessBadge("Требуется восстановление", "status-warning");
            }
            break;
        case "disabled":
            setWebAccessBadge("Выключен", "status-neutral");
            break;
        case "conflict":
            setWebAccessBadge("Конфликт", "status-error");
            break;
        case "recovery":
            setWebAccessBadge("Требуется восстановление", "status-error");
            break;
        default:
            setWebAccessBadge("Недоступен", "status-warning");
        }

        element("webAccessDescription").textContent =
            reason.message || "Состояние адреса WebUI получено.";
        element("webAccessAddress").textContent = address || "—";
        element("webAccessLocalAddress").textContent =
            data.localAddress || "—";
        element("webAccessMode").textContent = webAccessModeLabel(
            data.keenDns && data.keenDns.access
        );

        enableButton.hidden = data.canEnable !== true;
        disableButton.hidden = data.canDisable !== true;
        openButton.hidden = !enabled || !address;
        copyButton.hidden = !enabled || !address;
        setWebAccessButtonLabel(
            enableButton,
            enabled ? "Восстановить адрес" : "Включить"
        );
    }

    function webAccessButtons() {
        return [
            element("webAccessEnable"),
            element("webAccessDisable"),
            element("webAccessOpen"),
            element("webAccessCopy")
        ];
    }

    function setWebAccessBusy(busy, activeButton, busyLabel) {
        state.webAccessBusy = busy;
        webAccessButtons().forEach(function (button) {
            var label;

            if (!button) {
                return;
            }
            label = button.querySelector(".button-label");
            if (!button.getAttribute("data-default-label")) {
                button.setAttribute(
                    "data-default-label",
                    label ? label.textContent : button.textContent.trim()
                );
            }
            button.disabled = busy;
            button.classList.toggle(
                "is-loading",
                busy && button === activeButton
            );
            button.setAttribute(
                "aria-busy",
                busy && button === activeButton ? "true" : "false"
            );
            if (label) {
                label.textContent =
                    busy && button === activeButton
                        ? busyLabel
                        : button.getAttribute("data-default-label");
            }
        });
    }

    function actionButtons() {
        return [
            element("refresh"),
            element("checkUpstream"),
            element("create"),
            element("repair"),
            element("syncDescription"),
            element("deleteInterface")
        ];
    }

    function setBusy(busy, activeButton, busyLabel) {
        state.busy = busy;
        actionButtons().forEach(function (button) {
            var label;

            if (!button) {
                return;
            }
            label = button.querySelector(".button-label");
            if (!button.getAttribute("data-default-label")) {
                button.setAttribute(
                    "data-default-label",
                    label ? label.textContent : button.textContent.trim()
                );
            }
            button.disabled = busy;
            button.classList.toggle(
                "is-loading",
                busy && button === activeButton
            );
            button.setAttribute(
                "aria-busy",
                busy && button === activeButton ? "true" : "false"
            );
            if (label) {
                label.textContent =
                    busy && button === activeButton
                        ? busyLabel
                        : button.getAttribute("data-default-label");
            }
        });
    }

    function loadStatus(showToast, force) {
        var button = element("refresh");
        if (state.busy || state.webAccessBusy) {
            return Promise.resolve();
        }

        setBusy(true, button, "Обновление…");
        return request("/api/keenetic/status.cgi" + (force ? "?force=1" : ""), {
            method: "GET",
            credentials: "same-origin"
        })
            .then(function (data) {
                renderStatus(data);
                if (showToast) {
                    BROrayUI.toast("Состояние " + currentInterfaceName(data) + " обновлено.", "success");
                }
            })
            .catch(handleError)
            .then(function () {
                setBusy(false, button, "Обновление…");
            });
    }

    function loadWebAccessStatus(showToast) {
        if (state.busy || state.webAccessBusy) {
            return Promise.resolve();
        }

        setWebAccessBusy(true, null, "Обновление…");
        return request("/api/keenetic/web-access-status.cgi", {
            method: "GET",
            credentials: "same-origin"
        })
            .then(function (data) {
                renderWebAccess(data);
                if (showToast) {
                    BROrayUI.toast(
                        "Состояние адреса WebUI обновлено.",
                        "success"
                    );
                }
            })
            .catch(function (error) {
                state.webAccess = null;
                setWebAccessBadge("Ошибка", "status-error");
                element("webAccessDescription").textContent =
                    errorMessage(error);
                webAccessButtons().forEach(function (button) {
                    if (button) {
                        button.hidden = true;
                    }
                });
                handleError(error);
            })
            .then(function () {
                setWebAccessBusy(false, null, "Обновление…");
            });
    }

    function runWebAccessAction(action, button, busyLabel, successMessage) {
        if (state.busy || state.webAccessBusy) {
            return Promise.resolve();
        }

        setWebAccessBusy(true, button, busyLabel);
        return request("/api/keenetic/web-access-" + action + ".cgi", {
            method: "POST",
            credentials: "same-origin",
            body: {}
        })
            .then(function (data) {
                renderWebAccess(data);
                BROrayUI.toast(successMessage, "success");
            })
            .catch(handleError)
            .then(function () {
                setWebAccessBusy(false, button, busyLabel);
            });
    }

    function copyWebAccessAddress() {
        var address = state.webAccess && state.webAccess.address;
        var fallback;

        if (!address) {
            return Promise.reject(new Error("Адрес KeenDNS недоступен."));
        }
        if (navigator.clipboard && navigator.clipboard.writeText) {
            return navigator.clipboard.writeText(address);
        }

        fallback = document.createElement("textarea");
        fallback.value = address;
        fallback.setAttribute("readonly", "readonly");
        fallback.style.position = "fixed";
        fallback.style.opacity = "0";
        document.body.appendChild(fallback);
        fallback.select();
        if (!document.execCommand("copy")) {
            fallback.remove();
            return Promise.reject(new Error("Не удалось скопировать адрес."));
        }
        fallback.remove();
        return Promise.resolve();
    }

    function runAction(action, button, busyLabel, successMessage) {
        if (state.busy || state.webAccessBusy) {
            return;
        }

        setBusy(true, button, busyLabel);
        request("/api/keenetic/" + action + ".cgi", {
            method: "POST",
            credentials: "same-origin",
            body: {}
        })
            .then(function (data) {
                renderStatus(data);
                BROrayUI.toast(successMessage, "success");
            })
            .catch(handleError)
            .then(function () {
                setBusy(false, button, busyLabel);
            });
    }

    function bindActions() {
        element("refresh").addEventListener("click", function () {
            loadStatus(true, true).then(function () {
                return loadWebAccessStatus(false);
            });
        });

        element("checkUpstream").addEventListener("click", function () {
            runAction(
                "check-upstream",
                element("checkUpstream"),
                "Проверка…",
                "Проверка подключения к Xray завершена."
            );
        });

        element("create").addEventListener("click", function () {
            runAction(
                "create",
                element("create"),
                "Создание…",
                "Интерфейс " + currentInterfaceName() + " создан."
            );
        });

        element("repair").addEventListener("click", function () {
            runAction(
                "repair",
                element("repair"),
                "Восстановление…",
                "Интерфейс " + currentInterfaceName() + " восстановлен."
            );
        });

        element("syncDescription").addEventListener("click", function () {
            runAction(
                "sync-description",
                element("syncDescription"),
                "Синхронизация…",
                "Описание " + currentInterfaceName() + " синхронизировано."
            );
        });

        element("deleteInterface").addEventListener("click", async function () {
            var interfaceName = currentInterfaceName();
            var confirmed;

            try {
                confirmed = await window.BROrayDialogs.confirm({
                    eyebrow: "Опасное действие",
                    title: "Удалить " + interfaceName + "?",
                    message: "Прокси-интерфейс будет удалён. Серверы BROray и конфигурация Xray останутся без изменений.",
                    confirmText: "Удалить",
                    variant: "danger"
                });
            } catch (error) {
                handleError(error);
                return;
            }

            if (!confirmed) {
                return;
            }

            runAction(
                "delete",
                element("deleteInterface"),
                "Удаление…",
                "Интерфейс " + interfaceName + " удалён."
            );
        });

        element("webAccessEnable").addEventListener("click", function () {
            runWebAccessAction(
                "enable",
                element("webAccessEnable"),
                "Включение…",
                state.webAccess && state.webAccess.enabled
                    ? "Адрес WebUI восстановлен."
                    : "Адрес WebUI через KeenDNS включён."
            );
        });

        element("webAccessDisable").addEventListener("click", async function () {
            var confirmed;

            try {
                confirmed = await window.BROrayDialogs.confirm({
                    eyebrow: "Удалённый доступ",
                    title: "Выключить адрес WebUI через KeenDNS?",
                    message: "Текущая сессия, открытая через KeenDNS, прервётся. Локальный адрес WebUI останется доступен в домашней сети.",
                    confirmText: "Выключить",
                    variant: "danger"
                });
            } catch (error) {
                handleError(error);
                return;
            }

            if (!confirmed) {
                return;
            }

            runWebAccessAction(
                "disable",
                element("webAccessDisable"),
                "Выключение…",
                "Адрес WebUI через KeenDNS выключен."
            );
        });

        element("webAccessOpen").addEventListener("click", function () {
            var address = state.webAccess && state.webAccess.address;
            if (!address) {
                BROrayUI.toast("Адрес KeenDNS недоступен.", "error");
                return;
            }
            window.open(address, "_blank", "noopener,noreferrer");
        });

        element("webAccessCopy").addEventListener("click", function () {
            copyWebAccessAddress()
                .then(function () {
                    BROrayUI.toast("Адрес KeenDNS скопирован.", "success");
                })
                .catch(handleError);
        });
    }

    function findApplicationRoot() {
        var root;
        var main;

        root =
            document.getElementById("app") ||
            document.getElementById("application") ||
            document.querySelector(".app-shell");

        if (root) {
            return root;
        }

        main = document.querySelector("main.keenetic-page");

        if (main && main.closest) {
            root = main.closest("[hidden]");

            if (root) {
                return root;
            }
        }

        return main;
    }

    function findSessionLoader(applicationRoot) {
        var loader;
        var children;
        var index;
        var textValue;

        loader =
            document.getElementById("page-loader") ||
            document.getElementById("session-loader") ||
            document.querySelector(".page-loader") ||
            document.querySelector(".session-loader");

        if (loader) {
            return loader;
        }

        children = document.body.children;

        for (index = 0; index < children.length; index += 1) {
            if (children[index] === applicationRoot) {
                continue;
            }

            textValue =
                (children[index].textContent || "")
                    .replace(/\s+/g, " ")
                    .trim();

            if (textValue.indexOf("Проверка сессии") !== -1) {
                return children[index];
            }
        }

        return null;
    }

    function setCurrentUser(session) {
        var currentUser;

        currentUser =
            document.getElementById("current-user") ||
            document.getElementById("sidebar-current-user") ||
            document.querySelector("[data-current-user]");

        if (currentUser) {
            currentUser.textContent =
                session && session.user
                    ? session.user
                    : "admin";
        }
    }

    function openApplication(session) {
        var applicationRoot = findApplicationRoot();
        var sessionLoader = findSessionLoader(applicationRoot);

        setCurrentUser(session);

        if (sessionLoader) {
            sessionLoader.hidden = true;
        }

        if (applicationRoot) {
            applicationRoot.hidden = false;
        }
    }

    function initialize() {
        bindActions();

        request(
            "/api/session.cgi",
            {
                method: "GET",
                credentials: "same-origin"
            }
            )
            .then(function (session) {
                openApplication(session);
                loadStatus(false, true).then(function () {
                    return loadWebAccessStatus(false);
                });
            })
            .catch(function () {
                BROrayUI.redirectToLogin();
            });
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize, { once: true });
    } else {
        initialize();
    }
})();
