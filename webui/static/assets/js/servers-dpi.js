(function () {
    "use strict";
    var ui = window.BROrayUI, states = new Map();
    function message(error) { return error && error.payload && error.payload.error && error.payload.error.message || error.message || "Не удалось подтвердить состояние DPI."; }
    function saved() { return ui.readDPIProfileRequest(); }
    function blocked(s) {
        var pending;
        try { pending = saved(); } catch (_) { return true; }
        return !s.latest || s.busy || s.unknown || !!pending || s.latest.paused || s.latest.recoveryRequired || !s.latest.profileCompatible;
    }
    function render(s) {
        if (!s.nodes) return;
        var n = s.nodes;
        n.toggle.checked = s.busy && !s.unknown && typeof s.desired === "boolean" ? s.desired : !!(s.latest && s.latest.enabled);
        n.toggle.disabled = blocked(s);
        n.toggle.indeterminate = s.unknown || !s.latest;
        n.root.setAttribute("aria-busy", s.busy ? "true" : "false");
        n.refresh.disabled = s.busy;
        n.note.textContent = s.message || "Получение состояния…";
    }
    function renderAll() { states.forEach(render); }
    function valid(data, id) {
        return data && data.id === id && typeof data.enabled === "boolean" && typeof data.profileCompatible === "boolean" &&
            typeof data.reason === "string" && /^[0-9a-f]{64}$/.test(data.revision) && typeof data.paused === "boolean" && typeof data.recoveryRequired === "boolean";
    }
    async function read(s) {
        var generation = ++s.generation, controller = new AbortController();
        var timer = setTimeout(function () { controller.abort(); }, 30000);
        try {
            var data = await ui.apiRequest("/api/servers/dpi-status.cgi?id=" + encodeURIComponent(s.id), {signal:controller.signal});
            if (generation !== s.generation) return false;
            if (data && data.success === true) data = data.data;
            if (!valid(data, s.id)) throw new Error("Ответ о DPI неполон. Проверьте состояние повторно.");
            s.latest = data; s.unknown = false;
            s.message = data.recoveryRequired ? "Требуется восстановление незавершённой операции." : data.paused ? "Операции приостановлены." : !data.profileCompatible ? data.reason : data.enabled ? "Обход DPI включён." : "Обход DPI выключен.";
            render(s); return true;
        } catch (error) {
            if (generation === s.generation) { s.unknown = true; s.message = message(error); render(s); }
            return false;
        } finally { clearTimeout(timer); }
    }
    async function operation(s, result) {
        if (result.state === "queued" || result.state === "running") {
            s.message = result.state === "queued" ? "Изменение DPI ожидает выполнения." : "Применение и проверка DPI…";
            s.busy = true; renderAll(); return;
        }
        s.desired = null; s.busy = false; s.unknown = true;
        if (!["completed", "failed", "cancelled"].includes(result.state)) {
            s.message = "Результат не подтверждён. Нажмите «Проверить состояние»."; renderAll(); return;
        }
        s.busy = true; renderAll();
        var confirmed = await read(s);
        s.busy = false;
        if (confirmed && result.state !== "completed") s.message = (typeof result.message === "string" && result.message ? result.message : "Изменение не выполнено.") + " " + s.message;
        renderAll();
    }
    async function observers(s, terminal) {
        var watching = Array.from(s.observers || []);
        if (terminal && s.observers) s.observers.clear();
        await Promise.all(watching.map(async function (observer) {
            if (terminal) {
                observer.busy = true; render(observer);
                await read(observer);
                observer.busy = false;
            } else {
                observer.busy = s.busy; observer.unknown = s.unknown; observer.message = s.message;
            }
            render(observer);
        }));
    }
    async function follow(s, enabled, revision) {
        s.busy = true; s.unknown = false; ++s.generation;
        try {
            var request = enabled === null ? saved() : null;
            s.desired = request ? request.enabled : enabled; renderAll();
            await ui.followDPIProfile(enabled === null ? null : s.id, enabled, revision, async function (result) {
                await operation(s, result);
                await observers(s, ["completed", "failed", "cancelled"].includes(result.state));
            });
        } catch (error) {
            s.desired = null; s.busy = false; s.unknown = true; s.message = message(error);
            await observers(s, false); renderAll();
        }
    }
    async function refresh(s) {
        if (s.busy) return;
        try {
            var request = saved();
            if (request) {
                var owner = states.get(request.id);
                if (!owner) { owner = {id:request.id,latest:null,busy:false,unknown:true,generation:0,message:""}; states.set(request.id,owner); }
                s.message = "Проверяем результат сохранённого изменения DPI."; render(s);
                if (owner !== s) {
                    if (!owner.observers) owner.observers = new Set();
                    owner.observers.add(s);
                }
                if (!owner.busy) await follow(owner, null, null);
                else await observers(owner, false);
                return;
            }
        } catch (error) { s.unknown = true; s.message = message(error); render(s); return; }
        await read(s);
    }
    function create(server) {
        var s = states.get(server.id);
        if (!s) { s = {id:server.id,latest:null,busy:false,unknown:false,generation:0,message:""}; states.set(server.id,s); }
        var root = document.createElement("div"), label = document.createElement("label"), toggle = document.createElement("input");
        var title = document.createElement("span"), note = document.createElement("p"), button = document.createElement("button");
        root.className = "server-dpi"; toggle.type = "checkbox"; title.textContent = "Обход DPI";
        note.setAttribute("role", "status"); note.setAttribute("aria-live", "polite");
        button.type = "button"; button.className = "button button-secondary"; button.textContent = "Проверить состояние";
        label.appendChild(toggle); label.appendChild(title); root.appendChild(label); root.appendChild(note); root.appendChild(button);
        s.nodes = {root:root,toggle:toggle,note:note,refresh:button};
        toggle.addEventListener("change", function () {
            var enabled = toggle.checked;
            if (blocked(s)) { render(s); return; }
            return follow(s, enabled, s.latest.revision);
        });
        button.addEventListener("click", function () { return refresh(s); });
        render(s);
        if (!s.busy) {
            try { var request = saved(); if (request && request.id === s.id) follow(s,null,null); else read(s); }
            catch (error) { s.unknown = true; s.message = message(error); render(s); }
        }
        return root;
    }
    window.BROrayDPI = {create:create};
}());
