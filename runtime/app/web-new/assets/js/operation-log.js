(function () {
    "use strict";

    function prepareOperationLog() {
        const log = document.getElementById("operation-log");

        if (!log) {
            return;
        }

        if (log.closest(".operation-log-details")) {
            return;
        }

        const details = document.createElement("details");
        const summary = document.createElement("summary");
        const summaryText = document.createElement("span");

        details.className = "operation-log-details";

        summary.className = "operation-log-summary";
        summary.setAttribute(
            "aria-label",
            "Показать или скрыть технический журнал"
        );

        summaryText.textContent = "Показать технический журнал";

        summary.appendChild(summaryText);

        details.addEventListener("toggle", function () {
            summaryText.textContent = details.open
                ? "Скрыть технический журнал"
                : "Показать технический журнал";
        });

        log.parentNode.insertBefore(details, log);
        details.appendChild(summary);
        details.appendChild(log);
    }

    if (document.readyState === "loading") {
        document.addEventListener(
            "DOMContentLoaded",
            prepareOperationLog
        );
    } else {
        prepareOperationLog();
    }
})();
