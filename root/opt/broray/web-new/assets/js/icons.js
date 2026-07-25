(function () {
    "use strict";

    var SVG_NS = "http://www.w3.org/2000/svg";
    var XLINK_NS = "http://www.w3.org/1999/xlink";
    var SPRITE_ID = "broray-inline-icon-sprite";
    var SPRITE = "#icon-";
    var SPRITE_MARKUP = "<svg xmlns=\"http://www.w3.org/2000/svg\">\n  <symbol id=\"icon-home\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M3 10.5L12 3l9 7.5\"/>\n    <path d=\"M5.5 9.5V20h13V9.5\"/>\n    <path d=\"M10 20v-6h4v6\"/>\n  </symbol>\n  <symbol id=\"icon-xray\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M5 4l14 16\"/>\n    <path d=\"M19 4L5 20\"/>\n    <path d=\"M8.5 4H13l-3 3\"/>\n    <path d=\"M11 17l3 3h4.5\"/>\n  </symbol>\n  <symbol id=\"icon-servers\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <rect x=\"4\" y=\"4\" width=\"16\" height=\"4.5\" rx=\"1.5\"/>\n    <rect x=\"4\" y=\"9.75\" width=\"16\" height=\"4.5\" rx=\"1.5\"/>\n    <rect x=\"4\" y=\"15.5\" width=\"16\" height=\"4.5\" rx=\"1.5\"/>\n    <path d=\"M7 6.25h.01\"/>\n    <path d=\"M7 12h.01\"/>\n    <path d=\"M7 17.75h.01\"/>\n  </symbol>\n  <symbol id=\"icon-subscriptions\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M12 3v11\"/>\n    <path d=\"M8 10l4 4 4-4\"/>\n    <path d=\"M4 16.5V20h16v-3.5\"/>\n    <path d=\"M4 20h16\"/>\n  </symbol>\n  <symbol id=\"icon-keenetic\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <rect x=\"4\" y=\"10.5\" width=\"16\" height=\"6.5\" rx=\"2\"/>\n    <path d=\"M8 10V5\"/>\n    <path d=\"M16 10V5\"/>\n    <path d=\"M7 13.75h.01\"/>\n    <path d=\"M10 13.75h.01\"/>\n    <path d=\"M13 13.75h.01\"/>\n  </symbol>\n  <symbol id=\"icon-routes\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M8 20V4\"/>\n    <path d=\"M8 7h8l3-3v6l-3-3H8\"/>\n    <path d=\"M8 17h6l3-3v6l-3-3H8\"/>\n  </symbol>\n  <symbol id=\"icon-proxy\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <circle cx=\"10.5\" cy=\"10.5\" r=\"6.5\"/>\n    <path d=\"M4.5 10.5h12\"/>\n    <path d=\"M10.5 4a10 10 0 0 0 0 13\"/>\n    <path d=\"M10.5 4a10 10 0 0 1 0 13\"/>\n    <path d=\"M17 14.5l2 2.5 3-4\"/>\n  </symbol>\n  <symbol id=\"icon-status\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <rect x=\"3.5\" y=\"4.5\" width=\"17\" height=\"12\" rx=\"2\"/>\n    <path d=\"M8 13l3-3 2.5 2.5L17 9\"/>\n    <path d=\"M8 19.5h8\"/>\n  </symbol>\n  <symbol id=\"icon-connection\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M3 12h3l2-5 4 10 2-5h7\"/>\n  </symbol>\n  <symbol id=\"icon-security\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M12 3l7 3v5c0 4.5-3.2 8-7 10-3.8-2-7-5.5-7-10V6l7-3z\"/>\n    <path d=\"M9.5 12.5l1.8 1.8 3.7-4.1\"/>\n  </symbol>\n  <symbol id=\"icon-logs\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M7 3.5h7l4 4V20a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 6 20V5a1.5 1.5 0 0 1 1-1.5z\"/>\n    <path d=\"M14 3.5V8h4\"/>\n    <path d=\"M9 12h6\"/>\n    <path d=\"M9 16h6\"/>\n  </symbol>\n  <symbol id=\"icon-update\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M18 8a7 7 0 0 0-12-2\"/>\n    <path d=\"M6 6V10H2\"/>\n    <path d=\"M6 16a7 7 0 0 0 12 2\"/>\n    <path d=\"M18 18v-4h4\"/>\n  </symbol>\n  <symbol id=\"icon-backup\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M8 17H6.5A3.5 3.5 0 1 1 7.3 10a5.5 5.5 0 0 1 10.6 1.5A3.5 3.5 0 1 1 18.5 17H16\"/>\n    <path d=\"M12 17V9\"/>\n    <path d=\"M8.5 12.5L12 9l3.5 3.5\"/>\n  </symbol>\n  <symbol id=\"icon-restore\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M8 17H6.5A3.5 3.5 0 1 1 7.3 10a5.5 5.5 0 0 1 10.6 1.5A3.5 3.5 0 1 1 18.5 17H16\"/>\n    <path d=\"M12 9v8\"/>\n    <path d=\"M8.5 13.5L12 17l3.5-3.5\"/>\n  </symbol>\n  <symbol id=\"icon-integrations\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <circle cx=\"12\" cy=\"5\" r=\"1.5\"/>\n    <circle cx=\"6\" cy=\"16\" r=\"1.5\"/>\n    <circle cx=\"18\" cy=\"16\" r=\"1.5\"/>\n    <path d=\"M12 6.5v4\"/>\n    <path d=\"M12 10.5L7.5 14\"/>\n    <path d=\"M12 10.5l4.5 3.5\"/>\n  </symbol>\n  <symbol id=\"icon-speed\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M5 18a7 7 0 1 1 14 0\"/>\n    <path d=\"M12 12l4-3\"/>\n    <path d=\"M8 16h.01\"/>\n    <path d=\"M12 14h.01\"/>\n    <path d=\"M16 16h.01\"/>\n  </symbol>\n  <symbol id=\"icon-user\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <circle cx=\"12\" cy=\"8\" r=\"3.5\"/>\n    <path d=\"M5 20a7 7 0 0 1 14 0\"/>\n  </symbol>\n  <symbol id=\"icon-settings\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <circle cx=\"12\" cy=\"12\" r=\"2.5\"/>\n    <path d=\"M12 4v2\"/>\n    <path d=\"M12 18v2\"/>\n    <path d=\"M4 12h2\"/>\n    <path d=\"M18 12h2\"/>\n    <path d=\"M6.3 6.3l1.4 1.4\"/>\n    <path d=\"M16.3 16.3l1.4 1.4\"/>\n    <path d=\"M17.7 6.3l-1.4 1.4\"/>\n    <path d=\"M7.7 16.3l-1.4 1.4\"/>\n  </symbol>\n  <symbol id=\"icon-notifications\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M8 18h8\"/>\n    <path d=\"M9 18a3 3 0 0 0 6 0\"/>\n    <path d=\"M6.5 16c1-1 1.5-2.6 1.5-4.5V10a4 4 0 1 1 8 0v1.5c0 1.9.5 3.5 1.5 4.5\"/>\n  </symbol>\n  <symbol id=\"icon-theme\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M19 13.5A7.5 7.5 0 1 1 10.5 5 6 6 0 0 0 19 13.5z\"/>\n  </symbol>\n  <symbol id=\"icon-sun\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <circle cx=\"12\" cy=\"12\" r=\"4\"/>\n    <path d=\"M12 2v2\"/>\n    <path d=\"M12 20v2\"/>\n    <path d=\"M4.9 4.9l1.4 1.4\"/>\n    <path d=\"M17.7 17.7l1.4 1.4\"/>\n    <path d=\"M2 12h2\"/>\n    <path d=\"M20 12h2\"/>\n    <path d=\"M4.9 19.1l1.4-1.4\"/>\n    <path d=\"M17.7 6.3l1.4-1.4\"/>\n  </symbol>\n  <symbol id=\"icon-access\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <rect x=\"6\" y=\"11\" width=\"12\" height=\"9\" rx=\"2\"/>\n    <path d=\"M9 11V8.5a3 3 0 1 1 6 0V11\"/>\n  </symbol>\n  <symbol id=\"icon-license\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <circle cx=\"8.5\" cy=\"11.5\" r=\"3.5\"/>\n    <path d=\"M11.2 14.2L19 22\"/>\n    <path d=\"M12 10h5\"/>\n    <path d=\"M15 10v2\"/>\n    <path d=\"M17 10v1\"/>\n  </symbol>\n  <symbol id=\"icon-help\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <circle cx=\"12\" cy=\"12\" r=\"9\"/>\n    <path d=\"M9.5 9a2.8 2.8 0 1 1 4.8 2c-.8.8-1.8 1.4-1.8 3\"/>\n    <path d=\"M12 17h.01\"/>\n  </symbol>\n  <symbol id=\"icon-exit\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M10 5H5.5A1.5 1.5 0 0 0 4 6.5v11A1.5 1.5 0 0 0 5.5 19H10\"/>\n    <path d=\"M13 8l5 4-5 4\"/>\n    <path d=\"M8 12h10\"/>\n  </symbol>\n  <symbol id=\"icon-add\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <circle cx=\"12\" cy=\"12\" r=\"8.5\"/>\n    <path d=\"M12 8v8\"/>\n    <path d=\"M8 12h8\"/>\n  </symbol>\n  <symbol id=\"icon-edit\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M4 20h4l10-10-4-4L4 16v4z\"/>\n    <path d=\"M12.5 7.5l4 4\"/>\n  </symbol>\n  <symbol id=\"icon-delete\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M4.5 7h15\"/>\n    <path d=\"M9 7V4.5h6V7\"/>\n    <path d=\"M7 7l1 13h8l1-13\"/>\n    <path d=\"M10 11v5\"/>\n    <path d=\"M14 11v5\"/>\n  </symbol>\n  <symbol id=\"icon-search\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <circle cx=\"10.5\" cy=\"10.5\" r=\"5.5\"/>\n    <path d=\"M15 15l4 4\"/>\n  </symbol>\n  <symbol id=\"icon-filter\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M4 5h16l-6 7v5l-4 2v-7L4 5z\"/>\n  </symbol>\n  <symbol id=\"icon-sort\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M7 5v14\"/>\n    <path d=\"M4.5 7.5L7 5l2.5 2.5\"/>\n    <path d=\"M12 7h8\"/>\n    <path d=\"M12 12h6\"/>\n    <path d=\"M12 17h4\"/>\n  </symbol>\n  <symbol id=\"icon-menu\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M4 6h16\"/>\n    <path d=\"M4 12h16\"/>\n    <path d=\"M4 18h16\"/>\n  </symbol>\n  <symbol id=\"icon-chevron\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M15 5l-7 7 7 7\"/>\n  </symbol>\n  <symbol id=\"icon-close\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M5 5l14 14\"/>\n    <path d=\"M19 5L5 19\"/>\n  </symbol>\n  <symbol id=\"icon-visibility\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2.5\" stroke-linecap=\"round\" stroke-linejoin=\"round\">\n    <path d=\"M3 12s3.5-6 9-6 9 6 9 6-3.5 6-9 6-9-6-9-6z\"/>\n    <circle cx=\"12\" cy=\"12\" r=\"2.5\"/>\n  </symbol>\n</svg>\n";
    var observer = null;

    function ensureSprite() {
        var host;
        var sprite;

        if (!document || !document.body) {
            return;
        }

        if (document.getElementById(SPRITE_ID)) {
            return;
        }

        host = document.createElement("div");
        host.setAttribute("aria-hidden", "true");
        host.className = "icon-sprite-host";
        host.innerHTML = SPRITE_MARKUP;
        sprite = host.firstElementChild;

        if (!sprite) {
            return;
        }

        sprite.setAttribute("id", SPRITE_ID);
        sprite.setAttribute("focusable", "false");
        document.body.insertBefore(
            host,
            document.body.firstChild
        );
    }

    function create(name) {
        var svg = document.createElementNS(
            SVG_NS,
            "svg"
        );
        var use = document.createElementNS(
            SVG_NS,
            "use"
        );
        var href = SPRITE + name;

        ensureSprite();
        svg.setAttribute("class", "ui-icon");
        svg.setAttribute("viewBox", "0 0 24 24");
        svg.setAttribute("aria-hidden", "true");
        svg.setAttribute("focusable", "false");
        svg.setAttribute(
            "data-broray-icon",
            name
        );

        use.setAttribute("href", href);
        use.setAttributeNS(XLINK_NS, "xlink:href", href);
        svg.appendChild(use);

        return svg;
    }

    function directIcon(element) {
        var child = element.firstElementChild;

        while (child) {
            if (
                child.hasAttribute &&
                child.hasAttribute("data-broray-icon")
            ) {
                return child;
            }
            child = child.nextElementSibling;
        }

        return null;
    }

    function decorate(element) {
        var name;
        var icon;
        var href;
        var use;

        if (
            !element ||
            !element.getAttribute
        ) {
            return;
        }

        name = element.getAttribute("data-icon");

        if (!name) {
            return;
        }

        icon = directIcon(element);

        if (!icon) {
            if (
                element.classList &&
                element.classList.contains(
                    "sidebar-collapse-symbol"
                )
            ) {
                element.replaceChildren(create(name));
            } else {
                element.appendChild(create(name));
            }
            return;
        }

        if (
            element.classList &&
            element.classList.contains(
                "sidebar-collapse-symbol"
            ) &&
            (
                element.childElementCount !== 1 ||
                element.textContent.trim() !== ""
            )
        ) {
            element.replaceChildren(icon);
        }

        if (
            icon.getAttribute("data-broray-icon") ===
            name
        ) {
            return;
        }

        href = SPRITE + name;
        use = icon.querySelector("use");
        icon.setAttribute("data-broray-icon", name);

        if (use) {
            use.setAttribute("href", href);
            use.setAttributeNS(
                XLINK_NS,
                "xlink:href",
                href
            );
        }
    }

    function scan(root) {
        var elements;

        if (!root) {
            return;
        }

        ensureSprite();

        if (
            root.nodeType === 1 &&
            root.hasAttribute("data-icon")
        ) {
            decorate(root);
        }

        if (!root.querySelectorAll) {
            return;
        }

        elements = root.querySelectorAll(
            "[data-icon]"
        );

        Array.prototype.forEach.call(
            elements,
            decorate
        );
    }

    function mount(element, name) {
        if (!element) {
            return;
        }

        element.setAttribute("data-icon", name);
        decorate(element);
    }

    function start() {
        ensureSprite();
        scan(document);

        if (!window.MutationObserver) {
            return;
        }

        observer = new MutationObserver(
            function (records) {
                records.forEach(function (record) {
                    if (record.type === "attributes") {
                        decorate(record.target);
                        return;
                    }

                    scan(record.target);
                    Array.prototype.forEach.call(
                        record.addedNodes,
                        scan
                    );
                });
            }
        );

        observer.observe(document.body, {
            attributes: true,
            attributeFilter: ["data-icon"],
            childList: true,
            subtree: true
        });
    }

    function stop() {
        if (!observer) {
            return;
        }

        observer.disconnect();
        observer = null;
    }

    window.BROrayIcons = {
        create: create,
        mount: mount,
        scan: scan,
        stop: stop
    };

    window.addEventListener("pagehide", stop);
    window.addEventListener("unload", stop);

    if (document.readyState === "loading") {
        document.addEventListener(
            "DOMContentLoaded",
            start
        );
    } else {
        start();
    }
})();
