(function () {
    "use strict";
    var byId = function (id) { return document.getElementById(id); };
    var ui = window.BROrayUI;
    var isIP = document.body.dataset.page === "premium-ip";
    var endpoint = isIP ? "/api/routes/premium-ip.cgi" : "/api/routes/premium-domain.cgi";
    var form = byId("premium-form");
    var field = byId("premium-domain-input");
    var check = byId("premium-check");
    var add = byId("premium-add");
    var recover = byId("premium-recover");
    var results = byId("premium-results");
    var message = byId("premium-message");
    var token = "";
    var busy = false;

    function request(payload) {
        return ui.apiRequest(endpoint, {
            method: "POST",
            headers: {"Content-Type": "application/json", "X-BROray-Request": "operations",
                "X-BROray-Origin": window.location.origin},
            body: JSON.stringify(payload)
        }).then(function (reply) { return reply.data; });
    }

    function render(mode, result) {
        var badge = byId("premium-" + mode + "-status");
        var detail = byId("premium-" + mode + "-detail");
        var available = result.state === "available";
        var unknown = result.state === "unknown";
        badge.className = "status-badge " + (available ? "status-success" : unknown ? "status-unknown" : "status-error");
        badge.textContent = available ? "Доступен" : unknown ? "Не проверен" : "Недоступен";
        var reasons = {
            "dns": "DNS роутера не вернул адрес.",
            "non-public": isIP ? "Адрес находится в сети роутера." : "DNS вернул адрес из закрытой или локальной сети.",
            "ipv6-only": "У домена нет IPv4-адреса для этой проверки.",
            "network": "Не удалось подтвердить прямой выход через WAN.",
            "vpn-unconfirmed": "Не удалось подтвердить соединение через активный VPN-профиль.",
            "ipv6-egress": "Роутер не подтвердил прямой выход по IPv6.",
            "tcp": "Установлено соединение на порту 443.",
            "tcp-verified": "Получен ответ от адреса на порту 443 через VPN.",
            "tcp-unconfirmed": "VPN открыл туннель, но ответ адреса на порту 443 не подтверждён.",
            "tcp-refused": "Соединение на порту 443 отклонено.",
            "tcp-timeout": "Порт 443 не ответил за отведённое время.",
            "tcp-connection": "Не удалось установить соединение на порту 443.",
            "timeout": "Сайт не ответил за отведённое время.",
            "certificate": "Не удалось подтвердить сертификат сайта.",
            "connection": "Не удалось установить HTTPS-соединение.",
            "http": (available ? "Сайт ответил по HTTPS · " : "Сайт вернул ошибку · ") + "HTTP " + result.http
        };
        detail.textContent = reasons[result.code] || "Результат не подтверждён.";
    }

    function reset() {
        token = "";
        add.hidden = true;
        results.hidden = true;
        message.textContent = "";
    }

    function setBusy(value) {
        busy = value;
        form.setAttribute("aria-busy", String(value));
        field.readOnly = value;
        check.disabled = add.disabled = recover.disabled = value;
    }

    field.addEventListener("input", reset);
    form.addEventListener("submit", async function (event) {
        event.preventDefault();
        if (busy) return;
        reset();
        setBusy(true);
        message.textContent = "Проверяем доступность по двум путям…";
        try {
            var value = await request(isIP ? {action: "check", address: field.value} : {action: "check", domain: field.value});
            render("direct", value.direct);
            render("vpn", value.vpn);
            results.hidden = false;
            message.textContent = ({
                "route-conflict": "Для этого IP уже действует маршрут Keenetic с другим назначением.",
                "route-unconfirmed": "Не удалось проверить действующие маршруты Keenetic."
            })[value.addBlocked] || "";
            token = value.token || "";
            add.hidden = !token;
        } catch (error) {
            message.textContent = error.message || "Проверка не завершилась.";
        } finally {
            setBusy(false);
        }
    });

    add.addEventListener("click", async function () {
        if (!token || busy) return;
        setBusy(true);
        add.hidden = true;
        message.textContent = "Применяем маршрут…";
        try {
            var value = await request({action: "add", token: token});
            message.textContent = value.alreadyAdded
                ? value.domain + " уже есть в списке «" + value.list + "»."
                : value.domain + " добавлен в список «" + value.list + "». Маршрут через VPN подтверждён.";
        } catch (error) {
            message.textContent = error.message || "Не удалось добавить запись.";
            if (error.code === "PREMIUM_RECOVERY_REQUIRED") recover.hidden = false;
        } finally {
            token = "";
            setBusy(false);
        }
    });

    recover.addEventListener("click", async function () {
        if (busy) return;
        setBusy(true);
        try {
            await request({action: "recover"});
            recover.hidden = true;
            message.textContent = "Предыдущий список восстановлен.";
        } catch (error) {
            message.textContent = error.message || "Не удалось восстановить список.";
        } finally {
            setBusy(false);
        }
    });

    ui.apiRequest("/api/session.cgi").then(function (session) {
        byId("current-user").textContent = session.user || "";
        byId("page-loader").hidden = true;
        byId("app").hidden = false;
        return ui.apiRequest(endpoint).then(function (reply) {
            recover.hidden = !reply.data.recoveryRequired;
        }).catch(function (error) { message.textContent = error.message; });
    }).catch(function (error) {
        byId("page-loader").textContent = error.message || "Не удалось открыть страницу.";
    });
})();
