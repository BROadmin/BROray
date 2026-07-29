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

cat >"$WORK/lib/routes-router-sync.sh" <<'EOF'
broray_routes_sync_apply()
{
    echo sync-apply >>"$BRORAY_ROOT/order.log"
}
EOF

cat >"$WORK/lib/routes-download.sh" <<'EOF'
broray_routes_check_run() { :; }
broray_routes_download_run() { :; }
EOF

cat >"$WORK/lib/routes-router-delete.sh" <<'EOF'
broray_routes_router_delete_run() { echo delete-run >>"$BRORAY_ROOT/order.log"; }
broray_routes_delete_cleanup() { :; }
EOF

cat >"$WORK/lib/routes-operation-progress.sh" <<'EOF'
broray_routes_progress_request_stop() { echo stop-request >>"$BRORAY_ROOT/order.log"; }
broray_routes_progress_read()
{
    if [ -f "$BRORAY_ROOT/resume-delete" ]; then
        echo '{"operation":"delete","running":false,"resumable":true}'
    else
        echo '{"operation":"install","running":false,"resumable":true}'
    fi
}
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

if [ -n "${BRORAY_ASH_BIN:-}" ] && [ -x "$BRORAY_ASH_BIN" ]; then
    RUN_ASH="$BRORAY_ASH_BIN"
elif [ -x /opt/bin/ash ]; then
    RUN_ASH=/opt/bin/ash
elif command -v ash >/dev/null 2>&1; then
    RUN_ASH="$(command -v ash)"
else
    RUN_ASH="$(command -v busybox) ash"
fi

BRORAY_ROOT="$WORK" $RUN_ASH "$WORK/bin/broray-routes" stop tiktok >/dev/null
BRORAY_ROOT="$WORK" $RUN_ASH "$WORK/bin/broray-routes" resume tiktok >/dev/null
: >"$WORK/resume-delete"
BRORAY_ROOT="$WORK" $RUN_ASH "$WORK/bin/broray-routes" resume tiktok >/dev/null

cat >"$WORK/expected.log" <<'EOF'
build-export
sync-apply
stop-request
build-export
sync-apply
delete-run
EOF

diff -u "$WORK/expected.log" "$WORK/order.log"

echo "BROray routes r9 export chain self-test: PASS"
