#!/opt/bin/ash

. /opt/broray/web-new/api/routes/custom-common.sh

broray_api_require_method GET
broray_api_require_session
broray_custom_routes_bundle_from_query
bundle_id="$BRORAY_CUSTOM_BUNDLE_ID"

CUSTOM_INDEX="/opt/broray/routes/custom.json"
STATE_FILE="/opt/broray/routes/state/$bundle_id.json"
PRESENCE_LIBRARY="/opt/broray/lib/routes-router-presence.sh"
PRESENCE_FILE="/opt/broray/tmp/custom-routes-presence-$$.json"
trap 'rm -f "$PRESENCE_FILE"' EXIT HUP INT TERM

jq -e --arg id "$bundle_id" '.schemaVersion == 1 and (.bundles | any(.id == $id))' \
    "$CUSTOM_INDEX" >/dev/null 2>&1 ||
    broray_api_error \
        "404 Not Found" \
        "ROUTES_BUNDLE_NOT_FOUND" \
        "Пользовательский набор маршрутов не найден."

jq -e --arg id "$bundle_id" '.schemaVersion == 1 and .bundleId == $id' \
    "$STATE_FILE" >/dev/null 2>&1 ||
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_INVALID" \
        "Состояние пользовательского набора повреждено."

presence_json='{
  "available": false,
  "registered": false,
  "expectedRouteCount": null,
  "presentRouteCount": null,
  "missingRouteCount": null,
  "actualInstalled": null,
  "drift": null,
  "status": "unavailable",
  "missingRoutes": []
}'

if [ -r "$PRESENCE_LIBRARY" ]; then
    . "$PRESENCE_LIBRARY"
    if broray_routes_presence_bundle "$bundle_id" "$PRESENCE_FILE" &&
       jq -e 'type == "object"' "$PRESENCE_FILE" >/dev/null 2>&1
    then
        presence_json="$(jq -c . "$PRESENCE_FILE")"
    fi
fi

data_json="$(
    jq -c --argjson router_presence "$presence_json" '
        . + {routerPresence: $router_presence}
    ' "$STATE_FILE"
)" ||
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_READ_FAILED" \
        "Не удалось прочитать состояние пользовательского набора."

broray_api_success "$data_json"
