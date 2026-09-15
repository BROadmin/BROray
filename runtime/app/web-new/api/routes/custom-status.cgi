#!/opt/bin/ash

. /opt/broray/web-new/api/routes/custom-common.sh

broray_api_require_method GET
broray_api_require_session
broray_custom_routes_bundle_from_query
bundle_id="$BRORAY_CUSTOM_BUNDLE_ID"

ROOT="/opt/broray"
CUSTOM_INDEX="$ROOT/routes/custom.json"
STATE_FILE="$ROOT/routes/state/$bundle_id.json"
PRESENCE_LIBRARY="$ROOT/lib/routes-router-presence.sh"
PROGRESS_LIBRARY="$ROOT/lib/routes-operation-progress.sh"
API_LOCK_LIBRARY="$ROOT/lib/routes-api-operation.sh"
PRESENCE_FILE="/opt/broray/tmp/broray-custom-presence-$$.json"
PROGRESS_FILE="/opt/broray/tmp/broray-custom-progress-$$.json"
GLOBAL_FILE="/opt/broray/tmp/broray-custom-global-$$.json"
trap 'rm -f "$PRESENCE_FILE" "$PROGRESS_FILE" "$GLOBAL_FILE"' EXIT HUP INT TERM

jq -e --arg id "$bundle_id" '.schemaVersion == 1 and (.bundles | any(.id == $id))' \
    "$CUSTOM_INDEX" >/dev/null 2>&1 || broray_api_error \
    "404 Not Found" "ROUTES_BUNDLE_NOT_FOUND" \
    "Пользовательский набор маршрутов не найден."

jq -e --arg id "$bundle_id" '.schemaVersion == 1 and .bundleId == $id' \
    "$STATE_FILE" >/dev/null 2>&1 || broray_api_error \
    "500 Internal Server Error" "ROUTES_STATE_INVALID" \
    "Состояние пользовательского набора повреждено."

presence_json='{"available":false,"registered":false,"expectedRouteCount":null,"presentRouteCount":null,"missingRouteCount":null,"actualInstalled":null,"drift":null,"status":"unavailable","missingRoutes":[]}'
if [ -r "$PRESENCE_LIBRARY" ]; then
    . "$PRESENCE_LIBRARY"
    if broray_routes_presence_bundle "$bundle_id" "$PRESENCE_FILE" &&
       jq -e 'type == "object"' "$PRESENCE_FILE" >/dev/null 2>&1
    then
        presence_json="$(jq -c . "$PRESENCE_FILE")"
    fi
fi

progress_json="$(jq -nc --arg id "$bundle_id" '{schemaVersion:2,kind:"routes",bundleId:$id,operation:null,phase:"idle",current:0,total:0,percent:0,currentRoute:null,message:"Операция не выполняется.",running:false,success:null,rolledBack:false,resumable:false,stopRequested:false,stoppedByUser:false,resumed:false,errorRoute:null,pid:null,startedAt:null,updatedAt:null,completedAt:null}')"
if [ -r "$PROGRESS_LIBRARY" ]; then
    . "$PROGRESS_LIBRARY"
    if broray_routes_progress_read "$bundle_id" >"$PROGRESS_FILE" &&
       jq -e 'type == "object"' "$PROGRESS_FILE" >/dev/null 2>&1
    then
        progress_json="$(jq -c . "$PROGRESS_FILE")"
    fi
fi

global_json='{"active":false,"pending":false,"resumable":false,"pid":null,"scope":null,"action":null,"bundleId":null,"startedAt":null,"updatedAt":null,"stale":false}'
if [ -r "$API_LOCK_LIBRARY" ]; then
    . "$API_LOCK_LIBRARY"
    if broray_routes_api_lock_read_json >"$GLOBAL_FILE" &&
       jq -e 'type == "object"' "$GLOBAL_FILE" >/dev/null 2>&1
    then
        global_json="$(jq -c . "$GLOBAL_FILE")"
    fi
fi

data_json="$(
    jq -c \
        --argjson routerPresence "$presence_json" \
        --argjson operationProgress "$progress_json" \
        --argjson globalOperation "$global_json" '
        . + {
            routerPresence:$routerPresence,
            operationProgress:$operationProgress,
            globalOperation:$globalOperation
        }
    ' "$STATE_FILE"
)" || broray_api_error \
    "500 Internal Server Error" "ROUTES_STATE_READ_FAILED" \
    "Не удалось прочитать состояние пользовательского набора."

broray_api_success "$data_json"
