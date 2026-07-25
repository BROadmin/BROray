# BROray WebUI source architecture

This parallel foundation is generated from the verified 2026-07-25 baseline.
It does not replace `/opt/broray/web-new`.

Approved canonical decisions:

- final CSS target: one `assets/css/allpage.css`;
- one static `shell.html` and separate `login-shell.html`;
- generated HTML is never edited by hand;
- menu order: Главная → Серверы → Подписки → Маршруты → Keenetic → Xray → BROray;
- icon left, text right;
- one buildId per WebUI build;
- Active colors: BROray `#32B379`, night `#CDD3DB`, day `#5F7697`;
- final dialogs replace browser `alert`, `confirm`, `prompt`;
- routes and servers move to two-level compact presentation in later page migrations.

Stage 1 deliberately preserves current page-specific CSS and page business markup.
It only establishes source/build architecture, static shell, canonical navigation, and buildId.
No claim is made that `allpage.css` migration is complete at this stage.

## Stage 2 — allpage foundation

- `allpage.css` is the canonical final shared stylesheet.
- `app.css`, `themes.css`, `icons.css` and `broray-branding.css` are no longer connected globally in the staged build.
- Login is fully migrated and uses only `allpage.css`.
- Application pages temporarily keep only their page-specific legacy CSS before `allpage.css`.
- Xray temporarily keeps `app.css` as a page-specific compatibility source until its dedicated migration stage.
- The working `/opt/broray/web-new` remains unchanged.


## Stage 6 — final staged WebUI

- all seven application pages and login use only `assets/css/allpage.css`;
- page-specific and compatibility CSS files are removed from the staged source;
- all pages use the generated static shell, one buildId and the canonical navigation order;
- Xray and BROray use the shared `dialogs.js` implementation;
- browser `alert`, `confirm`, `prompt`, HTML `style` attributes and JavaScript `.style` writes are forbidden by validation;
- the staged build remains inactive until the separate atomic switch stage.
