#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
SOURCE_CHECK="$ROOT/lib/routes-source-check.sh"
WORK="${BRORAY_ROUTES_TEST_TMP:-$ROOT/tmp}/routes-r9-normalize.$$"

cleanup()
{
    rm -rf "$WORK"
}

trap cleanup EXIT HUP INT TERM

[ -r "$SOURCE_CHECK" ] || {
    echo "ОШИБКА self-test: не найден $SOURCE_CHECK" >&2
    exit 1
}

mkdir -p "$WORK"

cat >"$WORK/input.bat" <<'EOF'
route add 192.168.1.129 mask 255.255.255.128 0.0.0.0
route add 10.20.31.44 mask 255.255.240.0 0.0.0.0
route add 8.8.8.8 mask 255.255.255.255 0.0.0.0
EOF

cat >"$WORK/expected.txt" <<'EOF'
10.20.16.0/20
192.168.1.128/25
8.8.8.8/32
EOF

. "$SOURCE_CHECK"

broray_routes_parse_windows_bat \
    "$WORK/input.bat" \
    "$WORK/actual.txt" \
    "$WORK/error.txt" \
    100 || {
        cat "$WORK/error.txt" >&2 2>/dev/null || true
        exit 1
    }

diff -u "$WORK/expected.txt" "$WORK/actual.txt"

cat >"$WORK/default.bat" <<'EOF'
route add 1.2.3.4 mask 0.0.0.0 0.0.0.0
EOF

if broray_routes_parse_windows_bat \
    "$WORK/default.bat" \
    "$WORK/default.txt" \
    "$WORK/default.err" \
    100
then
    echo "ОШИБКА self-test: маршрут по умолчанию не был отклонён" >&2
    exit 1
fi

grep -Fq "маршрут по умолчанию запрещён" "$WORK/default.err" || {
    echo "ОШИБКА self-test: неверная причина отклонения маршрута по умолчанию" >&2
    exit 1
}

echo "BROray routes r9 source normalization self-test: PASS"
