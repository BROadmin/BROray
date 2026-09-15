#!/opt/bin/ash

set -u

ROOT="/opt/broray"
AUTH="$ROOT/web-new/api/auth-common.sh"
LIB="$ROOT/lib/routes-page-summary.sh"
OUT="$ROOT/tmp/routes-overview-api.$$.json"
ERR="$ROOT/tmp/routes-overview-api.$$.err"

cleanup()
{
    rm -f "$OUT" "$ERR"
}
trap cleanup EXIT HUP INT TERM

[ -r "$AUTH" ] || {
    printf '%s\r\n' 'Status: 500 Internal Server Error'
    printf '%s\r\n' 'Content-Type: application/json; charset=utf-8'
    printf '\r\n'
    printf '%s\n' '{"success":false,"data":null,"error":{"code":"AUTH_MODULE_UNAVAILABLE","message":"Модуль авторизации недоступен."}}'
    exit 0
}
. "$AUTH"
broray_api_require_method GET
broray_api_require_session

[ -r "$LIB" ] || broray_api_error "500 Internal Server Error" ROUTES_PAGE_SUMMARY_UNAVAILABLE "Модуль сводки маршрутов недоступен."
. "$LIB"
mkdir -p "$ROOT/tmp"

if ! broray_routes_page_overview "$OUT" 2>"$ERR"; then
    broray_api_error "500 Internal Server Error" ROUTES_OVERVIEW_FAILED "Не удалось получить обзор маршрутов." "$(tail -n 30 "$ERR" 2>/dev/null)"
fi
jq -e 'type == "object" and .schemaVersion == 1 and (.custom|type)=="object" and (.catalog|type)=="object"' "$OUT" >/dev/null 2>&1 ||
    broray_api_error "500 Internal Server Error" ROUTES_OVERVIEW_INVALID "Модуль маршрутов вернул некорректный обзор."

broray_api_success "$(jq -c . "$OUT")"
