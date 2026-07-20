"use strict";

window.BROray = window.BROray || {};

BROray.servers = {
    async load(){
        const j = await BROray.api.requestJson(
            "cgi-bin/servers.cgi?" + Date.now()
        );

        const servers = Array.isArray(j.servers)
            ? j.servers
            : [];

        let html = "";
        let activeServer = null;

        for(const server of servers){
            const id = encodeURIComponent(String(server.id));

            html +=
                '<div class="server ' +
                (server.active ? "active" : "") +
                '">' +

                '<div class="server-status ' +
                (server.active ? "is-active" : "is-inactive") +
                '">' +
                (server.active
                    ? "Активный"
                    : "Неактивный") +
                "</div>" +

                '<div class="pname">' +
                BROray.api.escapeHtml(
                    BROray.api.text(server.name)
                ) +
                "</div>" +

                '<div class="paddr">🌐 ' +
                BROray.api.escapeHtml(
                    BROray.api.text(server.address)
                ) +
                ":" +
                BROray.api.escapeHtml(
                    BROray.api.text(server.port)
                ) +
                "</div>" +

                '<div class="server-tags">' +
                '<span class="tag">' +
                BROray.api.escapeHtml(
                    BROray.api.text(server.network)
                ) +
                "</span>" +
                '<span class="tag">' +
                BROray.api.escapeHtml(
                    BROray.api.text(server.security)
                ) +
                "</span>" +
                "</div>" +

                '<div class="server-actions">' +

                (server.active
                    ? '<button type="button" ' +
                      'class="server-selected" disabled>' +
                      "✅ Выбрано" +
                      "</button>"
                    : '<button type="button" ' +
                      'class="server-select" ' +
                      'data-select-server-id="' +
                      id +
                      '">' +
                      "▶ Выбрать" +
                      "</button>" +
                      '<button type="button" class="server-rename" title="Переименовать сервер" aria-label="Переименовать сервер" onclick="BROray.servers.rename(decodeURIComponent(this.dataset.renameServerId))" data-rename-server-id="' +
                      id +
                      '">✏️</button>' +

                      '<button type="button" ' +
                      'class="server-delete" title="Удалить сервер" aria-label="Удалить сервер" ' +
                      'data-delete-server-id="' +
                      id +
                      '">' +
                      "🗑️" +
                      "</button>") +

                "</div>" +
                "</div>";

            if(server.active){
                activeServer = server;
            }
        }

        const container = document.getElementById("servers");

        container.innerHTML =
            html ||
            '<div class="info-text">Серверы не найдены.</div>';

        container
            .querySelectorAll(
                ".server-select[data-select-server-id]"
            )
            .forEach(function(button){
                button.addEventListener("click", function(){
                    BROray.servers.activate(
                        decodeURIComponent(
                            button.dataset.selectServerId
                        )
                    );
                });
            });

        container
            .querySelectorAll(
                ".server-delete[data-delete-server-id]"
            )
            .forEach(function(button){
                button.addEventListener("click", function(){
                    BROray.servers.remove(
                        decodeURIComponent(
                            button.dataset.deleteServerId
                        )
                    );
                });
            });

        document.getElementById("cnet").textContent =
            activeServer
                ? BROray.api.text(activeServer.network)
                : "—";

        document.getElementById("csec").textContent =
            activeServer
                ? BROray.api.text(activeServer.security)
                : "—";
    },

    async add(){
        const value = prompt("Вставьте ссылку конфигурации VLESS:");
        if(value === null){ return; }
        const uri = value.trim();
        if(!uri){ alert("Ссылка не указана"); return; }
        try{
            const j = await BROray.api.requestJson(
                "cgi-bin/import.cgi",
                {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({uri: uri})
                }
            );
            if(!j.ok){
                alert(j.error || "Не удалось добавить конфигурацию");
                return;
            }
            alert(j.message || "Конфигурация добавлена");
            await BROray.servers.load();
        }catch(error){
            alert("Ошибка добавления: " + error.message);
        }
    },
    async activate(id){
        if(!confirm("Выбрать этот сервер?")){
            return;
        }

        try{
            const j = await BROray.api.requestJson(
                "cgi-bin/use.cgi",
                {
                    method: "POST",
                    headers: {
                        "Content-Type":
                            "application/x-www-form-urlencoded"
                    },
                    body: "id=" + encodeURIComponent(id)
                }
            );

            if(!j.ok){
                alert(
                    j.error ||
                    "Не удалось выбрать сервер"
                );
                return;
            }

            await BROray.servers.load();
            await BROray.dashboard.refresh();
        }catch(error){
            alert(
                "Ошибка выбора сервера: " +
                error.message
            );
        }
    },

    async rename(id){
        const value = prompt("Введите новое имя сервера:");
        if(value === null){ return; }
        const name = value.trim();
        if(!name){ alert("Имя не указано"); return; }
        try{
            const j = await BROray.api.requestJson(
                "cgi-bin/rename.cgi",
                {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({id: id, name: name})
                }
            );
            if(!j.ok){
                alert(j.error || "Не удалось переименовать сервер");
                return;
            }
            await BROray.servers.load();
            await BROray.dashboard.refresh();
        }catch(error){
            alert("Ошибка переименования: " + error.message);
        }
    },
    async remove(id){
        if(!confirm("Удалить этот сервер?")){
            return;
        }

        try{
            const j = await BROray.api.requestJson(
                "cgi-bin/delete.cgi",
                {
                    method: "POST",
                    headers: {
                        "Content-Type":
                            "application/x-www-form-urlencoded"
                    },
                    body: "id=" + encodeURIComponent(id)
                }
            );

            if(!j.ok){
                alert(
                    j.error ||
                    "Не удалось удалить сервер"
                );
                return;
            }

            await BROray.servers.load();
        }catch(error){
            alert(
                "Ошибка удаления сервера: " +
                error.message
            );
        }
    }
};
