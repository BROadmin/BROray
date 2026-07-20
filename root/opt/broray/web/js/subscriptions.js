"use strict";

window.BROray = window.BROray || {};
BROray.state = BROray.state || {};

BROray.subscriptions = {
    busy: false,
    currentSubscriptionId: "",

    setMessage(message, type){
        const element =
            document.getElementById("subscription-action-message");

        if(!element){
            return;
        }

        element.textContent = message || "";
        element.className =
            "action-message" + (type ? " " + type : "");
    },

    async postJson(url, data){
        return BROray.api.requestJson(url, {
            method: "POST",
            headers: {
                "Content-Type": "application/json"
            },
            body: JSON.stringify(data || {})
        });
    },

    async load(){
        const output =
            document.getElementById("subscriptions-output");

        output.innerHTML =
            '<div class="loading">Получение списка подписок…</div>';

        try{
            const result =
                await BROray.api.requestJson(
                    "cgi-bin/subscriptions.cgi?" + Date.now()
                );

            if(!result.ok){
                throw new Error(
                    result.error ||
                    result.output ||
                    "Не удалось получить подписки"
                );
            }

            const subscriptions =
                Array.isArray(result.subscriptions)
                    ? result.subscriptions
                    : [];

            if(subscriptions.length === 0){
                output.innerHTML =
                    '<div class="empty-state">' +
                    "Подписки пока не добавлены." +
                    "</div>";

                BROray.state.subscriptionsLoaded = true;
                return;
            }

            output.innerHTML =
                subscriptions.map(
                    subscription =>
                        this.renderSubscription(subscription)
                ).join("");

            BROray.state.subscriptionsLoaded = true;
        }catch(error){
            output.innerHTML =
                '<div class="error-text">' +
                BROray.api.escapeHtml(
                    "Ошибка загрузки: " + error.message
                ) +
                "</div>";
        }
    },

    renderSubscription(subscription){
        const id = BROray.api.text(subscription.id, "");
        const name = BROray.api.text(subscription.name, id);
        const url = BROray.api.text(subscription.url, "");
        const updatedAt =
            BROray.api.text(subscription.updated_at, "");
        const nodeCount =
            Number(subscription.node_count) || 0;

        const encodedId = encodeURIComponent(id);
        const encodedName = encodeURIComponent(name);

        return (
            '<div class="subscription-card">' +

            '<div class="subscription-name">' +
            BROray.api.escapeHtml(name) +
            "</div>" +

            '<div class="subscription-id">' +
            "ID: " +
            BROray.api.escapeHtml(id) +
            "</div>" +

            (
                url
                    ? '<div class="subscription-url">' +
                      BROray.api.escapeHtml(url) +
                      "</div>"
                    : ""
            ) +

            '<div class="subscription-details">' +

            '<span class="subscription-detail">' +
            "Узлов: " +
            BROray.api.escapeHtml(String(nodeCount)) +
            "</span>" +

            '<span class="subscription-detail">' +
            (
                updatedAt
                    ? "Обновлено: " +
                      BROray.api.escapeHtml(updatedAt)
                    : "Ещё не обновлялась"
            ) +
            "</span>" +

            "</div>" +

            '<div class="subscription-actions">' +

            '<button type="button" ' +
            'class="subscription-action-button" ' +
            'data-subscription-id="' + encodedId + '" ' +
            'onclick="updateSubscriptionFromButton(this)">' +
            "Обновить" +
            "</button>" +

            '<button type="button" ' +
            'class="subscription-action-button secondary-action" ' +
            'data-subscription-id="' + encodedId + '" ' +
            'data-subscription-name="' + encodedName + '" ' +
            'onclick="showSubscriptionNodesFromButton(this)">' +
            "Узлы" +
            "</button>" +

            '<button type="button" ' +
            'class="subscription-action-button danger-action" ' +
            'data-subscription-id="' + encodedId + '" ' +
            'data-subscription-name="' + encodedName + '" ' +
            'onclick="deleteSubscriptionFromButton(this)">' +
            "Удалить" +
            "</button>" +

            "</div>" +
            "</div>"
        );
    },

    async add(event){
        event.preventDefault();

        if(this.busy){
            return;
        }

        const nameElement =
            document.getElementById("subscription-name");

        const urlElement =
            document.getElementById("subscription-url");

        const button =
            document.getElementById("subscription-add-button");

        const name = nameElement.value.trim();
        const url = urlElement.value.trim();

        if(!name || !url){
            this.setMessage(
                "Заполните название и ссылку.",
                "error-text"
            );
            return;
        }

        this.busy = true;
        button.disabled = true;
        button.textContent = "Добавление…";

        try{
            const result = await this.postJson(
                "cgi-bin/subscription_add.cgi",
                {name, url}
            );

            if(!result.ok){
                throw new Error(
                    result.output ||
                    result.error ||
                    "Не удалось добавить подписку"
                );
            }

            nameElement.value = "";
            urlElement.value = "";

            this.setMessage(
                "Подписка добавлена.",
                "success-text"
            );

            await this.load();
        }catch(error){
            this.setMessage(
                "Ошибка: " + error.message,
                "error-text"
            );
        }finally{
            this.busy = false;
            button.disabled = false;
            button.textContent = "Добавить подписку";
        }
    },

    async update(id, button){
        if(this.busy){
            return;
        }

        this.busy = true;

        const oldText = button.textContent;
        button.disabled = true;
        button.textContent = "Обновление…";

        try{
            const result = await this.postJson(
                "cgi-bin/subscription_update.cgi",
                {name: id}
            );

            if(!result.ok){
                throw new Error(
                    result.output ||
                    result.error ||
                    "Не удалось обновить подписку"
                );
            }

            this.setMessage(
                "Подписка обновлена.",
                "success-text"
            );

            await this.load();
        }catch(error){
            this.setMessage(
                "Ошибка: " + error.message,
                "error-text"
            );
        }finally{
            this.busy = false;
            button.disabled = false;
            button.textContent = oldText;
        }
    },

    async updateAll(button){
        if(this.busy){
            return;
        }

        this.busy = true;

        const oldText = button.textContent;
        button.disabled = true;
        button.textContent = "Обновление…";

        this.setMessage(
            "Обновление всех подписок…",
            "loading"
        );

        try{
            const result = await this.postJson(
                "cgi-bin/subscription_update_all.cgi",
                {}
            );

            if(result.total === 0){
                this.setMessage(
                    "Нет подписок для обновления.",
                    "loading"
                );
            }else if(result.failed > 0){
                this.setMessage(
                    "Обновлено: " + result.success +
                    ". Ошибок: " + result.failed + ".",
                    "error-text"
                );
            }else{
                this.setMessage(
                    "Все подписки обновлены: " +
                    result.success + ".",
                    "success-text"
                );
            }

            await this.load();
        }catch(error){
            this.setMessage(
                "Ошибка: " + error.message,
                "error-text"
            );
        }finally{
            this.busy = false;
            button.disabled = false;
            button.textContent = oldText;
        }
    },

    async remove(id, name, button){
        if(this.busy){
            return;
        }

        if(!confirm(
            'Удалить подписку «' + name + '»?\n\n' +
            "Папка подписки будет перенесена в резервную корзину."
        )){
            return;
        }

        this.busy = true;

        const oldText = button.textContent;
        button.disabled = true;
        button.textContent = "Удаление…";

        try{
            const result = await this.postJson(
                "cgi-bin/subscription_delete.cgi",
                {name: id}
            );

            if(!result.ok){
                throw new Error(
                    result.error ||
                    "Не удалось удалить подписку"
                );
            }

            this.setMessage(
                "Подписка удалена.",
                "success-text"
            );

            this.closeNodes();
            await this.load();
        }catch(error){
            this.setMessage(
                "Ошибка: " + error.message,
                "error-text"
            );
        }finally{
            this.busy = false;
            button.disabled = false;
            button.textContent = oldText;
        }
    },

    getNodeTitle(uri, index){
        try{
            const hashPosition = uri.indexOf("#");

            if(hashPosition >= 0){
                const title =
                    decodeURIComponent(
                        uri.substring(hashPosition + 1)
                    );

                if(title){
                    return title;
                }
            }

            const protocol =
                uri.split("://")[0].toUpperCase();

            return protocol + " #" + (index + 1);
        }catch(error){
            return "Узел #" + (index + 1);
        }
    },

    getNodeAddress(uri){
        try{
            const withoutProtocol =
                uri.substring(uri.indexOf("://") + 3);

            const beforeQuery =
                withoutProtocol.split("?")[0].split("#")[0];

            const atPosition =
                beforeQuery.lastIndexOf("@");

            return atPosition >= 0
                ? beforeQuery.substring(atPosition + 1)
                : beforeQuery;
        }catch(error){
            return "";
        }
    },

    renderNode(uri, index){
        const encodedUri = encodeURIComponent(uri);
        const title = this.getNodeTitle(uri, index);
        const address = this.getNodeAddress(uri);
        const protocol =
            uri.split("://")[0].toUpperCase();

        return (
            '<div class="subscription-node">' +

            '<div class="subscription-node-title">' +
            BROray.api.escapeHtml(title) +
            "</div>" +

            '<div class="subscription-node-info">' +
            BROray.api.escapeHtml(protocol) +
            (
                address
                    ? " • " +
                      BROray.api.escapeHtml(address)
                    : ""
            ) +
            "</div>" +

            '<button type="button" ' +
            'class="node-import-button" ' +
            'data-node-uri="' + encodedUri + '" ' +
            'data-node-index="' + index + '" ' +
            'onclick="importSubscriptionNodeFromButton(this)">' +
            "Импортировать сервер" +
            "</button>" +

            "</div>"
        );
    },

    async showNodes(id, name, button){
        this.currentSubscriptionId = id;
        if(this.busy){
            return;
        }

        const card =
            document.getElementById("subscription-nodes-card");

        const title =
            document.getElementById("subscription-nodes-title");

        const output =
            document.getElementById("subscription-nodes-output");

        this.busy = true;

        const oldText = button.textContent;
        button.disabled = true;
        button.textContent = "Загрузка…";

        card.classList.remove("hidden");
        title.textContent = "Узлы: " + name;

        output.className = "subscription-output loading";
        output.textContent = "Получение узлов…";

        try{
            const result = await this.postJson(
                "cgi-bin/subscription_nodes.cgi",
                {name: id}
            );

            if(!result.ok){
                throw new Error(
                    result.output ||
                    result.error ||
                    "Не удалось получить узлы"
                );
            }

            const nodes =
                Array.isArray(result.nodes)
                    ? result.nodes
                    : [];

            output.className = "subscription-nodes-list";

            output.innerHTML =
                nodes.length
                    ? nodes.map(
                        (uri, index) =>
                            this.renderNode(uri, index)
                      ).join("")
                    : '<div class="empty-state">' +
                      "Узлы не найдены." +
                      "</div>";

            card.scrollIntoView({
                behavior: "smooth",
                block: "start"
            });
        }catch(error){
            output.className =
                "subscription-output error-text";

            output.textContent =
                "Ошибка: " + error.message;
        }finally{
            this.busy = false;
            button.disabled = false;
            button.textContent = oldText;
        }
    },

    async importNode(uri, nodeIndex, button){
        if(this.busy){
            return;
        }

        this.busy = true;

        const oldText = button.textContent;
        button.disabled = true;
        button.textContent = "Импорт…";

        try{
            const result = await this.postJson(
                "cgi-bin/subscription_import_node.cgi",
                {uri, subscription_id:this.currentSubscriptionId, node_index:nodeIndex}
            );

            if(!result.ok){
                throw new Error(
                    result.output ||
                    result.error ||
                    "Не удалось импортировать узел"
                );
            }

            this.setMessage(
                "Сервер импортирован..",
                "success-text"
            );

            button.textContent = "Импортирован";

            if(typeof window.refreshHome === "function"){
                window.refreshHome();
            }

            setTimeout(function(){
                button.disabled = false;
                button.textContent = oldText;
            }, 1500);
        }catch(error){
            this.setMessage(
                "Ошибка импорта: " + error.message,
                "error-text"
            );

            button.disabled = false;
            button.textContent = oldText;
        }finally{
            this.busy = false;
        }
    },

    closeNodes(){
        const card =
            document.getElementById("subscription-nodes-card");

        const output =
            document.getElementById("subscription-nodes-output");

        if(card){
            card.classList.add("hidden");
        }

        if(output){
            output.innerHTML = "";
        }
    }
};

