#!/usr/bin/env node

"use strict";

const fs = require("fs");
const vm = require("vm");
const path = process.argv[2] || "/opt/broray/web-new/assets/js/routes.js";

function fail(message) {
    console.error("BROray routes UI action self-test: FAIL — " + message);
    process.exit(1);
}

function version(sha, sourceSha) {
    return {
        contentSha256: sha,
        sourceSetSha256: sourceSha || sha,
        sourceCommit: sha
    };
}

function verified(sha, status, localValid) {
    return {
        contentSha256: sha,
        success: status !== "conflict" && localValid !== false,
        local: {valid: localValid !== false},
        keenetic: {status: status}
    };
}

const sandbox = {
    window: {BROrayTestMode: true},
    document: {
        readyState: "loading",
        addEventListener: function () {}
    },
    console,
    setTimeout,
    clearTimeout,
    Promise,
    Date,
    Math,
    Number,
    String,
    Boolean,
    Array,
    Object,
    isFinite,
    isNaN
};
sandbox.window.window = sandbox.window;
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync(path, "utf8"), sandbox, {filename: path});

const hooks = sandbox.window.BROrayRoutesTestHooks;
if (!hooks) fail("тестовые функции не экспортированы");

const V1 = version("v1", "s1");
const V2 = version("v2", "s2");

const cases = [
    {
        name: "источник ещё не проверен",
        state: {},
        expected: "check"
    },
    {
        name: "источник найден, файлы не скачаны",
        state: {availableVersion: V1, downloadedVersion: null},
        expected: "download"
    },
    {
        name: "файлы скачаны, набор ещё не проверен",
        state: {availableVersion: V1, downloadedVersion: V1, installedVersion: null},
        expected: "verify"
    },
    {
        name: "набор проверен и готов к установке",
        state: {
            availableVersion: V1,
            downloadedVersion: V1,
            installedVersion: null,
            verifyResult: verified("v1", "not_installed")
        },
        expected: "export"
    },
    {
        name: "набор установлен и соответствует Keenetic",
        state: {
            availableVersion: V1,
            downloadedVersion: V1,
            installedVersion: V1,
            verifyResult: verified("v1", "complete"),
            routerPresence: {
                available: true,
                registered: true,
                actualInstalled: true,
                drift: false
            }
        },
        expected: "check"
    },
    {
        name: "доступно обновление источника",
        state: {
            availableVersion: V2,
            downloadedVersion: V1,
            installedVersion: V1,
            verifyResult: verified("v1", "complete"),
            checkResult: {downloadRequired: true}
        },
        expected: "download"
    },
    {
        name: "локальный набор повреждён",
        state: {
            availableVersion: V1,
            downloadedVersion: V1,
            installedVersion: null,
            verifyResult: verified("v1", "not_checked", false)
        },
        expected: "download"
    },
    {
        name: "в Keenetic отсутствуют маршруты",
        state: {
            availableVersion: V1,
            downloadedVersion: V1,
            installedVersion: V1,
            verifyResult: verified("v1", "restore_required"),
            routerPresence: {
                available: true,
                registered: true,
                actualInstalled: null,
                drift: true,
                expectedRouteCount: 10,
                presentRouteCount: 8,
                missingRouteCount: 2
            }
        },
        expected: "export"
    },
    {
        name: "обнаружен конфликт Keenetic",
        state: {
            availableVersion: V1,
            downloadedVersion: V1,
            installedVersion: V1,
            verifyResult: verified("v1", "conflict")
        },
        expected: null
    },
    {
        name: "долгая операция приостановлена",
        state: {
            availableVersion: V1,
            downloadedVersion: V1,
            installedVersion: null,
            operationProgress: {
                operation: "install",
                running: false,
                resumable: true,
                current: 200,
                total: 226,
                phase: "paused"
            }
        },
        expected: "resume"
    },
    {
        name: "Keenetic недоступен во время проверки набора",
        state: {
            availableVersion: V1,
            downloadedVersion: V1,
            installedVersion: V1,
            verifyResult: {
                contentSha256: "v1",
                success: false,
                local: {valid: true},
                keenetic: {available: false, status: "unavailable"}
            },
            lastError: {code: "ROUTES_VERIFY_ROUTER_FAILED", message: "Keenetic недоступен"}
        },
        expected: "verify"
    }
];

