(function () {
    "use strict";

    const form = document.getElementById("login-form");
    const loginInput = document.getElementById("login");
    const passwordInput = document.getElementById("password");
    const submitButton = document.getElementById("login-submit");
    const errorBox = document.getElementById("login-error");
    const passwordToggle =
        document.getElementById("password-toggle");

    function setLoading(loading) {
        submitButton.disabled = loading;
        loginInput.disabled = loading;
        passwordInput.disabled = loading;
        submitButton.classList.toggle("is-loading", loading);
    }

    function showError(message) {
        errorBox.textContent = message;
        errorBox.hidden = false;
    }

    function clearError() {
        errorBox.textContent = "";
        errorBox.hidden = true;
    }

    passwordToggle.addEventListener("click", function () {
        const isPassword =
            passwordInput.type === "password";

        passwordInput.type =
            isPassword ? "text" : "password";

        passwordToggle.setAttribute(
            "aria-label",
            isPassword ? "Скрыть пароль" : "Показать пароль"
        );
        passwordToggle.setAttribute(
            "title",
            isPassword ? "Скрыть пароль" : "Показать пароль"
        );
    });

    form.addEventListener("submit", async function (event) {
        event.preventDefault();
        clearError();

        const login = loginInput.value.trim();
        const password = passwordInput.value;

        if (!login || !password) {
            showError("Введите логин и пароль.");
            return;
        }

        setLoading(true);

        try {
            const result = await BROrayUI.apiRequest(
                "/api/login.cgi",
                {
                    method: "POST",
                    body: {
                        login: login,
                        password: password
                    }
                }
            );

            passwordInput.value = "";
            window.location.replace(
                result.redirect || "/home.html"
            );
        } catch (error) {
            passwordInput.value = "";

            showError(
                error.message ||
                "Не удалось выполнить вход."
            );

            passwordInput.focus();
        } finally {
            setLoading(false);
        }
    });

    BROrayUI.apiRequest("/api/session.cgi", {
        method: "GET"
    })
        .then(function () {
            window.location.replace("/home.html");
        })
        .catch(function () {
            loginInput.focus();
        });
})();
