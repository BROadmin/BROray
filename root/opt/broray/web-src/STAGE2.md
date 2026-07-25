# Stage 2 — canonical allpage foundation

This stage remains parallel and does not activate `web-stage`.

Completed:

- canonical variables for BROray, night and day themes;
- approved Active colors;
- canonical shell, navigation, header, footer and mobile menu;
- icon-left/text-right rule;
- shared buttons, statuses, forms, data rows, loaders, toasts and current modal;
- login page fully moved to `allpage.css`;
- the four former shared layers are no longer connected globally;
- one buildId across the staged build.

Temporary compatibility:

- page-specific legacy styles remain connected before `allpage.css`;
- Xray temporarily loads `app.css` because its page layout is still located there;
- compatibility CSS variables remain until every page is migrated.

Not changed:

- `/opt/broray/web-new`;
- lighttpd;
- live CGI/API;
- routes, ProxyN, Xray, servers and subscriptions.
