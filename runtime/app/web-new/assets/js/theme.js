(function () {
    "use strict";

    var STORAGE_KEY = "broray.theme";

    var definitions = {
        broray: {
            title: "BROray",
            colorScheme: "dark",
            themeColor: "#0b1117",
            indicator: "B",
            icon: null
        },
        night: {
            title: "Ночная",
            colorScheme: "dark",
            themeColor: "#090b0e",
            indicator: null,
            icon: "theme"
        },
        day: {
            title: "Дневная",
            colorScheme: "light",
            themeColor: "#f2f6f9",
            indicator: null,
            icon: "sun"
        }
    };

    function readTheme() {
        var theme = "broray";

        try {
            theme =
                window.localStorage.getItem(STORAGE_KEY) ||
                "broray";
        } catch (error) {
            theme = "broray";
        }

        if (!definitions[theme]) {
            theme = "broray";
        }

        return theme;
    }

    function saveTheme(theme) {
        try {
            window.localStorage.setItem(
                STORAGE_KEY,
                theme
            );
        } catch (error) {
            return;
        }
    }

    function updateMeta(theme) {
        var definition = definitions[theme];
        var themeMeta = document.querySelector(
            'meta[name="theme-color"]'
        );
        var schemeMeta = document.querySelector(
            'meta[name="color-scheme"]'
        );

        if (themeMeta) {
            themeMeta.setAttribute(
                "content",
                definition.themeColor
            );
        }

        if (schemeMeta) {
            schemeMeta.setAttribute(
                "content",
                definition.colorScheme
            );
        }
    }

    function renderIndicator(indicator, theme) {
        var definition = definitions[theme];

        if (!indicator) {
            return;
        }

        indicator.removeAttribute("data-icon");
        indicator.replaceChildren();
        indicator.setAttribute(
            "data-theme-indicator",
            theme
        );

        if (definition.icon) {
            if (window.BROrayIcons) {
                window.BROrayIcons.mount(
                    indicator,
                    definition.icon
                );
            } else {
                indicator.setAttribute(
                    "data-icon",
                    definition.icon
                );
            }
        } else {
            indicator.textContent =
                definition.indicator;
        }
    }

    function updateIndicator(theme) {
        renderIndicator(
            document.querySelector(
                ".theme-control-icon"
            ),
            theme
        );
    }

    function applyTheme(theme, save) {
        if (!definitions[theme]) {
            theme = "broray";
        }

        document.documentElement.setAttribute(
            "data-theme",
            theme
        );

        updateMeta(theme);

        if (save) {
            saveTheme(theme);
        }

        var select =
            document.getElementById("broray-theme-select");

        if (select && select.value !== theme) {
            select.value = theme;
        }

        updateIndicator(theme);
    }

    function createOption(value, title) {
        var option = document.createElement("option");

        option.value = value;
        option.textContent = title;

        return option;
    }

    function createControl() {
        var control = document.createElement("div");
        var label = document.createElement("label");
        var icon = document.createElement("span");
        var labelText = document.createElement("span");
        var select = document.createElement("select");

        control.id = "broray-theme-control";
        control.className = "theme-control";

        label.className = "theme-control-label";
        label.setAttribute(
            "for",
            "broray-theme-select"
        );

        icon.className = "theme-control-icon";
        icon.setAttribute("aria-hidden", "true");

        labelText.className = "theme-control-text";
        labelText.textContent = "Тема";

        select.id = "broray-theme-select";
        select.className = "theme-select";
        select.setAttribute(
            "aria-label",
            "Тема оформления"
        );
        select.title = "Тема оформления";

        select.appendChild(
            createOption("broray", "BROray")
        );

        select.appendChild(
            createOption("night", "Ночная")
        );

        select.appendChild(
            createOption("day", "Дневная")
        );

        select.value = readTheme();
        label.appendChild(icon);
        label.appendChild(labelText);
        label.appendChild(select);
        control.appendChild(label);

        renderIndicator(icon, select.value);

        select.addEventListener(
            "change",
            function () {
                applyTheme(select.value, true);
            }
        );

        return control;
    }

    function mountControl() {
        if (
            document.body.classList.contains(
                "login-body"
            )
        ) {
            return;
        }

        if (
            document.getElementById(
                "broray-theme-control"
            )
        ) {
            return;
        }

        var control = createControl();
        var mount = document.getElementById(
            "theme-control-mount"
        );
        var refresh = document.getElementById(
            "refresh-status"
        );
        var headerActions = document.querySelector(
            ".workspace-header-actions"
        );
        var oldHeaderActions = document.querySelector(
            ".header-actions"
        );
        var header = document.querySelector(
            ".workspace-header"
        );
        var oldHeader = document.querySelector(
            ".app-header"
        );

        if (mount) {
            mount.appendChild(control);
            return;
        }

        if (refresh && refresh.parentNode) {
            refresh.parentNode.insertBefore(
                control,
                refresh
            );
            return;
        }

        if (headerActions) {
            headerActions.appendChild(control);
            return;
        }

        if (oldHeaderActions) {
            oldHeaderActions.appendChild(control);
            return;
        }

        if (header) {
            header.appendChild(control);
            return;
        }

        if (oldHeader) {
            oldHeader.appendChild(control);
            return;
        }

        control.classList.add("theme-control-floating");
        document.body.appendChild(control);
    }

    applyTheme(readTheme(), false);

    if (document.readyState === "loading") {
        document.addEventListener(
            "DOMContentLoaded",
            mountControl
        );
    } else {
        mountControl();
    }

    window.addEventListener(
        "storage",
        function (event) {
            if (event.key === STORAGE_KEY) {
                applyTheme(readTheme(), false);
            }
        }
    );
})();
