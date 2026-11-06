/* Inert presentation of provider metadata. No network requests. */
(function () {
    "use strict";
    function escape(value) { return String(value).replace(/[&<>"']/g, function (c) { return {"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]; }); }
    function text(value, max) { return typeof value === "string" && value.length <= max ? escape(value) : ""; }
    function number(value) { return Number.isSafeInteger(value) && value >= 0; }
    function bytes(value) { if (!number(value)) return "Не указано"; if (value < 1024) return value + " Б"; var n=value, units=["Б","КиБ","МиБ","ГиБ","ТиБ","ПиБ"], i=0; while(n>=1024 && i<5) { n/=1024;i++; } return n.toFixed(2) + " " + units[i]; }
    function safeLink(value) {
        if(typeof value!=="string" || value.length>2048 || /[\u0000-\u0020\u007f]/.test(value)) return "";
        try { var u=new URL(value); return (u.protocol==="https:" || u.protocol==="http:") && !u.username && !u.password ? escape(u.href) : ""; } catch(ignore) { return ""; }
    }
    function render(item) {
        var p=item.providerMetadata;
        if(!p || p.schemaVersion!==1) return "";
        var fields=[], usage=p.usage || {}, links=[];
        function field(label, value) { if(value) fields.push("<div><dt>"+label+"</dt><dd>"+value+"</dd></div>"); }
        field("Название у провайдера",text(p.title,128));
        if(number(usage.upload)) field("Отправлено",bytes(usage.upload));
        if(number(usage.download)) field("Получено",bytes(usage.download));
        if(number(usage.total)) field("Лимит трафика",usage.total===0 ? "Провайдер не указал лимит" : bytes(usage.total));
        if(number(usage.total) && usage.total>0 && number(usage.upload) && number(usage.download) && Number.isSafeInteger(usage.upload+usage.download)) field("Осталось",bytes(Math.max(0,usage.total-usage.upload-usage.download)));
        if(number(usage.expire)) { var d=new Date(usage.expire*1000); field("Срок действия",usage.expire>0 && Number.isFinite(d.getTime()) ? escape(d.toLocaleString("ru-RU")) : "Провайдер не указал срок"); }
        if(number(p.suggestedUpdateMinutes) && p.suggestedUpdateMinutes>=60 && p.suggestedUpdateMinutes<=10080) field("Интервал провайдера",p.suggestedUpdateMinutes+" мин. Ваш интервал не изменён.");
        [["Поддержка",p.supportUrl],["Страница подписки",p.webPageUrl],["Подробнее об объявлении",p.announcementUrl]].forEach(function(pair){var href=safeLink(pair[1]);if(href) links.push('<a target="_blank" rel="noopener noreferrer" href="'+href+'">'+pair[0]+'</a>');});
        var announce=text(p.announcement,1024), invalid=Array.isArray(p.invalidFields) && p.invalidFields.length, ignored=Array.isArray(p.ignoredDirectives) && p.ignoredDirectives.length;
        if(!fields.length && !announce && !links.length && !invalid && !ignored) return "";
        return '<details class="subscription-provider"><summary>Сведения провайдера</summary>'+((item.lastUpdateStatus==="error" || item.lastUpdateStatus==="running") ? '<p>Сведения из последнего успешного обновления; сейчас они не подтверждены.</p>' : '')+'<dl class="subscription-card-meta">'+fields.join("")+'</dl>'+(announce ? '<p class="subscription-url">'+announce+'</p>' : '')+(links.length ? '<p>'+links.join(" · ")+'</p>' : '')+(invalid ? '<p>Часть сведений провайдера имеет неверный формат и не использована.</p>' : '')+(ignored ? '<p>Указания провайдера о маршрутах, DNS и принудительных обновлениях не применяются.</p>' : '')+'</details>';
    }
    window.BRORAYSubscriptionProvider={render:render, bytes:bytes, safeLink:safeLink};
}());
