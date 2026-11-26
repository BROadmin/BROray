(function () {
    "use strict";
    var isDomains = document.body.dataset.page === "routes-domains";
    if (!document.getElementById("policy-title")) return;
    var nativePresets = null;
    var nativePreviews = new Map();
    var nativePreviewRequests = new Map();
    var nativeListPath = "/api/routes/native-domain-preview.cgi";
    var nativeApplyPath = "/api/routes/native-domain-apply.cgi", nativeStates = null, nativeChangePending = null;
    var dnsDraftPath = "/api/routes/native-dns-draft.cgi", dnsDrafts = Object.create(null);
    var nativeDialog = null;
    var filters = [{id: "filter-ads", tag: "category-ads", name: "Реклама и рекламные трекеры"}, {id: "filter-telemetry", tag: "category-telemetry", name: "Телеметрия приложений"}];
    var client, state, uiBusy = false, editID = null, editBase = null, draftID = null, editorDirty = false, timer = null;
    var applyTimeRevision = null;
    function byId(id) { return document.getElementById(id); }
    function node(tag, cls, text) { var n = document.createElement(tag); if (cls) n.className = cls; if (text != null) n.textContent = text; return n; }
    function legacyRule(id) { return state && state.policy && state.policy.state.rules.find(function (r) { return r.id === id; }); }
    function rule(id) { return dnsDrafts[id] || legacyRule(id); }
    function editableSignature(r) { return r ? JSON.stringify([r.name, r.domains, r.sourceSHA256]) : null; }
    function blocked() { return uiBusy || !state || !state.policy || state.busy || state.uncertain || state.policy.busy || state.policy.pending; }
    function errorMessage(error) {
        if (error.status === 401) { window.BROrayUI.redirectToLogin(); return; }
        byId("policy-message").textContent = error.message || "Не удалось подтвердить результат. Обновите данные.";
        window.BROrayUI.toast(error.message || "Действие не завершено.", "error");
    }
    async function run(action) {
        if (blocked()) return;
        uiBusy = true; render();
        try { await action(); window.BROrayUI.toast("Сохранено", "success"); }
        catch (error) { errorMessage(error); }
        finally { uiBusy = false; state = client.snapshot(); render(); }
    }
    function actionLabel(action) { return {vpn: "Через VPN", direct: "Напрямую", deny: "Блокировка"}[action] || "Неизвестное действие"; }
    function switchControl(id, name, enabled, change) {
        var wrap = node("div", "policy-toggle-state"), status = node("span", "policy-state" + (enabled ? " is-on" : ""), enabled ? "Включено" : "Выключено");
        var label = node("label", "policy-switch"), input = node("input");
        input.type = "checkbox"; input.id = "switch-" + id; input.setAttribute("role", "switch"); input.setAttribute("aria-label", name);
        input.checked = Boolean(enabled); input.disabled = blocked();
        input.addEventListener("change", function () { var value = input.checked; run(function () { return change(value); }); });
        label.append(input, node("span")); label.lastChild.setAttribute("aria-hidden", "true"); wrap.append(status, label); return wrap;
    }
    function actionControl(id, name, action, change) {
        var select = node("select"); select.id = "action-" + id; select.setAttribute("aria-label", "Действие для «" + name + "»");
        [["deny", "Блокировать"], ["direct", "Напрямую"]].forEach(function (v) { var option = node("option", "", v[1]); option.value = v[0]; select.append(option); });
        select.value = action || "deny"; select.disabled = blocked();
        select.addEventListener("change", function () { var value = select.value; run(function () { return change(value); }); }); return select;
    }
    function cardRule(id, name, enabled, onToggle) {
        var row = node("article", "policy-rule"), heading = node("div", "policy-rule-heading");
        heading.append(node("h3", "", name), switchControl(id, name, enabled, onToggle)); row.append(heading); return row;
    }
    async function ensurePreset(preset, action) {
        var existing = rule(preset.id);
        if (existing && (!existing.geoSiteTags || existing.geoSiteTags.length !== 1 || existing.geoSiteTags[0] !== preset.tag || (existing.domains || []).length || (existing.countryCodes || []).length)) {
            throw new Error("Идентификатор набора уже занят другим правилом. Проверьте сохранённые правила.");
        }
        if (!existing) await client.change({operation: "geo-upsert", id: preset.id, name: preset.name, kind: "geosite", tags: [preset.tag]});
        existing = rule(preset.id);
        if (existing.action !== action) await client.change({operation: "action", id: preset.id, action: action});
    }
    function renderPresets() {
        var root = byId("policy-presets"); root.replaceChildren();
        if (nativePresets) {
            nativePresets.forEach(function (preset) {
                root.append(renderNativeCard(preset));
            });
        }
    }
    function nativeCurrent(id) { return nativeStates && nativeStates[id]; }
    function nativeAction(item, current) {
        if (current) return current.phase === "creating" || current.phase === "removing" ? "recover" : "disable";
        return item.enabled ? "legacy" : "enable";
    }
    function nativeCaption(action, current) {
        return {enable: "Включить в Keenetic", disable: "Отключить", legacy: "Отключить прежний маршрут",
            recover: current && current.phase === "removing" ? "Завершить удаление" : "Завершить применение"}[action];
    }
    function renderNativeCard(item, orphaned) {
        var row = node("article", "policy-rule policy-domain-card");
        var current = nativeCurrent(item.id), preview = current && current.preview || nativePreviews.get(item.id);
        var heading = node("div", "policy-rule-heading");
        var phase = nativeChangePending === item.id ? "busy" : !nativeStates ? "unknown" :
            current ? current.phase : item.enabled ? "legacy" : "disabled";
        var statuses = {
            enabled: ["Включён", "status-success"], disabled: ["Выключен", "status-badge-neutral"],
            creating: ["Нужно завершить применение", "status-warning"], removing: ["Нужно завершить удаление", "status-warning"],
            unconfirmed: ["Не подтверждён", "status-warning"], unknown: ["Статус недоступен", "status-unknown"],
            busy: ["Выполняется", "status-loading"], legacy: ["Прежний маршрут", "status-warning"]
        };
        var badge = statuses[phase] || statuses.unknown;
        var status = node("span", "status-badge " + badge[1], badge[0]);
        status.setAttribute("role", "status");
        heading.append(node("h3", "", item.name), status);
        row.append(heading);
        if (preview) {
            row.append(node("p", "policy-hint", "Совместимых записей: " + preview.domains.length +
                (preview.skipped.length ? " · Пропущено правил: " + preview.skipped.length : "")));
        } else if (item.domains) {
            row.append(node("p", "policy-hint", "Правил: " + item.domains.length));
        }
        if (orphaned) row.append(node("p", "policy-hint", "Исходный список отсутствует."));
        var actions = node("div", "policy-domain-actions");
        var details = node("button", "button button-secondary policy-domain-details", "Подробнее");
        details.type = "button"; details.id = "native-preview-" + item.id;
        details.setAttribute("aria-haspopup", "dialog");
        details.addEventListener("click", function () { openNativeDialog(item, null, details.id); });
        var action = nativeAction(item, current);
        var look = action === "enable" ? " button-primary" : action === "recover" ? " button-warning" : " button-secondary";
        var apply = node("button", "button" + look, nativeCaption(action, current));
        apply.type = "button"; apply.id = "native-apply-" + item.id;
        apply.setAttribute("aria-haspopup", "dialog");
        apply.disabled = Boolean(nativeChangePending || blocked() || !nativeStates);
        apply.addEventListener("click", function () { openNativeDialog(item, action, apply.id); });
        actions.append(details, apply);
        row.append(actions);
        return row;
    }
    function appendNativePreview(content, preview) {
        var matches = node("section"), omitted = node("section"), sections = node("div", "policy-domain-dialog-grid");
        matches.append(node("h3", "", "Совместимые записи · " + preview.domains.length));
        matches.append(preview.domains.length ? node("pre", "policy-domain-text", preview.domains.join("\n")) :
            node("p", "policy-hint", "В этом списке нет совместимых записей."));
        omitted.append(node("h3", "", "Пропущенные правила · " + preview.skipped.length));
        var reasons = {
            "keenetic-includes-subdomains": "Точное имя: Keenetic включил бы поддомены",
            "unsupported-regexp": "Регулярное выражение",
            "unsupported-keyword": "Поиск по фрагменту имени",
            "invalid-native-name": "Keenetic не принимает имя в этом формате"
        };
        if (preview.skipped.length) {
            var list = node("ul", "policy-native-skipped");
            preview.skipped.forEach(function (entry) {
                var li = node("li");
                li.append(node("code", "", entry.value), document.createTextNode(" — " + (reasons[entry.reason] || "Тип правила не поддерживается")));
                list.append(li);
            });
            omitted.append(list);
        } else omitted.append(node("p", "policy-hint", "Все правила совместимы."));
        sections.append(matches, omitted);
        content.append(sections);
    }
    function nativeStateSignature(current) {
        return current ? JSON.stringify([current.phase, current.preview.sourceRevision, current.preview.sourceSHA256]) : null;
    }
    function canConfirmNative(selection) {
        if (!selection.action || blocked() || nativeChangePending || !nativeStates) return false;
        var current = nativeCurrent(selection.item.id);
        var item = selection.item.domains ? rule(selection.item.id) : selection.item;
        return Boolean(item && editableSignature(item) === selection.ownSignature &&
            nativeStateSignature(current) === selection.nativeSignature &&
            nativeAction(item, current) === selection.action &&
            (selection.action !== "enable" || (selection.preview && selection.preview.domains.length)));
    }
    function renderNativeDialog(selection) {
        var content = byId("policy-domain-dialog-content");
        content.replaceChildren();
        if (selection.preview) appendNativePreview(content, selection.preview);
        if (selection.item.domains) {
            var original = node("details", "policy-domain-original");
            original.append(node("summary", "", "Исходные правила"), node("pre", "policy-domain-text", selection.item.domains.join("\n")));
            content.append(original);
        }
        var confirm = byId("policy-domain-dialog-confirm");
        confirm.hidden = !selection.action;
        confirm.disabled = !canConfirmNative(selection);
        if (selection.action) confirm.textContent = selection.action === "enable" ? "Подтвердить включение" :
            nativeCaption(selection.action, nativeCurrent(selection.item.id));
    }
    async function openNativeDialog(item, action, opener) {
        var dialog = byId("policy-domain-dialog"), current = nativeCurrent(item.id);
        if (dialog.open || (action && (blocked() || nativeChangePending || !nativeStates))) return;
        var selection = {item: item, action: action, opener: opener, ownSignature: editableSignature(item),
            nativeSignature: nativeStateSignature(current),
            preview: action === "enable" ? null : current && current.preview || nativePreviews.get(item.id)};
        nativeDialog = selection;
        var message = byId("policy-domain-dialog-message");
        byId("policy-domain-dialog-title").textContent = item.name;
        var description = {
            enable: "Keenetic применит совместимые записи этого списка.",
            disable: "Keenetic удалит маршрут и список. Набор останется доступен в BROray.",
            recover: current && current.phase === "removing" ? "Keenetic завершит удаление правила и доменного списка." :
                "Keenetic завершит применение существующего доменного списка.",
            legacy: "После отключения прежнего маршрута этот список можно применить в Keenetic."
        }[action] || "";
        if (action === "disable" && !item.domains && !(nativePresets || []).some(function (entry) { return entry.id === item.id; })) {
            description = "Keenetic удалит маршрут и список.";
        }
        var needsPreview = !selection.preview && (!action || action === "enable");
        message.textContent = needsPreview ? "Получение состава…" : description;
        renderNativeDialog(selection);
        dialog.showModal();
        if (!needsPreview) return;
        try {
            selection.preview = await loadNativePreview(item);
            if (nativeDialog !== selection || !dialog.open) return;
            message.textContent = description;
            renderNativeDialog(selection);
        } catch (error) {
            if (error.status === 401) { errorMessage(error); return; }
            if (nativeDialog !== selection || !dialog.open) return;
            message.textContent = error.message || "Не удалось прочитать состав доменного набора.";
        }
    }
    function nativeEnvelope(reply) {
        if (!reply || reply.success !== true || !reply.data) throw new Error("Ответ о доменном наборе не подтверждён.");
        return reply.data;
    }
    function refreshNativeStates() {
        return window.BROrayUI.apiRequest(nativeApplyPath).then(nativeEnvelope).then(function (response) {
            if (!response.states || typeof response.states !== "object" || Array.isArray(response.states)) {
                throw new Error("Не удалось прочитать состояние списков Keenetic.");
            }
            nativeStates = response.states;
            dnsDrafts = Object.assign(Object.create(null), response.drafts);
            render();
        }).catch(function (error) { nativeStates = null; render(); errorMessage(error); });
    }
    async function changeNativePreset(preset, action, preview) {
        if (nativeChangePending || !nativeStates) return;
        nativeChangePending = preset.id;
        render();
        try {
            var request = {id: preset.id, action: action};
            if (action === "enable") {
                request.sourceRevision = preview.sourceRevision;
                request.sourceSHA256 = preview.sourceSHA256;
            }
            await window.BROrayUI.apiRequest(nativeApplyPath, {method: "POST",
                headers: {"X-BROray-Request": "operations", "X-BROray-Origin": window.location.origin},
                body: request}).then(nativeEnvelope);
        } catch (error) { errorMessage(error); }
        finally { await refreshNativeStates(); nativeChangePending = null; render(); }
    }
    function loadNativePreview(preset) {
        if (nativePreviewRequests.has(preset.id)) return nativePreviewRequests.get(preset.id);
        var signature = editableSignature(preset);
        var request = window.BROrayUI.apiRequest(nativeListPath, {
            method: "POST", headers: {"Content-Type": "application/json", "X-BROray-Request": "operations", "X-BROray-Origin": window.location.origin},
            body: JSON.stringify({id: preset.id})
        }).then(nativeEnvelope).then(function (response) {
            if (!response || response.id !== preset.id || !Array.isArray(response.domains) || !Array.isArray(response.skipped)) {
                throw new Error("Не удалось прочитать состав доменного набора.");
            }
            if (preset.domains && editableSignature(rule(preset.id)) !== signature) {
                throw new Error("Список изменился. Откройте его заново.");
            }
            nativePreviews.set(preset.id, response);
            return response;
        }).finally(function () { nativePreviewRequests.delete(preset.id); render(); });
        nativePreviewRequests.set(preset.id, request);
        return request;
    }
    function tags(row, values) { var list = node("div", "policy-tags"); values.forEach(function (v) { list.append(node("span", "", v)); }); row.append(list); }
    async function removeRule(r) {
        if (blocked()) return;
        if (r.sourceSHA256) {
            var original = legacyRule(r.id);
            var consent = await window.BROrayDialogs.confirm({title: original ? "Вернуть исходный список?" : "Удалить «" + r.name + "»?",
                message: original ? "Сохранённые изменения будут удалены, исходный список появится в разделе маршрутов." : "Список будет удалён.",
                confirmText: original ? "Вернуть" : "Удалить", cancelText: "Отмена", variant: "danger"});
            if (consent) run(function () { return changeDNSDraft("delete", r.id, r.sourceSHA256); });
            return;
        }
        var confirmed = await window.BROrayDialogs.confirm({title: "Удалить «" + r.name + "»?", message: "Правило и его настройки будут удалены. Остальные правила сохранятся.", confirmText: "Удалить", cancelText: "Отмена", variant: "danger"});
        if (confirmed) run(function () { return client.change({operation: "delete", id: r.id}); });
    }
    function removeButton(r) { var button = node("button", "button button-danger", r.sourceSHA256 && legacyRule(r.id) ? "Вернуть исходный список" : "Удалить"); button.type = "button"; button.id = "delete-" + r.id; button.disabled = blocked(); button.addEventListener("click", function () { removeRule(r); }); return button; }
    async function changeDNSDraft(action, id, hash, details) {
        var body = Object.assign({action: action, id: id, expectedHash: hash || "", policyRevision: state.policy.revision}, details);
        await window.BROrayUI.apiRequest(dnsDraftPath, {method: "POST",
            headers: {"Content-Type": "application/json", "X-BROray-Request": "operations", "X-BROray-Origin": window.location.origin},
            body: JSON.stringify(body)}).then(nativeEnvelope);
        nativePreviews.delete(id);
        await refreshNativeStates();
    }
    function renderOwn() {
        var root = byId("policy-own"), lists = state.policy ? state.policy.state.rules.filter(function (r) {
            return r.domains && r.domains.length && !(r.countryCodes || []).length && !(r.geoSiteTags || []).length;
        }).filter(function (r) { return !dnsDrafts[r.id]; }) : [];
        lists = lists.concat(Object.values(dnsDrafts));
        root.replaceChildren();
        var orphaned = Object.entries(nativeStates || {}).filter(function (pair) {
            return pair[1].preview.sourceRevision === "own" && !lists.some(function (r) { return r.id === pair[0]; });
        });
        if (!lists.length && !orphaned.length) root.append(node("p", "policy-empty", "Своих списков пока нет. Добавьте домены и IP вручную или импортируйте текстовый файл."));
        lists.forEach(function (r) {
            var current = nativeCurrent(r.id), row = renderNativeCard(r);
            var buttons = node("div", "policy-domain-secondary"), edit = node("button", "button button-secondary", "Редактировать"); edit.type = "button"; edit.id = "edit-" + r.id;
            var remove = removeButton(r);
            edit.disabled = remove.disabled = Boolean(blocked() || nativeChangePending || !nativeStates || current);
            edit.addEventListener("click", function () { openEditor(r); }); buttons.append(edit, remove); row.append(buttons); root.append(row);
        });
        orphaned.forEach(function (pair) {
            var managed = pair[0] === "own-premium-ip" || pair[0] === "own-premium-domains";
            root.append(renderNativeCard({id: pair[0], name: pair[1].preview.name || pair[0]}, !managed));
        });
        ["policy-add", "policy-import", "policy-save"].forEach(function (id) { byId(id).disabled = Boolean(blocked() || nativeChangePending); });
    }
    function renderFilters() {
        var root = byId("policy-filters"); root.replaceChildren();
        filters.forEach(function (p) {
            var r = rule(p.id), row = cardRule(p.id, p.name, r && r.enabled, async function (enabled) {
                await ensurePreset(p, r ? r.action : "deny"); await client.change({operation: "enabled", id: p.id, enabled: enabled});
            });
            var controls = node("div", "policy-rule-controls");
            controls.append(actionControl(p.id, p.name, r && r.action, function (action) { return ensurePreset(p, action); })); row.append(controls); root.append(row);
        });
    }
    function renderGeo(kind) {
        var root = byId("policy-" + kind), rules = state.policy ? state.policy.state.rules : [];
        var key = kind === "geoip" ? "countryCodes" : "geoSiteTags";
        var all = rules.filter(function (r) { return r[key] && r[key].length; });
        var shown = all.filter(function (r) { return !filters.some(function (p) { return p.id === r.id; }) && !r.id.startsWith("preset-"); });
        root.replaceChildren();
        if (!shown.length) root.append(node("p", "policy-empty", kind === "geoip" ? "Добавьте страну назначения. Новое правило останется выключенным." : "Добавьте категорию. Новое правило останется выключенным."));
        shown.forEach(function (r) {
            var row = cardRule(r.id, r.name, r.enabled, function (enabled) { return client.change({operation: "enabled", id: r.id, enabled: enabled}); });
            tags(row, [kind === "geoip" ? "GeoIP" : "GeoSite", actionLabel(r.action)]);
            var controls = node("div", "policy-rule-controls");
            controls.append(actionControl(r.id, r.name, r.action, function (action) { return client.change({operation: "action", id: r.id, action: action}); }), removeButton(r)); row.append(controls); root.append(row);
        });
        var date = state.policy && state.policy.state.geoUpdatedAt && state.policy.state.geoUpdatedAt[kind];
        byId("policy-" + kind + "-date").textContent = date ? "Проверено: " + new Date(date).toLocaleString("ru-RU") : "Ещё не обновлялось";
        var attempt = state.schedule && state.schedule.attempts[kind];
        byId("policy-" + kind + "-result").textContent = !attempt ? "" : {ok: "Файлы проверены", failed: "Ошибка последней попытки", running: "Обновление не завершено"}[attempt.status] || "Результат не подтверждён";
    }
    function renderSchedule() {
        if (isDomains) return;
        var policy = state.policy, ready = policy && policy.geoPending, readError = policy && policy.geoPendingError;
        var selected = policy && policy.state.rules.some(function (r) {
            return (r.countryCodes && r.countryCodes.length) || (r.geoSiteTags && r.geoSiteTags.length);
        });
        byId("policy-geodata-check").disabled = blocked() || !selected;
        var apply = byId("policy-geodata-apply");
        apply.hidden = !ready || !!readError;
        apply.disabled = blocked() || !ready || !!readError;
        byId("policy-geodata-pending").textContent = readError ? "Состояние подготовленных геоданных недоступно." :
            ready ? "Геоданные готовы и ожидают применения." : "";
        var input = byId("policy-daily"); input.checked = Boolean(state.schedule && state.schedule.enabled);
        input.disabled = state.scheduleBusy || state.scheduleUncertain || !state.schedule;
        byId("policy-daily-status").textContent = state.scheduleBusy ? "Сохранение…" : state.scheduleUncertain ? "Настройка не подтверждена. Обновите данные." : state.schedule.automationPaused ? "Автоматизация приостановлена. Ручная проверка доступна." : state.schedule.enabled ? "Включено · проверка раз в сутки" : "Выключено · ручная проверка доступна";
        var time = byId("policy-geo-apply-time"), canSetTime = state.schedule && state.schedule.applyRevision;
        var stale = applyTimeRevision !== null && canSetTime !== applyTimeRevision;
        if (applyTimeRevision === null) time.value = canSetTime ? state.schedule.applyAtUTC : "";
        time.disabled = input.disabled || !canSetTime || stale;
        byId("policy-geo-apply-save").disabled = time.disabled || applyTimeRevision === null;
        byId("policy-geo-apply-reset").hidden = applyTimeRevision === null;
        byId("policy-geo-apply-time-status").textContent = stale ? "Настройка изменилась. Сбросьте введённое время." :
            canSetTime && state.schedule.applyAtUTC ? "Расписание: " + state.schedule.applyAtUTC + " UTC" : "Применение вручную";
    }
    function render() {
        if (!state) return;
        var focus = document.activeElement && document.activeElement.id, status = byId("policy-status");
        var pending = state.policy && state.policy.pending, busy = state.busy || uiBusy || (state.policy && state.policy.busy);
        status.textContent = pending ? "Нужно восстановление" : busy ? "Выполняется" : state.uncertain ? "Не подтверждено" : "Данные получены";
        status.className = "status-badge " + (pending ? "status-warning" : busy ? "status-loading" : state.uncertain ? "status-warning" : "status-success");
        byId("policy-message").textContent = pending ? "Предыдущая операция требует восстановления. Изменения правил пока заблокированы." : state.message || (isDomains ? "" : "Переключатели сохраняются автоматически.");
        byId("policy-refresh").disabled = state.busy || uiBusy;
        if (isDomains) {
            renderPresets(); renderOwn();
            if (nativeDialog) byId("policy-domain-dialog-confirm").disabled = !canConfirmNative(nativeDialog);
        }
        else { renderFilters(); renderGeo("geosite"); renderGeo("geoip"); renderSchedule(); ["policy-add-category", "policy-add-country"].forEach(function (id) { byId(id).disabled = blocked(); }); }
        if (focus && byId(focus) && !byId(focus).disabled && document.activeElement.id !== focus) byId(focus).focus({preventScroll: true});
    }
    async function openEditor(r, imported) {
        if (editorDirty && !await window.BROrayDialogs.confirm({title: "Заменить несохранённый список?", message: "Введённые изменения будут потеряны.", confirmText: "Заменить", cancelText: "Остаться"})) return;
        editID = r ? r.id : null; editBase = editableSignature(r); draftID = null;
        byId("policy-name").value = r ? r.name : imported ? imported.name : "";
        byId("policy-domains").value = r ? r.domains.join("\n") : imported ? imported.text : "";
        byId("policy-editor-title").textContent = r ? "Редактирование списка" : "Новый список";
        byId("policy-editor-error").textContent = ""; byId("policy-editor").hidden = false; editorDirty = Boolean(imported); byId("policy-name").focus();
    }
    function bindDomains() {
        var dialog = byId("policy-domain-dialog");
        byId("policy-domain-dialog-close").addEventListener("click", function () { dialog.close(); });
        dialog.addEventListener("close", function () {
            if (dialog.open || !nativeDialog) return;
            var opener = byId(nativeDialog.opener), fallback = byId("native-preview-" + nativeDialog.item.id);
            nativeDialog = null;
            if (opener && !opener.disabled) opener.focus({preventScroll: true});
            else if (fallback) fallback.focus({preventScroll: true});
        });
        byId("policy-domain-dialog-confirm").addEventListener("click", function () {
            var selection = nativeDialog;
            if (!dialog.open || !selection || !canConfirmNative(selection)) return;
            dialog.close();
            if (selection.action === "legacy") {
                nativePreviews.delete(selection.item.id);
                run(function () { return client.change({operation: "enabled", id: selection.item.id, enabled: false}); });
            } else changeNativePreset(selection.item, selection.action, selection.preview);
        });
        byId("policy-add").addEventListener("click", function () { openEditor(null); });
        byId("policy-import").addEventListener("click", function () { byId("policy-file").click(); });
        byId("policy-file").addEventListener("change", async function () {
            var file = this.files[0]; this.value = ""; if (!file) return;
            try { if (file.size > 2 * 1024 * 1024) throw new Error("Файл превышает 2 МБ."); var text = new TextDecoder("utf-8", {fatal: true}).decode(await file.arrayBuffer()); await openEditor(null, {name: file.name.replace(/\.[^.]+$/, "").slice(0, 128), text: text}); }
            catch (error) { errorMessage(error); }
        });
        byId("policy-editor").addEventListener("input", function () { editorDirty = true; });
        byId("policy-cancel").addEventListener("click", async function () {
            if (editorDirty && !await window.BROrayDialogs.confirm({title: "Отменить изменения?", message: "Несохранённый текст списка будет потерян.", confirmText: "Отменить изменения", cancelText: "Продолжить редактирование"})) return;
            byId("policy-editor").hidden = true; editorDirty = false;
        });
        byId("policy-editor").addEventListener("submit", function (event) {
            event.preventDefault(); if (blocked() || nativeChangePending) return;
            if (editID && editableSignature(rule(editID)) !== editBase) {
                byId("policy-editor-error").textContent = "Список изменён в другой вкладке или удалён. Ваш текст сохранён в редакторе. Скопируйте его и откройте актуальный список заново.";
                return;
            }
            if (!editID && draftID && rule(draftID)) {
                byId("policy-editor-error").textContent = "Список уже появился в сохранённых данных. Проверьте его и откройте заново перед следующими изменениями.";
                return;
            }
            var name = byId("policy-name").value.trim(), domains = byId("policy-domains").value;
            var random = new Uint8Array(16); window.crypto.getRandomValues(random);
            if (!draftID) draftID = "own-" + Array.from(random, function (n) { return n.toString(16).padStart(2, "0"); }).join("");
            var id = editID || draftID, isNew = !editID;
            run(async function () {
                try {
                    var draft = dnsDrafts[id];
                    var hasIP = domains.split("\n").some(function (line) {
                        line = line.trim();
                        return !line.startsWith("#") && (line.includes(":") && !/^(domain|full):/i.test(line) || /^\d+\.\d+\.\d+\.\d+(?:\/|$)/.test(line));
                    });
                    if (draft || hasIP) {
                        await changeDNSDraft("save", id, draft && draft.sourceSHA256, {name: name, targets: domains});
                        editorDirty = false; byId("policy-editor").hidden = true;
                        return;
                    }
                    await client.change({operation: "upsert", id: id, name: name, domains: domains});
                    nativePreviews.delete(id);
                    // New VPN lists stay off until their Keenetic route is applied.
                    editID = id; editBase = editableSignature(rule(id));
                    if (isNew) await client.change({operation: "action", id: id, action: "vpn"});
                    editorDirty = false; byId("policy-editor").hidden = true;
                } catch (error) { byId("policy-editor-error").textContent = error.message; throw error; }
            });
        });
        window.addEventListener("beforeunload", function (event) { if (editorDirty) { event.preventDefault(); event.returnValue = ""; } });
    }
    function bindBlocking() {
        // ISO 3166 codes; source availability is checked when the selected file is fetched.
        var codes = "AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW".split(" ");
        var names = typeof Intl.DisplayNames === "function" ? new Intl.DisplayNames(["ru"], {type: "region"}) : null;
        codes.map(function (code) { return {code: code.toLowerCase(), name: names ? names.of(code) : code}; }).sort(function (a, b) { return a.name.localeCompare(b.name, "ru"); }).forEach(function (c) { var option = node("option", "", c.name); option.value = c.code; byId("policy-country").append(option); });
        function add(kind, selectID) {
            var select = byId(selectID), tag = select.value; if (!tag) { select.focus(); return; }
            var id = kind + "-" + tag, name = select.selectedOptions[0].textContent;
            run(async function () { if (rule(id)) throw new Error("Это правило уже добавлено."); await client.change({operation: "geo-upsert", id: id, kind: kind, name: name, tags: [tag]}); select.value = ""; });
        }
        byId("policy-add-country").addEventListener("click", function () { add("geoip", "policy-country"); });
        byId("policy-add-category").addEventListener("click", function () { add("geosite", "policy-category"); });
        byId("policy-geodata-check").addEventListener("click", function () {
            run(async function () { await client.change({operation: "geo-check", id: "geodata"}); await client.refreshSchedule(); });
        });
        byId("policy-geodata-apply").addEventListener("click", async function () {
            var ready = state.policy && state.policy.geoPending;
            if (!ready || blocked()) return;
            if (!await window.BROrayDialogs.confirm({title: "Применить геоданные?", message: "Во время применения Xray может перезапуститься.", confirmText: "Применить", cancelText: "Отмена"})) return;
            run(function () { return client.change({operation: "geo-apply", id: "geodata", expectedPendingRevision: ready.revision}); });
        });
        byId("policy-daily").addEventListener("change", async function () { var enabled = this.checked; try { await client.setSchedule(enabled); window.BROrayUI.toast("Сохранено", "success"); } catch (error) { errorMessage(error); } });
        byId("policy-geo-apply-time").addEventListener("input", function () {
            if (!state.schedule || !state.schedule.applyRevision) return;
            if (applyTimeRevision === null) applyTimeRevision = state.schedule.applyRevision;
            render();
        });
        byId("policy-geo-apply-save").addEventListener("click", async function () {
            try {
                await client.setApplyTime(byId("policy-geo-apply-time").value, applyTimeRevision);
                applyTimeRevision = null;
                render();
                window.BROrayUI.toast("Время применения сохранено", "success");
            } catch (error) { errorMessage(error); }
        });
        byId("policy-geo-apply-reset").addEventListener("click", function () { applyTimeRevision = null; render(); });
    }
    async function refresh() {
        var jobs = [client.refresh(), client.refreshSchedule()];
        if (isDomains && !nativeChangePending) jobs.push(refreshNativeStates());
        var results = await Promise.allSettled(jobs);
        results.forEach(function (r) { if (r.status === "rejected") errorMessage(r.reason); });
    }
    function passive() {
        clearTimeout(timer); if (document.hidden) return;
        timer = setTimeout(async function () { if (!uiBusy && !state.busy) await refresh(); passive(); }, 30000);
    }
    client = window.BROrayPolicy.createClient({api: window.BROrayUI.apiRequest, onChange: function (next) { state = next; render(); }});
    state = client.snapshot();
    if (isDomains) bindDomains(); else bindBlocking();
    byId("policy-refresh").addEventListener("click", refresh);
    document.addEventListener("visibilitychange", function () { if (document.hidden) clearTimeout(timer); else { refresh(); passive(); } });
    window.addEventListener("pagehide", function () { clearTimeout(timer); });
    render();
    window.BROrayUI.apiRequest("/api/session.cgi").then(function (session) {
        if (byId("current-user")) byId("current-user").textContent = session.user || "admin";
        byId("page-loader").hidden = true; byId("app").hidden = false;
        var requests = [refresh()];
        if (isDomains) requests.push(window.BROrayUI.apiRequest(nativeListPath).then(nativeEnvelope).then(function (response) {
            if (!response || !Array.isArray(response.presets)) throw new Error("Не удалось прочитать готовые доменные наборы.");
            nativePresets = response.presets;
            byId("native-domain-hint").textContent = "Посмотрите состав перед включением. Каждый сервис создаёт отдельный штатный список Keenetic.";
            render();
        }).catch(function (error) {
            byId("native-domain-hint").textContent = "Не удалось загрузить перечень готовых наборов.";
            errorMessage(error);
        }));
        return Promise.all(requests);
    }).then(passive).catch(function (error) { byId("page-loader").hidden = true; byId("app").hidden = false; errorMessage(error); });
})();
