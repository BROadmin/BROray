#!/opt/bin/ash

. /opt/broray/web-new/api/routes/custom-common.sh

broray_api_require_method POST
broray_api_require_session
broray_custom_routes_bundle_from_query
bundle_id="$BRORAY_CUSTOM_BUNDLE_ID"

ROOT="/opt/broray"
STATE_FILE="$ROOT/routes/state/$bundle_id.json"
REGISTRY_FILE="$ROOT/routes/installed/bundles/$bundle_id.json"
PROGRESS_FILE="$ROOT/routes/operations/$bundle_id.json"
PRESENCE_LIBRARY="$ROOT/lib/routes-router-presence.sh"
PRESENCE_FILE="/opt/broray/tmp/broray-custom-remove-presence-$$.json"
trap 'rm -f "$PRESENCE_FILE"; command -v broray_routes_api_lock_release >/dev/null 2>&1 && broray_routes_api_lock_release' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

[ -r "$BRORAY_CUSTOM_ROUTES_API_LOCK_LIBRARY" ] || broray_api_error \
    "500 Internal Server Error" "ROUTES_API_LOCK_UNAVAILABLE" \
    "Модуль блокировки операций недоступен."
. "$BRORAY_CUSTOM_ROUTES_API_LOCK_LIBRARY"
lock_rc=0
broray_routes_api_lock_acquire "custom:remove-finalize" "$bundle_id" || lock_rc=$?
case "$lock_rc" in
    0) ;;
    2) broray_api_error "409 Conflict" "ROUTES_OPERATION_BUSY" \
        "Другая операция выполняется или ожидает продолжения." ;;
    *) broray_api_error "500 Internal Server Error" "ROUTES_API_LOCK_FAILED" \
        "Не удалось установить блокировку удаления." ;;
esac

if [ -r "$PROGRESS_FILE" ] && jq -e '.running == true or .resumable == true' \
    "$PROGRESS_FILE" >/dev/null 2>&1
then
    broray_api_error "409 Conflict" "ROUTES_CUSTOM_REMOVE_PROGRESS_PENDING" \
        "Сначала завершите или продолжите текущую операцию с этим набором."
fi

requires_delete=false
if [ -r "$STATE_FILE" ] && jq -e '.installedVersion != null' "$STATE_FILE" >/dev/null 2>&1; then
    requires_delete=true
fi
if [ -r "$REGISTRY_FILE" ] && jq -e '
    .installedVersion != null or (((.managedRouteKeys // []) | length) > 0)
' "$REGISTRY_FILE" >/dev/null 2>&1; then
    requires_delete=true
fi
if [ -r "$PRESENCE_LIBRARY" ]; then
    . "$PRESENCE_LIBRARY"
    if broray_routes_presence_bundle "$bundle_id" "$PRESENCE_FILE" &&
       jq -e '
           (.registered == true) and
           ((.actualInstalled == true) or ((.presentRouteCount // 0) > 0))
       ' "$PRESENCE_FILE" >/dev/null 2>&1
    then
        requires_delete=true
    fi
fi

[ "$requires_delete" = false ] || broray_api_error \
    "409 Conflict" "ROUTES_CUSTOM_REMOVE_REQUIRES_DELETE" \
    "Сначала безопасно удалите маршруты набора из Keenetic, затем удалите локальную карточку."

BRORAY_CUSTOM_ROUTES_PRELOCKED=true
export BRORAY_CUSTOM_ROUTES_PRELOCKED
broray_custom_routes_run "$BRORAY_CUSTOM_ROUTES_CLI" remove "$bundle_id"
