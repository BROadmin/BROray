#!/opt/bin/ash
set -eu

SOURCE_ROOT="${BRORAY_TEST_SOURCE_ROOT:-/opt/broray}"
LIB="$SOURCE_ROOT/lib/broray-cleanup.sh"
[ -r "$LIB" ] || { printf '%s\n' 'FAIL: модуль очистки не найден' >&2; exit 1; }

TEST_ROOT="/tmp/broray-cleanup-selftest-$$"
BASE="$TEST_ROOT/broray"
OUTSIDE="$TEST_ROOT/outside.txt"

fail()
{
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

cleanup()
{
    rm -rf "$TEST_ROOT"
}
trap cleanup EXIT HUP INT TERM

reset_tree()
{
    rm -rf "$BASE"
    mkdir -p \
        "$BASE/tmp" "$BASE/backup/xray-binaries" \
        "$BASE/routes/tmp" "$BASE/routes/transactions" "$BASE/routes/backup" \
        "$BASE/logs" "$BASE/run/broray"
}

make_old()
{
    case "${2:-10 days ago}" in
        '3 days ago') stamp=202601020000 ;;
        '30 days ago') stamp=202501010000 ;;
        *) stamp=202601010000 ;;
    esac
    /usr/bin/busybox touch -t "$stamp" "$1"
}

load_module()
{
    BRORAY_BASE="$BASE"
    BRORAY_CLEANUP_RUN="$BASE/run/cleanup"
    BRORAY_CLEANUP_PLAN_DIR="$BRORAY_CLEANUP_RUN/plans"
    BRORAY_CLEANUP_GLOBAL_LOCK="$BASE/run/global-operation.lock"
    BRORAY_CLEANUP_TTL=120
    export BRORAY_BASE BRORAY_CLEANUP_RUN BRORAY_CLEANUP_PLAN_DIR
    export BRORAY_CLEANUP_GLOBAL_LOCK BRORAY_CLEANUP_TTL
    . "$LIB"
}

create_fixture()
{
    printf managed >"$BASE/tmp/broray-old.part"
    printf unknown >"$BASE/tmp/customer-file.txt"
    printf outside >"$OUTSIDE"
    ln -s "$OUTSIDE" "$BASE/tmp/broray-external-link"
    make_old "$BASE/tmp/broray-old.part"

    for stamp in 20260101-000001 20260101-000002 20260101-000003 20260101-000004 20260101-000005; do
        printf '%s' "$stamp" >"$BASE/backup/system-before-$stamp.tar.gz"
        make_old "$BASE/backup/system-before-$stamp.tar.gz" '3 days ago'
    done
    printf unknown >"$BASE/backup/manual-copy.tar.gz"
    make_old "$BASE/backup/manual-copy.tar.gz" '30 days ago'

    for stamp in 20260101-000001 20260101-000002 20260101-000003 20260101-000004; do
        mkdir -p "$BASE/routes/backup/catalog-$stamp"
        printf route >"$BASE/routes/backup/catalog-$stamp/item"
        make_old "$BASE/routes/backup/catalog-$stamp" '3 days ago'
    done

    printf old >"$BASE/logs/broray.log.1"
    printf active >"$BASE/logs/broray.log"
    make_old "$BASE/logs/broray.log.1"
}

reset_tree
create_fixture
load_module

plan="$(broray_cleanup_plan_create true true true true)" || fail "$BRORAY_CLEANUP_ERROR_MESSAGE"
printf '%s' "$plan" | jq -e '.schemaVersion == 1 and .candidateCount == 5 and .estimatedBytes > 0' >/dev/null ||
    fail 'неверный план очистки'
printf '%s' "$plan" | jq -e '[.candidates[].path] | index("tmp/customer-file.txt") == null' >/dev/null ||
    fail 'неизвестный файл попал в план'
printf '%s' "$plan" | jq -e '[.candidates[].path] | index("tmp/broray-external-link") == null' >/dev/null ||
    fail 'симлинк попал в план'
printf '%s' "$plan" | jq -e '[.candidates[].path] | index("backup/manual-copy.tar.gz") == null' >/dev/null ||
    fail 'ручная резервная копия попала в план'

token="$(printf '%s' "$plan" | jq -r '.token')"
result="$(broray_cleanup_execute "$token")" || fail "$BRORAY_CLEANUP_ERROR_MESSAGE"
printf '%s' "$result" | jq -e '.ok == true and .deletedCount == 5 and .freedBytes > 0' >/dev/null ||
    fail 'неверный результат очистки'
