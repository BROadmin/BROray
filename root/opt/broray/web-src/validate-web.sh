#!/opt/bin/ash
set -eu

SRC="${BRORAY_WEB_SRC:-/opt/broray/web-src}"
OUT="${BRORAY_WEB_OUT:-/opt/broray/web-stage}"
BUILD_ID="$(sed -n '1p' "$SRC/BUILD" | tr -d '\r\n')"
PAGES="home servers subscriptions routes keenetic xray broray"
EXPECTED_NAV="/home.html /servers.html /subscriptions.html /routes.html /keenetic.html /xray.html /broray.html"
TMP="$OUT/.stage6-validation.$$"

fail()
{
    printf 'ОШИБКА ВАЛИДАЦИИ: %s\n' "$*" >&2
    exit 1
}

run_ash()
{
    if command -v ash >/dev/null 2>&1; then
        ash "$@"
    elif command -v busybox >/dev/null 2>&1; then
        busybox ash "$@"
    else
        return 127
    fi
}

cleanup()
{
    rm -rf "$TMP" 2>/dev/null || true
}

trap cleanup EXIT HUP INT TERM
mkdir -p "$TMP" || fail "не удалось создать временный каталог"

[ -d "$OUT" ] || fail "нет каталога $OUT"
[ -r "$OUT/build.json" ] || fail "нет build.json"
[ -r "$SRC/pages.json" ] || fail "нет pages.json"
[ -r "$OUT/assets/css/allpage.css" ] || fail "нет allpage.css"
[ "$(jq -r '.buildId // empty' "$OUT/build.json")" = "$BUILD_ID" ] ||
    fail "buildId в build.json не совпадает"

# Final CSS contract: exactly one physical stylesheet and no page styles.
css_count="$(find "$OUT/assets/css" -maxdepth 1 -type f 2>/dev/null | wc -l | tr -d ' ')"
[ "$css_count" = "1" ] || fail "в assets/css должно быть ровно 1 файл, найдено: $css_count"
[ "$(find "$OUT/assets/css" -maxdepth 1 -type f -printf '%f\n')" = "allpage.css" ] ||
    fail "единственным CSS должен быть allpage.css"
jq -e 'all(.pages[]; (((.styles // []) | length) == 0))' "$SRC/pages.json" >/dev/null ||
    fail "pages.json содержит локальные CSS страниц"

# Canonical palette and icon order.
grep -Fq -- '--ui-brand: #32b379;' "$OUT/assets/css/allpage.css" ||
    fail "нет Brand BROray #32B379"
grep -Fq -- '--ui-active: #32b379;' "$OUT/assets/css/allpage.css" ||
    fail "нет Active BROray #32B379"
grep -Fq -- '--ui-active: #cdd3db;' "$OUT/assets/css/allpage.css" ||
    fail "нет Active ночной темы #CDD3DB"
grep -Fq -- '--ui-active: #5f7697;' "$OUT/assets/css/allpage.css" ||
    fail "нет Active дневной темы #5F7697"
grep -Fq '.button[data-icon] > .ui-icon' "$OUT/assets/css/allpage.css" ||
    fail "не зафиксирован порядок иконки кнопки"
grep -Fq 'order: -1;' "$OUT/assets/css/allpage.css" ||
    fail "иконка не зафиксирована слева"
grep -Fq '.icon-sprite-host {' "$OUT/assets/css/allpage.css" ||
    fail "нет CSS-класса контейнера SVG-спрайта"
! grep -Eqi '@import[[:space:]]' "$OUT/assets/css/allpage.css" ||
    fail "allpage.css не должен использовать @import"

# Direct colors are allowed only in the canonical token section.
sed -n '/2\. Reset and base typography/,$p' "$OUT/assets/css/allpage.css" >"$TMP/css-after-tokens.css"
! grep -Eqi '#[0-9a-f]{3,8}|rgba?\(' "$TMP/css-after-tokens.css" ||
    fail "за пределами темы найден прямой цвет"

# Legacy variable aliases are fully removed.
! grep -Eq -- '--(background|surface|surface-raised|surface-soft|surface-color|panel-background|card-background|input-background|border|border-color|border-strong|text|text-primary|text-secondary|text-muted|muted|muted-text|action-color|action-text|action-soft|action-border|action-ring|accent|accent-strong|accent-text|success|warning|danger|shadow|card-shadow|radius-large|radius-medium|radius-small)([[:space:]]*:|[[:space:]]*\))' "$OUT/assets/css/allpage.css" ||
    fail "allpage.css содержит legacy-переменную"

