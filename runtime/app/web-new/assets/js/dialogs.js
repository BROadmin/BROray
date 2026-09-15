(function () {
    "use strict";

    if (window.BROrayDialogs) {
        return;
    }

    var root = document.getElementById("confirm-root");
    var backdrop = document.getElementById("confirm-backdrop");
    var dialog = root ? root.querySelector(".modal") : null;
    var eyebrow = document.getElementById("confirm-eyebrow");
    var title = document.getElementById("confirm-title");
    var message = document.getElementById("confirm-message");
    var inputGroup = document.getElementById("confirm-input-group");
    var inputLabel = document.getElementById("confirm-input-label");
    var input = document.getElementById("confirm-input");
    var inputHint = document.getElementById("confirm-input-hint");
    var inputError = document.getElementById("confirm-input-error");
    var cancelButton = document.getElementById("confirm-cancel");
    var acceptButton = document.getElementById("confirm-accept");
    var app = document.getElementById("app");
    var pendingResolve = null;
    var returnFocus = null;
    var open = false;
    var locked = false;
    var mode = "confirm";
    var settings = {};

    function unavailable() {
        return !root || !dialog || !title || !message ||
            !inputGroup || !inputLabel || !input || !inputHint ||
            !inputError || !cancelButton || !acceptButton;
    }

    function focusableElements() {
        if (!root) {
            return [];
        }

        return Array.prototype.filter.call(
            root.querySelectorAll(
                'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
            ),
            function (element) {
                return !element.hidden &&
                    !element.classList.contains("modal-backdrop");
            }
        );
    }

    function cancelValue() {
        return mode === "prompt" ? null : false;
    }

    function setApplicationInert(value) {
        if (!app) {
            return;
        }

        if ("inert" in app) {
            app.inert = Boolean(value);
        }

        if (value) {
            app.setAttribute("aria-hidden", "true");
        } else {
            app.removeAttribute("aria-hidden");
        }
    }

    function clearInputState() {
        input.value = "";
        input.removeAttribute("placeholder");
        input.removeAttribute("aria-invalid");
        inputHint.hidden = true;
        inputHint.textContent = "";
        inputError.hidden = true;
        inputError.textContent = "";
    }

    function finish(result) {
        var resolve;

        if (!open || locked || !root) {
            return;
        }

        open = false;
        root.hidden = true;
        root.removeAttribute("data-variant");
        root.removeAttribute("data-mode");
        root.classList.remove("is-locked");
        acceptButton.removeAttribute("data-icon");
        document.body.classList.remove("modal-open");
        setApplicationInert(false);

        resolve = pendingResolve;
        pendingResolve = null;
        clearInputState();
        cancelButton.hidden = false;
        cancelButton.disabled = false;
        acceptButton.disabled = false;

        if (resolve) {
            resolve(result);
        }

        if (returnFocus && typeof returnFocus.focus === "function") {
            returnFocus.focus();
        }

        returnFocus = null;
        mode = "confirm";
        settings = {};
    }

    function validationMessage() {
        var value = input.value;

        if (mode === "phrase") {
            return value === String(settings.phrase || "")
                ? ""
                : (settings.mismatchText || "Контрольная фраза не совпадает.");
        }

        if (mode === "prompt" && settings.required && !value.trim()) {
            return settings.requiredText || "Введите значение.";
        }

        return "";
    }

    function syncInputValidation(showError) {
        var error = validationMessage();
        var valid = error === "";

        if (mode === "phrase" || (mode === "prompt" && settings.required)) {
            acceptButton.disabled = locked || !valid;
        } else {
            acceptButton.disabled = locked;
        }

        input.setAttribute("aria-invalid", valid ? "false" : "true");
        inputError.textContent = error;
        inputError.hidden = !showError || valid;

        return valid;
    }

    function configureInput() {
        var usesInput = mode === "prompt" || mode === "phrase";

        inputGroup.hidden = !usesInput;
        clearInputState();

        if (!usesInput) {
            return;
        }

        inputLabel.textContent = settings.inputLabel || (
            mode === "phrase" ? "Контрольная фраза" : "Значение"
        );
        input.value = settings.defaultValue || "";

        if (settings.placeholder) {
            input.setAttribute("placeholder", settings.placeholder);
        }

        if (mode === "phrase") {
            inputHint.textContent = settings.inputHint ||
                "Введите точно: " + String(settings.phrase || "");
            inputHint.hidden = false;
        } else if (settings.inputHint) {
            inputHint.textContent = settings.inputHint;
            inputHint.hidden = false;
        }

        syncInputValidation(false);
    }

    function openDialog(options, requestedMode) {
        var variant;

        if (unavailable()) {
            return Promise.reject(new Error("Окно BROray недоступно."));
        }

        if (pendingResolve) {
            pendingResolve(cancelValue());
            pendingResolve = null;
        }

        settings = options || {};
        mode = requestedMode;
        variant = settings.variant === "danger" ? "danger" : "primary";
        returnFocus = document.activeElement;
        locked = false;
        open = true;

        root.hidden = false;
        root.setAttribute("data-variant", variant);
        root.setAttribute("data-mode", mode);
        document.body.classList.add("modal-open");
        setApplicationInert(true);

        eyebrow.textContent = settings.eyebrow || (
            mode === "info" ? "Информация" : "Подтверждение"
        );
        title.textContent = settings.title || (
            mode === "info" ? "BROray" : "Подтвердите действие"
        );
        message.textContent = settings.message || (
            mode === "info" ? "Операция завершена." : "Продолжить операцию?"
        );
        cancelButton.textContent = settings.cancelText || "Отмена";
        acceptButton.textContent = settings.confirmText || (
            mode === "info" ? "Закрыть" : "Продолжить"
        );
        acceptButton.className = "button " + (
            variant === "danger" ? "button-danger" : "button-primary"
        );
        acceptButton.setAttribute(
            "data-icon",
            settings.icon || (variant === "danger" ? "delete" : "security")
        );
        cancelButton.hidden = mode === "info";
        configureInput();

        window.setTimeout(function () {
            if (mode === "prompt" || mode === "phrase") {
                input.focus();
                input.select();
            } else {
                acceptButton.focus();
            }
        }, 0);

        return new Promise(function (resolve) {
            pendingResolve = resolve;
        });
    }

    function confirm(options) {
        return openDialog(options, "confirm");
    }

    function confirmPhrase(options) {
        var configured = Object.assign({}, options || {});

        if (!configured.phrase) {
            return Promise.reject(new Error("Не указана контрольная фраза."));
        }

        configured.variant = "danger";
        return openDialog(configured, "phrase");
    }

    function promptValue(options) {
        return openDialog(options, "prompt");
    }

    function inform(options) {
        return openDialog(options, "info");
    }

    function setLocked(value, busyText) {
        locked = Boolean(value);
        root.classList.toggle("is-locked", locked);
        cancelButton.disabled = locked;

        if (busyText && locked) {
            acceptButton.dataset.previousText = acceptButton.textContent;
            acceptButton.textContent = busyText;
        } else if (!locked && acceptButton.dataset.previousText) {
            acceptButton.textContent = acceptButton.dataset.previousText;
            delete acceptButton.dataset.previousText;
        }

        syncInputValidation(false);
    }

    function accept() {
        var value;

        if (locked) {
            return;
        }

        if ((mode === "prompt" || mode === "phrase") &&
            !syncInputValidation(true)) {
            input.focus();
            return;
        }

        if (mode === "prompt") {
            value = input.value;
            finish(value);
            return;
        }

        finish(true);
    }

    function onKeyDown(event) {
        var items;
        var first;
        var last;

        if (!open) {
            return;
        }

        if (event.key === "Escape") {
            event.preventDefault();
            if (!locked) {
                finish(cancelValue());
            }
            return;
        }

        if (event.key === "Enter" &&
            document.activeElement === input &&
            (mode === "prompt" || mode === "phrase")) {
            event.preventDefault();
            accept();
            return;
        }

        if (event.key !== "Tab") {
            return;
        }

        items = focusableElements();
        if (!items.length) {
            event.preventDefault();
            return;
        }

        first = items[0];
        last = items[items.length - 1];

        if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first.focus();
        }
    }

    cancelButton.addEventListener("click", function () {
        finish(cancelValue());
    });

    acceptButton.addEventListener("click", accept);

    backdrop.addEventListener("click", function () {
        if (!locked) {
            finish(cancelValue());
        }
    });

    input.addEventListener("input", function () {
        syncInputValidation(false);
    });

    document.addEventListener("keydown", onKeyDown);

    window.BROrayDialogs = {
        confirm: confirm,
        confirmPhrase: confirmPhrase,
        prompt: promptValue,
        inform: inform,
        close: finish,
        setLocked: setLocked
    };
})();
