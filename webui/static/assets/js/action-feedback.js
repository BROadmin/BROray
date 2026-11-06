/* Audit stage 04: presentation only. No requests or mutations on load. */
(function () {
    "use strict";
    if (window.BROrayActionFeedback) return;
    function text(value) {
        if (value == null) return "";
        if (typeof value === "string") return value;
        try { return JSON.stringify(value, null, 2); } catch (ignore) { return "Не удалось отобразить подробности."; }
    }
    function describe(error, phase) {
        var code = error && error.code;
        var readOnly = phase === "preflight" || phase === "verify";
        var message = readOnly
            ? "Не удалось завершить подготовку маршрутов."
            : "Результат операции пока не подтверждён.";
        if (code === "ROUTES_OPERATION_BUSY" || code === "OPERATION_BUSY" || code === "DOMAIN_OPERATION_BUSY") {
            message = "BROray сообщает, что другая конфликтующая операция уже выполняется.";
        } else if (code === "ROUTES_PREFLIGHT_PLAN_FAILED" || code === "ROUTES_API_LOCK_FAILED") {
            message = "BROray не смог завершить предварительную проверку маршрутов.";
        }
        return {
            title: readOnly ? "Подготовка не завершена" : "Проверьте состояние операции",
            message: message,
            consequence: readOnly
                ? "Запрос на установку или удаление маршрутов из этого окна не отправлен."
                : "Не повторяйте изменяющее действие, пока не проверите его результат в роутере.",
            next: readOnly
                ? "Обновите состояние и повторите подготовку. При повторной ошибке сохраните технические подробности."
                : "Обновите состояние. При необходимости откройте журнал операций на странице BROray.",
            details: [code ? "Код: " + text(code) : "Код причины не получен.",
                error && error.message ? "Сообщение: " + text(error.message) : "",
                error && error.details != null ? text(error.details) : ""].filter(Boolean).join("\n")
        };
    }
    function node(tag, value, className) {
        var item = document.createElement(tag);
        if (value != null) item.textContent = value;
        if (className) item.className = className;
        return item;
    }
    function clear(id) {
        var panel = document.getElementById(id);
        if (panel) { panel.replaceChildren(); panel.hidden = true; }
    }
    function show(id, content, refresh) {
        var host = document.querySelector(".workspace-content");
        var panel = document.getElementById(id);
        if (!host) return;
        if (!panel) {
            panel = node("section", null, "action-feedback ui-card");
            panel.id = id;
            panel.setAttribute("role", "status");
            panel.setAttribute("aria-live", "polite");
            host.insertBefore(panel, host.firstChild);
        }
        panel.replaceChildren();
        panel.hidden = false;
        panel.append(node("h2", content.title), node("p", content.message));
        if (content.consequence) panel.append(node("p", content.consequence));
        if (content.next) panel.append(node("p", content.next));
        var details = node("details", null, "action-feedback-details");
        details.append(node("summary", "Технические подробности"), node("pre", text(content.details)));
        panel.append(details);
        if (typeof refresh === "function") {
            var button = node("button", "Обновить состояние", "button button-secondary");
            button.type = "button";
            button.addEventListener("click", function () {
                if (button.disabled) return;
                button.disabled = true;
                Promise.resolve().then(refresh).catch(function (error) {
                    var note = panel.querySelector(".action-feedback-refresh-error");
                    if (!note) { note = node("p", "", "action-feedback-refresh-error"); panel.append(note); }
                    note.textContent = "Состояние не обновлено: " + text(error && error.message || "нет ответа");
                }).then(function () { button.disabled = false; });
            });
            panel.append(button);
        }
    }
    window.BROrayActionFeedback = {describe: describe, show: show, clear: clear};
})();
