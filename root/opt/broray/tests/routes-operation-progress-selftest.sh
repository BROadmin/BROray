#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
LIBRARY="$ROOT/lib/routes-operation-progress.sh"
WORK="${TMPDIR:-/tmp}/broray-routes-progress-selftest-$$"

cleanup()
{
    rm -rf "$WORK"
}

fail()
{
    echo "ОШИБКА self-test: $*" >&2
    exit 1
}

trap cleanup EXIT HUP INT TERM

[ -r "$LIBRARY" ] || fail "Модуль прогресса недоступен"
mkdir -p "$WORK"
BRORAY_ROUTES_PROGRESS_DIR="$WORK/operations"
export BRORAY_ROUTES_PROGRESS_DIR
. "$LIBRARY"

broray_routes_progress_begin telegram install 3 \
    "Подготовка установки." || fail "Не удалось начать операцию"

FILE="$BRORAY_ROUTES_PROGRESS_DIR/telegram.json"
[ -r "$FILE" ] || fail "Файл прогресса не создан"
jq -e '
    .bundleId == "telegram" and
    .operation == "install" and
    .phase == "preparing" and
    .current == 0 and .total == 3 and .percent == 0 and
    .running == true and .success == null
' "$FILE" >/dev/null || fail "Некорректное начальное состояние"

broray_routes_progress_update applying 1 3 \
    "Установка маршрутов в Keenetic." "198.51.100.0/24" ||
    fail "Не удалось обновить прогресс"

jq -e '
    .phase == "applying" and
    .current == 1 and .total == 3 and .percent == 33 and
    .currentRoute == "198.51.100.0/24" and .running == true
' "$FILE" >/dev/null || fail "Некорректное промежуточное состояние"

broray_routes_progress_complete "Установлено 3 из 3." ||
    fail "Не удалось завершить прогресс"

jq -e '
    .phase == "completed" and
    .current == 3 and .total == 3 and .percent == 100 and
    .running == false and .success == true and .completedAt != null
' "$FILE" >/dev/null || fail "Некорректное итоговое состояние"

broray_routes_progress_begin telegram delete 4 \
    "Подготовка удаления." || fail "Не удалось начать удаление"
broray_routes_progress_update applying 2 4 \
    "Удаление маршрутов из Keenetic." "203.0.113.0/24" ||
    fail "Не удалось обновить удаление"
broray_routes_progress_fail "Ошибка; изменения отменены." true ||
    fail "Не удалось записать откат"

jq -e '
    .operation == "delete" and
    .phase == "rolled_back" and
    .current == 2 and .total == 4 and .percent == 50 and
    .running == false and .success == false and .rolledBack == true
' "$FILE" >/dev/null || fail "Некорректное состояние отката"

broray_routes_progress_read telegram |
    jq -e '.bundleId == "telegram" and .running == false' >/dev/null ||
    fail "Чтение прогресса не прошло проверку"

broray_routes_progress_read whatsapp |
    jq -e '.bundleId == "whatsapp" and .phase == "idle"' >/dev/null ||
    fail "Не возвращено состояние ожидания"

broray_routes_progress_begin telegram install 5 \
    "Подготовка безопасной остановки." || fail "Не удалось начать тест остановки"
broray_routes_progress_tick 2 "192.0.2.0/24" || fail "Не удалось записать выполненную работу"
broray_routes_progress_request_stop telegram || fail "Не удалось запросить остановку"
broray_routes_progress_stop_requested telegram || fail "Запрос остановки не обнаружен"
broray_routes_progress_pause "Остановлено: 2 из 5." true "" || fail "Не удалось приостановить операцию"

jq -e '
    .schemaVersion == 2 and .phase == "paused" and
    .current == 2 and .total == 5 and .percent == 40 and
    .running == false and .success == null and
    .resumable == true and .stoppedByUser == true and
    .stopRequested == false
' "$FILE" >/dev/null || fail "Некорректное состояние безопасной остановки"

resume_values="$(broray_routes_progress_resume_values telegram install 3)"
[ "$(printf '%s' "$resume_values" | cut -f1)" = 2 ] || fail "Не восстановлен счётчик продолжения"
[ "$(printf '%s' "$resume_values" | cut -f2)" = 5 ] || fail "Не восстановлена исходная цель"
[ "$(printf '%s' "$resume_values" | cut -f3)" = true ] || fail "Продолжение не распознано"

broray_routes_progress_begin telegram install 5 "Продолжение: 2 из 5." 2 true ||
    fail "Не удалось продолжить операцию"
broray_routes_progress_tick 5 "198.51.100.0/24" || fail "Не удалось завершить продолженную работу"
broray_routes_progress_complete "Установлено 5 из 5." || fail "Не удалось завершить продолженную операцию"
jq -e '.phase == "completed" and .current == 5 and .total == 5 and .resumed == true and .resumable == false' \
    "$FILE" >/dev/null || fail "Некорректный результат продолжения"

broray_routes_progress_begin telegram delete 4 "Тест ошибки." || fail "Не удалось начать тест ошибки"
broray_routes_progress_tick 1 "203.0.113.0/24" || fail "Не удалось записать работу до ошибки"
broray_routes_progress_pause "Ошибка на следующем маршруте." false "203.0.114.0/24" ||
    fail "Не удалось сохранить возобновляемую ошибку"
jq -e '
    .phase == "failed_resumable" and .current == 1 and .total == 4 and
    .success == false and .resumable == true and .stoppedByUser == false and
    .errorRoute == "203.0.114.0/24"
' "$FILE" >/dev/null || fail "Некорректное состояние ошибки с продолжением"

echo "OK: progress routes self-test"
