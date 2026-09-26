#!/opt/bin/ash
. /opt/broray/web-new/api/servers/common.sh
broray_api_require_method POST
broray_api_require_session
broray_servers_api_lock import

body_file="/opt/broray/tmp/servers-import-body.$$.json"
trap 'rm -f "$body_file"; command -v broray_routes_api_lock_release >/dev/null 2>&1 && broray_routes_api_lock_release' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
broray_servers_api_read_body_to_file "$body_file"
body_json="$(cat "$body_file")"
rm -f "$body_file"

uri="$(
    broray_servers_api_body_field "$body_json" uri
)"

[ -n "$uri" ] ||
    broray_api_error \
        "400 Bad Request" \
        "URI_REQUIRED" \
        "Не указана конфигурация сервера."

# Preview uses the installed parser and the same connection identity as sync.
# It writes only a private, disposable RAM directory, never the live catalog.
if printf '%s\n' "$body_json" | jq -e '.preview == true' >/dev/null 2>&1; then
    umask 077
    preview_dir="$(mktemp -d /tmp/broray-import-preview.XXXXXX)" ||
        broray_api_error '500 Internal Server Error' PREVIEW_UNAVAILABLE 'Не удалось подготовить проверку.'
    trap 'rm -f "$body_file"; [ -z "${preview_dir:-}" ] || rm -rf -- "$preview_dir"' EXIT
    preview_rc=0
    (
        . "$BRORAY_ROOT/lib/server-subscription-service.sh" || exit 1
        . "$BRORAY_ROOT/lib/server-import.sh" || exit 1
        preview_live="$BRORAY_SERVERS"
        BRORAY_SERVERS="$preview_dir/servers"
        BRORAY_TMP="$preview_dir/tmp"
        mkdir -p "$BRORAY_SERVERS" "$BRORAY_TMP" || exit 1
        broray_server_import_dispatch "$uri" manual '' 0 >"$preview_dir/import.log" || exit 1
        # Dispatcher prints a human report; the model is the single staged file.
        set -- "$BRORAY_SERVERS"/*.json
        [ "$#" = 1 ] || exit 1
        preview_file="$1"
        [ -f "$preview_file" ] && [ ! -L "$preview_file" ] || exit 1
        preview_key="$(broray_server_subscription_import_key "$preview_file")" || exit 1
        preview_duplicate=''
        for preview_old in "$preview_live"/*.json; do
            [ -e "$preview_old" ] || continue
            [ -f "$preview_old" ] && [ ! -L "$preview_old" ] || exit 1
            preview_old_key="$(broray_server_subscription_import_key "$preview_old")" || exit 1
            if [ "$preview_key" = "$preview_old_key" ]; then
                preview_duplicate="$(jq -r '.name // .id' "$preview_old")" || exit 1
                break
            fi
        done
        jq --arg key "$preview_key" --arg duplicate "$preview_duplicate" '
          {protocol,name,host:.address,port,transport:.network,security,
           canonical:$key,duplicate:(if $duplicate=="" then null
             else "Такая конфигурация уже сохранена: " + $duplicate + "." end)}
        ' "$preview_file"
    ) >"$preview_dir/result.json" 2>"$preview_dir/error" || preview_rc=$?
    if [ "$preview_rc" != 0 ]; then
        preview_error="$(tail -n 6 "$preview_dir/error")"
        broray_api_error '400 Bad Request' SERVER_PREVIEW_FAILED "${preview_error:-Конфигурация не прошла проверку.}"
    fi
    broray_api_success "$(cat "$preview_dir/result.json")"
    exit 0
fi

broray_servers_api_run \
    broray_server_import \
    "$uri" \
    manual \
    "" \
    0
