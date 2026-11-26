(function () {
    "use strict";

    var SUMMARY_PATH = "/api/servers/summary.cgi";
    var lastSummaryPayload = null;
    var observer = null;

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

    function createLegendItem(tone, title) {
        var item = document.createElement("span");
        item.className = "status-badge status-" + tone;
        item.textContent = title;
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
            "«Обновить данные» только перечитывает сохранённое состояние и не запускает probes. " +
            "Кнопка «Проверить» выполняет реальный HTTPS-запрос через временный Xray и затем ICMP ping; " +
            "скорость передачи не измеряется. Автовыбор проверяет резервные серверы только после " +
            "подтверждённого отказа активного подключения.";

        legend = document.createElement("div");
        legend.className =
            "servers-status-legend";
        legend.setAttribute(
            "aria-label",
            "Состояния проверки серверов"
        );

        legend.appendChild(
            createLegendItem(
                "unknown",
                "Не проверен"
            )
        );

        legend.appendChild(
            createLegendItem(
                "loading",
                "Проверяется"
            )
        );

        legend.appendChild(
            createLegendItem(
                "success",
                "Доступен"
            )
        );

        legend.appendChild(
            createLegendItem(
                "error",
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
        var button = closest(event.target, "button");
        if (button && button.id === "refresh-servers") {
            setText("servers-data-updated", "Обновление данных…");
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
