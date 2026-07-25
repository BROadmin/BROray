(function () {
    "use strict";

    var theme = "broray";

    try {
        theme =
            window.localStorage.getItem("broray.theme") ||
            "broray";
    } catch (error) {
        theme = "broray";
    }

    if (
        theme !== "broray" &&
        theme !== "night" &&
        theme !== "day"
    ) {
        theme = "broray";
    }

    document.documentElement.setAttribute(
        "data-theme",
        theme
    );
})();
