(function () {
    "use strict";

    function vpnVerified(summary, expected) {
        const connection = summary && summary.activeConnection;
        const active = summary && summary.activeServer;
        return Boolean(active && active.id && (!expected || active.id === expected) &&
            summary.connectionState === "connected" && connection && connection.available === true &&
            connection.up === true && connection.freshness === "fresh" && connection.method === "current-socks-https");
    }

    function routeVerified(current, plan) {
        const observed = current && current.preview;
        return Boolean(observed && current.phase === "enabled" && observed.id === plan.id &&
            observed.sourceRevision === plan.sourceRevision && observed.sourceSHA256 === plan.sourceSHA256);
    }

    async function applyOnce(intent, save, submit, observe) {
        save(intent);
        try {
            const result = await submit();
            save(null);
            return result;
        } catch (error) {
            // A transport error or a server failure can arrive after a write.
            if ([400, 401, 403, 404, 413, 415, 422].includes(error.status) ||
                    (error.status === 409 && ["POLICY_BUSY", "OPERATION_BUSY", "NATIVE_DOMAIN_PREVIEW_CHANGED"].includes(error.code))) {
                save(null);
                throw error;
            }
            const confirmed = await observe();
            if (confirmed) {
                save(null);
                return confirmed;
            }
            throw new Error("Результат операции пока не подтверждён. Обновите состояние.");
        }
    }

    window.BROraySetup = {vpnVerified, routeVerified, applyOnce};
    const byId = (id) => document.getElementById(id);
    const wizard = byId("setup-wizard");
    if (!wizard) return;

    const ui = window.BROrayUI;
    const urls = {
        servers: "/api/servers/summary.cgi",
        check: "/api/servers/check.cgi",
        activate: "/api/servers/activate.cgi",
        import: "/api/servers/import.cgi",
        subscriptions: "/api/subscriptions/list.cgi",
        create: "/api/subscriptions/create.cgi",
        fetch: "/api/subscriptions/refresh.cgi",
        presets: "/api/routes/native-domain-preview.cgi",
        routes: "/api/routes/native-domain-apply.cgi"
    };
    const key = "broray.quick-setup.v1";
    const memory = load();
    const fields = {
        input: byId("setup-input"), kind: byId("setup-kind"), name: byId("setup-subscription-name"),
        servers: byId("setup-servers"), subscriptions: byId("setup-subscriptions"), presets: byId("setup-presets")
    };
    let summary, nativeStates, preview, checked, importPreview, busy = false;
    let step = 1;

    function load() {
        try {
            const stored = JSON.parse(sessionStorage.getItem(key));
            return stored && typeof stored === "object" && !Array.isArray(stored) ? stored : {};
        } catch { return {}; }
    }

    function save(patch) {
        const next = {...memory, ...patch};
        sessionStorage.setItem(key, JSON.stringify(next));
        Object.assign(memory, patch);
    }

    function status(text, error) {
        const output = byId("setup-status");
        output.textContent = text || "";
        output.classList.toggle("policy-error", Boolean(error));
    }

    function go(next, focus = true) {
        step = next;
        for (const panel of wizard.querySelectorAll("[data-setup-step]")) {
            panel.hidden = Number(panel.dataset.setupStep) !== next;
        }
        byId("setup-step-label").textContent = "Шаг " + next + " из 3";
        if (focus) byId("setup-step-" + next).focus();
        controls();
    }

    function controls() {
        wizard.setAttribute("aria-busy", String(busy));
        for (const el of wizard.querySelectorAll("button, input, select")) el.disabled = busy;
        const pending = memory.pending;
        const importBlocked = pending && (pending.kind === "import" || pending.kind === "subscription");
        byId("setup-import-preview").disabled = busy || Boolean(importBlocked);
        byId("setup-import-save").disabled = busy || Boolean(importBlocked) || !importPreview;
        byId("setup-add-subscription").disabled = busy || Boolean(importBlocked);
        byId("setup-check").disabled = busy || !fields.servers.value;
        byId("setup-fetch").disabled = busy || !fields.subscriptions.value;
        byId("setup-connect").disabled = busy || !checked || checked !== fields.servers.value;
        byId("setup-use-current").hidden = !summary || !summary.activeServer;
        byId("setup-route-preview").disabled = busy || !fields.presets.value;
        byId("setup-route-apply").disabled = busy || !preview || !preview.domains.length ||
            Boolean(pending && pending.kind === "route" && pending.id !== preview.id);
    }

    async function run(action, fallback) {
        if (busy) return;
        busy = true;
        status("");
        controls();
        try { await action(); }
        catch (error) {
            if (error.status === 401) return ui.redirectToLogin();
            status(fallback && (!error.status || !error.code) ? fallback : (error.message || "Операция не завершилась."), true);
        } finally {
            busy = false;
            controls();
        }
    }

    async function data(path, body) {
        const options = body === undefined ? {} : {
            method: "POST", body,
            headers: {"X-BROray-Request": "operations", "X-BROray-Origin": window.location.origin}
        };
        const reply = await ui.apiRequest(path, options);
        if (!reply || reply.success !== true || reply.data == null) throw new Error("Ответ роутера не подтверждён.");
        return reply.data;
    }

    function setOptions(select, rows, placeholder, desired) {
        const candidate = desired || select.value;
        const option = (id, name) => {
            const entry = document.createElement("option");
            entry.value = id;
            entry.textContent = name;
            return entry;
        };
        select.replaceChildren(option("", placeholder));
        for (const row of rows) {
            if (row && typeof row.id === "string" && typeof row.name === "string") select.append(option(row.id, row.name));
        }
        if (rows.some((row) => row.id === candidate)) select.value = candidate;
        else if (rows.length === 1) select.value = rows[0].id;
    }

    async function readServers(prefer) {
        const current = await data(urls.servers);
        if (!Array.isArray(current.servers) || current.total !== current.servers.length) throw new Error("Не удалось подтвердить список серверов.");
        summary = current;
        const matching = memory.subscriptionId && current.servers.filter((row) => row.subscriptionId === memory.subscriptionId);
        const options = current.servers.filter((row) => row.connectionSupport && row.connectionSupport.supported === true);
        setOptions(fields.servers, options, "Выберите сервер", prefer ||
            (matching && matching.length === 1 ? matching[0].id : undefined));
        const active = current.activeServer;
        byId("setup-current-status").textContent = active ?
            (vpnVerified(current) ? "VPN проверен на роутере · " : "VPN требует проверки · ") + active.name :
            "Активный сервер ещё не выбран.";
        return current;
    }

    async function readSubscriptions(prefer) {
        const rows = await data(urls.subscriptions);
        if (!Array.isArray(rows)) throw new Error("Список подписок недоступен.");
        setOptions(fields.subscriptions, rows, "Выберите подписку", prefer || memory.subscriptionId);
        return rows;
    }

    async function queue(kind, id, resume = false) {
        if (resume && !ui.hasQueuedRequest(urls[kind], id)) return false;
        save({queued: {kind, id}});
        return new Promise((resolve, reject) => {
            ui.followQueued(urls[kind], id, (result) => {
                status(ui.queueLabel(result));
                if (result.state === "completed") {
                    save({queued: null});
                    resolve(true);
                } else if (!["queued", "running"].includes(result.state)) {
                    if (result.state !== "unknown") save({queued: null});
                    reject(new Error(result.message || ui.queueLabel(result)));
                }
            }).catch(reject);
        });
    }

    async function readRoutes() {
        const result = await data(urls.routes);
        if (!result.states || Array.isArray(result.states)) throw new Error("Состояние маршрутов не подтверждено.");
        nativeStates = result.states;
        return nativeStates;
    }

    async function chooseRoutes() {
        save({step: 2});
        go(2);
        const list = await data(urls.presets);
        if (!Array.isArray(list.presets)) throw new Error("Готовые наборы недоступны.");
        setOptions(fields.presets, list.presets, "Выберите готовый набор", memory.route && memory.route.id);
    }

    function finish() {
        const plan = memory.route;
        const installed = !memory.skipped && plan && routeVerified(nativeStates && nativeStates[plan.id], plan);
        byId("setup-final-vpn").textContent = vpnVerified(summary) ? "VPN проверен на роутере" : "VPN требует проверки";
        byId("setup-final-routes").textContent = installed ? "Маршруты установлены · " + plan.name :
            memory.skipped ? "Маршруты можно настроить позже" : "Установка маршрутов не подтверждена";
        const destination = installed && plan.example;
        const link = byId("setup-example-site");
        link.hidden = !destination;
        if (destination && /^(?:[a-z0-9-]+\.)+[a-z0-9-]+$/i.test(destination)) {
            link.href = "https://" + destination;
            link.textContent = destination;
        } else { link.removeAttribute("href"); link.hidden = true; }
        byId("setup-lan-hint").hidden = link.hidden;
        byId("setup-lan-error").hidden = link.hidden;
        go(3);
    }

    async function resume() {
        const queued = memory.queued;
        if (queued && ["check", "activate", "fetch"].includes(queued.kind) &&
                typeof queued.id === "string" && ui.hasQueuedRequest(urls[queued.kind], queued.id)) {
            await queue(queued.kind, queued.id, true);
        }
        await readServers();
        await readSubscriptions();
        const pending = memory.pending;
        if (pending && pending.kind === "route") {
            const states = await readRoutes();
            if (routeVerified(states[pending.id], pending)) save({pending: null, route: pending, step: 3, skipped: false});
            else status("Установка набора ещё не подтверждена. Проверьте состояние в разделе «Маршруты».", true);
        } else if (pending) {
            status("Проверьте сохранённые серверы и подписки. Выберите нужную запись из списка.", true);
        }
        if (vpnVerified(summary) && memory.step === 3) {
            if (memory.route && !memory.skipped) await readRoutes();
            finish();
        } else if (vpnVerified(summary) && memory.step === 2) {
            await chooseRoutes();
        } else go(1, false);
    }

    const act = (id, task, fallback) => byId(id).addEventListener("click", () => run(task, fallback));
    const setInputKind = () => {
        const sub = fields.kind.value === "subscription";
        byId("setup-name-row").hidden = !sub;
        byId("setup-import-preview").hidden = sub;
        byId("setup-add-subscription").hidden = !sub;
        byId("setup-input-label").textContent = sub ? "Ссылка на подписку" : "Ключ сервера";
        fields.input.maxLength = sub ? 4096 : 60000;
        importPreview = null;
        byId("setup-import-save").hidden = true;
        byId("setup-import-result").textContent = "";
    };
    fields.kind.addEventListener("change", () => { fields.input.value = ""; setInputKind(); controls(); });
    fields.input.addEventListener("input", () => { setInputKind(); controls(); });
    fields.servers.addEventListener("change", () => { checked = null; controls(); });
    fields.subscriptions.addEventListener("change", controls);
    fields.presets.addEventListener("change", () => { preview = null; byId("setup-route-content").hidden = true; controls(); });
    act("setup-refresh", resume);
    act("setup-import-preview", async () => {
        const uri = fields.input.value.trim();
        if (!uri) throw new Error("Вставьте ключ сервера.");
        if (memory.pending) throw new Error("Сначала проверьте результат предыдущей операции.");
        const value = await data(urls.import, {uri, preview: true});
        importPreview = value.connectionSupport && value.connectionSupport.supported === true && !value.duplicate ? value : null;
        byId("setup-import-result").textContent = value.duplicate ?
            "Этот сервер уже сохранён. Выберите его в списке." :
            importPreview ? "Сервер готов к импорту · " + (value.name || value.protocol) : "Конфигурация не поддерживается.";
        byId("setup-import-save").hidden = !importPreview;
        if (value.duplicate) await readServers();
    }, "Не удалось проверить ключ. Проверьте его формат.");
    act("setup-import-save", async () => {
        if (!importPreview || memory.pending) return;
        const uri = fields.input.value.trim();
        const result = await applyOnce({kind: "import"}, (pending) => save({pending}),
            () => data(urls.import, {uri}), async () => { await readServers(); return null; });
        fields.input.value = "";
        setInputKind();
        await readServers(result.id);
        status("Сервер сохранён. Теперь его можно проверить.");
    }, "Результат импорта пока не подтверждён. Обновите состояние и проверьте список серверов.");
    act("setup-add-subscription", async () => {
        if (memory.pending) throw new Error("Сначала проверьте результат предыдущей операции.");
        const url = fields.input.value.trim();
        const name = fields.name.value.trim() || "Моя подписка";
        if (!url) throw new Error("Вставьте ссылку на подписку.");
        const result = await applyOnce({kind: "subscription"}, (pending) => save({pending}), () =>
            data(urls.create, {name, url, updateImmediately: false}),
            async () => { await readSubscriptions(); return null; });
        fields.input.value = "";
        save({subscriptionId: result.id});
        await readSubscriptions(result.id);
        await queue("fetch", result.id);
        await readServers();
        status("Выберите сервер из подписки.");
    }, "Не удалось подтвердить подписку. Проверьте ссылку или выберите сохранённую подписку.");
    act("setup-fetch", async () => {
        const id = fields.subscriptions.value;
        save({subscriptionId: id, pending: null});
        await queue("fetch", id);
        await readServers();
        status("Выберите сервер из подписки.");
    });
    act("setup-check", async () => {
        const id = fields.servers.value;
        checked = null;
        if (memory.pending && memory.pending.kind !== "route") save({pending: null});
        await queue("check", id);
        const summary = await readServers(id);
        const row = summary.servers.find((server) => server.id === id);
        const quality = row && row.quality;
        if (!vpnVerified(summary, id) && (!quality || quality.status !== "available" || quality.freshness !== "fresh")) {
            throw new Error("Сервер не ответил на проверку. Попробуйте другой сервер или повторите проверку.");
        }
        checked = id;
        status("Сервер ответил на проверку.");
    });
    act("setup-connect", async () => {
        const id = fields.servers.value;
        if (!id || checked !== id) return;
        const before = await readServers(id);
        if (before.activeServer && before.activeServer.id !== id) {
            const ok = await window.BROrayDialogs.confirm({title: "Сменить активный сервер?",
                message: "VPN переключится на выбранный сервер.", confirmText: "Подключить", cancelText: "Отмена"});
            if (!ok) return;
        }
        const latest = await readServers(id);
        if ((latest.activeServer && latest.activeServer.id) !== (before.activeServer && before.activeServer.id)) {
            checked = null;
            throw new Error("Активный сервер изменился. Проверьте выбор и повторите подключение.");
        }
        if (!latest.activeServer || latest.activeServer.id !== id) await queue("activate", id);
        if (!vpnVerified(await readServers(id), id)) {
            await queue("check", id);
            await readServers(id);
        }
        if (!vpnVerified(summary, id)) throw new Error("Сервер выбран, но HTTPS через активный VPN ещё не подтверждён.");
        await chooseRoutes();
    });
    act("setup-use-current", async () => {
        const current = await readServers();
        const id = current.activeServer && current.activeServer.id;
        if (!id) throw new Error("Выберите сервер.");
        if (!vpnVerified(current, id)) {
            await queue("check", id);
            await readServers(id);
        }
        if (!vpnVerified(summary, id)) throw new Error("Текущий VPN требует проверки. Проверьте его или выберите другой сервер.");
        if (memory.pending && memory.pending.kind !== "route") save({pending: null});
        if (memory.step === 3) {
            if (memory.route && !memory.skipped) await readRoutes();
            finish();
        } else await chooseRoutes();
    });
    act("setup-route-preview", async () => {
        const id = fields.presets.value;
        const value = await data(urls.presets, {id});
        if (value.id !== id || !Array.isArray(value.domains) || !Array.isArray(value.skipped)) throw new Error("Состав набора не подтверждён.");
        preview = value;
        byId("setup-route-content").hidden = false;
        byId("setup-route-domains").textContent = value.domains.join("\n");
        byId("setup-route-skipped").textContent = value.skipped.length ? "Несовместимых записей: " + value.skipped.length : "";
    });
    act("setup-route-apply", async () => {
        if (!preview || fields.presets.value !== preview.id) return;
        const active = summary && summary.activeServer;
        if (!active || !vpnVerified(await readServers(), active.id)) throw new Error("Подключение изменилось. Вернитесь к проверке VPN.");
        const plan = {kind: "route", id: preview.id, name: preview.name || fields.presets.selectedOptions[0].textContent,
            sourceRevision: preview.sourceRevision, sourceSHA256: preview.sourceSHA256, example: preview.domains[0]};
        const states = await readRoutes();
        if (!routeVerified(states[plan.id], plan)) {
            if (states[plan.id] || memory.pending) throw new Error("Проверьте состояние этого набора в разделе «Маршруты».");
            const verified = async () => routeVerified((await readRoutes())[plan.id], plan);
            await applyOnce(plan, (pending) => save({pending}), async () => {
                await data(urls.routes, {action: "enable", id: plan.id,
                    sourceRevision: plan.sourceRevision, sourceSHA256: plan.sourceSHA256});
                if (!await verified()) throw new Error("Не удалось подтвердить маршрут в Keenetic.");
                return true;
            }, verified);
        }
        save({step: 3, route: plan, skipped: false});
        finish();
    });
    act("setup-skip", async () => {
        save({step: 3, skipped: true});
        finish();
    });
    act("setup-back-vpn", async () => { save({step: 1}); go(1); });
    act("setup-back-routes", chooseRoutes);

    ui.apiRequest("/api/session.cgi").then((session) => {
        byId("current-user").textContent = session.user || "";
        byId("page-loader").hidden = true;
        byId("app").hidden = false;
        return run(resume);
    }).catch((error) => {
        if (error.status === 401) ui.redirectToLogin();
        else byId("page-loader").textContent = "Не удалось открыть быструю настройку. Обновите страницу.";
    });
}());
