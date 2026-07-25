(function () {
    "use strict";

    var SUMMARY_PATH = "/api/servers/summary.cgi";
    var lastSummaryPayload = null;
    var observer = null;
    var normalizing = false;

    function text(value) {
        if (value === null || value === undefined) {
            return "";
        }

        return String(value);
    }

    function trim(value) {
        return text(value).replace(
            /^[\s\u00a0]+|[\s\u00a0]+$/g,
            ""
        );
    }

    function two(value) {
        return value < 10
            ? "0" + value
            : String(value);
    }

    function formatDate(value) {
        var date = value instanceof Date
            ? value
            : new Date(value);

        if (isNaN(date.getTime())) {
            return "нет данных";
        }

        return (
            two(date.getDate()) +
            "." +
            two(date.getMonth() + 1) +
            "." +
            date.getFullYear() +
            ", " +
            two(date.getHours()) +
            ":" +
            two(date.getMinutes()) +
            ":" +
            two(date.getSeconds())
        );
    }

    function matches(element, selector) {
        var method;

        if (!element || element.nodeType !== 1) {
            return false;
        }

        method =
            element.matches ||
            element.msMatchesSelector ||
            element.webkitMatchesSelector;

        return method
            ? method.call(element, selector)
            : false;
    }

    function closest(element, selector) {
        var current = element;

        while (current && current.nodeType === 1) {
            if (matches(current, selector)) {
                return current;
            }

            current = current.parentElement;
        }

        return null;
    }

    function setText(id, value) {
        var element = document.getElementById(id);

        if (element && element.textContent !== value) {
            element.textContent = value;
        }
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

    function parseTimestamp(value) {
        var number;
        var parsed;

        if (typeof value === "number") {
            number = value;

            if (number > 0 && number < 100000000000) {
                number = number * 1000;
            }

            parsed = new Date(number);

            return isNaN(parsed.getTime())
                ? null
                : parsed;
        }

        if (typeof value !== "string") {
            return null;
        }

        value = trim(value);

        if (!value) {
            return null;
        }

        if (/^[0-9]+$/.test(value)) {
            number = Number(value);

            if (number > 0 && number < 100000000000) {
                number = number * 1000;
            }

            parsed = new Date(number);
        } else {
            parsed = new Date(value);
        }

        return isNaN(parsed.getTime())
            ? null
            : parsed;
    }

    function collectCheckTimes(
        value,
        result,
        depth
    ) {
        var key;
        var candidate;

        if (depth > 10 || value === null) {
            return;
        }

        if (Object.prototype.toString.call(value) ===
            "[object Array]") {
            value.forEach(function (item) {
                collectCheckTimes(
                    item,
                    result,
                    depth + 1
                );
            });

            return;
        }

        if (typeof value !== "object") {
            return;
        }

        for (key in value) {
            if (!Object.prototype.hasOwnProperty.call(
                value,
                key
            )) {
                continue;
            }

            if (
                /^(lastCheckedAt|checkedAt|lastCheckAt|lastCheck|last_checked_at)$/i
                    .test(key)
            ) {
                candidate = parseTimestamp(value[key]);

                if (candidate) {
                    result.push(candidate);
                }
            }

            if (
                value[key] &&
                typeof value[key] === "object"
            ) {
                collectCheckTimes(
                    value[key],
                    result,
                    depth + 1
                );
            }
        }
    }

    function latestCheckTime(payload) {
        var times = [];
        var latest = null;

        collectCheckTimes(
            unwrap(payload),
            times,
            0
        );

        times.forEach(function (date) {
            if (
                !latest ||
                date.getTime() > latest.getTime()
            ) {
                latest = date;
            }
        });

        return latest;
    }

    function stateFromText(value) {
        var normalized = trim(value).toLowerCase();

        if (!normalized) {
            return null;
        }

        if (
            normalized.indexOf("не провер") !== -1 ||
            normalized.indexOf("нет данных") !== -1
        ) {
            return "unchecked";
        }

        if (
            normalized.indexOf("проверяется") !== -1 ||
            normalized === "проверка" ||
            normalized.indexOf("проверяем") !== -1
        ) {
            return "checking";
        }

        if (
            normalized.indexOf("недоступ") !== -1 ||
            normalized.indexOf("ошиб") !== -1 ||
            normalized.indexOf("таймаут") !== -1 ||
            normalized.indexOf("отказ") !== -1
        ) {
            return "unavailable";
        }

        if (
            normalized.indexOf("доступен") !== -1 ||
            normalized.indexOf("доступна") !== -1 ||
            normalized.indexOf("успеш") !== -1
        ) {
            return "available";
        }

        return null;
    }

    function stateTitle(state) {
        switch (state) {
        case "unchecked":
            return "Не проверялся";

        case "checking":
            return "Проверяется";

        case "available":
            return "Доступен";

        case "unavailable":
            return "Недоступен";

        default:
            return "";
        }
    }

    function removeStateClasses(element) {
        [
            "servers-state-unchecked",
            "servers-state-checking",
            "servers-state-available",
            "servers-state-unavailable"
        ].forEach(function (className) {
            element.classList.remove(className);
        });
    }

    function applyState(element, state) {
        var title;

        if (!element || !state) {
            return;
        }

        title = stateTitle(state);

        removeStateClasses(element);

        element.classList.add(
            "servers-state-" + state
        );

        element.setAttribute(
            "data-server-state",
            state
        );

        element.setAttribute(
            "title",
            title
        );

        element.setAttribute(
            "aria-label",
            title
        );

        if (trim(element.textContent) !== title) {
            element.textContent = title;
        }
    }

    function statusElements(root) {
        if (!root) {
            return [];
        }

        return root.querySelectorAll(
            [
                ".status-badge",
                ".server-status-badge",
                ".server-card-status",
                "[class*=\"server-status-\"]",
                "[data-server-status]"
            ].join(",")
        );
    }

    function normalizeStatuses() {
        var root;
        var elements;

        if (normalizing) {
            return;
        }

        root =
            document.getElementById("servers-list") ||
            document.querySelector(".servers-list");

        if (!root) {
            return;
        }

        normalizing = true;
        elements = statusElements(root);

        Array.prototype.forEach.call(
            elements,
            function (element) {
                var currentText =
                    trim(element.textContent);

                var state =
                    element.getAttribute(
                        "data-server-state"
                    ) ||
                    stateFromText(currentText);

                if (state) {
                    applyState(element, state);
                }
            }
        );

        normalizing = false;
    }

    function findStatusInCard(card) {
        var candidates;

        if (!card) {
            return null;
        }

        candidates = statusElements(card);

        return candidates.length
            ? candidates[0]
            : null;
    }

    function createLegendItem(
        state,
        title
    ) {
        var item = document.createElement("span");
        var dot = document.createElement("span");
        var label = document.createElement("span");

        item.className =
            "servers-legend-item " +
            "servers-state-" +
            state;

        dot.className = "servers-legend-dot";
        dot.setAttribute("aria-hidden", "true");

        label.textContent = title;

        item.appendChild(dot);
        item.appendChild(label);

        return item;
    }

    function ensureCompletionPanel() {
        var refresh;
        var section;
        var heading;
        var panel;
        var times;
        var updated;
        var checked;
        var note;
        var legend;

        refresh =
            document.getElementById(
                "refresh-servers"
            );

        if (!refresh) {
            return;
        }

        if (
            refresh.textContent !==
            "Обновить данные"
        ) {
            refresh.textContent =
                "Обновить данные";
        }

        refresh.setAttribute(
            "title",
            "Перечитать сохранённые данные"
        );

        refresh.setAttribute(
            "aria-label",
            "Обновить данные списка серверов"
        );

        panel =
            document.getElementById(
                "servers-completion-panel"
            );

        if (panel) {
            return;
        }

        section = closest(
            refresh,
            ".servers-section"
        );

        if (!section) {
            return;
        }

        heading =
            section.querySelector(
                ".servers-section-heading"
            );

        if (!heading) {
            return;
        }

        panel = document.createElement("div");
        panel.id = "servers-completion-panel";
        panel.className =
            "servers-completion-panel";
        panel.setAttribute(
            "aria-live",
            "polite"
        );

        times = document.createElement("div");
        times.className = "servers-data-times";

        updated = document.createElement("span");
        updated.id = "servers-data-updated";
        updated.textContent =
            "Данные загружены: —";

        checked = document.createElement("span");
        checked.id = "servers-last-checked";
        checked.textContent =
            "Последняя проверка: —";

        times.appendChild(updated);
        times.appendChild(checked);

        note = document.createElement("p");
        note.className = "servers-refresh-note";
        note.textContent =
            "«Обновить данные» перечитывает " +
            "сохранённое состояние. Новую проверку " +
            "серверов запускают кнопки «Проверить».";

        legend = document.createElement("div");
        legend.className =
            "servers-status-legend";
        legend.setAttribute(
            "aria-label",
            "Состояния проверки серверов"
        );

        legend.appendChild(
            createLegendItem(
                "unchecked",
                "Не проверялся"
            )
        );

        legend.appendChild(
            createLegendItem(
                "checking",
                "Проверяется"
            )
        );

        legend.appendChild(
            createLegendItem(
                "available",
                "Доступен"
            )
        );

        legend.appendChild(
            createLegendItem(
                "unavailable",
                "Недоступен"
            )
        );

        panel.appendChild(times);
        panel.appendChild(note);
        panel.appendChild(legend);

        if (heading.nextSibling) {
            heading.parentNode.insertBefore(
                panel,
                heading.nextSibling
            );
        } else {
            heading.parentNode.appendChild(panel);
        }
    }

    function replaceHeaderLabel() {
        var scope;
        var candidates;

        scope = document.querySelector(
            ".workspace-header-primary"
        );

        if (!scope) {
            return;
        }

        candidates = scope.querySelectorAll(
            [
                ".workspace-kicker",
                ".workspace-header-label",
                "small",
                "span"
            ].join(",")
        );

        Array.prototype.some.call(
            candidates,
            function (element) {
                var value =
                    trim(element.textContent)
                        .toLowerCase();

                if (value === "система") {
                    element.textContent = "Серверы";
                    return true;
                }

                return false;
            }
        );
    }

    function handleSummary(payload) {
        var checkedAt;

        lastSummaryPayload = payload;

        setText(
            "servers-data-updated",
            "Данные загружены: " +
            formatDate(new Date())
        );

        checkedAt = latestCheckTime(payload);

        if (checkedAt) {
            setText(
                "servers-last-checked",
                "Последняя проверка: " +
                formatDate(checkedAt)
            );
        } else {
            setText(
                "servers-last-checked",
                "Последняя проверка: нет данных"
            );
        }

        window.setTimeout(
            normalizeStatuses,
            0
        );

        window.setTimeout(
            normalizeStatuses,
            120
        );

        window.setTimeout(
            normalizeStatuses,
            450
        );
    }

    function wrapFetch() {
        var nativeFetch;

        if (
            !window.fetch ||
            window.__brorayServersFetchWrapped
        ) {
            return;
        }

        nativeFetch = window.fetch;

        window.fetch = function () {
            var args = arguments;
            var input = args[0];
            var url = "";

            if (typeof input === "string") {
                url = input;
            } else if (input && input.url) {
                url = input.url;
            }

            return nativeFetch.apply(
                window,
                args
            ).then(function (response) {
                if (
                    url.indexOf(SUMMARY_PATH) !== -1
                ) {
                    try {
                        response
                            .clone()
                            .json()
                            .then(handleSummary)
                            .catch(function () {
                                return;
                            });
                    } catch (error) {
                        return response;
                    }
                }

                return response;
            });
        };

        window.__brorayServersFetchWrapped =
            true;
    }

    function handleClick(event) {
        var button;
        var card;
        var status;

        button = closest(
            event.target,
            "button"
        );

        if (!button) {
            return;
        }

        if (button.id === "refresh-servers") {
            setText(
                "servers-data-updated",
                "Обновление данных…"
            );

            return;
        }

        if (
            trim(button.textContent)
                .toLowerCase()
                .indexOf("провер") === -1
        ) {
            return;
        }

        card = closest(
            button,
            ".server-card"
        );

        if (!card) {
            return;
        }

        status = findStatusInCard(card);

        if (status) {
            applyState(
                status,
                "checking"
            );
        }
    }

    function startObserver() {
        var target =
            document.getElementById("servers-list") ||
            document.querySelector(".servers-list") ||
            document.querySelector(".servers-page");

        if (
            !target ||
            !window.MutationObserver ||
            observer
        ) {
            return;
        }

        observer = new MutationObserver(
            function () {
                ensureCompletionPanel();
                normalizeStatuses();
            }
        );

        observer.observe(
            target,
            {
                childList: true,
                subtree: true,
                characterData: true
            }
        );
    }

    function initialize() {
        replaceHeaderLabel();
        ensureCompletionPanel();
        normalizeStatuses();
        startObserver();

        document.addEventListener(
            "click",
            handleClick,
            false
        );

        if (lastSummaryPayload) {
            handleSummary(lastSummaryPayload);
        }
    }

    wrapFetch();

    if (document.readyState === "loading") {
        document.addEventListener(
            "DOMContentLoaded",
            initialize
        );
    } else {
        initialize();
    }
})();
