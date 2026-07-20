"use strict";

window.BROray = window.BROray || {};

BROray.dashboard = {
    proxySelected(current){
        if(!current){
            return false;
        }

        return Boolean(
            current.id ||
            current.name ||
            current.address ||
            current.port
        );
    },

    renderProxyStatus(type, text){
        const status =
            document.getElementById(
                "status"
            );

        if(!status){
            return;
        }

        status.innerHTML =
            '<div class="' +
                'proxy-main-status ' +
                BROray.api.escapeHtml(type) +
            '">' +
                BROray.api.escapeHtml(text) +
            '</div>';
    },

    async loadStatus(){
        const j =
            await BROray.api.requestJson(
                "cgi-bin/status.cgi?" +
                Date.now()
            );

        if(!j){
            throw new Error(
                "Пустой ответ status.cgi"
            );
        }

        const current =
            j.current || {};

        const broray =
            j.broray || {};

        const system =
            j.system || {};

        const running =
            Boolean(
                j.xray &&
                j.xray.status === "running"
            );

        const selected =
            this.proxySelected(
                current
            );

        if(selected && running){
            this.renderProxyStatus(
                "is-working",
                "Выбранный прокси работает"
            );
        }else if(selected && !running){
            this.renderProxyStatus(
                "is-stopped",
                "Выбранный прокси остановлен"
            );
        }else if(!selected && running){
            this.renderProxyStatus(
                "is-warning",
                "Требуется выбрать прокси"
            );
        }else{
            this.renderProxyStatus(
                "is-empty",
                "Прокси не выбран"
            );
        }

        document.getElementById(
            "cname"
        ).textContent =
            selected
                ? BROray.api.text(
                    current.name
                )
                : "—";

        document.getElementById(
            "caddr"
        ).textContent =
            selected
                ? (
                    BROray.api.text(
                        current.address
                    ) +
                    "   Порт " +
                    BROray.api.text(
                        current.port
                    )
                )
                : "—";

        document.getElementById(
            "version"
        ).textContent =
            "v" +
            BROray.api.text(
                broray.version
            );

        document.getElementById(
            "uptime"
        ).textContent =
            BROray.api
                .text(
                    system.uptime
                )
                .replace(
                    /, *load average.*/,
                    ""
                );
    },

    async refresh(){
        if(BROray.state.refreshBusy){
            return;
        }

        BROray.state.refreshBusy =
            true;

        try{
            await Promise.all([
                BROray.dashboard.loadStatus(),
                BROray.servers.load()
            ]);
        }catch(error){
            this.renderProxyStatus(
                "is-error",
                "Ошибка прокси"
            );

            console.error(
                "BROray dashboard:",
                error
            );
        }finally{
            BROray.state.refreshBusy =
                false;
        }
    }
};

window.refreshHome = function(){
    return BROray.dashboard.refresh();
};
