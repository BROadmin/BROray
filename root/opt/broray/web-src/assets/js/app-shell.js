(function () {
    "use strict";

    if (window.BROrayStaticShellInitialized) {
        return;
    }
    window.BROrayStaticShellInitialized = true;

    var MOBILE_BREAKPOINT = 760;
    var STORAGE_KEY = "broray.sidebar.collapsed";
    var currentPage = document.body.getAttribute("data-page") || "home";
    var app = document.getElementById("app");
    var collapseButton = document.getElementById("sidebar-collapse");
    var collapseSymbol = document.getElementById("sidebar-collapse-symbol");
    var mobileButton = document.getElementById("mobile-menu-button");
    var backdrop = document.getElementById("sidebar-backdrop");
    var logoutButton = document.getElementById("logout-button");

    function isMobile() {
        return window.innerWidth <= MOBILE_BREAKPOINT;
    }

    function readCollapsed() {
        try {
            return window.localStorage.getItem(STORAGE_KEY) === "true";
        } catch (error) {
            return false;
        }
    }

    function saveCollapsed(collapsed) {
        try {
            window.localStorage.setItem(STORAGE_KEY, collapsed ? "true" : "false");
        } catch (error) {
            return;
        }
    }

    function setCollapseIcon(name, reversed) {
        if (!collapseSymbol) return;
        collapseSymbol.classList.toggle("is-reversed", Boolean(reversed));
        if (window.BROrayIcons) {
            window.BROrayIcons.mount(collapseSymbol, name);
        } else {
            collapseSymbol.setAttribute("data-icon", name);
        }
    }

    function activateNavigation() {
        var links = document.querySelectorAll(".sidebar-navigation .sidebar-link");
        Array.prototype.forEach.call(links, function (link) {
            var active = link.getAttribute("data-page") === currentPage;
            link.classList.toggle("sidebar-link-active", active);
            if (active) link.setAttribute("aria-current", "page");
            else link.removeAttribute("aria-current");
        });
    }

    function setDesktopCollapsed(collapsed) {
        if (!app) return;
        app.classList.toggle("sidebar-is-collapsed", collapsed);
        if (collapseButton) {
            collapseButton.setAttribute("aria-expanded", collapsed ? "false" : "true");
            collapseButton.setAttribute("aria-label", collapsed ? "Развернуть боковое меню" : "Свернуть боковое меню");
        }
        setCollapseIcon("chevron", collapsed);
        saveCollapsed(collapsed);
    }

    function openMobileMenu() {
        if (!app || !isMobile()) return;
        app.classList.add("sidebar-is-open");
        document.body.classList.add("menu-open");
        if (backdrop) backdrop.hidden = false;
        if (mobileButton) mobileButton.setAttribute("aria-expanded", "true");
        if (collapseButton) collapseButton.setAttribute("aria-label", "Закрыть меню");
        setCollapseIcon("close", false);
    }

    function closeMobileMenu() {
        if (!app) return;
        app.classList.remove("sidebar-is-open");
        document.body.classList.remove("menu-open");
        if (backdrop) backdrop.hidden = true;
        if (mobileButton) mobileButton.setAttribute("aria-expanded", "false");
        if (isMobile()) setCollapseIcon("close", false);
    }

    function syncLayout() {
        if (!app) return;
        if (isMobile()) {
            app.classList.remove("sidebar-is-collapsed");
            closeMobileMenu();
            return;
        }
        closeMobileMenu();
        setDesktopCollapsed(readCollapsed());
    }

    function bindShell() {
        if (!app || app.dataset.staticShellBound === "true") return;
        app.dataset.staticShellBound = "true";
        if (collapseButton) collapseButton.addEventListener("click", function () {
            if (isMobile()) closeMobileMenu();
            else setDesktopCollapsed(!app.classList.contains("sidebar-is-collapsed"));
        });
        if (mobileButton) mobileButton.addEventListener("click", function () {
            if (app.classList.contains("sidebar-is-open")) closeMobileMenu();
            else openMobileMenu();
        });
        if (backdrop) backdrop.addEventListener("click", closeMobileMenu);
        document.addEventListener("keydown", function (event) {
            if (event.key === "Escape") closeMobileMenu();
        });
        window.addEventListener("resize", syncLayout);
        syncLayout();
    }

    function bindLogout() {
        if (!logoutButton || logoutButton.dataset.staticLogoutBound === "true") return;
        logoutButton.dataset.staticLogoutBound = "true";
        logoutButton.addEventListener("click", function () {
            fetch("/api/logout.cgi", {method:"POST", credentials:"same-origin", cache:"no-store"}).then(
                function () { window.location.replace("/"); },
                function () { window.location.replace("/"); }
            );
        });
    }

    activateNavigation();
    bindShell();
    bindLogout();
})();
