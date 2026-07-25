#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
CLI="$ROOT/bin/broray-routes"
WORK="${BRORAY_ROUTES_TEST_TMP:-$ROOT/tmp}/routes-r9-chain.$$"

cleanup()
{
    rm -rf "$WORK"
}

trap cleanup EXIT HUP INT TERM

[ -x "$CLI" ] || {
    echo "ОШИБКА self-test: не найден $CLI" >&2
    exit 1
}

mkdir -p "$WORK/bin" "$WORK/lib"
cp -p "$CLI" "$WORK/bin/broray-routes"
chmod 755 "$WORK/bin/broray-routes"

cat >"$WORK/lib/routes-export-build.sh" <<'EOF'
broray_routes_export_build_run()
{
    echo build-export >>"$BRORAY_ROOT/order.log"
}
EOF

cat >"$WORK/lib/routes-router-preflight.sh" <<'EOF'
broray_routes_preflight_run()
{
    echo preflight >>"$BRORAY_ROOT/order.log"
}
EOF

cat >"$WORK/lib/routes-router-export.sh" <<'EOF'
broray_routes_router_export_run()
{
    echo export >>"$BRORAY_ROOT/order.log"
}
EOF

cat >"$WORK/lib/routes-download.sh" <<'EOF'
broray_routes_check_run() { :; }
broray_routes_download_run() { :; }
EOF

cat >"$WORK/lib/routes-router-delete.sh" <<'EOF'
broray_routes_router_delete_run() { :; }
broray_routes_delete_cleanup() { :; }
EOF

: >"$WORK/order.log"

if [ -n "${BRORAY_ASH_BIN:-}" ] && [ -x "$BRORAY_ASH_BIN" ]; then
    BRORAY_ROOT="$WORK" "$BRORAY_ASH_BIN" \
        "$WORK/bin/broray-routes" export tiktok
elif [ -x /opt/bin/ash ]; then
    BRORAY_ROOT="$WORK" /opt/bin/ash \
        "$WORK/bin/broray-routes" export tiktok
elif command -v ash >/dev/null 2>&1; then
    BRORAY_ROOT="$WORK" ash \
        "$WORK/bin/broray-routes" export tiktok
else
    BRORAY_ROOT="$WORK" busybox ash \
        "$WORK/bin/broray-routes" export tiktok
fi

cat >"$WORK/expected.log" <<'EOF'
build-export
preflight
export
EOF

diff -u "$WORK/expected.log" "$WORK/order.log"

echo "BROray routes r9 export chain self-test: PASS"
