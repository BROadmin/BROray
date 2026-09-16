#!/opt/bin/ash
# One supervised mutation tree. No persistent service is started here.
[ "${BRORAY_OPS_SUPERVISED:-}" = ptrace/1 ] || exit 73
app="${BRORAY_ROOT:-/opt/broray}"
script="${BRORAY_RECONCILE_INTERFACE:-$app/lib/interface.sh}"
ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
export BRORAY_BASE="$app"
if "$ash" "$script" check && "$ash" "$script" sync-name && "$ash" "$script" check; then exit 0; fi
if "$ash" "$script" repair && "$ash" "$script" check; then exit 0; fi
# A failed command may still have completed or rolled back. Release only when
# both configurations again match the durable ownership receipt.
"$ash" "$script" ownership-check && exit 10
exit 75
