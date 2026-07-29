"use strict";

const fs = require("fs");
const path = require("path");

const root = process.argv[2];
if (!root) {
    throw new Error("Usage: node broray-reinstall-ui-selftest.js <broray-root>");
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
const sourceApi = read("web-src/api/broray/reinstall.cgi");
const builtApi = read("web-new/api/broray/reinstall.cgi");

requireText(sourceHtml, 'id="reinstall-current"', "Нет кнопки восстановительной переустановки");
requireText(sourceHtml, 'id="installed-package-version"', "Не отображается версия пакета OPKG");
requireText(sourceHtml, "сохранив настройки, подписки, серверы, маршруты и DNS-over-TLS", "Нет пояснения о сохранении данных");
requireText(sourceJs, 'byId("reinstall-current").disabled = state.busy || !info || !info.reinstallSupported', "Кнопка не связана с backend-возможностью");
requireText(sourceJs, 'reinstall: "Переустановка"', "Нет подписи операции переустановки");
requireText(sourceJs, '"/api/broray/reinstall.cgi"', "Не используется API переустановки");
requireText(sourceJs, 'byId("reinstall-current").addEventListener("click", reinstallCurrent)', "Кнопка не привязана к обработчику");
requireText(sourceApi, "/opt/broray/bin/broray-system reinstall-start", "API не запускает reinstall-start");

if (sourceJs !== builtJs) {
    throw new Error("web-src и web-new содержат разные broray.js");
}
if (sourceApi !== builtApi) {
    throw new Error("web-src и web-new содержат разные reinstall.cgi");
}
if (!builtHtml.includes('id="reinstall-current"')) {
    throw new Error("Собранная страница не содержит кнопку переустановки");
}
if ((sourceJs.match(/addEventListener\("click", reinstallCurrent\)/g) || []).length !== 1) {
    throw new Error("Обработчик переустановки должен быть зарегистрирован ровно один раз");
}

console.log("PASS: интерфейс восстановительной переустановки");