[ ! -e "$BASE/tmp/broray-old.part" ] || fail 'временный файл не удалён'
[ -e "$BASE/tmp/customer-file.txt" ] || fail 'неизвестный файл удалён'
[ -L "$BASE/tmp/broray-external-link" ] || fail 'симлинк удалён'
[ -e "$OUTSIDE" ] || fail 'внешняя цель симлинка удалена'
[ -e "$BASE/backup/manual-copy.tar.gz" ] || fail 'ручная копия удалена'
[ "$(find "$BASE/backup" -maxdepth 1 -type f -name 'system-before-*' | wc -l | tr -d ' ')" = 3 ] ||
    fail 'не сохранены три последние системные копии'
[ "$(find "$BASE/routes/backup" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')" = 3 ] ||
    fail 'не сохранены три последние копии маршрутов'
[ -e "$BASE/logs/broray.log" ] || fail 'активный журнал удалён'
[ ! -e "$BASE/logs/broray.log.1" ] || fail 'старый журнал не удалён'
[ ! -d "$BASE/run/global-operation.lock" ] || fail 'глобальная блокировка не освобождена'

# A changed candidate invalidates the short-lived confirmation.
reset_tree
printf old >"$BASE/tmp/broray-change.part"
make_old "$BASE/tmp/broray-change.part"
load_module
plan="$(broray_cleanup_plan_create true false false false)" || fail 'не создан план изменения'
token="$(printf '%s' "$plan" | jq -r '.token')"
printf changed >>"$BASE/tmp/broray-change.part"
if broray_cleanup_execute "$token" >/dev/null 2>&1; then
    fail 'изменённый план был принят'
fi
[ "$BRORAY_CLEANUP_ERROR_CODE" = CLEANUP_PLAN_CHANGED ] || fail 'неверная ошибка изменённого плана'
[ -e "$BASE/tmp/broray-change.part" ] || fail 'файл удалён после изменения плана'

# An expired confirmation is rejected without deleting anything.
reset_tree
printf old >"$BASE/tmp/broray-expired.part"
make_old "$BASE/tmp/broray-expired.part"
load_module
plan="$(broray_cleanup_plan_create true false false false)" || fail 'не создан просроченный план'
token="$(printf '%s' "$plan" | jq -r '.token')"
plan_file="$BRORAY_CLEANUP_PLAN_DIR/$token.json"
jq '.expiresEpoch = 0' "$plan_file" >"$plan_file.new" && mv "$plan_file.new" "$plan_file"
if broray_cleanup_execute "$token" >/dev/null 2>&1; then
    fail 'просроченный план был принят'
fi
[ "$BRORAY_CLEANUP_ERROR_CODE" = CLEANUP_PLAN_EXPIRED ] || fail 'неверная ошибка просроченного плана'
[ -e "$BASE/tmp/broray-expired.part" ] || fail 'файл удалён по просроченному плану'

# An active operation blocks both planning and deletion.
reset_tree
printf old >"$BASE/tmp/broray-busy.part"
make_old "$BASE/tmp/broray-busy.part"
load_module
mkdir -p "$BRORAY_CLEANUP_GLOBAL_LOCK"
printf '%s\n' "$$" >"$BRORAY_CLEANUP_GLOBAL_LOCK/pid"
printf '%s\n' routes >"$BRORAY_CLEANUP_GLOBAL_LOCK/scope"
printf '%s\n' export >"$BRORAY_CLEANUP_GLOBAL_LOCK/action"
if broray_cleanup_plan_create true false false false >/dev/null 2>&1; then
    fail 'план создан при активной операции'
fi
[ "$BRORAY_CLEANUP_ERROR_CODE" = CLEANUP_OPERATION_BUSY ] || fail 'неверная ошибка блокировки'
rm -rf "$BRORAY_CLEANUP_GLOBAL_LOCK"

# Empty selection is rejected.
if broray_cleanup_plan_create false false false false >/dev/null 2>&1; then
    fail 'пустой набор категорий принят'
fi
[ "$BRORAY_CLEANUP_ERROR_CODE" = CLEANUP_NOTHING_SELECTED ] || fail 'неверная ошибка пустого выбора'

printf '%s\n' 'PASS: safe cleanup planning, preservation, expiry, recheck and global lock'
