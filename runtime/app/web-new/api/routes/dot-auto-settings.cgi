#!/opt/bin/ash
set -u
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
. "$BRORAY_ROOT/web-new/api/routes/dot-common.sh"
. "$BRORAY_ROOT/lib/operation-job.sh"
. "$BRORAY_ROOT/lib/dot-auto.sh"
broray_api_require_method POST
broray_api_require_session
[ "${HTTP_X_BRORAY_REQUEST:-}" = operations ] || broray_api_error '403 Forbidden' CSRF_REJECTED 'Обновите страницу и повторите действие.'
[ -f "$BRORAY_ROOT/lib/operations-origin.sh" ] && [ ! -L "$BRORAY_ROOT/lib/operations-origin.sh" ] || broray_api_error '503 Service Unavailable' ORIGIN_VALIDATION_UNAVAILABLE 'Проверка источника запроса недоступна.'
. "$BRORAY_ROOT/lib/operations-origin.sh"
broray_operations_origin_allowed || broray_api_error '403 Forbidden' ORIGIN_REJECTED 'Источник запроса не подтверждён.'
request="$(mktemp "$BRORAY_ROOT/tmp/dot-auto-settings-XXXXXX")" || exit 74
output="$request.result"
trap 'saved_rc=$?; rm -f "$request" "$output"; broray_job_exit "$saved_rc"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
broray_dot_api_read_body "$request"
rc=0
broray_job_begin system dot:auto-settings dns-over-tls USER protected || rc=$?
[ "$rc" != 2 ] || broray_api_error '409 Conflict' ROUTES_OPERATION_BUSY 'Другая операция выполняется. Повторите сохранение позже.'
[ "$rc" = 0 ] || broray_api_error '500 Internal Server Error' DOT_AUTO_SETTINGS_FAILED 'Не удалось начать сохранение настройки.'
broray_dot_auto_save "$request" >"$output" || rc=$?
if [ "$rc" != 0 ]; then
 broray_job_finish failed || true
 [ "$rc" != 64 ] || broray_api_error '400 Bad Request' REQUEST_INVALID 'Поле enabled должно быть логическим.'
 broray_api_error '500 Internal Server Error' DOT_AUTO_SETTINGS_FAILED 'Настройка автопроверки не сохранена.'
fi
broray_job_finish completed || broray_api_error '500 Internal Server Error' DOT_AUTO_SETTINGS_FAILED 'Не удалось подтвердить завершение операции. Обновите состояние.'
broray_api_success "$(cat "$output")"