cases.forEach(function (testCase) {
    const actual = hooks.nextAction(testCase.state);
    if (actual !== testCase.expected) {
        fail(testCase.name + ": ожидалось " + String(testCase.expected) + ", получено " + String(actual));
    }
});

if (hooks.checkLabel({}) !== "Проверить обновление") {
    fail("кнопка источника имеет неверную подпись");
}

const invalidState = cases[6].state;
if (!hooks.localSetInvalid(invalidState) || !hooks.downloadActionRequired(invalidState)) {
    fail("повреждённый локальный набор не предлагает повторное скачивание");
}

const unverifiedState = cases[2].state;
if (!hooks.verificationRequired(unverifiedState)) {
    fail("скачанный набор не требует отдельной проверки");
}

const completeState = cases[4].state;
if (!hooks.verificationCurrent(completeState)) {
    fail("актуальная проверка набора не распознана");
}


const preflight = {
    ready: true,
    requestedAction: "export",
    operation: "install",
    message: "Предварительная проверка завершена. Операция готова к запуску.",
    checks: {
        operationLock: {ok: true},
        ndmc: {ok: true},
        keenetic: {ok: true, interface: "Proxy0"},
        storage: {ok: true, freeKb: 8192, requiredKb: 4096},
        localSet: {ok: true, routeCount: 226, invalidRouteCount: 0, duplicateRouteCount: 0}
    },
    summary: {
        total: 226,
        alreadyPresent: 26,
        toCreate: 200,
        toDelete: 0,
        sharedKept: 4,
        alreadyAbsent: 0,
        externalKept: 3,
        conflicts: 0
    }
};

const preflightText = hooks.preflightMessage(preflight);
[
    "Конфликтующих операций нет",
    "Keenetic: Proxy0 подключён",
    "Свободное место: 8.0 МБ",
    "Всего в наборе: 226",
    "Будет добавлено: 200",
    "Общие маршруты будут сохранены: 4",
    "Внешние маршруты не затрагиваются: 3",
    "Конфликты: 0"
].forEach(function (fragment) {
    if (preflightText.indexOf(fragment) === -1) {
        fail("в сводке предварительной проверки отсутствует: " + fragment);
    }
});

if (hooks.preflightTitle({name: "Telegram"}, preflight) !== "Установить «Telegram» в Keenetic") {
    fail("неверный заголовок предварительной установки");
}
if (hooks.preflightTitle({name: "Telegram"}, {operation: "delete"}) !== "Удалить «Telegram» из Keenetic") {
    fail("неверный заголовок предварительного удаления");
}
if (hooks.preflightTitle({name: "Telegram"}, {operation: "update"}) !== "Обновить «Telegram» в Keenetic") {
    fail("неверный заголовок предварительного обновления");
}
const resumeText = hooks.preflightMessage(Object.assign({}, preflight, {
    requestedAction: "resume",
    resume: {current: 100, total: 5454}
}));
if (resumeText.indexOf("Продолжение с позиции: 100 из 5454") === -1) {
    fail("предварительная проверка продолжения не показывает сохранённую позицию");
}


if (hooks.globalOperationMessage({active: true, scope: "system", action: "update"}).indexOf("обновление BROray") === -1) {
    fail("системная блокировка не объясняется пользователю");
}
if (hooks.globalOperationMessage({active: true, scope: "routes", action: "delete", bundleId: "telegram"}).indexOf("Удаляются маршруты «Telegram»") === -1) {
    fail("блокировка другой карточкой не показывает выполняемое действие");
}

console.log("BROray routes UI action self-test: PASS");
