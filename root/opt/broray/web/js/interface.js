"use strict";

window.BROray = window.BROray || {};

BROray.interfaceControl = {
    busy: false,

    value(value, fallback = "—"){
        if(
            value === undefined ||
            value === null ||
            value === ""
        ){
            return fallback;
        }

        return String(value);
    },

    setText(id, value){
        const element =
            document.getElementById(id);

        if(element){
            element.textContent =
                this.value(value);
        }
    },

    setButtonsDisabled(disabled){
        const buttons =
            document.querySelectorAll(
                "[data-interface-action]"
            );

        buttons.forEach(function(button){
            button.disabled = disabled;
        });
    },

    render(data){
        const block =
            document.getElementById(
                "interface-status-block"
            );

        if(!block){
            return;
        }

        const state =
            data &&
            data.interface
                ? data.interface
                : {};

        const exists =
            Boolean(state.exists);

        const healthy =
            Boolean(state.healthy);

        let statusClass =
            "interface-status-missing";

        let statusText =
            "Интерфейс не создан";

        if(exists && healthy){
            statusClass =
                "interface-status-working";
            statusText =
                "Интерфейс подключён";
        }else if(exists){
            statusClass =
                "interface-status-warning";
            statusText =
                "Интерфейс требует восстановления";
        }

        const upstreamHost =
            this.value(
                state.upstream &&
                state.upstream.host
            );

        const upstreamPort =
            this.value(
                state.upstream &&
                state.upstream.port
            );

        let upstream =
            "—";

        if(
            upstreamHost !== "—" ||
            upstreamPort !== "—"
        ){
            upstream =
                upstreamHost +
                ":" +
                upstreamPort;
        }

        block.innerHTML =
            '<div class="' +
                'interface-status-line ' +
                BROray.api.escapeHtml(
                    statusClass
                ) +
            '">' +
                BROray.api.escapeHtml(
                    statusText
                ) +
            '</div>' +

            '<div class="interface-details">' +

                '<div class="interface-detail-row">' +
                    '<span>Интерфейс</span>' +
                    '<strong>' +
                        BROray.api.escapeHtml(
                            this.value(state.name)
                        ) +
                    '</strong>' +
                '</div>' +

                '<div class="interface-detail-row">' +
                    '<span>Описание</span>' +
                    '<strong>' +
                        BROray.api.escapeHtml(
                            this.value(
                                state.description
                            )
                        ) +
                    '</strong>' +
                '</div>' +

                '<div class="interface-detail-row">' +
                    '<span>Тип и протокол</span>' +
                    '<strong>' +
                        BROray.api.escapeHtml(
                            this.value(state.type)
                        ) +
                        " / " +
                        BROray.api.escapeHtml(
                            this.value(
                                state.protocol
                            )
                        ) +
                    '</strong>' +
                '</div>' +

                '<div class="interface-detail-row">' +
                    '<span>SOCKS5 upstream</span>' +
                    '<strong>' +
                        BROray.api.escapeHtml(
                            upstream
                        ) +
                    '</strong>' +
                '</div>' +

                '<div class="interface-detail-row">' +
                    '<span>Состояние</span>' +
                    '<strong>' +
                        BROray.api.escapeHtml(
                            this.value(state.link)
                        ) +
                        " / " +
                        BROray.api.escapeHtml(
                            this.value(
                                state.connected
                            )
                        ) +
                        " / " +
                        BROray.api.escapeHtml(
                            this.value(state.state)
                        ) +
                    '</strong>' +
                '</div>' +

                '<div class="interface-detail-row">' +
                    '<span>Подключение через</span>' +
                    '<strong>' +
                        BROray.api.escapeHtml(
                            this.value(state.via)
                        ) +
                    '</strong>' +
                '</div>' +

                '<div class="interface-detail-row">' +
                    '<span>Локальный адрес</span>' +
                    '<strong>' +
                        BROray.api.escapeHtml(
                            this.value(
                                state.localAddress
                            )
                        ) +
                    '</strong>' +
                '</div>' +

            '</div>';

        const createButton =
            document.getElementById(
                "interface-create-button"
            );

        const repairButton =
            document.getElementById(
                "interface-repair-button"
            );

        const deleteButton =
            document.getElementById(
                "interface-delete-button"
            );

        if(createButton){
            createButton.disabled =
                this.busy || exists;
        }

        if(repairButton){
            repairButton.disabled =
                this.busy || !exists;
        }

        if(deleteButton){
            deleteButton.disabled =
                this.busy || !exists;
        }
    },

    showMessage(type, text){
        const message =
            document.getElementById(
                "interface-action-message"
            );

        if(!message){
            return;
        }

        message.className =
            "action-message";

        if(type){
            message.classList.add(type);
        }

        message.textContent =
            text || "";
    },

    showOutput(text){
        const output =
            document.getElementById(
                "interface-operation-output"
            );

        if(output){
            output.textContent =
                text || "Операция завершена.";
        }
    },

    async load(showErrors){
        try{
            const data =
                await BROray.api.requestJson(
                    "cgi-bin/interface.cgi" +
                    "?action=status&" +
                    Date.now()
                );

            this.render(data);

            if(
                data &&
                data.check &&
                data.check.output
            ){
                this.showOutput(
                    data.check.output
                );
            }

            return data;
        }catch(error){
            if(showErrors){
                this.showMessage(
                    "error",
                    "Не удалось получить состояние интерфейса"
                );
            }

            console.error(
                "BROray interface:",
                error
            );

            return null;
        }
    },

    async action(action, button){
        if(this.busy){
            return;
        }

        if(
            action === "delete" &&
            !window.confirm(
                "Удалить интерфейс Proxy0?"
            )
        ){
            return;
        }

        this.busy = true;
        this.setButtonsDisabled(true);
        this.showMessage(
            "",
            "Выполнение операции…"
        );

        const oldText =
            button
                ? button.textContent
                : "";

        if(button){
            button.textContent =
                "Выполняется…";
        }

        try{
            const data =
                await BROray.api.requestJson(
                    "cgi-bin/interface.cgi" +
                    "?action=" +
                    encodeURIComponent(action) +
                    "&" +
                    Date.now(),
                    {
                        method: "POST",
                        cache: "no-store"
                    }
                );

            const actionData =
                data && data.action
                    ? data.action
                    : {};

            this.showOutput(
                actionData.output ||
                (
                    data &&
                    data.check &&
                    data.check.output
                ) ||
                "Операция завершена."
            );

            if(data && data.ok){
                this.showMessage(
                    "success",
                    "Операция выполнена"
                );
            }else{
                this.showMessage(
                    "error",
                    (
                        data &&
                        data.error
                    ) ||
                    "Операция завершилась с ошибкой"
                );
            }

            this.render(data);
        }catch(error){
            this.showMessage(
                "error",
                "Ошибка обращения к interface.cgi"
            );

            this.showOutput(
                String(error)
            );
        }finally{
            this.busy = false;

            if(button){
                button.textContent =
                    oldText;
            }

            await this.load(false);
        }
    },

    init(){
        if(
            !document.getElementById(
                "interface-status-block"
            )
        ){
            return;
        }

        this.load(false);
    }
};

if(document.readyState === "loading"){
    document.addEventListener(
        "DOMContentLoaded",
        function(){
            BROray.interfaceControl.init();
        },
        {
            once: true
        }
    );
}else{
    BROray.interfaceControl.init();
}
