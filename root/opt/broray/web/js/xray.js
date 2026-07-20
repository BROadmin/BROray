"use strict";

window.BROray = window.BROray || {};

BROray.xray = {
    busy: false,

    formatBytes(value){
        const bytes = Number(value);

        if(!Number.isFinite(bytes)){
            return "—";
        }

        if(bytes < 1024){
            return bytes + " Б";
        }

        if(bytes < 1024 * 1024){
            return (
                bytes / 1024
            ).toFixed(1) + " КБ";
        }

        return (
            bytes / 1024 / 1024
        ).toFixed(1) + " МБ";
    },

    row(label, value, monospace){
        const safeLabel =
            BROray.api.escapeHtml(label);

        const safeValue =
            BROray.api.escapeHtml(
                BROray.api.text(value)
            );

        return (
            '<div class="xray-info-row">' +
                '<div class="xray-info-label">' +
                    safeLabel +
                '</div>' +
                '<div class="xray-info-value' +
                    (monospace ? " is-monospace" : "") +
                '">' +
                    safeValue +
                '</div>' +
            '</div>'
        );
    },

    render(result){
        const container =
            document.getElementById(
                "xray-status-block"
            );

        if(!container){
            return;
        }

        const status = result.status || {};
        const version = result.version || {};
        const config = result.config || {};
        const socks = status.socks || {};
        const storage = result.storage || {};
        const temporaryStorage =
            result.temporaryStorage || {};
        const broray = result.broray || {};

        const running = status.running === true;

        let html =
            '<div class="xray-runtime-status ' +
                (
                    running
                        ? "is-running"
                        : "is-stopped"
                ) +
            '">' +
                '<span class="xray-runtime-dot"></span>' +
                '<span>' +
                    (
                        running
                            ? "Xray запущен"
                            : "Xray остановлен"
                    ) +
                '</span>' +
            '</div>';

        if(storage.level === "warning"){
            html +=
                '<div class="home-xray-warning">' +
                    'На накопителе осталось мало места.' +
                '</div>';
        }

        if(storage.level === "critical"){
            html +=
                '<div class="home-xray-error">' +
                    'Критически мало свободного места.' +
                '</div>';
        }

        html += '<div class="xray-info-list">';

        html += this.row(
            "PID",
            running ? status.pid : "—"
        );

        html += this.row(
            "Версия",
            version.version || status.version
        );

        html += this.row(
            "Архитектура",
            status.architecture
        );

        html += this.row(
            "Исполняемый файл",
            status.binary || version.binary,
            true
        );

        html += this.row(
            "Размер Xray",
            this.formatBytes(
                status.binarySizeBytes
            )
        );

        html += this.row(
            "Конфигурация",
            config.file || status.config,
            true
        );

        html += this.row(
            "Размер конфигурации",
            this.formatBytes(
                config.sizeBytes
            )
        );

        html += this.row(
            "Изменена",
            config.modified
        );

        html += this.row(
            "Активный сервер",
            config.activeServer,
            true
        );

        html += this.row(
            "SHA256",
            config.sha256,
            true
        );

        html += this.row(
            "SOCKS",
            socks.listen && socks.port
                ? socks.listen + ":" + socks.port
                : "—",
            true
        );

        html += this.row(
            "SOCKS активен",
            socks.active === true
                ? "Да"
                : "Нет"
        );

        html += this.row(
            "Свободно на /opt",
            this.formatBytes(
                storage.freeBytes
            )
        );

        html += this.row(
            "Занято на /opt",
            this.formatBytes(
                storage.usedBytes
            )
        );

        html += this.row(
            "Всего на /opt",
            this.formatBytes(
                storage.totalBytes
            )
        );

        html += this.row(
            "Размер BROray",
            this.formatBytes(
                broray.sizeBytes
            )
        );

        html += this.row(
            "Свободно в /tmp",
            this.formatBytes(
                temporaryStorage.freeBytes
            )
        );

        html += "</div>";

        container.innerHTML = html;

        const startButton =
            document.getElementById(
                "xray-start-button"
            );

        const stopButton =
            document.getElementById(
                "xray-stop-button"
            );

        if(startButton){
            startButton.disabled = running;
        }

        if(stopButton){
            stopButton.disabled = !running;
        }
    },

    async load(force){
        if(this.busy && !force){
            return;
        }

        const container =
            document.getElementById(
                "xray-status-block"
            );

        if(container){
            container.innerHTML =
                '<div class="loading">' +
                    'Загрузка состояния Xray…' +
                '</div>';
        }

        try{
            const result =
                await BROray.api.requestJson(
                    "cgi-bin/xray.cgi?" +
                    Date.now()
                );

            if(!result.ok){
                throw new Error(
                    result.error ||
                    "Не удалось получить состояние Xray"
                );
            }

            this.render(result);
            BROray.state.xrayLoaded = true;
        }catch(error){
            if(container){
                container.innerHTML =
                    '<div class="error-text">' +
                        BROray.api.escapeHtml(
                            error.message
                        ) +
                    '</div>';
            }
        }
    },

    parseOutput(value){
        if(
            value === undefined ||
            value === null ||
            value === ""
        ){
            return "Команда завершена без вывода.";
        }

        try{
            const parsed = JSON.parse(value);

            if(
                parsed &&
                typeof parsed === "object"
            ){
                if(parsed.output){
                    return parsed.output;
                }

                return JSON.stringify(
                    parsed,
                    null,
                    2
                );
            }
        }catch(error){
            return String(value);
        }

        return String(value);
    },

    setButtonsDisabled(disabled){
        document
            .querySelectorAll(
                "#page-xray button"
            )
            .forEach(function(button){
                button.disabled = disabled;
            });
    },

    async action(action){
        if(this.busy){
            return;
        }

        this.busy = true;
        this.setButtonsDisabled(true);

        const message =
            document.getElementById(
                "xray-action-message"
            );

        const output =
            document.getElementById(
                "xray-operation-output"
            );

        if(message){
            message.className =
                "action-message";

            message.textContent =
                "Выполняется команда: " +
                action +
                "…";
        }

        if(output){
            output.textContent = "Выполнение…";
        }

        try{
            const result =
                await BROray.api.requestJson(
                    "cgi-bin/xray-" +
                    action +
                    ".cgi?" +
                    Date.now(),
                    {
                        method: "POST",
                        cache: "no-store"
                    }
                );

            if(output){
                output.textContent =
                    this.parseOutput(
                        result.output
                    );
            }

            if(message){
                message.className =
                    "action-message " +
                    (
                        result.ok
                            ? "success-text"
                            : "error-text"
                    );

                message.textContent =
                    result.ok
                        ? "Команда выполнена успешно."
                        : "Команда завершилась ошибкой.";
            }

            if(
                BROray.dashboard &&
                typeof BROray.dashboard.refresh ===
                    "function"
            ){
                BROray.dashboard.refresh();
            }

            if(
                BROray.xrayHome &&
                typeof BROray.xrayHome.refresh ===
                    "function"
            ){
                BROray.xrayHome.refresh();
            }
        }catch(error){
            if(message){
                message.className =
                    "action-message error-text";

                message.textContent =
                    error.message;
            }

            if(output){
                output.textContent =
                    error.message;
            }
        }finally{
            this.busy = false;
            this.setButtonsDisabled(false);
            await this.load(true);
        }
    }
};

