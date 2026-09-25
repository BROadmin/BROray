#!/opt/bin/ash
# Isolated fixture on tmpfs. No installed services or native ownership mocked
# as physical acceptance: readiness is stubbed only for schedule branch tests.
set -eu
export PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin
here="$1"
case "$here" in /tmp/broray-ram-fix-tests-*) ;; *) exit 90 ;; esac
[ "$(readlink -f "$here")" = "$here" ]
case_name="$2"
export BRORAY_UPDATER_ROOT_PREFIX="$here/root" BRORAY_UPDATER_TEST_MODE=1
. "$here/updater-library.sh"
CURRENT_OPERATION_ID=update-fixture
CURRENT_OPERATION_DIR="$OPERATION_ROOT/$CURRENT_OPERATION_ID"
work="$WORK_ROOT/$CURRENT_OPERATION_ID"
mkdir -p "$CURRENT_OPERATION_DIR" "$work" "$SLOT_META_ROOT" "$RELEASES_ROOT" "$APP_ROOT/runtime" "${XRAY_WRAPPER%/*}" "$ROOT_PREFIX/opt/bin"
cp /opt/bin/busybox "$ASH_BIN"
cp "$here/xray-wrapper" "$XRAY_WRAPPER"
printf '#!/opt/bin/ash\nexit 0\n' >"$XRAY_RUNTIME"
chmod 755 "$ASH_BIN" "$XRAY_RUNTIME" "$XRAY_WRAPPER"
mkdir -p "${OPKG_CONTROL%/*}"
printf 'Package: broray\nVersion: 3.0.0-r14\nArchitecture: aarch64-3.10\n' >"$OPKG_CONTROL"
target="$(cat "$here/target.json")"

