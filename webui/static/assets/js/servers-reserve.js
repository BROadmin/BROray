(function () {
    "use strict";
    var ui = window.BROrayUI, latest = null, summary = null, ready = false;
    var dirty = false, draftRevision = null, busy = false, pending = false;
    var unknown = false, receiptError = false, sequence = 0, desired = null;
    var selects = ["reserve-primary", "reserve-backup-1", "reserve-backup-2"];
    var roster = ["", "", ""], rows = new Map(), groups = new Map(), sourceLabel;
    function element(id) { return document.getElementById(id); }
    function text(id, value) { if (element(id).textContent !== value) element(id).textContent = value; }
    function message(error) { return error && error.payload && error.payload.error && error.payload.error.message || error.message || "Не удалось подтвердить состояние."; }
    function servers() { return summary && Array.isArray(summary.servers) ? summary.servers : []; }
    function groupId(id) {
        var row = rows.get(id);
        return row && row.displayGroupId || id;
    }
    function supported(server) { return !server.connectionSupport || server.connectionSupport.supported !== false; }
    function preferred(members) {
        var choices = members.filter(supported);
        return choices.find(function (item) { return item.active; }) ||
            choices.find(function (item) { return item.sourceType !== "subscription"; }) || choices[0];
    }
    function name(id) {
        var server = rows.get(id);
        return server ? (server.name || server.id) : (id ? "Сервер отсутствует в списке (" + id + ")" : "—");
    }
    function option(value, label, disabled) {
        var node = document.createElement("option");
        node.value = value;
        node.textContent = label;
        node.disabled = !!disabled;
        return node;
    }
    function rebuildOptions() {
        var occupied = roster.map(groupId);
        var names = new Map();
        groups.forEach(function (members) {
            var item = preferred(members) || members[0], label = name(item.id);
            names.set(label, (names.get(label) || 0) + 1);
        });
        selects.forEach(function (id, index) {
            var select = element(id), source = element(id + "-source"), exact = roster[index], selected = occupied[index];
            var options = [option("", index === 2 ? "Не использовать" : "Выберите сервер")];
            groups.forEach(function (members, key) {
                var available = preferred(members), item = available || members[0], label = name(item.id);
                if (names.get(label) > 1 && item.address) label += " · " + item.address + ":" + item.port;
                options.push(option(key, label, key !== selected && (!available || occupied.includes(key))));
            });
            if (exact && !rows.has(exact)) options.push(option(exact, name(exact)));
            select.replaceChildren.apply(select, options);
            select.value = selected;

            var variants = groups.get(selected) || [];
            var labels = variants.map(function (member) {
                return sourceLabel ? sourceLabel(member) : name(member.id);
            });
            var variantOptions = variants.map(function (member, idx) {
                var label = labels[idx], position = labels.slice(0, idx + 1).filter(function (item) { return item === label; }).length;
                if (labels.indexOf(label) !== labels.lastIndexOf(label)) label += " · " + position;
                return option(member.id, label, !supported(member) && member.id !== exact);
            });
            source.replaceChildren.apply(source, variantOptions);
            source.value = exact;
            element(id + "-source-control").hidden = variants.length < 2;
        });
    }
    function applyRoster() {
        var settings = latest.settings;
        roster = [settings.primaryServerId, settings.backupServerIds[0] || "", settings.backupServerIds[1] || ""];
        rebuildOptions();
        draftRevision = settings.revision; dirty = false;
    }
    function blocked() { return !latest || latest.paused || latest.recoveryRequired || busy || unknown || receiptError; }
    function repairRequired() { return !!latest && latest.runtimeRepairRequired === true; }
    function render() {
        if (!ready) return;
        var enabled = !!(latest && latest.settings.enabled), disabled = blocked(), needsRepair = repairRequired();
        element("server-reserve-section").setAttribute("aria-busy", busy ? "true" : "false");
        element("reserve-enabled").disabled = disabled || needsRepair || (!enabled && (!latest.settings.configured || dirty));
        element("reserve-enabled").checked = pending && desired !== null ? desired : enabled;
        element("reserve-enabled").indeterminate = unknown || receiptError;
        selects.forEach(function (id) {
            var locked = disabled || enabled || !summary;
            element(id).disabled = locked;
            element(id + "-source").disabled = locked;
        });
        element("reserve-save").disabled = disabled || enabled || !dirty || !summary;
        element("reserve-cancel").disabled = busy || !dirty;
        element("reserve-repair").hidden = !needsRepair;
        element("reserve-repair").disabled = disabled || !needsRepair;
        text("reserve-mode-label", unknown || receiptError ? "Состояние не подтверждено" : pending ? "Ожидание операции" : !latest ? "Получение состояния…" : needsRepair ? "Требуется восстановление" : enabled ? "Включено" : "Выключено");
        element("reserve-mode-label").className = enabled && !unknown && !receiptError && !pending && !needsRepair
            ? "status-badge reserve-badge-enabled" : "status-badge status-badge-neutral";
        text("reserve-active", "Текущий сервер: " + name(latest && latest.activeServerId));
        text("reserve-draft-note", dirty ? (latest && draftRevision !== latest.settings.revision ? "Сохранённый состав изменился. Ваш черновик сохранён; загрузите актуальный состав перед сохранением." : "Состав изменён. Сохраните его перед включением резервирования.") : enabled ? "Чтобы изменить состав, сначала отключите резервирование." : "");
    }
    function describe() {
        if (latest.recoveryRequired) return "Требуется восстановление незавершённой операции. Изменения временно недоступны.";
        if (latest.paused) return "Операции временно приостановлены.";
        if (repairRequired()) return "Служебная проверка соединения отсутствует. Восстановите резервирование, чтобы возобновить автоматическое переключение.";
        if (!latest.settings.enabled) return "Резервирование выключено.";
        if (latest.checking) return "Выполняется проверка соединения.";
        var reason = latest.state && latest.state.Reason;
        var labels = {"no-ready-backup":"Нет подтверждённого резерва.","awaiting-probes":"Ожидание проверок соединения.","awaiting-active-probe":"Ожидание проверки текущего сервера.","monitoring":"Соединение проверяется автоматически.","switched":"Сервер переключён; наблюдение продолжается.","switch-unconfirmed":"Результат переключения не подтверждён.","configuration-unconfirmed":"Настройки соединения не подтверждены.","active-outside-roster":"Текущий сервер не входит в состав резерва.","switch-failed-confirmed":"Переключение не удалось; сохранено прежнее соединение.","clock-unconfirmed":"Время проверок не подтверждено.","invalid-policy":"Не удалось подтвердить состав резерва.","sequence-exhausted":"Не удалось продолжить проверки резервирования."};
        labels["auto-choice"] = "Подключение к серверу из автовыбора.";
        labels["auto-choice-active"] = "Временно используется сервер из автовыбора. Проверяем основной и резервные серверы.";
        labels["roster-recovered"] = "Возврат к доступному серверу из состава резервирования.";
        return labels[reason] || "Резервирование включено. Ожидание подтверждённых проверок.";
    }
    function validStatus(data) {
        var s = data && data.settings;
        return s && s.schemaVersion === 1 && typeof s.enabled === "boolean" && typeof s.configured === "boolean" &&
            typeof s.primaryServerId === "string" && Array.isArray(s.backupServerIds) && s.backupServerIds.every(function (id) { return typeof id === "string"; }) &&
            typeof s.revision === "string" && /^[0-9a-f]{64}$/.test(s.revision) && typeof data.activeServerId === "string" &&
            typeof data.paused === "boolean" && typeof data.recoveryRequired === "boolean" && typeof data.checking === "boolean";
    }
    async function request(url, options) {
        var controller = new AbortController(), timer = setTimeout(function () { controller.abort(); }, 30000);
        try {
            var data = await ui.apiRequest(url, Object.assign({}, options || {}, {signal:controller.signal}));
            return data && data.success === true && data.data !== undefined ? data.data : data;
        } finally { clearTimeout(timer); }
    }
    async function readStatus(resetDraft) {
        var current = ++sequence;
        try {
            var data = await request("/api/servers/reserve-status.cgi");
            if (current !== sequence) return false;
            if (!validStatus(data)) throw new Error("Ответ о резервировании неполон. Повторите проверку состояния.");
            latest = data;
            if (!dirty || resetDraft) applyRoster();
            if (!pending && !receiptError) unknown = false;
            if (!pending && !receiptError) text("reserve-status", describe());
            render(); return true;
        } catch (error) {
            if (current !== sequence) return false;
            unknown = true; text("reserve-status", message(error)); render(); return false;
        }
    }
    function savedRequest() {
        try { return ui.readReserveModeRequest(); }
        catch (error) { receiptError = true; unknown = true; text("reserve-status", message(error)); return null; }
    }
    async function operation(result) {
        if (result.state === "queued" || result.state === "running") {
            text("reserve-status", result.state === "queued" ? "Запрос принят в очередь. Ожидание выполнения." : "Изменение режима выполняется."); render(); return;
        }
        if (!["completed", "failed", "cancelled"].includes(result.state)) {
            pending = true; busy = false; unknown = true; text("reserve-status", "Результат операции не подтверждён. Нажмите «Проверить состояние»."); render(); return;
        }
        pending = false; unknown = true; busy = true;
        var confirmed = await readStatus(false);
        busy = false;
        if (confirmed) {
            if (result.state !== "completed") text("reserve-status", "Изменение режима не выполнено. " + describe());
            else if (latest.settings.enabled !== desired) text("reserve-status", "Операция завершена, но режим уже изменён. Показано текущее состояние.");
        }
        desired = null; render();
    }
    async function follow(enabled, revision) {
        busy = true; pending = true; unknown = false; ++sequence;
        var saved = savedRequest(); desired = saved ? saved.enabled : enabled; render();
        try { await ui.followReserveMode(enabled, revision, operation); }
        catch (error) {
            pending = !!savedRequest(); busy = false; unknown = true;
            text("reserve-status", message(error)); render();
        }
    }
    async function toggle() {
        var enabled = element("reserve-enabled").checked;
        if (blocked() || repairRequired() || (enabled && (dirty || !latest.settings.configured))) { render(); return; }
        await follow(enabled, latest.settings.revision);
    }
    async function save(event) {
        event.preventDefault();
        if (blocked() || latest.settings.enabled || !dirty || !summary) return;
        var ids = roster.filter(Boolean);
        if (!roster[0] || !roster[1] || new Set(ids.map(groupId)).size !== ids.length ||
            ids.some(function (id) { return !rows.has(id) || !supported(rows.get(id)); })) {
            text("reserve-status", "Выберите основной сервер и один или два разных резервных сервера из списка."); return;
        }
        busy = true; ++sequence; render();
        try {
            await request("/api/servers/reserve-settings.cgi", {method:"POST",headers:{"X-BROray-Request":"operations","X-BROray-Origin":window.location.origin},body:{expectedRevision:draftRevision,primaryServerId:roster[0],backupServerIds:roster.slice(1).filter(Boolean)}});
            await readStatus(true);
        } catch (error) { unknown = true; text("reserve-status", message(error) + " Черновик сохранён. Проверьте состояние перед повторным действием."); }
        busy = false; render();
    }
    async function refresh() {
        var saved = savedRequest();
        if (saved && !receiptError) await follow(null, null);
        else await readStatus(false);
    }
    function init() {
        if (!element("server-reserve-section")) return;
        ready = true;
        selects.forEach(function (id, index) {
            element(id).addEventListener("change", function (event) {
                var selected = event.target.value;
                if (groupId(roster[index]) !== selected) {
                    var member = preferred(groups.get(selected) || []);
                    roster[index] = member ? member.id : "";
                }
                dirty = true; rebuildOptions(); render();
            });
            element(id + "-source").addEventListener("change", function (event) {
                var selected = event.target.value;
                if (groupId(selected) === groupId(roster[index]) && rows.has(selected)) {
                    roster[index] = selected;
                    dirty = true; rebuildOptions(); render();
                }
            });
        });
        element("reserve-roster-form").addEventListener("submit", save);
        element("reserve-enabled").addEventListener("change", toggle);
        element("reserve-refresh").addEventListener("click", refresh);
        element("reserve-repair").addEventListener("click", function () {
            if (!blocked() && repairRequired()) return follow(true, latest.settings.revision);
        });
        element("reserve-cancel").addEventListener("click", function () { return readStatus(true); });
        var saved = savedRequest();
        if (saved) follow(null, null);
        readStatus(false);
        setInterval(function () { if (!document.hidden && !busy && !pending) readStatus(false); }, 15000);
        document.addEventListener("visibilitychange", function () { if (!document.hidden && !busy && !pending) readStatus(false); });
    }
    window.BROrayReserve = {updateSummary:function (value, label) {
        summary = value;
        sourceLabel = label;
        rows.clear(); groups.clear();
        servers().forEach(function (server) {
            rows.set(server.id, server);
            var key = groupId(server.id);
            if (!groups.has(key)) groups.set(key, []);
            groups.get(key).push(server);
        });
        if (ready) { rebuildOptions(); render(); }
    }};
    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
    else init();
})();
