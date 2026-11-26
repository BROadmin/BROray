import {readFileSync} from "node:fs";
import assert from "node:assert/strict";
import {test} from "node:test";

function read(path) { return readFileSync(new URL("./static/" + path, import.meta.url), "utf8"); }

test("Premium IP adds one check page and the routes editor accepts one mixed list", () => {
    const shell = read("assets/js/app-shell.js");
    assert.match(shell, /По доменам и IP/);
    assert.match(shell, /Проверка IP/);
    const page = read("premium-ip.html");
    assert.match(page, /id="premium-domain-input"/);
    assert.match(page, /data-page="premium-ip"/);
    assert.match(read("routes-domains.html"), /Домены, IPv4 и IPv6/);
});
