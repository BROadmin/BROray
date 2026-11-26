(function () {
    "use strict";
    var ui = window.BROrayUI, latest = null, summary = null, ready = false;
    var dirty = false, draftRevision = null, busy = false, pending = false;
    var unknown = false, receiptError = false, sequence = 0, desired = null;
    var selectionOpen = false, selectionBusy = false;
    var selects = ["reserve-primary", "reserve-backup-1", "reserve-backup-2"];
    var roster = ["", "", ""], rows = new Map(), groups = new Map(), sourceLabel;
    function element(id) { return document.getElementById(id); }
    function text(id, value) { if (element(id).textContent !== value) element(id).textContent = value; }
    function reserveStatus(value) {
        text("reserve-status", value);
        element("reserve-status").hidden = !value;
    }
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
    function automationMode() {
        if (unknown || receiptError) return {label: "Режим не подтверждён", tone: "unknown"};
        if (pending) return {label: "Ожидание операции", tone: "loading"};
        if (!latest) return {label: "Получение состояния…", tone: "loading"};
        if (latest.recoveryRequired) return {label: "Требуется восстановление", tone: "warning"};
        var label;
        if (latest.settings.enabled) {
            if (repairRequired()) return {label: "Резервирование · требуется восстановление", tone: "warning"};
            label = latest.autoSwitchEnabled === true ? "Резервирование + автовыбор" : "Резервирование";
        } else if (latest.autoSwitchEnabled === true) {
            label = "Автовыбор";
        } else if (latest.autoSwitchEnabled === false) {
            return {label: "Ручной режим", tone: "neutral"};
        } else {
            return {label: "Режим не подтверждён", tone: "unknown"};
        }
        return latest.paused ? {label: label + " · на паузе", tone: "warning"} : {label: label, tone: "success"};
    }
    function activeLabel() {
        if (unknown || receiptError) return "Текущий сервер: не подтверждён";
        var id = latest && latest.activeServerId;
        if (!id) return "Текущий сервер: —";
        var settings = latest.settings, role = "обычное подключение";
        if (settings.enabled) {
            var backup = settings.backupServerIds.indexOf(id);
            if (id === settings.primaryServerId) role = "основной сервер";
            else if (backup >= 0) role = backup === 0 ? "первый резерв" : "второй резерв";
            else if (settings.emergency && settings.emergency.serverId === id) role = "временный сервер";
            else role = "сервер вне состава";
        }
        return "Текущий сервер: " + name(id) + " · " + role;
    }
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
        var mode = automationMode();
        text("server-automation-mode", mode.label);
        element("server-automation-mode").className = "status-badge status-" + mode.tone;
        text("reserve-active", activeLabel());
        text("reserve-draft-note", dirty ? (latest && draftRevision !== latest.settings.revision ? "Сохранённый состав изменился. Ваш черновик сохранён; загрузите актуальный состав перед сохранением." : "Состав изменён. Сохраните его перед включением резервирования.") : enabled ? "Чтобы изменить состав, сначала отключите резервирование." : "");
        renderSelection();
    }
    function describe() {
        if (latest.recoveryRequired) return "Требуется восстановление незавершённой операции. Изменения временно недоступны.";
        if (latest.paused) return "Операции временно приостановлены.";
        if (repairRequired()) {
            if (outsideRoster(latest.activeServerId)) return "Служебная проверка отсутствует. При восстановлении BROray проверит серверы сохранённого состава по порядку и подключит первый доступный.";
            return "Служебная проверка соединения отсутствует. Восстановите резервирование, чтобы возобновить автоматическое переключение.";
        }
        if (!latest.settings.enabled) return "";
        if (latest.checking) return "Выполняется проверка соединения.";
        var reason = latest.state && latest.state.Reason;
        var labels = {"no-ready-backup":"Нет подтверждённого резерва.","awaiting-probes":"Ожидание проверок соединения.","awaiting-active-probe":"Ожидание проверки текущего сервера.","monitoring":"Соединение проверяется автоматически.","switched":"Сервер переключён; наблюдение продолжается.","switch-unconfirmed":"Результат переключения не подтверждён.","configuration-unconfirmed":"Настройки соединения не подтверждены.","active-outside-roster":"Текущий сервер не входит в состав резерва.","switch-failed-confirmed":"Переключение не удалось; сохранено прежнее соединение.","clock-unconfirmed":"Время проверок не подтверждено.","invalid-policy":"Не удалось подтвердить состав резерва.","sequence-exhausted":"Не удалось продолжить проверки резервирования."};
        labels["auto-choice"] = "Подключение к серверу из автовыбора.";
        labels["auto-choice-active"] = "Временно используется сервер из автовыбора. Проверяем основной и резервные серверы.";
        labels["roster-recovered"] = "Возврат к доступному серверу из состава резервирования.";
        return labels[reason] || "Ожидание подтверждённых проверок.";
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
            if (!pending && !receiptError) reserveStatus(describe());
            render(); return true;
        } catch (error) {
            if (current !== sequence) return false;
            unknown = true; reserveStatus(message(error)); render(); return false;
        }
    }
    function savedRequest() {
        try { return ui.readReserveModeRequest(); }
        catch (error) { receiptError = true; unknown = true; reserveStatus(message(error)); return null; }
    }
    async function operation(result) {
        if (result.state === "queued" || result.state === "running") {
            reserveStatus(result.state === "queued" ? "Запрос принят в очередь. Ожидание выполнения." : "Изменение режима выполняется."); render(); return;
        }
        if (!["completed", "failed", "cancelled"].includes(result.state)) {
            pending = true; busy = false; unknown = true; reserveStatus("Результат операции не подтверждён. Нажмите «Проверить состояние»."); render(); return;
        }
        pending = false; unknown = true; busy = true;
        var confirmed = await readStatus(false);
        busy = false;
        if (confirmed) {
            if (result.state !== "completed") reserveStatus(("Изменение режима не выполнено. " + describe()).trim());
            else if (latest.settings.enabled !== desired) reserveStatus("Операция завершена, но режим уже изменён. Показано текущее состояние.");
        }
        desired = null; render();
    }
    async function follow(enabled, revision) {
        busy = true; pending = true; unknown = false; ++sequence;
        var saved = savedRequest(); desired = saved ? saved.enabled : enabled; render();
        try { await ui.followReserveMode(enabled, revision, operation); }
        catch (error) {
            pending = !!savedRequest(); busy = false; unknown = true;
            reserveStatus(message(error)); render();
        }
    }
    async function toggle() {
        var enabled = element("reserve-enabled").checked;
        if (blocked() || repairRequired() || (enabled && (dirty || !latest.settings.configured))) { render(); return; }
        if (!enabled && latest.settings.enabled &&
            !await confirmReserveExit("Отключить резервирование", "Текущий сервер и сохранённый состав останутся на месте.")) return;
        await follow(enabled, latest.settings.revision);
    }
    async function confirmReserveExit(title, description) {
        var revision = latest.settings.revision, auto = latest.autoSwitchEnabled;
        var nextMode = auto === true ? "Автовыбор" : auto === false ? "Ручной режим" : "Режим не подтверждён";
        busy = true; render();
        try {
            if (!await window.BROrayDialogs.confirm({title:title, confirmText:title,
                message:description + " После отключения: " + nextMode + "."})) return false;
            if (!await readStatus(false) || latest.settings.revision !== revision ||
                latest.autoSwitchEnabled !== auto || latest.paused || latest.recoveryRequired || repairRequired()) {
                ui.toast("Настройки изменились или не подтверждены. Проверьте режим перед повторным действием.", "warning");
                return false;
            }
            return true;
        } finally { busy = false; render(); }
    }
    async function save(event) {
        event.preventDefault();
        if (blocked() || latest.settings.enabled || !dirty || !summary) return;
        var ids = roster.filter(Boolean);
        if (!roster[0] || !roster[1] || new Set(ids.map(groupId)).size !== ids.length ||
            ids.some(function (id) { return !rows.has(id) || !supported(rows.get(id)); })) {
            reserveStatus("Выберите основной сервер и один или два разных резервных сервера из списка."); return;
        }
        busy = true; ++sequence; render();
        try {
            await request("/api/servers/reserve-settings.cgi", {method:"POST",headers:{"X-BROray-Request":"operations","X-BROray-Origin":window.location.origin},body:{expectedRevision:draftRevision,primaryServerId:roster[0],backupServerIds:roster.slice(1).filter(Boolean)}});
            await readStatus(true);
        } catch (error) { unknown = true; reserveStatus(message(error) + " Черновик сохранён. Проверьте состояние перед повторным действием."); }
        busy = false; render();
    }
    async function refresh() {
        var saved = savedRequest();
        if (saved && !receiptError) await follow(null, null);
        else await readStatus(false);
    }
    function savedRoster() {
        return latest ? [latest.settings.primaryServerId].concat(latest.settings.backupServerIds) : [];
    }
    function renderRosterPreview(serverIds) {
        var preview = element("reserve-selection-preview"), names = serverIds.map(name);
        var current = preview.children[1];
        if (current && current.children.length === names.length && names.every(function (label, index) {
            return current.children[index].children[1].textContent === label;
        })) return;

        var heading = document.createElement("p"), list = document.createElement("dl");
        var roles = ["Основной сервер", "Первый резерв", "Второй резерв"];
        heading.textContent = "Состав после смены основного";
        names.forEach(function (label, index) {
            var row = document.createElement("div"), role = document.createElement("dt"), server = document.createElement("dd");
            role.textContent = roles[index];
            server.textContent = label;
            row.appendChild(role);
            row.appendChild(server);
            list.appendChild(row);
        });
        preview.replaceChildren(heading, list);
    }
    function renderSelection() {
        var available = !!(latest && latest.settings.enabled && !repairRequired());
        var open = element("reserve-selection-open"), panel = element("reserve-selection-panel");
        open.hidden = !available;
        open.disabled = blocked();
        panel.hidden = !selectionOpen || (!available && !selectionBusy);
        if (panel.hidden) return;
        var current = savedRoster(), target = element("reserve-selection-target"), id = target.value;
        var outside = current.indexOf(id) === -1, mustRemove = !!id && outside && current.length === 3;
        var remove = element("reserve-selection-remove"), prev = remove.value;
        element("reserve-selection-remove-control").hidden = !mustRemove;
        if (mustRemove) {
            remove.replaceChildren(option("", "Выберите сервер"), ...current.map(function (member) { return option(member, name(member)); }));
            remove.value = current.includes(prev) ? prev : "";
        } else remove.value = "";
        var locked = blocked() || !id || !rows.has(id) || !supported(rows.get(id));
        target.disabled = remove.disabled = blocked();
        element("reserve-selection-close").disabled = selectionBusy;
        element("reserve-selection-now").hidden = !id || outside;
        element("reserve-selection-now").disabled = locked || outside || id === latest.activeServerId;
        element("reserve-selection-promote").disabled = locked || id === current[0] || (mustRemove && !remove.value);
        element("reserve-selection-ordinary").hidden = !outside || !id;
        element("reserve-selection-ordinary").disabled = locked || !outside;
        if (selectionBusy) return;
        if (!id) {
            text("reserve-selection-preview", "Выберите сервер.");
        } else if (mustRemove && !remove.value) {
            text("reserve-selection-preview", "Для смены основного укажите, какой сервер исключить из состава. Для обычного подключения выбор исключаемого сервера не нужен.");
        } else {
            renderRosterPreview([id].concat(current.filter(function (member) { return member !== id && member !== remove.value; })));
        }
    }
    function selectionTargets(id) {
        var target = element("reserve-selection-target");
        var options = servers().filter(supported).map(function (server) {
            var label = name(server.id);
            if (sourceLabel) label += " · " + sourceLabel(server);
            return option(server.id, label);
        });
        target.replaceChildren(option("", "Выберите сервер"), ...options);
        target.value = options.some(function (item) { return item.value === id; }) ? id : "";
    }
    function openSelection(id) {
        if (blocked() || repairRequired() || !latest.settings.enabled) {
            ui.toast("Проверьте состояние резервирования перед подключением.", "warning");
            return;
        }
        selectionOpen = true;
        element("server-reserve-section").open = true;
        selectionTargets(id);
        element("reserve-selection-remove").value = "";
        renderSelection();
        element("reserve-selection-panel").scrollIntoView?.({block:"nearest"});
        element("reserve-selection-target").focus?.();
    }
    async function confirmSelection(mode) {
        if (!selectionOpen || blocked() || repairRequired()) return;
        var id = element("reserve-selection-target").value;
        var current = savedRoster(), remove = mode === "promote" ? element("reserve-selection-remove").value : "";
        var outside = !current.includes(id), mustRemove = outside && current.length === 3, now = mode === "now";
        if (!rows.has(id) || !supported(rows.get(id)) || (mode === "ordinary" && !outside) ||
            (now && (outside || id === latest.activeServerId)) ||
            (mode === "promote" && (id === current[0] || (mustRemove && !current.includes(remove))))) return;
        var revision = latest.settings.revision, promoting = mode === "promote";
        var title = promoting ? "Сделать основным" : now ? "Подключить сейчас" : "Отключить резервирование и подключить";
        var next = [id].concat(current.filter(function (member) { return member !== id && member !== remove; }));
        var confirmed = mode === "ordinary"
            ? await confirmReserveExit(title, "Подключить «" + name(id) + "»? Сохранённый состав резервирования останется доступен.")
            : await window.BROrayDialogs.confirm({title:title, confirmText:title, message:promoting
                ? "Новый состав: " + next.map(name).join(" → ") + "."
                : "Подключить «" + name(id) + "»? Состав и порядок серверов сохранятся."});
        if (!confirmed) return;
        if (blocked() || revision !== latest.settings.revision) {
            ui.toast("Состав изменился. Проверьте данные перед повторным действием.", "warning");
            return;
        }
        selectionBusy = busy = true;
        text("reserve-selection-preview", "Запрос в очереди…");
        render();
        try {
            await ui.followQueued(now ? "/api/servers/activate.cgi" : "/api/servers/reserve-selection.cgi", id, function (result) {
                if (result.state === "queued" || result.state === "running") {
                    text("reserve-selection-preview", ui.queueLabel(result));
                    return;
                }
                ui.toast(result.message || ui.queueLabel(result), result.state === "completed" ? "success" : "warning");
                selectionBusy = busy = false;
                if (result.state === "completed") selectionOpen = false;
                readStatus(false);
                element("refresh-servers").click?.();
            }, now ? undefined : {mode:mode,expectedRevision:revision,removeServerId:remove});
        } catch (error) {
            selectionBusy = busy = false;
            ui.toast(message(error), "error");
            await readStatus(false);
        }
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
        element("reserve-selection-open").addEventListener("click", function () { openSelection(""); });
        element("reserve-selection-close").addEventListener("click", function () { selectionOpen = false; renderSelection(); });
        element("reserve-selection-target").addEventListener("change", function () { element("reserve-selection-remove").value = ""; renderSelection(); });
        element("reserve-selection-remove").addEventListener("change", renderSelection);
        element("reserve-selection-now").addEventListener("click", function () { return confirmSelection("now"); });
        element("reserve-selection-promote").addEventListener("click", function () { return confirmSelection("promote"); });
        element("reserve-selection-ordinary").addEventListener("click", function () { return confirmSelection("ordinary"); });
        var saved = savedRequest();
        if (saved) follow(null, null);
        readStatus(false);
        setInterval(function () { if (!document.hidden && !busy && !pending) readStatus(false); }, 15000);
        document.addEventListener("visibilitychange", function () { if (!document.hidden && !busy && !pending) readStatus(false); });
    }
    function outsideRoster(id) {
        if (!latest || unknown || !latest.settings.enabled || !id) return false;
        return id !== latest.settings.primaryServerId && !latest.settings.backupServerIds.includes(id);
    }
    window.BROrayReserve = {outsideRoster:outsideRoster, openSelection:openSelection, refreshAutomationMode:function () {
        if (ready && !busy && !pending && !document.hidden) readStatus(false);
    }, updateSummary:function (value, label) {
        summary = value;
        sourceLabel = label;
        rows.clear(); groups.clear();
        servers().forEach(function (server) {
            rows.set(server.id, server);
            var key = groupId(server.id);
            if (!groups.has(key)) groups.set(key, []);
            groups.get(key).push(server);
        });
        if (ready) {
            if (selectionOpen && !selectionBusy) selectionTargets(element("reserve-selection-target").value);
            rebuildOptions(); render();
        }
    }};
    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
    else init();
})();
