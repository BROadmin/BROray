#!/opt/bin/ash
set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
SRC="${BRORAY_WEB_SRC:-$ROOT/web-src}"
OUT="${BRORAY_WEB_OUT:-$ROOT/web-stage}"
TMP="$OUT.new.$$"
BUILD_FILE="$SRC/BUILD"
PAGES="$SRC/pages.json"

fail() { printf 'ОШИБКА: %s\n' "$*" >&2; exit 1; }
run_ash() { if command -v ash >/dev/null 2>&1; then ash "$@"; elif command -v busybox >/dev/null 2>&1; then busybox ash "$@"; else return 127; fi; }
cleanup() { rm -rf "$TMP" 2>/dev/null || true; }
trap cleanup EXIT HUP INT TERM

[ -r "$BUILD_FILE" ] || fail "не найден $BUILD_FILE"
[ -r "$PAGES" ] || fail "не найден $PAGES"
[ -r "$SRC/shell.html" ] || fail "не найден shell.html"
[ -r "$SRC/login-shell.html" ] || fail "не найден login-shell.html"
command -v jq >/dev/null 2>&1 || fail "не найден jq"

BUILD_ID="$(sed -n '1p' "$BUILD_FILE" | tr -d '\r\n')"
case "$BUILD_ID" in ''|*[!A-Za-z0-9._-]*) fail "некорректный buildId: $BUILD_ID";; esac

rm -rf "$TMP"
mkdir -p "$TMP"
cp -Rp "$SRC/api" "$TMP/api"
cp -Rp "$SRC/assets" "$TMP/assets"
cp -p "$SRC/manifest.json" "$TMP/manifest.json"

render_nav() {
    active="$1"
    jq -r --arg active "$active" '
      .navigation[] |
      "                <a class=\"sidebar-link" + (if .id == $active then " sidebar-link-active" else "" end) +
      "\" href=\"" + .href + "\" title=\"" + .label + "\" data-page=\"" + .id + "\"" +
      (if .id == $active then " aria-current=\"page\"" else "" end) + ">\n" +
      "                    <span class=\"sidebar-link-icon\" data-icon=\"" + .icon + "\" aria-hidden=\"true\"></span>\n" +
      "                    <span class=\"sidebar-link-label\">" + .label + "</span>\n" +
      "                </a>"
    ' "$PAGES"
}

render_links() {
    page_json="$1"
    {
      printf '%s\n' "$page_json" | jq -r '.styles[]?' | while IFS= read -r style; do
        [ -n "$style" ] || continue
        [ "$style" != "allpage.css" ] || continue
        printf '    <link rel="stylesheet" href="/assets/css/%s?v=%s">\n' "$style" "$BUILD_ID"
      done
      printf '    <link rel="stylesheet" href="/assets/css/allpage.css?v=%s">\n' "$BUILD_ID"
    }
}

render_scripts() {
    page_json="$1"
    printf '%s\n' "$page_json" | jq -r '.scripts[]?' | while IFS= read -r script; do
      [ -n "$script" ] || continue
      printf '    <script src="/assets/js/%s?v=%s"></script>\n' "$script" "$BUILD_ID"
    done
}

replace_token_file() {
    file="$1" token="$2" value_file="$3"
    awk -v token="{{${token}}}" -v repl_file="$value_file" '
      BEGIN { while ((getline line < repl_file) > 0) { repl = repl line "\n" } close(repl_file); sub(/\n$/, "", repl) }
      { line=$0; while ((pos=index(line,token)) > 0) { line = substr(line,1,pos-1) repl substr(line,pos+length(token)); } print line }
    ' "$file" > "$file.tmp.$$" && mv -f "$file.tmp.$$" "$file"
}