window.loadSubscriptions = function(){
    return BROray.subscriptions.load();
};

window.addSubscription = function(event){
    return BROray.subscriptions.add(event);
};

window.updateAllSubscriptions = function(button){
    return BROray.subscriptions.updateAll(button);
};

window.updateSubscriptionFromButton = function(button){
    const id =
        decodeURIComponent(
            button.dataset.subscriptionId || ""
        );

    return BROray.subscriptions.update(id, button);
};

window.deleteSubscriptionFromButton = function(button){
    const id =
        decodeURIComponent(
            button.dataset.subscriptionId || ""
        );

    const name =
        decodeURIComponent(
            button.dataset.subscriptionName || ""
        );

    return BROray.subscriptions.remove(
        id,
        name,
        button
    );
};

window.showSubscriptionNodesFromButton = function(button){
    const id =
        decodeURIComponent(
            button.dataset.subscriptionId || ""
        );

    const name =
        decodeURIComponent(
            button.dataset.subscriptionName || ""
        );

    return BROray.subscriptions.showNodes(
        id,
        name,
        button
    );
};

window.importSubscriptionNodeFromButton = function(button){
    const uri =
        decodeURIComponent(
            button.dataset.nodeUri || ""
        );

    const nodeIndex = button.dataset.nodeIndex || "0";
    return BROray.subscriptions.importNode(
        uri,
        nodeIndex,
        button
    );
};

window.closeSubscriptionNodes = function(){
    BROray.subscriptions.closeNodes();
};