BROray.xray.checkUpdate = async function(button){
    if(this.busy){
        return;
    }

    const message =
        document.getElementById(
            "xray-update-check-message"
        );

    const resultBlock =
        document.getElementById(
            "xray-update-check-result"
        );

    this.busy = true;

    if(button){
        button.disabled = true;
        button.dataset.originalText =
            button.textContent;
        button.textContent =
            "Проверка…";
    }

    if(message){
        message.className =
            "action-message";
        message.textContent =
            "Получение информации о релизах Xray…";
    }

    if(resultBlock){
        resultBlock.innerHTML = "";
    }

    try{
        const result =
            await BROray.api.requestJson(
                "cgi-bin/xray-update-check.cgi?" +
                Date.now()
            );

        if(!result.success){
            throw new Error(
                result.error ||
                "Не удалось проверить обновления Xray."
            );
        }

        this.setUpdateAvailability(
            result.updateAvailable === true
        );
        const currentVersion =
            result.currentVersion || "—";

        const latestVersion =
            result.latestVersion || "—";

        const publishedAt =
            result.publishedAt || "—";

        const channel =
            result.channel === "pre-release"
                ? "Предварительный релиз"
                : (
                    result.channel === "stable"
                        ? "Стабильный релиз"
                        : result.channel || "—"
                );

        const asset =
            result.asset || {};

        const storage =
            result.storage || {};

        const temporaryStorage =
            result.temporaryStorage || {};

        let statusText = "";

        if(result.updateAvailable === true){
            statusText =
                "Доступна новая версия Xray.";
        }else if(result.installedNewer === true){
            statusText =
                "Установленная версия новее опубликованной.";
        }else{
            statusText =
                "Установлена актуальная версия Xray.";
        }

        let html =
            '<div class="xray-runtime-status ' +
                (
                    result.updateAvailable === true
                        ? "is-stopped"
                        : "is-running"
                ) +
            '">' +
                '<span class="xray-runtime-dot"></span>' +
                '<span>' +
                    BROray.api.escapeHtml(statusText) +
                '</span>' +
            '</div>' +
            '<div class="xray-info-list">' +
                this.row(
                    "Текущая версия",
                    currentVersion
                ) +
                this.row(
                    "Последняя версия",
                    latestVersion
                ) +
                this.row(
                    "Канал",
                    channel
                ) +
                this.row(
                    "Дата публикации",
                    publishedAt
                ) +
                this.row(
                    "Архив",
                    asset.name || "—",
                    true
                ) +
                this.row(
                    "Размер архива",
                    this.formatBytes(
                        asset.size
                    )
                ) +
                this.row(
                    "Свободно на /opt",
                    this.formatBytes(
                        storage.freeBytes
                    )
                ) +
                this.row(
                    "Свободно в /tmp",
                    this.formatBytes(
                        temporaryStorage.freeBytes
                    )
                ) +
            '</div>';

        if(result.cached === true){
            html +=
                '<div class="home-xray-warning">' +
                    'Использованы сохранённые данные о релизах.' +
                '</div>';
        }

        if(result.updateAvailable === true){
            html +=
                '<div class="home-xray-warning">' +
                    'Установка обновления будет доступна после ' +
                    'завершения модуля безопасного обновления.' +
                '</div>';
        }

        if(resultBlock){
            resultBlock.innerHTML = html;
        }

        if(message){
            message.className =
                "action-message success";
            message.textContent =
                result.message || statusText;
        }
    }catch(error){
        if(message){
            message.className =
                "action-message error";
            message.textContent =
                error.message;
        }

        if(resultBlock){
            resultBlock.innerHTML =
                '<div class="error-text">' +
                    BROray.api.escapeHtml(
                        error.message
                    ) +
                '</div>';
        }
    }finally{
        this.busy = false;

        if(button){
            button.disabled = false;
            button.textContent =
                button.dataset.originalText ||
                "Проверить наличие новой версии";
        }
    }
};