page_count="$(jq '.pages | length' "$PAGES")"
i=0
while [ "$i" -lt "$page_count" ]; do
    page_json="$(jq -c ".pages[$i]" "$PAGES")"
    id="$(printf '%s\n' "$page_json" | jq -r '.id')"
    output="$(printf '%s\n' "$page_json" | jq -r '.output')"
    html_title="$(printf '%s\n' "$page_json" | jq -r '.html_title')"
    section="$(printf '%s\n' "$page_json" | jq -r '.section')"
    title="$(printf '%s\n' "$page_json" | jq -r '.title')"
    content_rel="$(printf '%s\n' "$page_json" | jq -r '.content')"
    extras_rel="$(printf '%s\n' "$page_json" | jq -r '.extras')"
    [ -r "$SRC/$content_rel" ] || fail "нет содержимого $id"
    [ -r "$SRC/$extras_rel" ] || fail "нет extras $id"
    cp -p "$SRC/shell.html" "$TMP/$output"
    printf '%s\n' "$html_title" > "$TMP/.value"
    replace_token_file "$TMP/$output" HTML_TITLE "$TMP/.value"
    printf '%s\n' "$id" > "$TMP/.value"
    replace_token_file "$TMP/$output" PAGE_ID "$TMP/.value"
    printf '%s\n' "$section" > "$TMP/.value"
    replace_token_file "$TMP/$output" WORKSPACE_SECTION "$TMP/.value"
    printf '%s\n' "$title" > "$TMP/.value"
    replace_token_file "$TMP/$output" WORKSPACE_TITLE "$TMP/.value"
    printf '%s\n' "$BUILD_ID" > "$TMP/.value"
    replace_token_file "$TMP/$output" BUILD_ID "$TMP/.value"
    render_nav "$id" > "$TMP/.nav"
    replace_token_file "$TMP/$output" NAVIGATION "$TMP/.nav"
    render_links "$page_json" > "$TMP/.styles"
    replace_token_file "$TMP/$output" STYLE_LINKS "$TMP/.styles"
    cp -p "$SRC/$content_rel" "$TMP/.content"
    replace_token_file "$TMP/$output" PAGE_CONTENT "$TMP/.content"
    cp -p "$SRC/$extras_rel" "$TMP/.extras"
    replace_token_file "$TMP/$output" PAGE_EXTRAS "$TMP/.extras"
    render_scripts "$page_json" > "$TMP/.scripts"
    replace_token_file "$TMP/$output" PAGE_SCRIPTS "$TMP/.scripts"
    i=$((i+1))
done

cp -p "$SRC/login-shell.html" "$TMP/index.html"
printf '%s\n' "$BUILD_ID" > "$TMP/.value"
replace_token_file "$TMP/index.html" BUILD_ID "$TMP/.value"
cp -p "$SRC/pages/index.html" "$TMP/.content"
replace_token_file "$TMP/index.html" PAGE_CONTENT "$TMP/.content"

cat > "$TMP/modules.html" <<EOF
<!doctype html>
<html lang="ru" data-build-id="$BUILD_ID">
<head><meta charset="utf-8"><meta name="robots" content="noindex,nofollow"><title>BROray</title></head>
<body><p><a href="/home.html">Открыть BROray</a></p><script src="/assets/js/modules-redirect.js?v=$BUILD_ID"></script></body>
</html>
EOF
cat > "$TMP/assets/js/modules-redirect.js" <<'EOF'
window.location.replace("/home.html");
EOF

jq -n --arg buildId "$BUILD_ID" --arg builtAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" '{schemaVersion:1,buildId:$buildId,builtAt:$builtAt,source:"web-src"}' > "$TMP/build.json"
rm -f "$TMP/.value" "$TMP/.nav" "$TMP/.styles" "$TMP/.content" "$TMP/.extras" "$TMP/.scripts"

BRORAY_WEB_OUT="$TMP" BRORAY_WEB_SRC="$SRC" run_ash "$SRC/validate-web.sh"
rm -rf "$OUT.previous"
[ ! -d "$OUT" ] || mv "$OUT" "$OUT.previous"
mv "$TMP" "$OUT"
rm -rf "$OUT.previous"
trap - EXIT HUP INT TERM
printf 'WebUI stage built: %s\nBuild: %s\n' "$OUT" "$BUILD_ID"