case "$case_name" in
 ram-stage|damaged-archive|copy-failure)
   tar() {
     case " $* " in *' -C '*) printf '%s\n' "$*" >>"$here/tar-targets" ;; esac
     command tar "$@"
   }
   if [ "$case_name" = copy-failure ]; then
     cp() { command cp "$here/xray-wrapper" "$RELEASES_ROOT/$(printf '%s' "$target" | jq -r .candidateId)--$CURRENT_OPERATION_ID/partial"; return 1; }
     rc=0
     stage_release "$here/archive.tar.gz" "$target" "$work" >"$here/stage.out" || rc=$?
     [ "$rc" != 0 ]
     operation_target_cleanup
     [ -z "$(ls -A "$RELEASES_ROOT")" ]
   elif [ "$case_name" = damaged-archive ]; then
     rc=0
     stage_release "$here/bad.tar.gz" "$target" "$work" >"$here/stage.out" || rc=$?
     [ "$rc" != 0 ]
     [ -z "$(ls -A "$RELEASES_ROOT")" ]
   else
     stage_release "$here/archive.tar.gz" "$target" "$work" >"$here/stage.out"
     final="$(cat "$here/stage.out")"
     [ "$final" = "$RELEASES_ROOT/$(printf '%s' "$target" | jq -r .candidateId)--update-fixture" ]
     [ "$(cat "$here/tar-targets")" = "-xzof $here/archive.tar.gz -C $work/app-tree" ]
     (cd "$final" && sha256sum -c SHA256SUMS >/dev/null)
   fi
   ;;
 reject-opt-work|reject-symlink-work|ram-work)
   mkdir -p "$ROOT_PREFIX/tmp"
   if [ "$case_name" = reject-opt-work ]; then WORK_ROOT="$ROOT_PREFIX/opt/tmp/work"; fi
   if [ "$case_name" = reject-symlink-work ]; then
     mkdir -p "$ROOT_PREFIX/opt/elsewhere"
     ln -s "$ROOT_PREFIX/opt/elsewhere" "$ROOT_PREFIX/tmp/escape"
     WORK_ROOT="$ROOT_PREFIX/tmp/escape/work"
   fi
   rc=0
   workspace_ram_valid || rc=$?
   if [ "$case_name" = ram-work ]; then [ "$rc" = 0 ]; else [ "$rc" != 0 ]; fi
   ;;
 shell-valid|shell-invalid)
   mkdir -p "$here/shell/app" "$here/shell/init"
   printf '#!/opt/bin/ash\necho ok\n' >"$here/shell/app/good"
   printf '%s' '#!/opt/bin/ash' >"$here/shell/app/no-newline"
   printf 'not a script\000\001\n' >"$here/shell/app/data"
   if [ "$case_name" = shell-invalid ]; then printf '#!/opt/bin/ash\nif\n' >"$here/shell/init/bad"; fi
   sed() { echo UNEXPECTED_SED >>"$here/unexpected-sed"; command sed "$@"; }
   rc=0; shell_tree_valid "$here/shell" || rc=$?
   [ ! -e "$here/unexpected-sed" ]
   if [ "$case_name" = shell-valid ]; then [ "$rc" = 0 ]; else [ "$rc" != 0 ]; fi
   ;;
 obsolete-ready|obsolete-unready|installing-unready|live-worker|ambiguous-worker|preflight-replay|failed-preflight)
   rm -f "$here/host-busy"
   export BRORAY_HANDOFF_ROOT_PREFIX="$here/handoff-root" BRORAY_HANDOFF_TEST_MODE=1 BRORAY_HANDOFF_ASH=/opt/bin/ash
   . "$here/handoff-library.sh"
   mkdir -p "$STATE_ROOT"
   printf 'preparing\n' >"$PHASE_FILE"
   [ "$case_name" != installing-unready ] || printf 'installing\n' >"$PHASE_FILE"
   printf '{"candidateId":"old"}\n' >"$REQUEST_FILE"
   original="$(sha256sum "$REQUEST_FILE")"
   legacy_root="$STATE_ROOT"
   mkdir -p "$BACKUP_ROOT" "$here/code/lib"
   printf 'retained backup\n' >"$BACKUP_ROOT/evidence"
   backup_original="$(sha256sum "$BACKUP_ROOT/evidence")"
   : >"$here/code/lib/operation-client.sh"
   # The physical stale request is invalid for the new slot. Only a proven
   # current platform can supersede the pre-install preparing branch.
   payload_valid() { return 0; }
   payload_manifest_sha() { printf '%064d\n' 1; }
   platform_current() { return 0; }
   preflight_installed() {
     # Readiness requires both arguments and a responsive independent host.
     # The host is occupied while it executes S25, so schedule cannot call it.
     [ "$#" = 2 ] && [ "$1" = "$(payload_manifest_sha)" ] && [ "$2" = "$here/code" ] || return 75
     [ ! -e "$here/host-busy" ] || return 75
     pf_old_manifest="$(payload_manifest_sha)"
     [ "$case_name" = obsolete-ready ] || [ "$case_name" = preflight-replay ]
   }
   worker_running() { [ "$case_name" = live-worker ]; }
   request_valid() { return 1; }
   current_candidate() { echo new; }
   process_starttime() { echo 12345; }
   preflight_paths_safe() { return 0; }
   preflight_resume() { [ "$case_name" != failed-preflight ] || return 75; return 0; }
   export BRORAY_OPS_CODE_ROOT="$here/code"
   PLATFORM_TARGET_FILES='lib/operation-client.sh'
   PAYLOAD_ROOT="$here/code"
   chmod 700 "$here/code/lib/operation-client.sh"
   [ "$case_name" != ambiguous-worker ] || mkdir "$LOCK_DIR"
   # The real preflight uses a different namespace. It must settle legacy
   # bookkeeping before the service host launches the new application's S25.
   STATE_ROOT="$ROOT_PREFIX/opt/var/lib/broray-updater-preflight"
   rc=0; preflight "$(payload_manifest_sha)" || rc=$?
   STATE_ROOT="$legacy_root"
   if [ "$case_name" = obsolete-ready ] || [ "$case_name" = preflight-replay ]; then
     [ "$rc" = 0 ] && [ "$(cat "$PHASE_FILE")" = complete ] || exit 92
     touch "$here/host-busy"
     schedule || exit 93
     if [ "$case_name" = preflight-replay ]; then
       preflight "$(payload_manifest_sha)" || exit 94
     fi
   fi
   [ "$(sha256sum "$REQUEST_FILE")" = "$original" ]
   [ "$(sha256sum "$BACKUP_ROOT/evidence")" = "$backup_original" ]
   case "$case_name" in
     obsolete-ready|preflight-replay) [ "$rc" = 0 ] && [ "$(cat "$PHASE_FILE")" = complete ] || exit 92 ;;
     *) [ "$rc" != 0 ] && [ "$(cat "$PHASE_FILE")" != complete ] || exit 92 ;;
   esac
   ;;
 *) exit 91 ;;
esac
echo "PASS $case_name"
