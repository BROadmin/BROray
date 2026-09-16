#!/opt/bin/ash
# Runs under the persistent cache guard. NDMC uses a different inner guard.
set -u
. "$BRORAY_ROOT/lib/routes-router-config.sh" || exit 1
[ ! -e "$BRORAY_ROUTES_CONFIG_LOCK" ] && [ ! -L "$BRORAY_ROUTES_CONFIG_LOCK" ] || exit 1
broray_routes_config_cache_fresh "$BRORAY_ROUTES_CONFIG_CACHE" && exit 0
tmp="$(mktemp "$BRORAY_ROUTES_CONFIG_CACHE.new.XXXXXX")" || exit 1
if broray_routes_config_fetch "$tmp"; then
    chmod 600 "$tmp" && mv -f "$tmp" "$BRORAY_ROUTES_CONFIG_CACHE" && exit 0
fi
rm -f "$tmp"
[ -s "$BRORAY_ROUTES_CONFIG_CACHE" ] && jq -e '
    (.source == "running-config") and ((.routes | type) == "array") and
    ((.serializationComplete | type) == "boolean")
' "$BRORAY_ROUTES_CONFIG_CACHE" >/dev/null 2>&1