important_count="$(grep -c '!important' "$OUT/assets/css/allpage.css" || true)"
[ "$important_count" = "5" ] ||
    fail "ожидалось 5 технических !important, найдено: $important_count"

# Every var() reference must resolve to a declaration in allpage.css.
grep -o 'var(--[A-Za-z0-9_-]*' "$OUT/assets/css/allpage.css" |
    sed 's/^var(//' | sort -u >"$TMP/used-vars"
grep -o -- '--[A-Za-z0-9_-]*[[:space:]]*:' "$OUT/assets/css/allpage.css" |
    sed 's/[[:space:]]*:$//' | sort -u >"$TMP/declared-vars"
while IFS= read -r css_var; do
    grep -Fxq -- "$css_var" "$TMP/declared-vars" ||
        fail "CSS-переменная используется, но не объявлена: $css_var"
done <"$TMP/used-vars"

for page in $PAGES; do
    file="$OUT/$page.html"
    [ -r "$file" ] || fail "нет $file"
    ! grep -q '{{[A-Z_][A-Z_]*}}' "$file" ||
        fail "в $page остались шаблонные токены"
    ! grep -Eqi '<style([[:space:]>])' "$file" ||
        fail "в $page найден блок style"
    ! grep -Eqi '[[:space:]]style[[:space:]]*=' "$file" ||
        fail "в $page найден атрибут style"
    ! grep -Eqi '<script[^>]*>[[:space:]]*[^<[:space:]]' "$file" ||
        fail "в $page найден inline script"

    ids="$(grep -o 'id="[^"]*"' "$file" | sed 's/^id="//;s/"$//' | sort | uniq -d)"
    [ -z "$ids" ] || fail "повторяющиеся id в $page: $ids"

    nav="$(
        sed -n '/<nav class="sidebar-navigation"/,/<\/nav>/p' "$file" |
            grep -o 'href="/[^"]*\.html"' |
            sed 's/^href="//;s/"$//' |
            tr '\n' ' ' |
            sed 's/[[:space:]]*$//'
    )"
    [ "$nav" = "$EXPECTED_NAV" ] || fail "порядок меню в $page: $nav"
    [ "$(grep -c 'aria-current="page"' "$file")" -eq 1 ] ||
        fail "активный пункт меню в $page"

    versions="$(
        grep -oE '/assets/[^" ]+\?v=[A-Za-z0-9._-]+' "$file" |
            sed 's/.*?v=//' | sort -u
    )"
    [ "$versions" = "$BUILD_ID" ] ||
        fail "смешанные версии ресурсов в $page: $versions"

    [ "$(grep -c '<link rel="stylesheet"' "$file")" -eq 1 ] ||
        fail "$page должен использовать один CSS"
    [ "$(grep -c '/assets/css/allpage.css?v=' "$file")" -eq 1 ] ||
        fail "$page должен подключать allpage.css ровно один раз"

    for ref in $(grep -oE '(href|src)="/[^"?#]+' "$file" | sed 's/^[^=]*="//'); do
        case "$ref" in
            /api/*|/) continue ;;
        esac
        [ -e "$OUT$ref" ] || fail "$page ссылается на отсутствующий ресурс $ref"
    done
done

# Login shares the same CSS and has no inline styles or scripts.
[ -r "$OUT/index.html" ] || fail "нет index.html"
! grep -q '{{[A-Z_][A-Z_]*}}' "$OUT/index.html" || fail "в login остались токены"
! grep -Eqi '<style([[:space:]>])' "$OUT/index.html" || fail "в login найден блок style"
! grep -Eqi '[[:space:]]style[[:space:]]*=' "$OUT/index.html" || fail "в login найден атрибут style"
[ "$(grep -c '<link rel="stylesheet"' "$OUT/index.html")" -eq 1 ] ||
    fail "страница входа должна использовать только один CSS"
[ "$(grep -c '/assets/css/allpage.css?v=' "$OUT/index.html")" -eq 1 ] ||
    fail "страница входа не использует allpage.css"
[ "$(
    grep -oE '/assets/[^" ]+\?v=[A-Za-z0-9._-]+' "$OUT/index.html" |
        sed 's/.*?v=//' | sort -u
)" = "$BUILD_ID" ] || fail "buildId login"

# CGI scripts retain executable bits and ash syntax.
[ -x "$OUT/api/session.cgi" ] || fail "CGI потеряли executable bit"
find "$OUT/api" -type f \( -name '*.cgi' -o -name '*.sh' \) | sort >"$TMP/api-files"
while IFS= read -r file; do
    run_ash -n "$file" || fail "ash -n: $file"
done <"$TMP/api-files"

# Final active JavaScript set: no native dialogs or inline style writes.
{
    printf '%s\n' theme-init.js common.js icons.js app-shell.js theme.js login.js
    jq -r '.pages[].scripts[]?' "$SRC/pages.json"
} | sort -u >"$TMP/active-js"
while IFS= read -r js_name; do
    js_file="$OUT/assets/js/$js_name"
    [ -r "$js_file" ] || fail "нет активного JavaScript: $js_name"
    ! grep -Eq 'window\.(alert|confirm|prompt)[[:space:]]*\(' "$js_file" ||
        fail "$js_name использует системный браузерный диалог"
    ! grep -Eq '\.style\.' "$js_file" ||
        fail "$js_name записывает inline style через JavaScript"
done <"$TMP/active-js"

# No inactive legacy JavaScript remains in the final staged assets.
for legacy_js in \
    app.js navigation.js placeholder.js routes-actions.js routes-static.js \
    subscriptions-nav-addon.js theme.js.new.20785
 do
    [ ! -e "$OUT/assets/js/$legacy_js" ] || fail "не удалён legacy JavaScript: $legacy_js"
done

grep -Fq 'host.className = "icon-sprite-host"' "$OUT/assets/js/icons.js" ||
    fail "icons.js не использует CSS-класс спрайта"
! grep -Fq 'document.documentElement.style.colorScheme' "$OUT/assets/js/theme-init.js" ||
    fail "theme-init.js записывает inline color-scheme"
! grep -Fq 'document.documentElement.style.colorScheme' "$OUT/assets/js/theme.js" ||
    fail "theme.js записывает inline color-scheme"

# Shared dialogs are present on every page that modifies system state.
for dialog_page in servers subscriptions routes keenetic xray broray; do
    [ "$(grep -c '/assets/js/dialogs.js?v=' "$OUT/$dialog_page.html")" -eq 1 ] ||
        fail "$dialog_page не подключает dialogs.js"
done
[ -r "$OUT/assets/js/dialogs.js" ] || fail "нет dialogs.js"
grep -Fq 'confirmPhrase: confirmPhrase' "$OUT/assets/js/dialogs.js" ||
    fail "dialogs.js не поддерживает усиленное подтверждение"
grep -Fq 'prompt: promptValue' "$OUT/assets/js/dialogs.js" ||
    fail "dialogs.js не поддерживает ввод значения"
for dialog_id in confirm-eyebrow confirm-input-group confirm-input confirm-input-hint confirm-input-error; do
    grep -Fq "id=\"$dialog_id\"" "$OUT/broray.html" ||
        fail "общая оболочка не содержит $dialog_id"
done

grep -Fq 'window.BROrayDialogs.confirm' "$OUT/assets/js/xray.js" ||
    fail "Xray не использует общий диалог"
! grep -Eq 'confirmRoot|confirmBackdrop|confirmResolver|closeConfirm' "$OUT/assets/js/xray.js" ||
    fail "Xray сохраняет локальную реализацию диалога"
grep -Fq 'window.BROrayDialogs.confirmPhrase' "$OUT/assets/js/broray.js" ||
    fail "BROray не использует усиленное подтверждение"
grep -Fq 'УДАЛИТЬ BROray ПОЛНОСТЬЮ' "$OUT/assets/js/broray.js" ||
    fail "BROray не содержит контрольную фразу полного удаления"
grep -Fq 'УДАЛИТЬ BROray' "$OUT/assets/js/broray.js" ||
    fail "BROray не содержит контрольную фразу обычного удаления"

# Canonical page titles and structures.
for title_pair in \
    'home:home-title:Главная' \
    'servers:servers-page-title:Серверы' \
    'subscriptions:subscriptions-page-title:Подписки' \
    'routes:routes-page-title:Маршруты' \
    'keenetic:keenetic-page-title:Keenetic' \
    'xray:xray-page-title:Xray' \
    'broray:broray-page-title:BROray'
 do
    page_name="${title_pair%%:*}"
    remainder="${title_pair#*:}"
    title_id="${remainder%%:*}"
    title_text="${remainder#*:}"
    title_compact="$(
        awk -v marker="id=\"$title_id\"" '
            index($0, marker) { capture = 1 }
            capture {
                print
                if (index($0, "</h1>")) { exit }
            }
        ' "$OUT/$page_name.html" | tr -d '[:space:]'
    )"
    [ "$title_compact" = "<h1id=\"$title_id\">$title_text</h1>" ] ||
        fail "неканонический заголовок страницы $page_name: $title_compact"
done

[ "$(grep -c 'class="home-module-card ui-card"' "$OUT/home.html")" -eq 6 ] ||
    fail "Главная должна содержать шесть модульных карточек"
home_order="$(
    grep -o 'data-module="[^"]*"' "$OUT/home.html" |
        sed 's/^data-module="//;s/"$//' | tr '\n' ' ' | sed 's/[[:space:]]*$//'
)"
[ "$home_order" = "servers subscriptions routes keenetic xray broray" ] ||
    fail "порядок карточек Главной: $home_order"

# Page-specific functional contracts retained from the migrations.
grep -Fq 'normalizeDescription' "$OUT/assets/js/keenetic.js" ||
    fail "Keenetic не нормализует тире в описании"
! grep -Fq 'Configuration OK' "$OUT/assets/js/xray.js" ||
    fail "в Xray осталась английская пользовательская строка"
grep -Fq 'server-details-toggle' "$OUT/assets/js/servers.js" ||
    fail "карточки серверов не имеют раскрываемых подробностей"
grep -Fq 'document.createElement("details")' "$OUT/assets/js/servers-auto-switch.js" ||
    fail "автопереключение не использует details"
[ "$(grep -c '^[[:space:]]*id: "' "$OUT/assets/js/routes.js")" -eq 9 ] ||
    fail "routes.js должен содержать девять наборов"
grep -Fq 'route-card-details' "$OUT/assets/js/routes.js" ||
    fail "маршруты не имеют раскрываемых подробностей"
grep -Fq 'file.htmlUrl' "$OUT/assets/js/routes.js" ||
    fail "маршруты не используют реальные ссылки исходных файлов"
grep -Fq 'hasRouterDrift' "$OUT/assets/js/routes.js" ||
    fail "маршруты не показывают требование восстановления"
grep -Fq 'requestError.details' "$OUT/assets/js/common.js" ||
    fail "common.js не сохраняет технические детали ошибки"

# BROray final migration contracts.
[ "$(grep -c '/assets/js/broray.js?v=' "$OUT/broray.html")" -eq 1 ] ||
    fail "BROray не подключает broray.js"
grep -Fq 'id="webui-build"' "$OUT/broray.html" ||
    fail "страница BROray не показывает buildId WebUI"
grep -Fq '<progress id="operation-progress-bar"' "$OUT/broray.html" ||
    fail "страница BROray не использует семантический progress"
grep -Fq 'row.className = "broray-component-row"' "$OUT/assets/js/broray.js" ||
    fail "BROray не формирует канонические строки компонентов"
grep -Fq 'Установка повреждена' "$OUT/assets/js/broray.js" ||
    fail "BROray не использует канонический статус повреждённой установки"
! grep -Fq 'Установка исправна' "$OUT/assets/js/broray.js" ||
    fail "BROray сохраняет старый статус установки"
! grep -Fq 'Требуется внимание' "$OUT/assets/js/broray.js" ||
    fail "BROray сохраняет расплывчатый статус"

broray_section="$TMP/broray.css"
sed -n '/BEGIN PAGE BRORAY/,/END PAGE BRORAY/p' "$OUT/assets/css/allpage.css" >"$broray_section"
[ -s "$broray_section" ] || fail "не найдена CSS-секция BROray"
! grep -Fq 'border-top' "$broray_section" ||
    fail "в BROray снова появилась разделительная верхняя линия"
! grep -Fq '!important' "$broray_section" ||
    fail "CSS-секция BROray содержит !important"
! grep -Eqi '#[0-9a-f]{3,8}|rgba?\(' "$broray_section" ||
    fail "CSS-секция BROray содержит прямой цвет"

# Every migrated page section remains canonical.
for section in HOME XRAY SUBSCRIPTIONS KEENETIC SERVERS ROUTES BRORAY; do
    section_file="$TMP/$section.css"
    sed -n "/BEGIN PAGE $section/,/END PAGE $section/p" "$OUT/assets/css/allpage.css" >"$section_file"
    [ -s "$section_file" ] || fail "не найдена CSS-секция $section"
    ! grep -Eqi '#[0-9a-f]{3,8}|rgba?\(' "$section_file" ||
        fail "CSS-секция $section содержит прямой цвет"
    ! grep -Fq '!important' "$section_file" ||
        fail "CSS-секция $section содержит !important"
done

printf 'Validation PASS: %s (%s)\n' "$OUT" "$BUILD_ID"
printf 'Stage 6 contracts PASS: all pages use only allpage.css; legacy CSS and native dialogs are removed\n'
