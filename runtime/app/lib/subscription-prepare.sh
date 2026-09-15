#!/opt/bin/ash
# Bounded helper: reads private inputs and writes only its private preparation.
# Persistent server activation must stay in the owning process after drain.
set -u
umask 077
[ "${BRORAY_OPS_SUPERVISED:-}" = ptrace/1 ] || exit 73
[ "$#" = 3 ] || exit 64
PREP_MODE="$1"; PREP_DIR="$2"; PREP_INPUT="$3"
case "$PREP_MODE" in fetch|parse) ;; *) exit 64 ;; esac
[ -d "$PREP_DIR" ] && [ ! -L "$PREP_DIR" ] || exit 74
[ "$(cat "$PREP_DIR/operation-id")" = "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || exit 73
. "${BRORAY_ROOT:-/opt/broray}/lib/subscription-service.sh"
BRORAY_SUB_TMP="$PREP_DIR"
prep_rc=0
case "$PREP_MODE" in
  fetch)
    [ -f "$PREP_INPUT" ] && [ ! -L "$PREP_INPUT" ] || exit 74
    prep_url="$(jq -er '.url' "$PREP_INPUT")" || exit 74
    prep_hwid="$(jq -er '.clientHwid' "$PREP_INPUT")" || exit 74
    broray_subscription_fetch "$prep_url" "$PREP_DIR/download" "$prep_hwid" || prep_rc=$?
    ;;
  parse)
    broray_subscription_extract_nodes "$PREP_DIR/download" "$PREP_DIR/nodes" || prep_rc=$?
    if [ "$prep_rc" = 0 ]; then
      prep_id="$(jq -er '.id' "$PREP_INPUT")" || exit 74
      prep_enabled="$(jq -er '.enabled|tostring' "$PREP_INPUT")" || exit 74
      broray_subscription_stage_nodes "$prep_id" "$PREP_DIR/nodes" "$PREP_DIR/stage" "$prep_enabled" || prep_rc=$?
    fi
    if [ -f "${BRORAY_SUB_WARNINGS_FILE:-}" ]; then
      cp "$BRORAY_SUB_WARNINGS_FILE" "$PREP_DIR/warnings.txt" || exit 74
    fi
    ;;
esac
jq -nc --argjson rc "$prep_rc" --arg code "${BRORAY_SUB_ERROR_CODE:-}" --arg message "${BRORAY_SUB_ERROR_MESSAGE:-}" \
  --argjson received "${BRORAY_SUB_RECEIVED:-0}" --argjson parsed "${BRORAY_SUB_PARSED:-0}" \
  --argjson accepted "${BRORAY_SUB_ACCEPTED:-0}" --argjson rejected "${BRORAY_SUB_REJECTED:-0}" \
  '{ok:($rc==0),errorCode:$code,errorMessage:$message,received:$received,parsed:$parsed,accepted:$accepted,rejected:$rejected}' \
  >"$PREP_DIR/$PREP_MODE-result.json" || exit 74
exit "$prep_rc"
