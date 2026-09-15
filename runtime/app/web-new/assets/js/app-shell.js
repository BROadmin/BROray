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
    var supportButton = document.getElementById("support-button");
    var supportRoot = document.getElementById("support-root");
    var supportBackdrop = document.getElementById("support-backdrop");
    var supportClose = document.getElementById("support-close");
    var supportPreviousFocus = null;
    var routesToggle = null;
    var routesSubmenu = null;

    function isRoutesPage(page) {
        return page === "routes" || page === "routes-custom" || page === "routes-import";
    }

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

    function createRouteSubLink(page, href, label, icon) {
        var link = document.createElement("a");
        var iconNode = document.createElement("span");
        var labelNode = document.createElement("span");

        link.className = "sidebar-route-submenu-link sidebar-link";
        link.href = href;
        link.setAttribute("data-page", page);
        link.setAttribute("title", label);
        iconNode.className = "sidebar-route-submenu-icon";
        iconNode.setAttribute("data-icon", icon);
        iconNode.setAttribute("aria-hidden", "true");
        labelNode.className = "sidebar-route-submenu-label";
        labelNode.textContent = label;
        link.appendChild(iconNode);
        link.appendChild(labelNode);
        return link;
    }

    function setRoutesExpanded(expanded) {
        var group = routesSubmenu ? routesSubmenu.closest(".sidebar-route-group") : null;
        if (!routesSubmenu || !routesToggle || !group) return;
        group.classList.toggle("is-expanded", Boolean(expanded));
        routesSubmenu.hidden = !expanded;
        routesToggle.setAttribute("aria-expanded", expanded ? "true" : "false");
        routesToggle.setAttribute("aria-label", expanded ? "Свернуть подпункты маршрутов" : "Развернуть подпункты маршрутов");
        if (window.BROrayIcons) window.BROrayIcons.mount(routesToggle, "chevron");
    }

    function enhanceRoutesNavigation() {
        var navigation = document.querySelector(".sidebar-navigation");
        var routeLink = navigation && navigation.querySelector('.sidebar-link[data-page="routes"]');
        var group;
        var row;

        if (!navigation || !routeLink || routeLink.closest(".sidebar-route-group")) return;

        group = document.createElement("div");
        group.className = "sidebar-route-group";
        row = document.createElement("div");
        row.className = "sidebar-route-parent-row";
        routeLink.classList.add("sidebar-route-parent-link");

        routesToggle = document.createElement("button");
        routesToggle.type = "button";
        routesToggle.className = "sidebar-route-toggle";
        routesToggle.setAttribute("data-icon", "chevron");
        routesToggle.setAttribute("aria-controls", "sidebar-routes-submenu");

        routesSubmenu = document.createElement("div");
        routesSubmenu.id = "sidebar-routes-submenu";
        routesSubmenu.className = "sidebar-route-submenu";
        routesSubmenu.appendChild(createRouteSubLink("routes", "/routes.html?v=WebUI-3.1.0-r09c02", "Обзор", "status"));
        routesSubmenu.appendChild(createRouteSubLink("routes-custom", "/routes-custom.html?v=WebUI-3.1.0-r09c02", "Свои маршруты", "backup"));
        routesSubmenu.appendChild(createRouteSubLink("routes-import", "/routes-import.html?v=WebUI-3.1.0-r09c02", "Готовые маршруты", "integrations"));

        routeLink.parentNode.insertBefore(group, routeLink);
        row.appendChild(routeLink);
        row.appendChild(routesToggle);
        group.appendChild(row);
        group.appendChild(routesSubmenu);

        routesToggle.addEventListener("click", function () {
            setRoutesExpanded(!group.classList.contains("is-expanded"));
        });
        group.addEventListener("mouseenter", function () {
            if (!isMobile() && app && app.classList.contains("sidebar-is-collapsed")) {
                setRoutesExpanded(true);
            }
        });
        group.addEventListener("mouseleave", function () {
            if (!isMobile() && app && app.classList.contains("sidebar-is-collapsed")) {
                setRoutesExpanded(false);
            }
        });
        group.addEventListener("focusin", function () {
            if (!isMobile() && app && app.classList.contains("sidebar-is-collapsed")) {
                setRoutesExpanded(true);
            }
        });
        group.addEventListener("focusout", function (event) {
            if (!isMobile() && app && app.classList.contains("sidebar-is-collapsed") &&
                !group.contains(event.relatedTarget)) {
                setRoutesExpanded(false);
            }
        });

        setRoutesExpanded(isRoutesPage(currentPage));
        if (window.BROrayIcons) window.BROrayIcons.scan(group);
    }

    function activateNavigation() {
        var links = document.querySelectorAll(".sidebar-navigation .sidebar-link");
        var parent = document.querySelector(".sidebar-route-parent-link");
        Array.prototype.forEach.call(links, function (link) {
            var page = link.getAttribute("data-page");
            var active = page === currentPage;
            link.classList.toggle("sidebar-link-active", active);
            if (active) link.setAttribute("aria-current", "page");
            else link.removeAttribute("aria-current");
        });
        if (parent) {
            parent.classList.remove("sidebar-link-active");
            parent.removeAttribute("aria-current");
            parent.classList.toggle("sidebar-route-family-active", isRoutesPage(currentPage));
        }
    }

    function setDesktopCollapsed(collapsed) {
        if (!app) return;
        app.classList.toggle("sidebar-is-collapsed", collapsed);
        if (collapseButton) {
            collapseButton.setAttribute("aria-expanded", collapsed ? "false" : "true");
            collapseButton.setAttribute("aria-label", collapsed ? "Развернуть боковое меню" : "Свернуть боковое меню");
        }
        setCollapseIcon("chevron", collapsed);
        if (routesSubmenu) {
            setRoutesExpanded(collapsed ? false : isRoutesPage(currentPage));
        }
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
            if (isRoutesPage(currentPage)) setRoutesExpanded(true);
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
            if (event.key !== "Escape") return;
            if (supportRoot && !supportRoot.hidden) closeSupport();
            else closeMobileMenu();
        });
        window.addEventListener("resize", syncLayout);
        syncLayout();
    }

    function openSupport() {
        if (!supportRoot) return;
        supportPreviousFocus = document.activeElement;
        closeMobileMenu();
        supportRoot.hidden = false;
        document.body.classList.add("modal-open");
        if (supportClose) supportClose.focus();
    }

    function closeSupport() {
        if (!supportRoot || supportRoot.hidden) return;
        supportRoot.hidden = true;
        document.body.classList.remove("modal-open");
        if (supportPreviousFocus && typeof supportPreviousFocus.focus === "function") {
            supportPreviousFocus.focus();
        }
        supportPreviousFocus = null;
    }

    function bindSupport() {
        if (!supportButton || !supportRoot || supportRoot.dataset.staticSupportBound === "true") return;
        supportRoot.dataset.staticSupportBound = "true";
        supportButton.addEventListener("click", openSupport);
        if (supportBackdrop) supportBackdrop.addEventListener("click", closeSupport);
        if (supportClose) supportClose.addEventListener("click", closeSupport);
    }

    function bindLogout() {
        if (!logoutButton || logoutButton.dataset.staticLogoutBound === "true") return;
        logoutButton.dataset.staticLogoutBound = "true";
        logoutButton.addEventListener("click", function () {
            fetch("/api/logout.cgi", {method:"POST", credentials:"same-origin", cache:"no-store", body:"{}", headers:{"Content-Type":"application/json"}}).then(
                function () { window.location.replace("/"); },
                function () { window.location.replace("/"); }
            );
        });
    }

    enhanceRoutesNavigation();
    activateNavigation();
    bindShell();
    bindSupport();
    bindLogout();
})();
