#!/usr/bin/env node
"use strict";

const fs = require("fs");
const path = require("path");

const root = process.argv[2];
if (!root) {
    console.error("Usage: broray-support-ui-selftest.js <repository-root>");
    process.exit(2);
}

function read(relativePath) {
    return fs.readFileSync(path.join(root, relativePath), "utf8");
}

function assert(condition, message) {
    if (!condition) {
        console.error("FAIL: " + message);
        process.exit(1);
    }
}

const shell = read("root/opt/broray/web-src/shell.html");
const appShell = read("root/opt/broray/web-src/assets/js/app-shell.js");
const page = read("root/opt/broray/web-src/pages/broray.html");
const readme = read("README.md");
const supportDoc = read("docs/support.md");
const site = read("site/docs.brovibe.cloud/broray/index.html");
const deploy = read("site/docs.brovibe.cloud/deploy.sh");
const url = "https://pay.cloudtips.ru/p/09b23d0a";

assert(shell.includes('id="support-button"'), "в меню нет кнопки поддержки");
assert(shell.includes('id="support-root"'), "нет общего окна поддержки");
assert(shell.includes("/assets/images/support/cloudtips-qr.svg"), "общее окно не использует локальный QR");
assert(appShell.includes("function openSupport()"), "окно поддержки не открывается");
assert(appShell.includes("function closeSupport()"), "окно поддержки не закрывается");
assert(page.includes('id="donate-link"'), "на странице BROray нет раскрываемого блока поддержки");
assert(page.includes("/assets/images/support/cloudtips-qr.svg"), "на странице BROray нет QR");
assert(readme.includes("docs/assets/cloudtips-qr.svg"), "README не содержит QR");
assert(supportDoc.includes("assets/cloudtips-qr.svg"), "документ поддержки не содержит QR");
assert(site.includes("/assets/cloudtips-qr.svg"), "сайт не содержит QR");
assert(deploy.includes("assets/cloudtips-qr.svg"), "deploy.sh не публикует QR");
for (const content of [shell, page, readme, supportDoc, site]) {
    assert(content.includes(url), "один из материалов не содержит прямую ссылку CloudTips");
}
console.log("PASS: support project UI and documentation");
