import {readFileSync} from "node:fs";
import assert from "node:assert/strict";
import {test} from "node:test";

test("Premium domain check uses one card and links from the shared sidebar", () => {
    const page = readFileSync(new URL("./static/premium-domain.html", import.meta.url), "utf8");
    const shell = readFileSync(new URL("./static/assets/js/app-shell.js", import.meta.url), "utf8");
    assert.match(page, /data-page="premium-domain"/);
    assert.equal((page.match(/class="[^"]*\bpremium-domain-card\b/g) || []).length, 1);
    assert.match(page, /id="premium-add"[^>]*hidden/);
    assert.match(shell, /\/premium-domain\.html/);
    assert.match(shell, /Проверка домена/);
});

test("Premium sends the same-origin proof preserved by KeenDNS", () => {
    const client = readFileSync(new URL("./static/assets/js/premium-domain.js", import.meta.url), "utf8");
    assert.match(client, /"X-BROray-Origin": window\.location\.origin/);
});
