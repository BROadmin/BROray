(function () {
    "use strict";

    var STORAGE_KEY = "broray-theme";
    var root = document.documentElement;

    function savedTheme() {
        try {
            return localStorage.getItem(STORAGE_KEY);
        } catch (error) {
            return null;
        }
    }

    function storeTheme(theme) {
        try {
            localStorage.setItem(STORAGE_KEY, theme);
        } catch (error) {
            /* localStorage может быть недоступен */
        }
    }

    function applyTheme(theme) {
        var selected = theme === "dark" ? "dark" : "light";

        root.setAttribute("data-theme", selected);

        var button = document.getElementById("theme-toggle");

        if (button) {
            if (selected === "dark") {
                button.textContent = "☀️";
                button.title = "Включить светлую тему";
                button.setAttribute(
                    "aria-label",
                    "Включить светлую тему"
                );
            } else {
                button.textContent = "🌙";
                button.title = "Включить тёмную тему";
                button.setAttribute(
                    "aria-label",
                    "Включить тёмную тему"
                );
            }
        }
    }

    function toggleTheme() {
        var current = root.getAttribute("data-theme");
        var next = current === "dark" ? "light" : "dark";

        storeTheme(next);
        applyTheme(next);
    }

    function createButton() {
        var header = document.querySelector("header");

        if (!header || document.getElementById("theme-toggle")) {
            return;
        }

        var button = document.createElement("button");

        button.id = "theme-toggle";
        button.className = "theme-toggle";
        button.type = "button";
        button.addEventListener("click", toggleTheme);

        header.appendChild(button);
    }

    applyTheme(savedTheme() === "dark" ? "dark" : "light");

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", function () {
            createButton();
            applyTheme(
                root.getAttribute("data-theme")
            );
        });
    } else {
        createButton();
        applyTheme(root.getAttribute("data-theme"));
    }
})();
