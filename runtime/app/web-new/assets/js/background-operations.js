(function () {
    "use strict";
    var snapshot = null, busy = false, timer = null, refreshing = false, fingerprint = "";
    var types = {subscription_update:"Обновление подписок",server_operation:"Работа с серверами",auto_switch:"Автовыбор сервера",xray_maintenance:"Обслуживание Xray",dns_operation:"DNS-over-TLS",router_operation:"Настройка роутера",route_operation:"Маршруты"};
    var sources = {USER:"Вручную",SCHEDULER:"По расписанию",SUBSCRIPTION_AUTO:"Автообновление подписок",SERVER_CHECK_AUTO:"Автопроверка серверов",AUTO_SWITCH:"Автовыбор",UPDATER:"Обновление BROray",SYSTEM_RECOVERY:"Восстановление"};
    var phases = {starting:"Подготовка",working:"Выполняется",checking:"Проверка",fetching:"Загрузка",parsing:"Обработка",committing:"Сохранение изменений",switching:"Переключение",waiting:"Ожидание",recovering:"Восстановление",finished:"Завершено"};
    var messages = {started:"Операция запущена",lock_acquired:"Ресурсы зарезервированы",lock_conflict:"Действие отложено: выполняется другая операция",phase_changed:"Этап операции изменился",owner_transferred:"Операция передана фоновому исполнителю",cancel_requested:"Запрошена остановка",completed:"Операция завершена",failed:"Операция завершилась с ошибкой",aborted:"Операция прервана",recovered:"Незавершённая операция восстановлена",ambiguous_owner:"Владелец операции не подтверждён",heartbeat_problem:"Нет свежего сообщения о ходе операции",term:"Управляемому процессу отправлен сигнал завершения",kill:"Управляемый процесс принудительно завершён"};
    function byId(id) { return document.getElementById(id); }
    function node(tag, text, className) { var el = document.createElement(tag); if (text !== undefined) el.textContent = text; if (className) el.className = className; return el; }
    function text(id, value) { byId(id).textContent = value; }
    function date(value, timeOnly) { var d = new Date(value); return !value || isNaN(d.getTime()) ? "—" : timeOnly ? d.toLocaleTimeString("ru-RU") : d.toLocaleString("ru-RU"); }
    function badge(id, label, kind) { var el = byId(id); el.textContent = label; el.className = "status-badge status-" + kind; }
    async function request(endpoint, payload) {
        var controller = new AbortController(), timeout = setTimeout(function () { controller.abort(); }, 15000);
        try {
            var options = {method:payload === undefined ? "GET" : "POST",credentials:"same-origin",cache:"no-store",signal:controller.signal,headers:{Accept:"application/json"}};
            if (payload !== undefined) { options.headers["Content-Type"] = "application/json"; options.headers["X-BROray-Request"] = "operations"; options.body = JSON.stringify(payload); }
            var response = await fetch("/api/operations/" + endpoint + ".cgi", options), data = await response.json();
            if (!response.ok || data.ok === false || data.success === false) { var error = new Error("Состояние временно недоступно."); error.data = data; error.status = response.status; throw error; }
            return data;
        } finally { clearTimeout(timeout); }
    }
    function render(data, stale) {
        var active = data && Array.isArray(data.operations) ? data.operations.filter(function (op) { return op.running !== false; }) : [];
        var unknown = stale || !data || data.complete !== true || data.globalFence === "ambiguous";
        badge("bg-badge", unknown ? "Нужно проверить состояние" : active.length ? "Выполняется: " + active.length : "Нет операций", unknown ? "warning" : active.length ? "loading" : "success");
        text("bg-summary", unknown ? (snapshot ? "Не удалось обновить состояние. Ниже — последние полученные данные; они могут быть устаревшими." : "Не удалось получить состояние операций.") : active.length ? "Текущие фоновые задачи BROray." : "Активных фоновых операций нет.");
        byId("bg-recover").hidden = !unknown && !active.some(function (op) { return op.ownerStatus !== "ACTIVE"; });
        var nextFingerprint = JSON.stringify([active,unknown,busy]);
        if (nextFingerprint !== fingerprint) {
            var focused = document.activeElement && document.activeElement.dataset.operation;
            byId("bg-list").replaceChildren();
            active.forEach(function (op) {
                var row = node("article",undefined,"bg-operation"), facts = node("dl",undefined,"bg-facts");
                row.appendChild(node("strong",types[op.type] || "Фоновая операция"));
                [["Источник",sources[op.source] || "Неизвестен"],["Этап",phases[op.phase] || "Неизвестен"],["Начало",date(op.startedAt)],["Владелец",op.ownerStatus === "ACTIVE" ? "Подтверждён" : "Требуется проверка"]].forEach(function (fact) { var div=node("div"); div.append(node("dt",fact[0]),node("dd",fact[1])); facts.append(div); });
                row.appendChild(facts);
                if (op.cancelability === "cooperative" && op.operationId) {
                    var actions=node("div",undefined,"bg-actions"), button=node("button",op.cancelRequested ? "Остановка запрошена" : "Остановить операцию","button button-secondary");
                    button.type="button"; button.dataset.operation=op.operationId; button.disabled=busy || unknown || op.cancelRequested;
                    button.addEventListener("click",function () { mutate("cancel",{operationId:op.operationId}); }); actions.append(button); row.append(actions);
                } else row.appendChild(node("p","Выполняется защищённый этап. Остановка недоступна до его завершения.","section-note"));
                byId("bg-list").appendChild(row);
            });
            if (focused) { var restored=Array.from(byId("bg-list").querySelectorAll("button")).find(function (el) { return el.dataset.operation===focused; }); if (restored) restored.focus({preventScroll:true}); }
            fingerprint=nextFingerprint;
        }
        var paused=data && data.automationPaused;
        badge("bg-automation-badge",stale || typeof paused !== "boolean" ? "Состояние неизвестно" : paused ? "На паузе" : "Включена",stale || paused ? "warning" : "neutral");
        byId("bg-resume").hidden=paused !== true;
        ["bg-stop-all","bg-resume","bg-recover"].forEach(function (id) { byId(id).disabled=busy; });
    }
    async function refresh() {
        if (refreshing) return; refreshing=true;
        try { var data=await request("status"); snapshot=data; render(snapshot,false); }
        catch (error) { if (error.data && Array.isArray(error.data.operations)) snapshot=error.data; render(snapshot,true); }
        finally { refreshing=false; }
    }
    async function journal() {
        try {
            var data=await request("events");
            if (!Array.isArray(data.events)) throw new Error("Invalid snapshot");
            badge("bg-journal-badge",data.complete === true ? "Последние события" : "Журнал неполный",data.complete === true ? "neutral" : "warning");
            byId("bg-events").replaceChildren();
            data.events.slice(-20).reverse().forEach(function (event) { var li=node("li");li.append(node("time",date(event.timestamp,true)),node("span",messages[event.event] || "Событие не распознано"));byId("bg-events").append(li); });
            if (!data.events.length) byId("bg-events").appendChild(node("li","Событий пока нет."));
        } catch (error) { badge("bg-journal-badge","Не удалось обновить журнал","warning"); }
    }
    async function mutate(endpoint, payload) {
        if (busy) return; busy=true; render(snapshot,false); text("bg-feedback","Выполняется запрос…");
        try {
            var data=await request(endpoint,payload);
            var message="Настройка автоматики сохранена.";
            if (endpoint === "cancel") message=data.alreadyFinished ? "Операция уже завершена." : "Остановка запрошена. Ожидаем завершения текущего шага.";
            if (endpoint === "stop-background") message="Новые автоматические задачи поставлены на паузу. Для доступных операций запрошена остановка; защищённые этапы сохраняются.";
            if (endpoint === "recover") message="Проверка завершена. Подтверждённые остаточные блокировки обработаны.";
            text("bg-feedback",message);
        } catch (error) {
            text("bg-feedback",endpoint === "recover" ? "Блокировка сохранена: завершение владельца или его задач не подтверждено. Скачайте диагностический отчёт." : error.status === 409 ? "Операция перешла на защищённый этап или занята другой задачей. Обновите состояние." : "Запрос не подтверждён. Обновите состояние перед повторным действием.");
        } finally { busy=false; await refresh(); await journal(); }
    }
    async function report(copy) {
        text("bg-report-feedback","Подготовка отчёта…");
        try {
            var data=await request("report"), raw=JSON.stringify(data,null,2);
            if (data.reportKind !== "broray-diagnostics" || raw.length > 1048576) throw new Error("Invalid report");
            if (copy) {
                try { await navigator.clipboard.writeText(raw); }
                catch (error) { var area=byId("bg-report-copy");area.hidden=false;area.value=raw;area.focus();area.select();text("bg-report-feedback","Выделите и скопируйте отчёт из поля ниже.");return; }
            } else {
                var url=URL.createObjectURL(new Blob([raw+"\n"],{type:"application/json"})), link=node("a");link.href=url;link.download="BROray-diagnostics.json";link.click();setTimeout(function () { URL.revokeObjectURL(url); },1000);
            }
            text("bg-report-feedback",(copy ? "Отчёт скопирован." : "Отчёт скачан.")+(data.complete === false ? " Недоступные сведения отмечены в отчёте." : ""));
        } catch (error) { text("bg-report-feedback","Не удалось получить отчёт. Повторите после обновления состояния."); }
    }
    function schedule() { clearTimeout(timer); if (!document.hidden) timer=setTimeout(async function () { if (!busy) await refresh();schedule(); },10000); }
    function init() {
        if (!byId("bg-title")) return;
        byId("bg-refresh").addEventListener("click",refresh);
        byId("bg-journal-refresh").addEventListener("click",journal);
        byId("bg-stop-all").addEventListener("click",function () { mutate("stop-background",{pauseAutomation:true}); });
        byId("bg-resume").addEventListener("click",function () { mutate("automation",{paused:false}); });
        byId("bg-recover").addEventListener("click",function () { mutate("recover",{}); });
        byId("bg-copy").addEventListener("click",function () { report(true); });
        byId("bg-download").addEventListener("click",function () { report(false); });
        document.addEventListener("visibilitychange",schedule);refresh();journal();schedule();
    }
    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded",init,{once:true});else init();
})();
