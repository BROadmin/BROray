/* Test utility only: observe real rendered components; never touch a router. */
(function () {
    'use strict';
    var resumed = [];
    var expectedActive = new URL(location.href).searchParams.get('mode') !== 'inactive';
    document.addEventListener('broray:routes-operation-resume', function (event) {
        event.preventDefault();
        resumed.push(event.detail);
    });
    function rect(element) {
        var r = element.getBoundingClientRect();
        return {x:r.x, y:r.y, width:r.width, height:r.height, right:r.right, bottom:r.bottom};
    }
    function visible(element) { return !element.hidden && element.getBoundingClientRect().height > 0; }
    function overlap(a, b) {
        return Math.max(0, Math.min(a.right, b.right) - Math.max(a.x, b.x)) *
            Math.max(0, Math.min(a.bottom, b.bottom) - Math.max(a.y, b.y));
    }
    function hitPoints(element) {
        var r = element.getBoundingClientRect();
        return [[0.5,0.5],[0.2,0.5],[0.8,0.5]].map(function (fraction) {
            var x = r.x + r.width * fraction[0], y = r.y + r.height * fraction[1];
            var target = document.elementFromPoint(x, y);
            return {x:x, y:y, target:target ? target.closest('button')?.id || target.className : null,
                hitsControl:!!target && (element === target || element.contains(target))};
        });
    }
    function capture() {
        var toast = document.getElementById('toast-root');
        var full = document.getElementById('routes-operation-float');
        var compact = document.getElementById('routes-operation-compact');
        var widget = visible(full) ? full : visible(compact) ? compact : null;
        var controls = widget ? Array.from(widget.querySelectorAll('button')).filter(function (b) {
            return !b.disabled && visible(b);
        }) : [];
        var toasts = Array.from(toast.children).filter(visible);
        var toastRect = rect(toast), widgetRect = widget ? rect(widget) : null;
        var points = controls.map(function (button) { return {id:button.id, rect:rect(button), points:hitPoints(button)}; });
        var reasons = [];
        if (expectedActive && !widget) reasons.push('fixture_expected_operation_widget_not_visible');
        if (!expectedActive && widget) reasons.push('operation_widget_not_hidden_after_completion');
        if (!toasts.length) reasons.push('fixture_has_no_visible_toast');
        if (widgetRect && overlap(toastRect, widgetRect) > 1) reasons.push('toast_overlaps_route_widget');
        if (points.some(function (button) { return button.points.some(function (point) { return !point.hitsControl; }); })) reasons.push('route_control_obscured');
        if (toastRect.x < -1 || toastRect.right > innerWidth + 1 || toastRect.y < 0 || toastRect.bottom > innerHeight + 1) reasons.push('toast_outside_viewport');
        if (widgetRect && (widgetRect.x < -1 || widgetRect.right > innerWidth + 1 || widgetRect.y < 0 || widgetRect.bottom > innerHeight + 1)) reasons.push('route_widget_outside_viewport');
        if (!widget && (toastRect.bottom < innerHeight - 32 || toastRect.right < innerWidth - 32)) reasons.push('inactive_toast_lost_bottom_right_placement');
        return {pass:!reasons.length, reasons:reasons, viewport:{width:innerWidth,height:innerHeight},
            widget:widget ? widget.id : null, toastCount:toasts.length, toastRect:toastRect, widgetRect:widgetRect,
            toastStyle:{bottom:getComputedStyle(toast).bottom,maxHeight:getComputedStyle(toast).maxHeight,zIndex:getComputedStyle(toast).zIndex},
            controls:points, resumeEvents:resumed.slice()};
    }
    async function setMode(mode, longTitle) {
        expectedActive = mode !== 'inactive';
        var response = await fetch('/__fixture__/state', {method:'POST',headers:{'Content-Type':'application/json'},
            body:JSON.stringify({mode:mode,longTitle:longTitle === true})});
        if (!response.ok) throw new Error('fixture state failed: ' + response.status);
        await BROrayRoutesOperationUI.refresh();
    }
    async function prepare() {
        var query = new URL(location.href).searchParams;
        // Authentication/page boot is outside this local component fixture.
        document.getElementById('app').hidden = false;
        document.getElementById('page-loader').hidden = true;
        await document.fonts.ready;
        await setMode(query.get('mode') || 'paused', query.get('long') === '1');
        var count = query.get('stack') === '3' ? 3 : 1;
        for (var i = 0; i < count; i++) BROrayUI.toast(
            'Удаление маршрутов остановлено по запросу пользователя. Операцию можно продолжить.' + (i ? ' Сообщение ' + (i + 1) + '.' : ''), 'warning');
        await Promise.all(Array.from(document.querySelectorAll('.toast')).flatMap(function (element) {
            return element.getAnimations().map(function (animation) { return animation.finished; });
        }));
        return true;
    }
    async function frameViewport(width, height, query) {
        var frame = document.createElement('iframe');
        frame.style.cssText = 'display:block;border:0;width:' + width + 'px;height:' + height + 'px';
        frame.src = '/layout?' + query;
        await new Promise(function (resolve, reject) {
            frame.onload = resolve;
            frame.onerror = reject;
            document.body.replaceChildren(frame);
        });
        await frame.contentWindow.layoutProbe.ready;
        window.layoutFrame = frame;
        return {nestedViewport:true, capture:frame.contentWindow.layoutProbe.capture()};
    }
    window.layoutProbe = {capture:capture, setMode:setMode, resumeEvents:resumed, frameViewport:frameViewport};
    window.layoutProbe.ready = prepare();
})();