BROray.xray.setUpdateAvailability = function(
    available
){
    const button =
        document.getElementById(
            "xray-update-install-button"
        );

    if(!button){
        return;
    }

    button.dataset.updateAvailable =
        available === true
            ? "true"
            : "false";

    if(!this.busy){
        button.disabled =
            available !== true;
    }
};

BROray.xray.setUpdateButtonsBusy = function(
    busy,
    activeButton
){
    const updateButton =
        document.getElementById(
            "xray-update-install-button"
        );

    const reinstallButton =
        document.getElementById(
            "xray-reinstall-button"
        );

    if(updateButton){
        updateButton.disabled =
            busy ||
            updateButton.dataset.updateAvailable !==
                "true";
    }

    if(reinstallButton){
        reinstallButton.disabled = busy;
    }

    if(!activeButton){
        return;
    }

    if(busy){
        activeButton.dataset.originalText =
            activeButton.textContent;

        activeButton.textContent =
            "Выполнение…";
    }else if(
        activeButton.dataset.originalText
    ){
        activeButton.textContent =
            activeButton.dataset.originalText;

        delete activeButton.dataset.originalText;
    }
};

BROray.xray.installUpdate = async function(
    mode,
    button
){
    if(this.busy){
        return;
    }

    if(
        mode !== "update" &&
        mode !== "reinstall"
    ){
        return;
    }

    const reinstall =
        mode === "reinstall";

    const confirmed =
        window.confirm(
            reinstall
                ? (
                    "Переустановить текущую версию " +
                    "Xray?\n\n" +
                    "Xray будет кратковременно " +
                    "остановлен. Настройки BROray " +
                    "не изменятся."
                )
                : (
                    "Установить новую версию " +
                    "Xray?\n\n" +
                    "Архив и конфигурация будут " +
                    "проверены. При ошибке будет " +
                    "выполнен автоматический откат."
                )
        );

    if(!confirmed){
        return;
    }

    const message =
        document.getElementById(
            "xray-update-check-message"
        );

    const resultBlock =
        document.getElementById(
            "xray-update-check-result"
        );

    const output =
        document.getElementById(
            "xray-operation-output"
        );

    this.busy = true;

    this.setButtonsDisabled(true);
    this.setUpdateButtonsBusy(
        true,
        button
    );

    if(message){
        message.className =
            "action-message";

        message.textContent =
            reinstall
                ? "Переустановка Xray…"
                : "Установка новой версии Xray…";
    }

    if(output){
        output.textContent =
            "Скачивание официального архива, " +
            "проверка SHA256 и конфигурации. " +
            "Не закрывайте страницу.";
    }

    try{
        const endpoint =
            reinstall
                ? "cgi-bin/xray-reinstall.cgi?"
                : "cgi-bin/xray-update.cgi?";

        const result =
            await BROray.api.requestJson(
                endpoint + Date.now()
            );

        if(result.success !== true){
            let errorText =
                result.error ||
                "Операция завершилась ошибкой.";

            if(
                result.details
            ){
                errorText +=
                    "\n\n" +
                    result.details;
            }

            throw new Error(errorText);
        }

        const statusText =
            result.message ||
            (
                reinstall
                    ? "Xray успешно переустановлен."
                    : "Xray успешно обновлён."
            );

        if(message){
            message.className =
                "action-message success";

            message.textContent =
                statusText;
        }

        if(output){
            output.textContent =
                JSON.stringify(
                    result,
                    null,
                    2
                );
        }

        if(resultBlock){
            resultBlock.innerHTML =
                '<div class="' +
                    'xray-runtime-status ' +
                    'is-running">' +
                    '<span class="' +
                        'xray-runtime-dot">' +
                    '</span>' +
                    '<span>' +
                        BROray.api.escapeHtml(
                            statusText
                        ) +
                    '</span>' +
                '</div>';
        }
    }catch(error){
        if(message){
            message.className =
                "action-message error";

            message.textContent =
                error.message;
        }

        if(output){
            output.textContent =
                error.message;
        }

        if(resultBlock){
            resultBlock.innerHTML =
                '<div class="error-text">' +
                    BROray.api.escapeHtml(
                        error.message
                    ) +
                '</div>';
        }
    }finally{
        this.busy = false;

        this.setButtonsDisabled(false);
        this.setUpdateButtonsBusy(
            false,
            button
        );

        await this.load(true);
    }
};

/* BROray Xray update availability guard v1 */
BROray.xray.setButtonsDisabledOriginal =
    BROray.xray.setButtonsDisabled;

BROray.xray.setButtonsDisabled = function(disabled){
    this.setButtonsDisabledOriginal(disabled);

    const updateButton =
        document.getElementById(
            "xray-update-install-button"
        );

    if(!updateButton){
        return;
    }

    if(disabled){
        updateButton.disabled = true;
        return;
    }

    updateButton.disabled =
        updateButton.dataset.updateAvailable !==
            "true";
};
