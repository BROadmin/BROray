/* BROray routes immediate operation window r1 */
(function () {
    "use strict";

    var REQUEST_TIMEOUT_MS = 180000;
    var LONG_OPERATION_TIMEOUT_MS = 8 * 60 * 60 * 1000;
    var PROGRESS_POLL_INTERVAL_MS = 750;
    var PASSIVE_REFRESH_INTERVAL_MS = 30000;
    var OPERATION_EVENT = "broray:routes-operation-state";
    var CONTROL_EVENT = "broray:routes-operation-control";
    var states = Object.create(null);
    var operationRunning = false;
    var globalOperation = null;
    var progressWatchers = Object.create(null);
    var longOperationRequests = Object.create(null);
    var progressPollErrors = Object.create(null);
    var expandedBundles = Object.create(null);
    var peerOperation = null;
    var passiveRefreshTimer = null;
    var lastPublishedOperationSignature = "";
    var lastPeerOperationSignature = "";
    var latestSummary = null;
    var summaryRequest = null;
    var summaryController = null;

    var ROUTE_LOGOS = {
        telegram: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#229ED9"/><path fill="#fff" d="M15.2 30.7 47 18.4c1.5-.6 2.8.4 2.3 2.7l-5.4 25.3c-.4 1.8-1.5 2.3-3 1.4l-8.2-6.1-4 3.9c-.4.4-.8.8-1.7.8l.6-8.4 15.3-13.8c.7-.6-.1-1-.9-.4L23.1 35.7 15 33.2c-1.8-.6-1.8-1.8.2-2.5Z"/></svg>',
        whatsapp: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#25D366"/><path fill="#fff" d="M18.4 47.3 20.5 40A21 21 0 1 1 28 47.5l-9.6-.2Zm10-3.6a16.8 16.8 0 1 0-4.2-4.1L23 43.9l5.4-1.4Z"/><path fill="#fff" d="M27 23.3c-.4-1-1-1-1.5-1h-1.3c-.5 0-1.2.2-1.8 1-.6.7-2.3 2.3-2.3 5.5s2.4 6.4 2.7 6.8c.3.4 4.6 7.1 11.4 9.7 5.7 2.2 6.8 1.8 8 1.7 1.2-.1 4-1.7 4.6-3.3.6-1.6.6-3 .4-3.3-.2-.3-.7-.5-1.5-.9l-4.8-2.3c-.7-.3-1.2-.5-1.7.3l-2.2 2.7c-.4.5-.9.6-1.6.2-4.5-2.2-7.4-4-10.4-9-.4-.7 0-1 .3-1.4l1.6-1.9c.3-.4.4-.8.6-1.3.1-.4.1-.9-.1-1.3L27 23.3Z"/></svg>',
        youtube: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect x="3" y="13" width="58" height="38" rx="12" fill="#FF0000"/><path fill="#fff" d="m27 23 16 9-16 9V23Z"/></svg>',
        chatgpt: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#10A37F"/><g fill="none" stroke="#fff" stroke-width="4.2" stroke-linecap="round" stroke-linejoin="round"><path d="M32 15a11 11 0 0 1 10.5 7.8A11 11 0 0 1 47 42a11 11 0 0 1-16.5 7.5A11 11 0 0 1 14.8 35 11 11 0 0 1 21.5 18.7 11 11 0 0 1 32 15Z"/><path d="m23 24 9-5 9 5v10l-9 5-9-5V24Z"/><path d="m23 34 9 5v10M41 34l-9 5M32 19v10l-9 5M41 24l-9 5"/></g></svg>',
        facebook: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#1877F2"/><path fill="#fff" d="M36.7 52V34.6h5.8l.9-6.8h-6.7v-4.3c0-2 .5-3.3 3.4-3.3h3.6v-6.1c-.6-.1-2.8-.3-5.3-.3-5.2 0-8.8 3.2-8.8 9v5h-5.9v6.8h5.9V52h7.1Z"/></svg>',
        instagram: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><defs><radialGradient id="route-instagram-gradient" cx="30%" cy="100%" r="115%"><stop offset="0" stop-color="#FEDA75"/><stop offset=".35" stop-color="#FA7E1E"/><stop offset=".62" stop-color="#D62976"/><stop offset=".82" stop-color="#962FBF"/><stop offset="1" stop-color="#4F5BD5"/></radialGradient></defs><rect x="3" y="3" width="58" height="58" rx="17" fill="url(#route-instagram-gradient)"/><rect x="16" y="16" width="32" height="32" rx="10" fill="none" stroke="#fff" stroke-width="4"/><circle cx="32" cy="32" r="8" fill="none" stroke="#fff" stroke-width="4"/><circle cx="43" cy="21" r="2.8" fill="#fff"/></svg>',
        meta: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><path fill="none" stroke="#0866FF" stroke-width="7" stroke-linecap="round" stroke-linejoin="round" d="M8 40c5-18 10-25 17-25 8 0 14 19 20 26 4 5 8 4 11-1 4-7-1-22-9-25-11-4-19 20-25 27-5 6-10 5-14-2Z"/></svg>',
        tiktok: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect x="3" y="3" width="58" height="58" rx="14" fill="#111"/><path fill="none" stroke="#25F4EE" stroke-width="7" stroke-linecap="round" stroke-linejoin="round" d="M34 16v27a10 10 0 1 1-8-9.8M34 16c2 8 7 12 14 12" transform="translate(-2 1)"/><path fill="none" stroke="#FE2C55" stroke-width="7" stroke-linecap="round" stroke-linejoin="round" d="M34 16v27a10 10 0 1 1-8-9.8M34 16c2 8 7 12 14 12" transform="translate(2 -1)"/><path fill="none" stroke="#fff" stroke-width="5" stroke-linecap="round" stroke-linejoin="round" d="M34 16v27a10 10 0 1 1-8-9.8M34 16c2 8 7 12 14 12"/></svg>',
        speedtest: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#1995E8"/><path fill="none" stroke="#fff" stroke-width="5" stroke-linecap="round" d="M14 41a20 20 0 1 1 36 0"/><path fill="#fff" d="M31 34.5 44.5 22 35 38a5 5 0 1 1-4-3.5Z"/><rect x="20" y="45" width="24" height="4" rx="2" fill="#fff"/></svg>',
        wikipedia: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#fff" stroke="#A2A9B1" stroke-width="2"/><path fill="#202122" d="M10 18h13v4h-3l7.1 20L32 29l-2.5-7H27v-4h12v4h-2.5l2.6 7L44 22h-3v-4h13v4h-3L40.5 47h-5L30 35.5 25.5 47h-5L10 22H7v-4h3Z"/></svg>'
    };

    var BUNDLES = [
        {
            id: "telegram",
            logo: ROUTE_LOGOS.telegram,
            name: "Telegram",
            description: "Маршруты для Telegram и его сервисов.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Telegram"
        },
        {
            id: "whatsapp",
            logo: ROUTE_LOGOS.whatsapp,
            name: "WhatsApp",
            description: "Маршруты для WhatsApp.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Whatsapp"
        },
        {
            id: "youtube",
            logo: ROUTE_LOGOS.youtube,
            name: "YouTube",
            description: "Все маршруты из .bat-файлов раздела YouTube.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Youtube"
        },
        {
            id: "chatgpt",
            logo: ROUTE_LOGOS.chatgpt,
            name: "ChatGPT",
            description: "Маршруты для ChatGPT и OpenAI.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/ChatGPT(OpenAI)"
        },
        {
            id: "facebook",
            logo: ROUTE_LOGOS.facebook,
            name: "Facebook",
            description: "Маршруты для Facebook.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Facebook"
        },
        {
            id: "instagram",
            logo: ROUTE_LOGOS.instagram,
            name: "Instagram",
            description: "Маршруты для Instagram.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Instagram"
        },
        {
            id: "meta",
            logo: ROUTE_LOGOS.meta,
            name: "Meta",
            description: "Маршруты для сервисов Meta.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/Meta"
        },
        {
            id: "tiktok",
            logo: ROUTE_LOGOS.tiktok,
            name: "TikTok",
            description: "Маршруты для TikTok.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/TikTok"
        },
        {
            id: "speedtest",
            logo: ROUTE_LOGOS.speedtest,
            name: "Speedtest",
            description: "Маршруты для сервисов проверки скорости.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/speedtest"
        },
        {
            id: "wikipedia",
            logo: ROUTE_LOGOS.wikipedia,
            name: "Wikipedia",
            description: "Маршруты для Wikipedia и инфраструктуры Wikimedia.",
            source: "https://github.com/RockBlack-VPN/ip-address/tree/main/Global/wikipedia"
        }
    ];

    window.BROrayRouteBundles = BUNDLES.slice();


    var busyBundles = Object.create(null);

    function byId(id) {
        return document.getElementById(id);
    }

    function create(tag, className, text) {
        var node = document.createElement(tag);
        if (className) node.className = className;
        if (text !== undefined && text !== null) node.textContent = String(text);
        return node;
    }

    function unwrap(payload) {
        var error;
        if (payload && payload.success === false) {
            error = new Error(payload.error && payload.error.message
                ? payload.error.message
                : "Операция с маршрутами завершилась ошибкой.");
            error.code = payload.error ? payload.error.code : null;
            error.details = payload.error ? payload.error.details : null;
            error.status = payload.status || null;
            throw error;
        }
        return payload && Object.prototype.hasOwnProperty.call(payload, "data")
            ? payload.data
            : payload;
    }

    function request(url, options) {
        return window.BROrayUI.apiRequest(url, options || {}).then(unwrap);
    }

    function withTimeout(promise, milliseconds) {
        return new Promise(function (resolve, reject) {
            var finished = false;
            var timer = window.setTimeout(function () {
                if (!finished) {
                    finished = true;
                    reject(new Error("Операция не завершилась за отведённое время."));
                }
            }, milliseconds);
            promise.then(function (value) {
                if (!finished) {
                    finished = true;
                    window.clearTimeout(timer);
                    resolve(value);
                }
            }, function (error) {
                if (!finished) {
                    finished = true;
                    window.clearTimeout(timer);
                    reject(error);
                }
            });
        });
    }

    function sameRouteVersion(left, right) {
        if (!left || !right) return false;
        if (left.contentSha256 && right.contentSha256) {
            return left.contentSha256 === right.contentSha256;
        }
        return String(left.sourceCommit || "") === String(right.sourceCommit || "") &&
            String(left.sourceDate || "") === String(right.sourceDate || "");
    }

    function sameSourceVersion(left, right) {
        if (!left || !right) return false;
        if (left.sourceSetSha256 && right.sourceSetSha256) {
            return left.sourceSetSha256 === right.sourceSetSha256;
        }
        return sameRouteVersion(left, right);
    }

    function formatDate(value) {
        var date;
        if (!value) return "Не выполнялась";
        date = new Date(value);
        return isNaN(date.getTime()) ? String(value) : date.toLocaleString("ru-RU");
    }

    function formatVersion(value, emptyValue) {
        var parts = [];
        if (!value) return emptyValue;
        if (typeof value === "string") return value;
        if (value.sourceDate) parts.push(formatDate(value.sourceDate));
        if (value.sourceCommit) parts.push(String(value.sourceCommit).slice(0, 7));
        return parts.length ? parts.join(" · ") : emptyValue;
    }

    function formatBytes(value) {
        var size = Number(value);
        if (!isFinite(size) || size <= 0) return "—";
        if (size < 1024) return size + " Б";
        if (size < 1024 * 1024) return Math.round(size / 1024) + " КБ";
        return (size / (1024 * 1024)).toFixed(1).replace(".", ",") + " МБ";
    }

    function sourceFiles(state) {
        if (state && state.availableVersion && Array.isArray(state.availableVersion.sourceFiles)) {
            return state.availableVersion.sourceFiles;
        }
        if (state && state.checkResult && Array.isArray(state.checkResult.sourceFiles)) {
            return state.checkResult.sourceFiles;
        }
        return [];
    }

    function routerPresence(state) {
        return state && state.routerPresence ? state.routerPresence : null;
    }

    function hasRouterDrift(state) {
        var presence = routerPresence(state);
        return Boolean(state && state.installedVersion && presence &&
            presence.available === true && presence.registered === true &&
            presence.drift === true);
    }

    function isActuallyInstalled(state) {
        var presence = routerPresence(state);
        if (!presence || presence.available !== true || presence.registered !== true) return false;
        return presence.actualInstalled === true;
    }

    function routerStateUnknown(state) {
        var presence = routerPresence(state);
        return Boolean(state && state.installedVersion &&
            (!presence || presence.available !== true || presence.registered !== true));
    }

    function downloadRequired(state) {
        if (!state || !state.availableVersion) return false;
        return !sameSourceVersion(state.availableVersion, state.downloadedVersion);
    }

    function sourceUpdateAvailable(state) {
        return Boolean(state && state.downloadedVersion && downloadRequired(state));
    }

    function verificationResult(state) {
        return state && state.verifyResult && typeof state.verifyResult === "object"
            ? state.verifyResult
            : null;
    }

    function localSetInvalid(state) {
        var result = verificationResult(state);
        return Boolean(result && result.local && result.local.valid === false &&
            state && state.downloadedVersion &&
            String(result.contentSha256 || "") === String(state.downloadedVersion.contentSha256 || ""));
    }

    function verificationCurrent(state) {
        var result = verificationResult(state);
        var presence;
        var status;
        if (!state || !state.downloadedVersion || !result || !result.local) return false;
        if (result.local.valid !== true ||
            !result.contentSha256 ||
            String(result.contentSha256) !== String(state.downloadedVersion.contentSha256 || "")) {
            return false;
        }

        presence = routerPresence(state);
        if (!presence || !result.keenetic) return true;
        status = String(result.keenetic.status || "");

        if (presence.available !== true) {
            return result.keenetic.available === false && status === "unavailable";
        }
        if (result.keenetic.available !== true) return false;
        if (presence.registered !== true) {
            return status === "not_installed" || status === "conflict";
        }
        if (presence.complete === true) {
            return status === "complete" || status === "update_pending" || status === "conflict";
        }
        return status === "restore_required" || status === "conflict";
    }

    function verificationRequired(state) {
        return Boolean(state && state.downloadedVersion &&
            !localSetInvalid(state) && !verificationCurrent(state));
    }

    function verificationConflict(state) {
        var result = verificationResult(state);
        return Boolean(verificationCurrent(state) && result && result.keenetic &&
            result.keenetic.status === "conflict");
    }

    function verificationRetryRequired(state) {
        var result = verificationResult(state);
        return Boolean(verificationCurrent(state) && result && result.keenetic &&
            (result.keenetic.available === false || result.keenetic.status === "unavailable"));
    }

    function downloadActionRequired(state) {
        return downloadRequired(state) || localSetInvalid(state);
    }

    function downloadStageRequired(state) {
        return Boolean(!state || !state.downloadedVersion || downloadActionRequired(state));
    }

    function keeneticAction(state) {
        var presence;
        if (!state || !state.downloadedVersion) return null;
        presence = routerPresence(state);
        if (state.installedVersion &&
            (!presence || presence.available !== true || presence.registered !== true)) {
            return null;
        }
        if (hasRouterDrift(state)) {
            return {action: "export", text: "Восстановить в Keenetic", icon: "restore", mode: "restore"};
        }
        if (!state.installedVersion || (presence && presence.actualInstalled === false)) {
            return {action: "export", text: "Установить в Keenetic", icon: "routes", mode: "install"};
        }
        if (!sameRouteVersion(state.downloadedVersion, state.installedVersion)) {
            return {action: "export", text: "Обновить в Keenetic", icon: "update", mode: "update"};
        }
        return null;
    }

    function nextAction(state) {
        var keenetic = keeneticAction(state);
        var progress = operationProgress(state);
        if (progress && progress.resumable && !progress.running) return "resume";
        if (downloadStageRequired(state)) return "download";
        if (verificationRequired(state) || verificationRetryRequired(state) || verificationConflict(state) || routerStateUnknown(state)) return "verify";
        if (hasRouterDrift(state) && keenetic) return "export";
        if (keenetic) return "export";
        return null;
    }

    function actionVisibility(state) {
        var keenetic = keeneticAction(state);
        var download = downloadStageRequired(state);
        var verify = !download && Boolean(state && state.downloadedVersion) &&
            (verificationRequired(state) || verificationRetryRequired(state) ||
                verificationConflict(state) || routerStateUnknown(state));
        var apply = !download && !verify && Boolean(keenetic) &&
            verificationCurrent(state) && !verificationRetryRequired(state) &&
            !verificationConflict(state);
        var check = isActuallyInstalled(state) && !download && !verify && !apply &&
            !verificationConflict(state) && !hasRouterDrift(state);

        return {
            download: download,
            verify: verify,
            keenetic: apply,
            check: check
        };
    }

    function checkLabel() {
        return "Проверить обновления";
    }

    function statusPresentation(state) {
        var action;
        var progress = operationProgress(state);
        if (progress && progress.resumable && !progress.running) {
            return {text: progress.stoppedByUser ? "Остановлено" : "Можно продолжить", className: "status-badge-warning", icon: "restore"};
        }
        if (localSetInvalid(state)) {
            return {text: "Набор повреждён", className: "status-badge-danger", icon: "status"};
        }
        if (verificationRetryRequired(state) || routerStateUnknown(state)) {
            return {text: "Состояние Keenetic не определено", className: "status-badge-warning", icon: "status"};
        }
        if (state && state.lastError) {
            return {text: "Ошибка", className: "status-badge-danger", icon: "status"};
        }
        if (verificationConflict(state)) {
            return {text: "Обнаружен конфликт", className: "status-badge-danger", icon: "status"};
        }
        if (hasRouterDrift(state)) {
            return {text: "Требуется восстановление", className: "status-badge-warning", icon: "restore"};
        }
        if (sourceUpdateAvailable(state)) {
            if (state.checkResult && state.checkResult.routesChanged === false) {
                return {text: "Источник изменился", className: "status-badge-warning", icon: "update"};
            }
            return {text: "Доступно обновление", className: "status-badge-warning", icon: "update"};
        }
        if (verificationRequired(state)) {
            return {text: "Требуется проверка", className: "status-badge-warning", icon: "status"};
        }

        action = keeneticAction(state);
        if (action && action.mode === "update") {
            return {text: "Готово обновление", className: "status-badge-warning", icon: "update"};
        }
        if (action && action.mode === "install") {
            return {text: "Готово к установке", className: "status-badge-neutral", icon: "routes"};
        }
        if (isActuallyInstalled(state)) {
            return {text: "Установлено", className: "status-badge-success", icon: "status"};
        }
        if (state && state.availableVersion) {
            return {text: "Доступно для загрузки", className: "status-badge-neutral", icon: "backup"};
        }
        return {text: "Источник не проверен", className: "status-badge-neutral", icon: "search"};
    }

    function fileChangeText(state) {
        var changes = state && state.checkResult ? state.checkResult.fileChanges : null;
        if (!changes) return "";
        return "На GitHub добавлено " + Number((changes.addedFiles || []).length) +
            ", изменено " + Number((changes.changedFiles || []).length) +
            ", удалено " + Number((changes.removedFiles || []).length) + " файлов.";
    }

    function routeChangeText(state) {
        var changes = state && state.checkResult ? state.checkResult.routeChanges : null;
        if (!changes) return "";
        return "Маршруты: добавлено " + Number(changes.added || 0) +
            ", удалено " + Number(changes.removed || 0) +
            ", без изменений " + Number(changes.unchanged || 0) + ".";
    }

    function messageFor(state) {
        var presence;
        var action;
        var lastError;
        var parts = [];

        if (localSetInvalid(state)) {
            return verificationResult(state).message ||
                "Локальный набор не прошёл проверку. Скачайте файлы заново.";
        }

        if (state && state.lastError) {
            lastError = typeof state.lastError === "string"
                ? state.lastError
                : state.lastError.message;
            return lastError || "Последняя операция завершилась ошибкой.";
        }

        if (verificationConflict(state)) {
            return verificationResult(state).message ||
                "Локальный набор исправен, но в Keenetic обнаружен конфликт.";
        }

        if (state && state.verifyResult && state.downloadedVersion && !verificationCurrent(state)) {
            return "Состояние маршрутов изменилось. Подготовьте набор повторно.";
        }

        if (hasRouterDrift(state)) {
            presence = routerPresence(state) || {};
            return "Маршруты были установлены ранее, но сейчас в Keenetic отсутствуют " +
                Number(presence.missingRouteCount || 0) + " из " +
                Number(presence.expectedRouteCount || 0) +
                ". Нажмите «Восстановить в Keenetic».";
        }

        if (downloadActionRequired(state)) {
            if (localSetInvalid(state)) {
                return "Локальный набор повреждён. Нажмите «Скачать заново».";
            }
            if (state.checkResult && state.checkResult.sourceChanged) parts.push(fileChangeText(state));
            if (state.checkResult && state.checkResult.routesChanged) parts.push(routeChangeText(state));
            if (state.checkResult && state.checkResult.routesChanged === false && state.downloadedVersion) {
                parts.push("Итоговый список маршрутов не изменился. Обновление Keenetic не требуется.");
            } else {
                parts.push(state && state.downloadedVersion
                    ? "Нажмите «Скачать обновление»."
                    : "Нажмите «Скачать маршруты».");
            }
            return parts.filter(Boolean).join(" ");
        }

        if (verificationRequired(state)) {
            return "Маршруты скачаны. Нажмите «Подготовить к установке», чтобы проверить локальные данные и их фактическое состояние в Keenetic.";
        }

        action = keeneticAction(state);
        if (action && action.mode === "update") {
            parts.push("Новая версия файлов загружена.");
            parts.push(routeChangeText(state));
            parts.push("Нажмите «Обновить в Keenetic».");
            return parts.filter(Boolean).join(" ");
        }
        if (action && action.mode === "install") {
            return (verificationResult(state) && verificationResult(state).message
                ? verificationResult(state).message + " "
                : "") + "Нажмите «Установить в Keenetic».";
        }
        if (routerStateUnknown(state)) {
            return "Локальная запись установки есть, но фактическое состояние Keenetic не подтверждено. Нажмите «Повторить проверку».";
        }
        if (isActuallyInstalled(state)) {
            if (state.checkResult && state.checkResult.result === "source_changed_routes_unchanged") {
                return "Источник изменился, но итоговый список маршрутов не изменился. Обновление Keenetic не требуется.";
            }
            return "Все зарегистрированные маршруты присутствуют в Keenetic.";
        }
        if (state && state.checkResult && state.checkResult.message) {
            return state.checkResult.message;
        }
        if (state && state.availableVersion) {
            return "Источник проверен. Нажмите «Скачать маршруты».";
        }
        return "Нажмите «Скачать маршруты». BROray сначала проверит источник, затем загрузит точную найденную версию.";
    }

    function button(bundleId, action, text, variant, icon) {
        var node = create("button", "button " + variant, text);
        node.type = "button";
        node.setAttribute("data-bundle-id", bundleId);
        node.setAttribute("data-action", action);
        node.setAttribute("data-icon", icon);
        node.addEventListener("click", onAction);
        return node;
    }

    function dataRow(label, fieldName) {
        var row = create("div", "ui-data-row");
        var labelNode = create("span", "ui-data-row__label", label);
        var valueNode = create("strong", "ui-data-row__value", "—");
        valueNode.setAttribute("data-field", fieldName);
        row.append(labelNode, valueNode);
        return row;
    }

    function createCard(bundle) {
        var card = create("article", "route-bundle-card ui-card");
        var summary = create("div", "route-card-summary");
        var service = create("div", "route-card-service");
        var logoBox = create("span", "route-card-logo");
        var logo;
        var copy = create("div", "route-card-copy");
        var title = create("h2", "route-card-title", bundle.name);
        var status = create("span", "status-badge status-loading", "Загрузка…");
        var metrics = create("div", "route-card-metrics");
        var notice = create("div", "route-card-notice status-neutral");
        var actions = create("div", "route-card-actions");
        var details = create("div", "route-card-details");
        var detailsGrid = create("div", "route-details-grid");
        var sourceBlock = create("section", "route-source-block");
        var sourceHeading = create("div", "route-source-heading");
        var sourceList = create("ul", "route-source-list");
        var technical = create("details", "route-technical-details");
        var technicalSummary = create("summary", "route-technical-summary", "Последняя операция");
        var operationOutput = create("pre", "route-operation-output", "Нет данных.");
        var danger = create("div", "route-danger-zone");
        var detailsButton;

        card.setAttribute("data-route-card", "");
        card.setAttribute("data-bundle-id", bundle.id);
        title.id = "route-title-" + bundle.id;
        card.setAttribute("aria-labelledby", title.id);
        logoBox.setAttribute("aria-hidden", "true");
        logoBox.innerHTML = bundle.logo;
        logo = logoBox.firstElementChild;
        if (logo) {
            logo.classList.add("route-card-logo-image");
            logo.setAttribute("focusable", "false");
        } else {
            logoBox.setAttribute("data-icon", "routes");
        }

        copy.append(
            create("span", "eyebrow route-card-eyebrow", "Готовый набор"),
            title,
            create("p", "route-card-description", bundle.description)
        );
        service.append(logoBox, copy);
        status.setAttribute("data-field", "status");
        status.setAttribute("data-icon", "status");
        summary.append(service, status);

        [["route-count", "маршрутов"], ["source-count", "исходных файлов"], ["presence-count", "в Keenetic"]].forEach(function (item) {
            var metric = create("div", "route-card-metric");
            var value = create("strong", "", "—");
            value.setAttribute("data-field", item[0]);
            metric.append(value, create("span", "", item[1]));
            metrics.appendChild(metric);
        });

        notice.setAttribute("data-field", "notice-box");
        notice.setAttribute("role", "status");
        notice.append(create("span", "route-card-notice-icon", ""), create("p", "", "Загрузка состояния…"));
        notice.firstChild.setAttribute("data-icon", "status");
        notice.lastChild.setAttribute("data-field", "message");

        detailsButton = button(bundle.id, "toggle-details", "Подробнее", "button-secondary", "chevron");
        detailsButton.setAttribute("aria-expanded", "false");
        detailsButton.setAttribute("aria-controls", "route-details-" + bundle.id);
        actions.append(
            button(bundle.id, "check", "Проверить обновления", "button-secondary", "update"),
            button(bundle.id, "download", "Скачать маршруты", "button-secondary", "backup"),
            button(bundle.id, "verify", "Подготовить к установке", "button-secondary", "status"),
            button(bundle.id, "export", "Установить в Keenetic", "button-secondary", "routes"),
            detailsButton
        );

        details.id = "route-details-" + bundle.id;
        details.hidden = true;
        detailsGrid.append(
            dataRow("Установленная версия", "installed-version"),
            dataRow("Доступная версия", "available-version"),
            dataRow("Проверка обновления", "last-checked"),
            dataRow("Проверка набора", "last-verified"),
            dataRow("Локальный набор", "local-verification"),
            dataRow("Состояние в Keenetic", "keenetic-verification"),
            dataRow("Уникальных маршрутов", "owned-unique"),
            dataRow("Пересечений с другими наборами", "owned-shared"),
            dataRow("Добавлено файлов", "files-added"),
            dataRow("Изменено файлов", "files-changed"),
            dataRow("Удалено файлов", "files-removed"),
            dataRow("Добавлено маршрутов", "routes-added"),
            dataRow("Удалено маршрутов", "routes-removed"),
            dataRow("Без изменений", "routes-unchanged"),
            dataRow("Интерфейс", "target-interface"),
            dataRow("Метрика", "managed-metric")
        );

        sourceHeading.append(create("div", "route-source-title", "Файлы источника"), create("a", "route-source-root", "Открыть раздел GitHub"));
        sourceHeading.lastChild.href = bundle.source;
        sourceHeading.lastChild.target = "_blank";
        sourceHeading.lastChild.rel = "noopener noreferrer";
        sourceList.setAttribute("data-field", "source-files");
        sourceBlock.append(sourceHeading, sourceList);
        technical.append(technicalSummary, operationOutput);
        operationOutput.setAttribute("data-field", "operation-output");
        danger.append(create("div", "route-danger-copy"), button(bundle.id, "delete", "Удалить из Keenetic", "button-danger-outline", "delete"));
        danger.firstChild.append(create("strong", "", "Удаление набора"), create("p", "", "Общие маршруты, принадлежащие другим наборам, будут сохранены."));
        details.append(detailsGrid, sourceBlock, technical, danger);
        card.append(summary, metrics, notice, actions, details);
        return card;
    }

    function cardFor(bundleId) {
        return document.querySelector('[data-route-card][data-bundle-id="' + bundleId + '"]');
    }

    function setField(card, name, value) {
        var node = card.querySelector('[data-field="' + name + '"]');
        if (node) node.textContent = value === null || value === undefined || value === "" ? "—" : String(value);
    }

    function setButtonLabel(node, label, icon) {
        if (!node) return;
        node.textContent = label;
        if (icon) node.setAttribute("data-icon", icon);
        if (window.BROrayIcons) window.BROrayIcons.scan(node);
    }

    function setButtonVariant(node, primary, warning) {
        if (!node) return;
        node.classList.remove("button-primary", "button-secondary", "button-warning");
        node.classList.add(warning ? "button-warning" : (primary ? "button-primary" : "button-secondary"));
        if (primary) node.setAttribute("data-recommended-action", "true");
        else node.removeAttribute("data-recommended-action");
    }

    function setStatus(card, presentation) {
        var node = card.querySelector('[data-field="status"]');
        if (!node) return;
        node.className = "status-badge " + presentation.className;
        node.textContent = presentation.text;
        node.setAttribute("data-icon", presentation.icon);
        if (window.BROrayIcons) window.BROrayIcons.scan(node);
    }

    function bundleName(bundleId) {
        var match = BUNDLES.filter(function (item) { return item.id === bundleId; })[0];
        if (match) return match.name;
        if (peerOperation && peerOperation.bundleId === bundleId && peerOperation.name) return peerOperation.name;
        return bundleId;
    }

    function operationSignature(operation) {
        var progress = operation && operation.progress ? operation.progress : null;
        if (!operation || operation.active !== true) return "inactive";
        return [
            operation.source || "",
            operation.bundleId || "",
            operation.action || "",
            operation.pending ? "1" : "0",
            operation.resumable ? "1" : "0",
            progress ? String(progress.running === true) : "",
            progress ? String(progress.resumable === true) : "",
            progress ? String(progress.current || 0) : "",
            progress ? String(progress.total || 0) : "",
            progress ? String(progress.stopRequested === true) : "",
            progress ? String(progress.message || "") : ""
        ].join("|");
    }

    function currentCatalogContext() {
        var found = null;
        BUNDLES.some(function (bundle) {
            var progress = operationProgress(states[bundle.id]);
            if (progress && (progress.running || progress.resumable)) {
                found = {
                    source: "catalog",
                    active: true,
                    pending: Boolean(progress.resumable && !progress.running),
                    resumable: Boolean(progress.resumable && !progress.running),
                    bundleId: bundle.id,
                    name: bundle.name,
                    action: progress.operation || "operation",
                    progress: progress
                };
                return true;
            }
            return false;
        });
        if (found) return found;
        if (globalOperation && globalOperation.active) {
            return {
                source: "catalog",
                active: true,
                pending: Boolean(globalOperation.pending),
                resumable: Boolean(globalOperation.resumable),
                bundleId: globalOperation.bundleId || null,
                name: globalOperation.bundleId ? bundleName(globalOperation.bundleId) : null,
                action: globalOperation.action || "operation",
                scope: globalOperation.scope || "routes",
                progress: globalOperation.bundleId ? operationProgress(states[globalOperation.bundleId]) : null
            };
        }
        return null;
    }

    function publishOperationState() {
        var context = currentCatalogContext();
        var detail = context || {source: "catalog", active: false, pending: false, resumable: false, bundleId: null, progress: null};
        var signature = operationSignature(detail);
        if (signature === lastPublishedOperationSignature) return;
        lastPublishedOperationSignature = signature;
        document.dispatchEvent(new CustomEvent(OPERATION_EVENT, {detail: detail}));
    }

    function effectiveGlobalOperation() {
        if (peerOperation && peerOperation.active) return peerOperation;
        if (globalOperation && globalOperation.active) return globalOperation;
        return currentCatalogContext();
    }

    function globalOperationMessage(operation) {
        var action;
        var name;
        if (!operation || operation.active !== true) return "";
        action = String(operation.action || "operation");
        name = operation.bundleId ? " «" + bundleName(operation.bundleId) + "»" : "";
        if (operation.pending || operation.resumable) {
            return "Операция с маршрутами" + name + " приостановлена. Завершите её кнопкой «Продолжить».";
        }
        if (operation.scope === "system") {
            if (action === "update") return "Сейчас выполняется обновление BROray. Операции с маршрутами временно недоступны.";
            if (action === "restore") return "Сейчас выполняется восстановление BROray. Операции с маршрутами временно недоступны.";
            if (action === "uninstall") return "Сейчас выполняется удаление BROray. Операции с маршрутами временно недоступны.";
            if (action === "cleanup") return "Сейчас выполняется очистка BROray. Операции с маршрутами временно недоступны.";
            if (action === "reinstall") return "Сейчас выполняется переустановка BROray. Операции с маршрутами временно недоступны.";
            return "Сейчас выполняется системная операция BROray. Операции с маршрутами временно недоступны.";
        }
        if (action.indexOf("preflight:") === 0) return "Выполняется предварительная проверка" + name + ". Остальные действия временно заблокированы.";
        if (action.indexOf("custom:") === 0) return "Выполняется операция с пользовательскими маршрутами. Остальные действия временно заблокированы.";
        if (action === "check") return "Проверяется обновление" + name + ". Остальные действия временно заблокированы.";
        if (action === "download") return "Загружаются маршруты" + name + ". Остальные действия временно заблокированы.";
        if (action === "verify" || action === "plan") return "Проверяется набор" + name + ". Остальные действия временно заблокированы.";
        if (action === "delete") return "Удаляются маршруты" + name + ". Остальные действия временно заблокированы.";
        if (action === "resume") return "Продолжается операция" + name + ". Остальные действия временно заблокированы.";
        return "Выполняется операция с маршрутами" + name + ". Остальные действия временно заблокированы.";
    }

    function setGlobalOperationNotice(card, operation) {
        var box = card.querySelector('[data-field="notice-box"]');
        if (!box) return;
        box.className = "route-card-notice status-neutral";
        box.firstChild.setAttribute("data-icon", "access");
        setField(card, "message", globalOperationMessage(operation));
        if (window.BROrayIcons) window.BROrayIcons.scan(box);
    }

    function setNotice(card, state) {
        var box = card.querySelector('[data-field="notice-box"]');
        var status = statusPresentation(state);
        if (!box) return;
        box.className = "route-card-notice ";
        if (status.className === "status-badge-danger") box.className += "status-error";
        else if (status.className === "status-badge-warning") box.className += "status-warning";
        else if (status.className === "status-badge-success") box.className += "status-success";
        else box.className += "status-neutral";
        box.firstChild.setAttribute("data-icon", status.icon);
        setField(card, "message", messageFor(state));
        if (window.BROrayIcons) window.BROrayIcons.scan(box);
    }

    function operationProgress(state) {
        return state && state.operationProgress && typeof state.operationProgress === "object"
            ? state.operationProgress
            : null;
    }

    function operationProgressTitle(progress) {
        if (!progress) return "Операция с маршрутами";
        if (progress.operation === "delete") return "Удаление маршрутов из Keenetic";
        if (progress.operation === "update") return "Обновление маршрутов в Keenetic";
        if (progress.operation === "restore") return "Восстановление маршрутов в Keenetic";
        return "Установка маршрутов в Keenetic";
    }

    function initialOperationProgress(bundleId, action) {
        var state = states[bundleId] || {};
        var existing = operationProgress(state);
        var keenetic = keeneticAction(state);
        var operation = action === "delete" ? "delete" :
            action === "resume" && existing && existing.operation ? existing.operation :
            (keenetic && keenetic.mode ? keenetic.mode : "install");
        return {
            schemaVersion: 2,
            kind: "routes",
            bundleId: bundleId,
            operation: operation,
            phase: "preparing",
            current: action === "resume" && existing ? Number(existing.current || 0) : 0,
            total: action === "resume" && existing ? Number(existing.total || 0) : 0,
            percent: action === "resume" && existing ? Number(existing.percent || 0) : 0,
            currentRoute: null,
            message: "Подготовка безопасного плана операции.",
            running: true,
            success: null,
            rolledBack: false,
            resumable: false,
            stopRequested: false,
            stoppedByUser: false,
            resumed: action === "resume"
        };
    }

    function renderOperationProgress(card, progress) {
        var box = card ? card.querySelector('[data-field="operation-progress"]') : null;
        var bar;
        var current;
        var total;
        var route;
        if (!box) return;
        if (!progress || progress.phase === "idle") {
            box.hidden = true;
            box.className = "route-card-progress";
            return;
        }

        current = Math.max(0, Number(progress.current || 0));
        total = Math.max(0, Number(progress.total || 0));
        if (total > 0 && current > total) current = total;
        box.hidden = false;
        box.className = "route-card-progress";
        if (progress.running) box.classList.add("is-running");
        else if (progress.resumable) box.classList.add("is-paused");
        else if (progress.success === true) box.classList.add("is-success");
        else if (progress.success === false) box.classList.add("is-error");

        setField(card, "operation-progress-title", operationProgressTitle(progress));
        setField(card, "operation-progress-counter", current + " из " + total);
        setField(card, "operation-progress-message", (progress.message || "Операция выполняется.") + (progress.running ? " Остановка операций с маршрутами недоступна." : ""));
        route = progress.currentRoute ? "Текущий маршрут: " + progress.currentRoute : "";
        var routeNode = card.querySelector('[data-field="operation-progress-route"]');
        if (routeNode) {
            routeNode.textContent = route;
            routeNode.hidden = !route;
        }
        bar = card.querySelector('[data-field="operation-progress-bar"]');
        if (bar) {
            if (total > 0) {
                bar.max = total;
                bar.value = current;
            } else {
                bar.max = 1;
                bar.removeAttribute("value");
            }
            bar.setAttribute("aria-label", operationProgressTitle(progress) + ": " + current + " из " + total);
        }
        var stopButton = card.querySelector('[data-action="stop"]');
        var resumeButton = card.querySelector('[data-action="resume"]');
        if (stopButton) {
            stopButton.hidden = true;
            stopButton.disabled = true;
            setButtonLabel(stopButton, progress.stopRequested ? "Остановка запрошена…" : "Остановить после текущего маршрута", "stop");
        }
        if (resumeButton) {
            resumeButton.hidden = !(progress.resumable && !progress.running);
            resumeButton.disabled = progress.running;
            setButtonVariant(resumeButton, progress.resumable && !progress.running);
        }
    }

    function renderSourceFiles(card, state) {
        var list = card.querySelector('[data-field="source-files"]');
        var files = sourceFiles(state);
        if (!list) return;
        list.textContent = "";
        if (!files.length) {
            list.appendChild(create("li", "route-source-empty", "Локальные файлы ещё не загружены. Источник набора доступен по ссылке выше; нажмите «Скачать маршруты»."));
            return;
        }
        files.forEach(function (file) {
            var item = create("li", "route-source-item");
            var link = create("a", "route-source-link", file.name || "Файл маршрутов");
            var meta = create("span", "route-source-meta");
            var hash = file.sha256 ? String(file.sha256).slice(0, 12) : "—";
            if (file.htmlUrl) {
                link.href = file.htmlUrl;
                link.target = "_blank";
                link.rel = "noopener noreferrer";
            } else {
                link.href = "#";
                link.addEventListener("click", function (event) { event.preventDefault(); });
            }
            meta.textContent = String(file.routeCount || 0) + " маршрутов · " + formatBytes(file.sizeBytes) + " · SHA " + hash;
            item.append(link, meta);
            list.appendChild(item);
        });
    }

    function operationOutput(state) {
        var output;
        if (state && state.operation && state.operation.output) return state.operation.output;
        if (state && state.lastError) {
            output = typeof state.lastError === "string" ? state.lastError : state.lastError.message;
            if (typeof state.lastError === "object" && state.lastError.details) {
                output = (output || "Последняя операция завершилась ошибкой.") +
                    "\n\nТехническая причина:\n" + String(state.lastError.details);
            }
            return output || "Последняя операция завершилась ошибкой.";
        }
        if (state && state.exportResult) return "Последняя установка: " + (state.exportResult.message || "завершена") + ".";
        if (state && state.verifyResult && verificationCurrent(state)) {
            return state.verifyResult.message || "Подготовка набора завершена.";
        }
        if (state && state.verifyResult && state.downloadedVersion) {
            return "Состояние маршрутов изменилось. Подготовьте набор повторно.";
        }
        if (state && state.downloadResult) return state.downloadResult.message || "Файлы маршрутов загружены.";
        if (state && state.checkResult) return state.checkResult.message || "Проверка обновления завершена.";
        if (state && state.deleteResult) return "Последнее удаление: " + (state.deleteResult.message || "завершено") + ".";
        return "Нет данных о выполненных операциях.";
    }

    function renderCard(bundleId, state) {
        var card = cardFor(bundleId);
        var presence = routerPresence(state);
        var files = sourceFiles(state);
        var changes = state && state.checkResult ? state.checkResult : {};
        var fileChanges = changes.fileChanges || {};
        var routeChanges = changes.routeChanges || {};
        var next = nextAction(state);
        var keenetic = keeneticAction(state);
        var checkButton;
        var downloadButton;
        var verifyButton;
        var keeneticButton;
        var deleteButton;
        var stopButton;
        var resumeButton;
        var detailsButton;
        var actions;
        var orderedButtons;
        var visibleActions;
        var downloadVisible;
        var verifyVisible;
        var keeneticVisible;
        var checkVisible;
        var progress = operationProgress(state);
        var progressRunning = Boolean(progress && progress.running);
        var progressResumable = Boolean(progress && progress.resumable && !progress.running);
        var effectiveOperation = effectiveGlobalOperation();
        var operationBelongsToCard = Boolean(effectiveOperation && effectiveOperation.active && effectiveOperation.bundleId === bundleId);
        var globalBusy = Boolean(effectiveOperation && effectiveOperation.active && effectiveOperation.bundleId !== bundleId);
        var busy;
        var expected = presence && presence.registered ? Number(presence.expectedRouteCount || 0) : 0;
        var present = presence && presence.registered ? Number(presence.presentRouteCount || 0) : 0;
        var targetInterface = (latestSummary && latestSummary.managedInterfaceDisplay) ||
            (state.exportBuild && state.exportBuild.targetInterfaceDisplay) ||
            (state.checkResult && state.checkResult.managedInterfaceDisplay) ||
            (state.exportResult && state.exportResult.targetInterfaceDisplay) || "BROray";
        var metric = (state.exportBuild && state.exportBuild.managedMetric) ||
            (state.exportResult && state.exportResult.managedMetric) || 1200;

        if (!card) return;
        states[bundleId] = state;
        if (progressRunning) {
            operationRunning = bundleId;
            busyBundles[bundleId] = progress.operation || "export";
        }
        busy = Boolean(busyBundles[bundleId]) || Boolean(operationRunning) || Boolean(effectiveOperation && effectiveOperation.active) || progressResumable;
        setStatus(card, progressRunning
            ? {text: progress && progress.stopRequested ? "Останавливается" : "Выполняется", className: "status-badge-warning", icon: "update"}
            : operationBelongsToCard
                ? {text: "Подготовка", className: "status-badge-warning", icon: "update"}
                : globalBusy
                    ? {text: "Временно недоступно", className: "status-badge-neutral", icon: "access"}
                    : statusPresentation(state));
        setNotice(card, state);
        renderOperationProgress(card, progress);
        setField(card, "route-count", state.routeCount || 0);
        setField(card, "source-count", files.length);
        setField(card, "presence-count", presence && presence.registered ? present + "/" + expected : "—");
        setField(card, "installed-version", formatVersion(state.installedVersion, "Не установлено"));
        setField(card, "available-version", formatVersion(state.availableVersion, "Не проверялась"));
        setField(card, "last-checked", formatDate(state.lastCheckedAt));
        setField(card, "last-verified", formatDate(state.lastVerifiedAt));
        setField(card, "local-verification", localSetInvalid(state)
            ? "Повреждён"
            : verificationCurrent(state) ? "Исправен" : "Не проверен");
        setField(card, "keenetic-verification", verificationCurrent(state) && verificationResult(state).keenetic
            ? ({
                complete: "Соответствует",
                not_installed: "Не установлен",
                update_pending: "Ожидает обновления",
                restore_required: "Требуется восстановление",
                conflict: "Конфликт",
                unavailable: "Недоступен"
            }[verificationResult(state).keenetic.status] || "Проверен")
            : "Не проверен");
        setField(card, "files-added", (fileChanges.addedFiles || []).length || 0);
        setField(card, "files-changed", (fileChanges.changedFiles || []).length || 0);
        setField(card, "files-removed", (fileChanges.removedFiles || []).length || 0);
        setField(card, "routes-added", routeChanges.added || 0);
        setField(card, "routes-removed", routeChanges.removed || 0);
        setField(card, "routes-unchanged", routeChanges.unchanged || 0);
        setField(card, "target-interface", targetInterface);
        setField(card, "managed-metric", metric);
        setField(card, "owned-unique", state.ownership ? Number(state.ownership.uniqueRouteCount || 0) : 0);
        setField(card, "owned-shared", state.ownership ? Number(state.ownership.sharedRouteCount || 0) : 0);
        setField(card, "operation-output", operationOutput(state));
        renderSourceFiles(card, state);

        checkButton = card.querySelector('[data-action="check"]');
        downloadButton = card.querySelector('[data-action="download"]');
        verifyButton = card.querySelector('[data-action="verify"]');
        keeneticButton = card.querySelector('[data-action="export"]');
        deleteButton = card.querySelector('[data-action="delete"]');
        stopButton = card.querySelector('[data-action="stop"]');
        resumeButton = card.querySelector('[data-action="resume"]');
        detailsButton = card.querySelector('[data-action="toggle-details"]');

        visibleActions = actionVisibility(state);
        downloadVisible = visibleActions.download;
        verifyVisible = visibleActions.verify;
        keeneticVisible = visibleActions.keenetic;
        checkVisible = visibleActions.check;

        setButtonLabel(checkButton, busyBundles[bundleId] === "check" ? "Проверка обновления…" : checkLabel(state), "update");
        checkButton.disabled = busy;
        checkButton.hidden = !checkVisible;
        setButtonVariant(checkButton, false);

        setButtonLabel(downloadButton,
            localSetInvalid(state) ? "Скачать заново" : state.downloadedVersion ? "Скачать обновление" : "Скачать маршруты",
            state.downloadedVersion ? "update" : "backup");
        downloadButton.disabled = busy;
        downloadButton.hidden = !downloadVisible;
        setButtonVariant(downloadButton, next === "download");

        setButtonLabel(verifyButton,
            busyBundles[bundleId] === "verify" ? "Подготовка набора…" :
                verificationConflict(state) ? "Повторить подготовку" :
                    verificationRetryRequired(state) || routerStateUnknown(state) ? "Повторить проверку" :
                        "Подготовить к установке",
            "status");
        verifyButton.disabled = busy || !state.downloadedVersion;
        verifyButton.hidden = !verifyVisible;
        setButtonVariant(verifyButton, next === "verify");

        if (keeneticVisible) {
            setButtonLabel(keeneticButton, keenetic.text, keenetic.icon);
            keeneticButton.disabled = busy;
            keeneticButton.hidden = false;
            setButtonVariant(keeneticButton, next === "export", hasRouterDrift(state));
        } else {
            keeneticButton.hidden = true;
            keeneticButton.disabled = true;
            setButtonVariant(keeneticButton, false);
        }

        deleteButton.disabled = busy || progressResumable || !state.installedVersion;
        if (stopButton) {
            stopButton.hidden = true;
            stopButton.disabled = true;
        }
        if (resumeButton) {
            resumeButton.hidden = !progressResumable;
            resumeButton.disabled = progressRunning;
            setButtonVariant(resumeButton, progressResumable);
        }
        detailsButton.disabled = false;

        actions = checkButton.parentElement;
        orderedButtons = [downloadButton, verifyButton, keeneticButton, checkButton, detailsButton];
        orderedButtons.forEach(function (node) {
            if (node && node.parentElement === actions) actions.appendChild(node);
        });

        card.classList.toggle("has-warning", hasRouterDrift(state));
        card.classList.toggle("has-error", Boolean(state.lastError) && !progressResumable);
        card.classList.toggle("is-paused", progressResumable);
    }

    function renderLoadError(bundleId, error) {
        var card = cardFor(bundleId);
        var checkButton;
        if (!card) return;
        setStatus(card, {text: "Ошибка", className: "status-badge-danger", icon: "status"});
        setField(card, "message", error && error.message ? error.message : "Не удалось загрузить состояние набора.");
        card.classList.add("has-error");
        Array.prototype.forEach.call(card.querySelectorAll("button"), function (node) {
            node.disabled = node.getAttribute("data-action") !== "check" && node.getAttribute("data-action") !== "toggle-details";
        });
        checkButton = card.querySelector('[data-action="check"]');
        if (checkButton) {
            setButtonLabel(checkButton, "Повторить загрузку состояния", "update");
            setButtonVariant(checkButton, true);
            checkButton.disabled = false;
        }
    }

    function activeOperationBundleId() {
        var effective = effectiveGlobalOperation();
        if (effective && effective.active && effective.bundleId) return effective.bundleId;
        if (operationRunning && typeof operationRunning === "string") return operationRunning;
        if (globalOperation && globalOperation.active && globalOperation.bundleId) return globalOperation.bundleId;
        var found = null;
        BUNDLES.some(function (bundle) {
            var progress = operationProgress(states[bundle.id]);
            if (progress && (progress.running || progress.resumable)) {
                found = bundle.id;
                return true;
            }
            return false;
        });
        return found;
    }

    function activeOperationProgress(bundleId) {
        if (peerOperation && peerOperation.active && peerOperation.bundleId === bundleId && peerOperation.progress) {
            return peerOperation.progress;
        }
        return bundleId ? operationProgress(states[bundleId]) : null;
    }

    function renderGlobalProgress() {
        if (window.BROrayRoutesOperationUI) {
            window.BROrayRoutesOperationUI.refresh();
        }
    }

    function refreshAllStatesAfterOperation() {
        return loadAllStates().then(function () {
            renderAll();
        });
    }

    function renderSummary() {
        var totals = latestSummary && latestSummary.totals ? latestSummary.totals : {};
        var status = byId("routes-page-status");
        var message = byId("routes-summary-message");
        var operation = latestSummary && latestSummary.operation ? latestSummary.operation : null;
        var loaded = Object.keys(states).length;
        var attention = Number(totals.attentionCount || 0);

        byId("routes-installed-count").textContent = String(Number(totals.installedCount || 0));
        byId("routes-attention-count").textContent = String(attention);
        byId("routes-total-count").textContent = String(Number(totals.bundleCount || BUNDLES.length));

        if (message) {
            if (operation && operation.active) {
                message.textContent = "Выполняется операция с набором «" + (operation.bundleName || operation.bundleId || "маршрутов") + "». Прогресс показан в общем окне.";
            } else if (loaded < BUNDLES.length) {
                message.textContent = "Получено " + loaded + " из " + BUNDLES.length + " наборов.";
            } else if (attention > 0) {
                message.textContent = "Некоторые наборы требуют действия. Причина показана в соответствующей карточке.";
            } else {
                message.textContent = "Состояние готовых наборов получено. Автоматическая проверка внешних источников не выполняется.";
            }
        }

        if (status) {
            if (operation && operation.active) {
                status.className = "status-badge status-badge-warning";
                status.textContent = operation.resumable ? "Можно продолжить" : "Операция выполняется";
            } else if (loaded < BUNDLES.length) {
                status.className = "status-badge status-loading";
                status.textContent = "Загрузка…";
            } else if (attention > 0) {
                status.className = "status-badge status-badge-warning";
                status.textContent = "Требуется действие";
            } else {
                status.className = "status-badge status-badge-success";
                status.textContent = "Состояние получено";
            }
            status.setAttribute("data-icon", "status");
            if (window.BROrayIcons) window.BROrayIcons.scan(status);
        }
    }

    function renderAll() {
        BUNDLES.forEach(function (bundle) {
            if (states[bundle.id]) renderCard(bundle.id, states[bundle.id]);
        });
        renderSummary();
        renderGlobalProgress();
        publishOperationState();
    }

    function loadProgress() {
        if (window.BROrayRoutesOperationUI) {
            return window.BROrayRoutesOperationUI.refresh();
        }
        return Promise.resolve(null);
    }

    function stopProgressWatcher() {
        return;
    }

    function watchProgress(bundleId) {
        if (window.BROrayRoutesOperationUI) {
            window.BROrayRoutesOperationUI.notifyStarted(bundleId);
        }
    }

    function loadState(bundleId) {
        if (states[bundleId]) return Promise.resolve(states[bundleId]);
        return loadAllStates().then(function () { return states[bundleId] || null; });
    }

    function loadAllStates() {
        if (summaryRequest) return summaryRequest;
        summaryController = window.AbortController ? new AbortController() : null;
        summaryRequest = request("/api/routes/catalog-summary.cgi", {
            method: "GET",
            credentials: "same-origin",
            signal: summaryController ? summaryController.signal : undefined
        }).then(function (summary) {
            var nextStates = Object.create(null);
            latestSummary = summary || null;
            operationRunning = false;
            busyBundles = Object.create(null);
            (summary && Array.isArray(summary.bundles) ? summary.bundles : []).forEach(function (state) {
                if (state && state.id) nextStates[state.id] = state;
            });
            states = nextStates;
            globalOperation = summary && summary.globalOperation && summary.globalOperation.active
                ? summary.globalOperation
                : null;
            renderAll();
            return summary;
        }).catch(function (error) {
            if (error && error.name === "AbortError") return null;
            throw error;
        }).then(function (value) {
            summaryRequest = null;
            summaryController = null;
            return value;
        }, function (error) {
            summaryRequest = null;
            summaryController = null;
            throw error;
        });
        return summaryRequest;
    }

    function operationText(action, bundleId) {
        if (action === "check") return "Проверка обновления…";
        if (action === "verify") return "Подготовка набора…";
        if (action === "download") return "Загрузка…";
        if (action === "export") {
            return hasRouterDrift(states[bundleId]) ? "Восстановление…" :
                states[bundleId] && states[bundleId].installedVersion ? "Обновление…" : "Установка…";
        }
        if (action === "delete") return "Удаление…";
        if (action === "resume") return "Продолжение…";
        return "Выполнение…";
    }

    function successMessage(action, bundle) {
        if (action === "check") return "Проверка обновления «" + bundle.name + "» завершена.";
        if (action === "verify") return "Набор «" + bundle.name + "» подготовлен к установке.";
        if (action === "download") return "Файлы маршрутов «" + bundle.name + "» загружены.";
        if (action === "export") return "Маршруты «" + bundle.name + "» применены в Keenetic.";
        if (action === "delete") return "Маршруты «" + bundle.name + "» удалены.";
        if (action === "resume") return "Операция «" + bundle.name + "» продолжена.";
        return "Операция завершена.";
    }

    function toggleDetails(bundleId, buttonNode) {
        var card = cardFor(bundleId);
        var details = card ? card.querySelector(".route-card-details") : null;
        var expanded;
        if (!details || !buttonNode) return;
        expanded = buttonNode.getAttribute("aria-expanded") === "true";
        expandedBundles[bundleId] = !expanded;
        details.hidden = expanded;
        buttonNode.setAttribute("aria-expanded", expanded ? "false" : "true");
        setButtonLabel(buttonNode, expanded ? "Подробнее" : "Скрыть", "chevron");
        card.classList.toggle("is-expanded", !expanded);
    }

    function formatKilobytes(value) {
        var number = Number(value || 0);
        if (number >= 1024) return (number / 1024).toFixed(number >= 10240 ? 0 : 1) + " МБ";
        return number + " КБ";
    }

    function preflightTitle(bundle, preflight) {
        var operation = preflight && preflight.operation;
        if (operation === "delete") return "Удалить «" + bundle.name + "» из Keenetic";
        if (operation === "update") return "Обновить «" + bundle.name + "» в Keenetic";
        if (operation === "restore") return "Восстановить «" + bundle.name + "» в Keenetic";
        return "Установить «" + bundle.name + "» в Keenetic";
    }

    function preflightMessage(preflight) {
        var checks = preflight && preflight.checks ? preflight.checks : {};
        var summary = preflight && preflight.summary ? preflight.summary : {};
        var storage = checks.storage || {};
        var keenetic = checks.keenetic || {};
        var localSet = checks.localSet || {};
        var dnsOverTls = checks.dnsOverTls || {};
        var lines = [];
        lines.push(preflight && preflight.message ? preflight.message : "Предварительная проверка завершена.");
        lines.push("");
        lines.push((checks.operationLock && checks.operationLock.ok ? "✓" : "✕") + " Конфликтующих операций нет");
        lines.push((checks.ndmc && checks.ndmc.ok ? "✓" : "✕") + " Команда ndmc доступна");
        lines.push((keenetic.ok ? "✓" : "✕") + " Keenetic: " +
            (keenetic.ok ? String(keenetic.interface || "BROray") + " подключён и находится в состоянии up" : "интерфейс недоступен"));
        lines.push((storage.ok ? "✓" : "✕") + " Свободное место: " + formatKilobytes(storage.freeKb) +
            "; требуется не менее " + formatKilobytes(storage.requiredKb));
        lines.push((localSet.ok ? "✓" : "✕") + " Локальный набор: " + Number(localSet.routeCount || 0) +
            " маршрутов; ошибок " + Number(localSet.invalidRouteCount || 0) +
            ", дубликатов " + Number(localSet.duplicateRouteCount || 0));
        if (dnsOverTls.required === true) {
            lines.push((dnsOverTls.ok ? "✓" : "✕") + " DNS-over-TLS: " +
                (dnsOverTls.ok ? "выбранные серверы проверены и применены" : "требуется свежая проверка и применение"));
        }
        lines.push("");
        lines.push("Всего в наборе: " + Number(summary.total || 0));
        if (Number(summary.alreadyPresent || 0) > 0) lines.push("Уже присутствует в Keenetic: " + Number(summary.alreadyPresent || 0));
        if (Number(summary.toCreate || 0) > 0) lines.push("Будет добавлено: " + Number(summary.toCreate || 0));
        if (Number(summary.toDelete || 0) > 0) lines.push("Будет удалено: " + Number(summary.toDelete || 0));
        if (Number(summary.sharedKept || 0) > 0) lines.push("Общие маршруты будут сохранены: " + Number(summary.sharedKept || 0));
        if (Number(summary.alreadyAbsent || 0) > 0) lines.push("Уже отсутствует: " + Number(summary.alreadyAbsent || 0));
        if (Number(summary.externalKept || 0) > 0) lines.push("Внешние маршруты не затрагиваются: " + Number(summary.externalKept || 0));
        lines.push("Конфликты: " + Number(summary.conflicts || 0));
        if (preflight && preflight.requestedAction === "resume" && preflight.resume) {
            lines.push("Продолжение с позиции: " + Number(preflight.resume.current || 0) + " из " + Number(preflight.resume.total || 0));
        }
        return lines.join("\n");
    }

    function confirmPreflight(bundle, preflight) {
        var danger = preflight && preflight.operation === "delete";
        if (!preflight || preflight.ready !== true) {
            return Promise.reject(new Error(preflightMessage(preflight)));
        }
        if (!window.BROrayDialogs || typeof window.BROrayDialogs.confirm !== "function") {
            return Promise.reject(new Error("Фирменное окно подтверждения недоступно."));
        }
        return window.BROrayDialogs.confirm({
            eyebrow: "Предварительная проверка",
            title: preflightTitle(bundle, preflight),
            message: preflightMessage(preflight),
            confirmText: preflight.requestedAction === "resume" ? "Продолжить" :
                (danger ? "Удалить" : (preflight.operation === "update" ? "Обновить" :
                    (preflight.operation === "restore" ? "Восстановить" : "Установить"))),
            cancelText: "Отмена",
            variant: danger ? "danger" : "primary",
            icon: danger ? "delete" : "security"
        });
    }

    function executeOperation(bundle, action, buttonNode, preflightToken) {
        var isLongOperation = action === "export" || action === "delete" || action === "resume";
        var timeout = isLongOperation ? LONG_OPERATION_TIMEOUT_MS : REQUEST_TIMEOUT_MS;
        var url = "/api/routes/" + action + ".cgi?bundleId=" + encodeURIComponent(bundle.id);

        if (isLongOperation) {
            url += "&preflightToken=" + encodeURIComponent(preflightToken || "");
        }
        busyBundles[bundle.id] = action;
        operationRunning = bundle.id;
        globalOperation = {active:true, bundleId:bundle.id, action:action};
        renderAll();
        setButtonLabel(buttonNode, operationText(action, bundle.id), buttonNode.getAttribute("data-icon"));
        buttonNode.setAttribute("aria-busy", "true");

        if (isLongOperation && window.BROrayRoutesOperationUI) {
            window.BROrayRoutesOperationUI.notifyStarted(bundle.id, bundle.name, "catalog", action);
        }

        var operationPromise = withTimeout(request(url, {
            method: "POST",
            credentials: "same-origin",
            headers: {"Accept": "application/json"},
            body: {}
        }), timeout);

        return operationPromise.then(function (newState) {
            if (newState && newState.bundleId) states[bundle.id] = newState;
            if (newState && newState.operationProgress && newState.operationProgress.resumable) {
                window.BROrayUI.toast(newState.operationProgress.message || "Операция приостановлена и может быть продолжена.", "warning");
            } else if (action === "verify" && newState && newState.verifyResult && newState.verifyResult.success === false) {
                window.BROrayUI.toast(newState.verifyResult.message || "Подготовка набора выявила проблему.", "warning");
            } else {
                window.BROrayUI.toast(successMessage(action, bundle), "success");
            }
        }).catch(function (error) {
            if (error && (error.status === 401 || error.code === "AUTH_REQUIRED" || error.code === "SESSION_REQUIRED")) {
                window.BROrayUI.redirectToLogin();
                return;
            }
            window.BROrayUI.toast(error && error.message ? error.message : "Операция с маршрутами завершилась ошибкой.", "error");
        }).then(function () {
            delete busyBundles[bundle.id];
            if (operationRunning === bundle.id) operationRunning = false;
            globalOperation = null;
            buttonNode.removeAttribute("aria-busy");
            if (window.BROrayRoutesOperationUI) window.BROrayRoutesOperationUI.clearPending(true);
            return refreshAllStatesAfterOperation();
        });
    }

    function executeFirstDownload(bundle, buttonNode) {
        var urlOptions = {
            method: "POST",
            credentials: "same-origin",
            headers: {"Accept": "application/json"},
            body: {}
        };
        var current = states[bundle.id] || null;
        var discoverPromise;

        busyBundles[bundle.id] = "download";
        operationRunning = bundle.id;
        globalOperation = {active:true, bundleId:bundle.id, action:"download"};
        renderAll();
        setButtonLabel(buttonNode, "Поиск и загрузка…", "backup");
        buttonNode.setAttribute("aria-busy", "true");

        discoverPromise = current && current.availableVersion
            ? Promise.resolve(current)
            : withTimeout(request("/api/routes/check.cgi?bundleId=" + encodeURIComponent(bundle.id), urlOptions), REQUEST_TIMEOUT_MS);

        return discoverPromise.then(function (checkedState) {
            if (checkedState && checkedState.bundleId) states[bundle.id] = checkedState;
            if (!checkedState || !checkedState.availableVersion) {
                throw new Error("Источник проверен, но версия маршрутов для загрузки не найдена.");
            }
            return withTimeout(request("/api/routes/download.cgi?bundleId=" + encodeURIComponent(bundle.id), urlOptions), REQUEST_TIMEOUT_MS);
        }).then(function (newState) {
            if (newState && newState.bundleId) states[bundle.id] = newState;
            window.BROrayUI.toast("Маршруты «" + bundle.name + "» скачаны.", "success");
        }).catch(function (error) {
            if (error && (error.status === 401 || error.code === "AUTH_REQUIRED" || error.code === "SESSION_REQUIRED")) {
                window.BROrayUI.redirectToLogin();
                return;
            }
            window.BROrayUI.toast(error && error.message ? error.message : "Не удалось скачать маршруты.", "error");
        }).then(function () {
            delete busyBundles[bundle.id];
            if (operationRunning === bundle.id) operationRunning = false;
            globalOperation = null;
            buttonNode.removeAttribute("aria-busy");
            if (window.BROrayRoutesOperationUI) window.BROrayRoutesOperationUI.clearPending(true);
            return refreshAllStatesAfterOperation();
        });
    }

    function prepareLongOperation(bundle, action, buttonNode) {
        var originalIcon = buttonNode.getAttribute("data-icon");
        busyBundles[bundle.id] = "preflight";
        operationRunning = bundle.id;
        globalOperation = {active:true, bundleId:bundle.id, action:"preflight"};
        renderAll();
        setButtonLabel(buttonNode, "Предварительная проверка…", "security");
        buttonNode.setAttribute("aria-busy", "true");
        if (window.BROrayRoutesOperationUI) {
            window.BROrayRoutesOperationUI.showPending(
                bundle.id,
                bundle.name,
                "catalog",
                "preflight",
                "Выполняется предварительная проверка набора и состояния Keenetic…",
                "preflight",
                false
            );
        }

        return withTimeout(request("/api/routes/preflight.cgi?bundleId=" + encodeURIComponent(bundle.id) +
            "&action=" + encodeURIComponent(action), {
            method: "POST", credentials: "same-origin", headers: {"Accept": "application/json"}, body: {}
        }), REQUEST_TIMEOUT_MS).then(function (preflight) {
            delete busyBundles[bundle.id];
            if (operationRunning === bundle.id) operationRunning = false;
            globalOperation = null;
            buttonNode.removeAttribute("aria-busy");
            renderAll();
            if (window.BROrayRoutesOperationUI) {
                window.BROrayRoutesOperationUI.showPending(
                    bundle.id,
                    bundle.name,
                    "catalog",
                    "preflight",
                    "Предварительная проверка завершена. Подтвердите действие.",
                    "confirmation",
                    false
                );
            }
            return confirmPreflight(bundle, preflight).then(function (confirmed) {
                if (!confirmed) {
                    if (window.BROrayRoutesOperationUI) window.BROrayRoutesOperationUI.clearPending(false);
                    return null;
                }
                return executeOperation(bundle, action, buttonNode, preflight.token);
            });
        }).catch(function (error) {
            delete busyBundles[bundle.id];
            if (operationRunning === bundle.id) operationRunning = false;
            globalOperation = null;
            buttonNode.removeAttribute("aria-busy");
            setButtonLabel(buttonNode, operationText(action, bundle.id).replace("…", ""), originalIcon);
            renderAll();
            if (window.BROrayRoutesOperationUI) window.BROrayRoutesOperationUI.clearPending(false);
            window.BROrayUI.toast(error && error.message ? error.message : "Предварительная проверка не завершена.", "error");
            return null;
        });
    }

    function requestStop() {
        window.BROrayUI.toast("Остановка операций с маршрутами недоступна. Дождитесь завершения операции.", "warning");
        return Promise.resolve(null);
    }

    function onAction(event) {
        var buttonNode = event.currentTarget;
        var bundleId = buttonNode.getAttribute("data-bundle-id");
        var action = buttonNode.getAttribute("data-action");
        var bundle = BUNDLES.filter(function (item) { return item.id === bundleId; })[0];
        if (!bundle) return;
        if (action === "toggle-details") {
            toggleDetails(bundleId, buttonNode);
            return;
        }
        if (action === "stop") {
            requestStop(bundle, buttonNode);
            return;
        }
        if (busyBundles[bundleId]) return;
        if (action === "download" && (!states[bundleId] || !states[bundleId].availableVersion)) {
            executeFirstDownload(bundle, buttonNode);
            return;
        }
        if (action === "resume" || action === "delete" || action === "export") {
            prepareLongOperation(bundle, action, buttonNode);
            return;
        }
        executeOperation(bundle, action, buttonNode, null);
    }

    function startPassiveRefresh() {
        function schedule() {
            if (passiveRefreshTimer) window.clearTimeout(passiveRefreshTimer);
            passiveRefreshTimer = null;
            if (document.hidden) return;
            passiveRefreshTimer = window.setTimeout(function () {
                passiveRefreshTimer = null;
                if (operationRunning || Object.keys(busyBundles).length) {
                    schedule();
                    return;
                }
                loadAllStates().catch(function () { return null; }).then(schedule);
            }, PASSIVE_REFRESH_INTERVAL_MS);
        }
        schedule();
    }

    function onPeerOperation(event) {
        var detail = event && event.detail ? event.detail : null;
        var next;
        var signature;
        if (!detail || detail.source === "catalog") return;
        next = detail.active ? detail : null;
        signature = operationSignature(next);
        if (signature === lastPeerOperationSignature) return;
        lastPeerOperationSignature = signature;
        peerOperation = next;
        renderAll();
    }

    function onAuthoritativeOperation(event) {
        var detail = event && event.detail ? event.detail : {active:false};
        var authoritative = detail.globalOperation && detail.globalOperation.active
            ? detail.globalOperation
            : (detail.active ? detail : null);
        if (authoritative) {
            globalOperation = authoritative;
        } else if (!operationRunning && !Object.keys(busyBundles).length && !Object.keys(longOperationRequests).length) {
            globalOperation = null;
            peerOperation = null;
        }
        renderAll();
    }

    function dispatchPeerControl(action, bundleId) {
        document.dispatchEvent(new CustomEvent(CONTROL_EVENT, {
            detail: {source: "catalog", action: action, bundleId: bundleId}
        }));
    }

    function revealApplication(session) {
        var app = byId("app");
        var loader = byId("page-loader");
        var user = byId("current-user");
        if (user) user.textContent = session && session.user ? session.user : "admin";
        if (loader) loader.hidden = true;
        if (app) app.hidden = false;
    }

    function initialize() {
        var mount = byId("routes-bundles");
        var fragment = document.createDocumentFragment();
        var resumeId = new URLSearchParams(window.location.search).get("resume");
        BUNDLES.forEach(function (bundle) { fragment.appendChild(createCard(bundle)); });
        mount.replaceChildren(fragment);

        document.addEventListener("broray:routes-operation-resume", function (event) {
            var detail = event && event.detail ? event.detail : {};
            var bundle = BUNDLES.filter(function (item) { return item.id === detail.bundleId; })[0];
            var card;
            var actionButton;
            if (!bundle || (detail.bundleType && detail.bundleType !== "catalog")) return;
            event.preventDefault();
            card = cardFor(bundle.id);
            actionButton = card ? card.querySelector('[data-action="export"]') : null;
            if (actionButton) prepareLongOperation(bundle, "resume", actionButton);
        });

        document.addEventListener("broray:routes-operation-finished", function () {
            loadAllStates().catch(function () { return null; });
        });
        document.addEventListener("broray:routes-operation-authoritative", onAuthoritativeOperation);
        document.addEventListener("visibilitychange", function () {
            if (document.hidden) {
                if (passiveRefreshTimer) window.clearTimeout(passiveRefreshTimer);
                passiveRefreshTimer = null;
                if (summaryController) summaryController.abort();
            } else {
                loadAllStates().catch(function () { return null; }).then(startPassiveRefresh);
            }
        });
        window.addEventListener("pagehide", function () {
            if (passiveRefreshTimer) window.clearTimeout(passiveRefreshTimer);
            if (summaryController) summaryController.abort();
        });

        request("/api/session.cgi", {method: "GET", credentials: "same-origin"}).then(function (session) {
            revealApplication(session);
            return loadAllStates();
        }).then(function () {
            startPassiveRefresh();
            if (resumeId) {
                var bundle = BUNDLES.filter(function (item) { return item.id === resumeId; })[0];
                var card = bundle ? cardFor(bundle.id) : null;
                var actionButton = card ? card.querySelector('[data-action="export"]') : null;
                if (bundle && actionButton) prepareLongOperation(bundle, "resume", actionButton);
            }
        }).catch(function (error) {
            if (error && error.status === 401) {
                window.BROrayUI.redirectToLogin();
                return;
            }
            revealApplication(null);
            window.BROrayUI.toast(error && error.message ? error.message : "Не удалось открыть готовые маршруты.", "error");
        });
    }

    if (window.BROrayTestMode === true) {
        window.BROrayRoutesCatalogTestHooks = {
            sameRouteVersion: sameRouteVersion,
            sameSourceVersion: sameSourceVersion,
            downloadRequired: downloadRequired,
            downloadActionRequired: downloadActionRequired,
            downloadStageRequired: downloadStageRequired,
            localSetInvalid: localSetInvalid,
            verificationCurrent: verificationCurrent,
            verificationRequired: verificationRequired,
            verificationConflict: verificationConflict,
            verificationRetryRequired: verificationRetryRequired,
            keeneticAction: keeneticAction,
            nextAction: nextAction,
            actionVisibility: actionVisibility,
            statusPresentation: statusPresentation,
            messageFor: messageFor,
            checkLabel: checkLabel,
            preflightMessage: preflightMessage,
            preflightTitle: preflightTitle,
            globalOperationMessage: globalOperationMessage
        };
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initialize, {once: true});
    } else {
        initialize();
    }
})();
