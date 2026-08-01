#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
export LC_ALL=C

CONTROLLER="/root/BROray-2.2.6-full-control-production-r41.sh"
EXPECTED_CONTROLLER_SHA="1807bd06eb7e5dd329d65dab2fcba5209f08989b00289a27c0eedbf01aff6921"
EXPECTED_CONTROLLER_SIZE="424919"

SOURCE_IPK="/root/BROray-2.2.6-candidate-r10/broray_2.2.6_aarch64-3.10.ipk"
EXPECTED_SOURCE_SHA="c0bc9a8d0c17afe4987989a24ab03ed906e0c4acb5169a5216334e6548b3f9e2"
EXPECTED_SOURCE_SIZE="13997155"

STABLE_PACKAGES="/var/www/api.brovibe.cloud/releases/opkg/aarch64-3.10/Packages"
STABLE_PACKAGES_GZ="${STABLE_PACKAGES}.gz"
EXPECTED_STABLE_SHA="8e37d0f02825056fe96b267548cfc09836f911d1bf9c7d0b735ebdc6576256ea"
EXPECTED_STABLE_GZ_SHA="083521021e13975c2d63959ca6678a5d0fca6bf1eb9fe46eefda1bb3827067da"

fail() {
    printf '\nОШИБКА: %s\n' "$*" >&2
    printf 'Stable и staging не изменялись этой командой.\n' >&2
    exit 1
}

sha_of() {
    sha256sum "$1" | awk '{print $1}'
}

size_of() {
    wc -c <"$1" | tr -d ' '
}

printf '%s\n' "=================================================="
printf '%s\n' "BROray 2.2.6 — exact production full-control r41"
printf '%s\n' "=================================================="

[ "$(id -u)" -eq 0 ] || fail "требуется root"

for command_name in bash busybox node python3 tar gzip sha256sum awk find sort stat cmp jq env wc tr; do
    command -v "$command_name" >/dev/null 2>&1 || fail "не найдена команда $command_name"
done

[ -s "$CONTROLLER" ] || fail "контроллер отсутствует: $CONTROLLER"
[ "$(sha_of "$CONTROLLER")" = "$EXPECTED_CONTROLLER_SHA" ] || fail "SHA-256 контроллера не совпадает"
[ "$(size_of "$CONTROLLER")" = "$EXPECTED_CONTROLLER_SIZE" ] || fail "размер контроллера не совпадает"
bash -n "$CONTROLLER" || fail "контроллер не прошёл bash -n"

[ -s "$SOURCE_IPK" ] || fail "точный production source r10 отсутствует"
[ "$(sha_of "$SOURCE_IPK")" = "$EXPECTED_SOURCE_SHA" ] || fail "SHA-256 production source r10 не совпадает"
[ "$(size_of "$SOURCE_IPK")" = "$EXPECTED_SOURCE_SIZE" ] || fail "размер production source r10 не совпадает"
tar -tzf "$SOURCE_IPK" >/dev/null 2>&1 || fail "production source r10 не читается"

[ -s "$STABLE_PACKAGES" ] || fail "stable Packages отсутствует"
[ -s "$STABLE_PACKAGES_GZ" ] || fail "stable Packages.gz отсутствует"
[ "$(sha_of "$STABLE_PACKAGES")" = "$EXPECTED_STABLE_SHA" ] || fail "stable Packages изменён"
[ "$(sha_of "$STABLE_PACKAGES_GZ")" = "$EXPECTED_STABLE_GZ_SHA" ] || fail "stable Packages.gz изменён"
gzip -cd "$STABLE_PACKAGES_GZ" | cmp - "$STABLE_PACKAGES" >/dev/null || fail "stable Packages.gz не соответствует Packages"

for forbidden in \
    BRORAY_FULL_CONTROL_TEST_MODE \
    BRORAY_FULL_CONTROL_TEST_SOURCE_IPK \
    BRORAY_FULL_CONTROL_TEST_SOURCE_SHA \
    BRORAY_FULL_CONTROL_TEST_SOURCE_SIZE \
    BRORAY_BUILDER_TEST_MODE \
    BRORAY_BUILDER_TEST_SOURCE_IPK \
    BRORAY_BUILDER_TEST_SOURCE_SHA \
    BRORAY_BUILDER_TEST_SOURCE_SIZE \
    BRORAY_TAR_BIN \
    BRORAY_OPT_ROOT \
    BRORAY_TMP_ROOT \
    BRORAY_LOCAL_ROLLBACK_TREE \
    BRORAY_LOCAL_ROLLBACK_META
do
    eval "value=\${$forbidden:-}"
    [ -z "$value" ] || fail "запрещена переменная окружения $forbidden"
done

bash "$CONTROLLER"
result=$?

printf '\nКод завершения: %s\n' "$result"
printf '%s\n' "Контроллер выполняет три чистые exact-production сборки A/B/C, независимый валидатор, финальную холодную проверку, негативные controls и контроль неизменности stable/staging."
printf '%s\n' "Контроллер не публикует staging и не изменяет stable."
printf '%s\n' "Только итог BROray 2.2.6-r41 FULL CONTROL: PASS разрешает следующий отдельный этап staging."
printf '%s\n' "=================================================="
exit "$result"
