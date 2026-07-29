#!/usr/bin/env node
"use strict";

const fs = require("fs");
const path = require("path");

const root = process.argv[2];
if (!root) {
    throw new Error("Usage: node broray-cleanup-ui-selftest.js <broray-root>");
}

function read(relative) {
    return fs.readFileSync(path.join(root, relative), "utf8");
}

function requireText(text, needle, message) {
    if (!text.includes(needle)) {
        throw new Error(message + ": " + needle);
    }
}

const sourceHtml = read("web-src/pages/broray.html");
const builtHtml = read("web-new/broray.html");
const sourceJs = read("web-src/assets/js/broray.js");
const builtJs = read("web-new/assets/js/broray.js");
const sourceCss = read("web-src/assets/css/allpage.css");
const builtCss = read("web-new/assets/css/allpage.css");
const planApi = read("web-src/api/broray/cleanup-plan.cgi");
const cleanupApi = read("web-src/api/broray/cleanup.cgi");
const backend = read("lib/broray-cleanup.sh");
const systemBackend = read("lib/broray-page.sh");

requireText(sourceHtml, 'id="broray-cleanup-title"', "Нет карточки безопасной очистки");
requireText(sourceHtml, 'id="cleanup-plan"', "Нет кнопки проверки очистки");
requireText(sourceHtml, 'id="cleanup-run"', "Нет кнопки запуска очистки");
requireText(sourceHtml, 'id="cleanup-candidates"', "Нет списка кандидатов очистки");
requireText(sourceJs, '"/api/broray/cleanup-plan.cgi"', "Интерфейс не вызывает предварительную проверку");
requireText(sourceJs, '"/api/broray/cleanup.cgi"', "Интерфейс не вызывает очистку");
requireText(sourceJs, "function invalidateCleanupPlan()", "Изменение параметров не отменяет старый план");
requireText(sourceJs, "plan.token", "Токен плана не передаётся на выполнение");
requireText(sourceCss, ".broray-cleanup-options", "Нет стилей карточки очистки");
requireText(planApi, "broray_cleanup_plan_create", "API не формирует план");
requireText(cleanupApi, "broray_cleanup_execute", "API не выполняет проверенный план");
requireText(backend, "BRORAY_CLEANUP_TTL", "Нет срока действия подтверждения");
requireText(backend, "broray_cleanup_path_allowed", "Нет повторной проверки разрешённых путей");
requireText(backend, "broray_cleanup_global_lock_acquire", "Нет общей блокировки очистки");
requireText(systemBackend, '$BRORAY_BASE/run/global-operation.lock', "Системные операции используют другую глобальную блокировку");

if (sourceJs !== builtJs) {
    throw new Error("web-src и web-new содержат разные broray.js");
}
if (sourceCss !== builtCss) {
    throw new Error("web-src и web-new содержат разные allpage.css");
}
if (!builtHtml.includes('id="cleanup-run"')) {
    throw new Error("Собранная страница не содержит очистку");
}
for (const name of ["cleanup-plan.cgi", "cleanup.cgi"]) {
    if (read("web-src/api/broray/" + name) !== read("web-new/api/broray/" + name)) {
        throw new Error("web-src и web-new содержат разные " + name);
    }
}

console.log("PASS: safe cleanup UI, API and global operation contract");
