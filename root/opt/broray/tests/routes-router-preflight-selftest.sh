#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
TEST="$ROOT/tests/routes-static-conflict-selftest.sh"

[ -x "$TEST" ] || {
    echo "ОШИБКА self-test: не найден $TEST" >&2
    exit 1
}

if [ -n "${BRORAY_ASH_BIN:-}" ] &&
   [ -x "$BRORAY_ASH_BIN" ]
then
    exec "$BRORAY_ASH_BIN" "$TEST"
elif [ -x /opt/bin/ash ]; then
    exec /opt/bin/ash "$TEST"
elif command -v ash >/dev/null 2>&1; then
    exec ash "$TEST"
else
    exec /bin/sh "$TEST"
fi
