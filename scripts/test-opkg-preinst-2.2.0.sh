#!/usr/bin/env bash
set -euo pipefail

REPOSITORY_ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
PREINST="$REPOSITORY_ROOT/packaging/opkg/preinst"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/broray-preinst-2.2.0.XXXXXX")"
OPT="$WORK/opt"
TMP="$WORK/tmp"
LOG="$WORK/services.log"

cleanup() { rm -rf "$WORK"; }
fail() { printf 'PREINST TEST ERROR: %s\n' "$*" >&2; exit 1; }
trap cleanup EXIT HUP INT TERM

mkdir -p "$OPT/bin" "$OPT/etc/init.d" "$OPT/broray/config" "$OPT/broray/logs" "$OPT/broray/tmp" "$OPT/broray/routes/tmp" "$TMP"
ln -s "$(command -v tar)" "$OPT/bin/tar"

cat >"$WORK/service" <<'SERVICE'
#!/bin/sh
name="${0##*/}"
case "${1:-}" in
    status) exit 0 ;;
    stop) printf 'stop %s\n' "$name" >>"$SERVICE_LOG" ;;
    start) printf 'start %s\n' "$name" >>"$SERVICE_LOG" ;;
    *) exit 2 ;;
esac
SERVICE
chmod 755 "$WORK/service"
for name in S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions; do
    cp "$WORK/service" "$OPT/etc/init.d/$name"
done
export SERVICE_LOG="$LOG"

# Scenario 1: updater-prepared archive is accepted and no second archive is created.
mkdir -p "$TMP/broray-safe-upgrade-2.2.0-1-123" "$WORK/prepared/config"
printf 'prepared-user-data\n' >"$WORK/prepared/config/marker"
tar -czf "$TMP/broray-safe-upgrade-2.2.0-1-123/backup.tar.gz" -C "$WORK/prepared" .
printf '%s\n%s\n' \
    "$TMP/broray-safe-upgrade-2.2.0-1-123/backup.tar.gz" \
    "$(date '+%s')" >"$TMP/broray-opkg-prepared-update"
BRORAY_OPT_ROOT="$OPT" BRORAY_TMP_ROOT="$TMP" sh "$PREINST" >"$WORK/prepared.out"
grep -Fq 'Резервная копия подготовлена обновителем' "$WORK/prepared.out" || fail 'prepared archive was not accepted'
[ ! -e "$TMP/broray-opkg-prepared-update" ] || fail 'prepared marker was not removed'
[ ! -d "$OPT/broray/backups" ] || ! find "$OPT/broray/backups" -type f -print -quit | grep -q . || fail 'duplicate archive was created'

# Scenario 2: direct OPKG upgrade creates a safe archive with user data and without runtime junk.
: >"$LOG"
printf 'user-settings\n' >"$OPT/broray/config/user-settings"
printf 'runtime-log\n' >"$OPT/broray/logs/runtime.log"
printf 'runtime-temp\n' >"$OPT/broray/tmp/runtime.tmp"
printf 'route-temp\n' >"$OPT/broray/routes/tmp/runtime.tmp"
BRORAY_OPT_ROOT="$OPT" BRORAY_TMP_ROOT="$TMP" sh "$PREINST" >"$WORK/direct.out"
archive="$(find "$OPT/broray/backups" -type f -name 'opkg-before-2.2.0-1-*.tar.gz' | head -n1)"
[ -n "$archive" ] && [ -s "$archive" ] || fail 'direct archive was not created'
tar -tzf "$archive" >"$WORK/archive.list"
grep -Eq '^broray/config/user-settings$|^\./broray/config/user-settings$' "$WORK/archive.list" || fail 'user settings missing from archive'
! grep -Eq 'broray/(logs|tmp|routes/tmp)(/|$)' "$WORK/archive.list" || fail 'runtime data entered archive'

# Scenario 3: unsafe prepared path is rejected and previously running services are restarted.
: >"$LOG"
set +e
BRORAY_OPT_ROOT="$OPT" BRORAY_TMP_ROOT="$TMP" BRORAY_OPKG_BACKUP="$WORK/outside.tar.gz" sh "$PREINST" >"$WORK/reject.out" 2>&1
rc=$?
set -e
[ "$rc" -ne 0 ] || fail 'unsafe prepared path was accepted'
grep -Fq 'небезопасный путь' "$WORK/reject.out" || fail 'unsafe-path error is missing'
for name in S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions; do
    grep -Fq "start $name" "$LOG" || fail "$name was not restarted after rejection"
done

printf 'BROray 2.2.0 OPKG preinst safety test: PASS\n'
