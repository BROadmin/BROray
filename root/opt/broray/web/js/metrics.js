"use strict";

(function(){
    function element(id){
        return document.getElementById(id);
    }

    function setValue(id, value){
        const target = element(id);

        if(target){
            target.textContent = value;
        }
    }

    function numberValue(value){
        const number = Number(value);

        return Number.isFinite(number)
            ? number
            : null;
    }

    function ageText(timestamp){
        const checkedAt = numberValue(timestamp);

        if(checkedAt === null){
            return "—";
        }

        const age = Math.max(
            0,
            Math.floor(Date.now() / 1000) - checkedAt
        );

        if(age < 60){
            return age + " с";
        }

        if(age < 3600){
            return Math.floor(age / 60) + " мин";
        }

        return Math.floor(age / 3600) + " ч";
    }

    function render(connection){
        connection = connection || {};

        const ping = numberValue(
            connection.ping_ms
        );

        const jitter = numberValue(
            connection.jitter_ms
        );

        const breaks = numberValue(
            connection.breaks
        );

        setValue(
            "metric-ping",
            ping === null
                ? "—"
                : Math.round(ping) + " мс"
        );

        setValue(
            "metric-jitter",
            jitter === null
                ? "—"
                : jitter.toFixed(1) + " мс"
        );

        setValue(
            "metric-quality",
            connection.quality || "—"
        );

        setValue(
            "metric-check",
            ageText(connection.checked_at)
        );

        setValue(
            "metric-breaks",
            breaks === null
                ? "0"
                : String(Math.round(breaks))
        );
    }

    async function refresh(){
        try{
            const response = await fetch(
                "cgi-bin/status.cgi?" + Date.now(),
                {
                    cache:"no-store"
                }
            );

            if(!response.ok){
                throw new Error(
                    "HTTP " + response.status
                );
            }

            const result = await response.json();

            render(result.connection);
        }catch(error){
            render({
                quality:"Ошибка"
            });
        }
    }

    document.addEventListener(
        "DOMContentLoaded",
        function(){
            refresh();

            window.setInterval(
                refresh,
                10000
            );
        }
    );
})();
