#!/bin/sh
set -eu
umask 077
PATH="${BRORAY_INSTALLER_PATH:-/opt/broray/bin:/opt/sbin:/opt/bin:/opt/usr/bin:/opt/usr/sbin:/usr/sbin:/usr/bin:/sbin:/bin}"
LC_ALL=C
export PATH LC_ALL
TARGET_CANDIDATE=''
TARGET_RELEASE=''
TARGET_PACKAGE=''
TARGET_WEBUI=''
TARGET_RELEASE_INDEX_URL="${BRORAY_INSTALLER_RELEASE_INDEX_URL:-https://api.brovibe.cloud/releases/stable/broray/3.2.0-r02/release.json}"
RELEASE_PUBLIC_KEY="${BRORAY_INSTALLER_RELEASE_PUBLIC_KEY:-RWTlNQ0uUR+MmbfELjB7v4VVML2xgK4Ri1ZmR8ZqDqomMQ0GJPMpYO/O}"
MINISIGN_ARCHIVE_URL='https://github.com/jedisct1/minisign/releases/download/0.12/minisign-0.12-linux.tar.gz'
MINISIGN_ARCHIVE_SHA256='9a599b48ba6eb7b1e80f12f36b94ceca7c00b7a5173c95c3efc88d9822957e73'
MINISIGN_ARCHIVE_BYTES='271043'
MINISIGN_AARCH64_SHA256='cec9f88be8c975af76854a53b4d49c3d257feae38d916edb0d16fb55aacd3000'
MINISIGN_AARCH64_BYTES='195288'
UPD='/opt/bin/broray-updaterctl'
SYS='/opt/broray/bin/broray-system'
INIT='/opt/etc/init.d/S22broray-updater'
BB=''
MINISIGN_ARCHIVE=''
MINISIGN_ROOT=''
MINISIGN_BIN=''
SIGNED_INDEX=''
SIGNED_INDEX_SIGNATURE=''
INSTALLED_RELEASE=''
CLEAN_BOOTSTRAP_URL=''
CLEAN_BOOTSTRAP_SHA256=''
CLEAN_BOOTSTRAP_BYTES=''
CLEAN_BOOTSTRAP_RELEASE=''
CLEAN_BOOTSTRAP_PACKAGE=''
CLEAN_BOOTSTRAP=''
CLEAN_INSTALL_PERFORMED=false
APP_STAGE=''
APP_SLOT=''
APP_SHA=''
PREFLIGHT_HELPER=''
RUNTIME_SHA=''
PLATFORM_MANIFEST_SHA=''
INSTALL_ROOT="${BRORAY_INSTALLER_TEST_ROOT:-}"
TMP_ROOT="${BRORAY_INSTALLER_TMP_ROOT:-/tmp}"

fail(){ echo "BROray universal installer ERROR: $*" >&2; exit 1; }
cleanup(){ rc=$?; if [ -n "$APP_STAGE" ]; then case "$APP_STAGE" in /tmp/broray-public-preflight.*) [ ! -L "$APP_STAGE" ] && [ "$(readlink -f "$APP_STAGE")" = "$APP_STAGE" ] && rm -rf "$APP_STAGE" ;; esac; fi; trap - EXIT HUP INT TERM; [ -z "$SIGNED_INDEX" ] || rm -f "$SIGNED_INDEX" 2>/dev/null || true; [ -z "$SIGNED_INDEX_SIGNATURE" ] || rm -f "$SIGNED_INDEX_SIGNATURE" 2>/dev/null || true; [ -z "$CLEAN_BOOTSTRAP" ] || rm -f "$CLEAN_BOOTSTRAP" 2>/dev/null || true; [ -z "$MINISIGN_ARCHIVE" ] || rm -f "$MINISIGN_ARCHIVE" 2>/dev/null || true; [ -z "$MINISIGN_ROOT" ] || rm -rf "$MINISIGN_ROOT" 2>/dev/null || true; exit "$rc"; }
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if [ -n "${BRORAY_INSTALLER_BUSYBOX:-}" ]; then BB="$BRORAY_INSTALLER_BUSYBOX"; fi
for c in /bin/busybox /usr/bin/busybox /opt/bin/busybox; do [ -n "$BB" ] || { [ -x "$c" ] && BB="$c"; }; done
[ -n "$BB" ] || { c="$(command -v busybox 2>/dev/null || true)"; [ -n "$c" ] && [ -x "$c" ] && BB="$c"; }
[ -n "$BB" ] || fail 'BusyBox executable not found'
for a in awk base64 chmod cmp cp find gzip mkdir mktemp mv rm rmdir sed sh sha256sum sleep sync tar tr wc; do "$BB" "$a" --help </dev/null >/dev/null 2>&1 || fail "required BusyBox applet unavailable: $a"; done
command -v opkg >/dev/null 2>&1 || fail 'opkg unavailable'

installer_path()
{
    case "$1" in /*) ;; *) return 1 ;; esac
    if [ -n "$INSTALL_ROOT" ]; then
        printf '%s%s\n' "${INSTALL_ROOT%/}" "$1"
    else
        printf '%s\n' "$1"
    fi
}

release_channel_current()
{
    channel="$(installer_path /opt/var/lib/broray-updater/release-index-url)" || return 1
    [ -f "$channel" ] && [ ! -L "$channel" ] || return 1
    [ "$("$BB" wc -l <"$channel" | "$BB" tr -d ' ')" = 1 ] || return 1
    [ "$("$BB" sed -n '1p' "$channel" 2>/dev/null || true)" = "$TARGET_RELEASE_INDEX_URL" ]
}

persist_release_channel()
{
    case "$TARGET_RELEASE_INDEX_URL" in
        https://*) ;;
        *) return 1 ;;
    esac
    [ "${#TARGET_RELEASE_INDEX_URL}" -le 512 ] || return 1
    case "$TARGET_RELEASE_INDEX_URL" in *[!A-Za-z0-9:/._-]*) return 1 ;; esac
    channel="$(installer_path /opt/var/lib/broray-updater/release-index-url)" || return 1
    parent="${channel%/*}"
    temporary="$parent/.release-index-url.$$"
    [ -d "$parent" ] && [ ! -L "$parent" ] || return 1
    if [ -e "$channel" ] || [ -L "$channel" ]; then
        [ -f "$channel" ] && [ ! -L "$channel" ] || return 1
    fi
    [ ! -e "$temporary" ] && [ ! -L "$temporary" ] || return 1
    printf '%s\n' "$TARGET_RELEASE_INDEX_URL" >"$temporary" || { "$BB" rm -f "$temporary"; return 1; }
    "$BB" chmod 0600 "$temporary" || { "$BB" rm -f "$temporary"; return 1; }
    "$BB" mv -f "$temporary" "$channel" || { "$BB" rm -f "$temporary"; return 1; }
    release_channel_current
}

legacy_opkg_feed_absent()
{
    feed="$(installer_path /opt/etc/opkg/broray.conf)" || return 1
    [ ! -e "$feed" ] && [ ! -L "$feed" ]
}

retire_legacy_opkg_feed()
{
    feed="$(installer_path /opt/etc/opkg/broray.conf)" || return 1
    legacy_opkg_feed_absent && return 0
    [ -f "$feed" ] && [ ! -L "$feed" ] || return 1
    bytes="$("$BB" wc -c <"$feed" | "$BB" tr -d ' ')" || return 1
    case "$bytes" in ''|*[!0-9]*) return 1 ;; esac
    [ "$bytes" -gt 0 ] && [ "$bytes" -le 512 ] || return 1
    [ "$("$BB" wc -l <"$feed" | "$BB" tr -d ' ')" = 1 ] || return 1
    line="$("$BB" sed -n '1p' "$feed" 2>/dev/null || true)"
    prefix='src/gz broray https://api.brovibe.cloud/releases/staging/broray/3.0.0-r14c'
    suffix='/opkg/aarch64-3.10'
    case "$line" in "$prefix"*"$suffix") ;; *) return 1 ;; esac
    candidate="${line#"$prefix"}"
    candidate="${candidate%"$suffix"}"
    case "$candidate" in ''|*[!0-9]*) return 1 ;; esac
    "$BB" rm -f "$feed" || return 1
    legacy_opkg_feed_absent
}

release_version_parts()
{
    value="$1"
    case "$value" in *-r*) ;; *) return 1 ;; esac
    version="${value%%-r*}"
    revision="${value#*-r}"
    old_ifs="$IFS"
    IFS=.
    set -- $version
    IFS="$old_ifs"
    [ "$#" -eq 3 ] || return 1
    case "$1$2$3$revision" in ''|*[!0-9]*) return 1 ;; esac
    [ "$revision" -gt 0 ] || return 1
    printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$revision"
}

release_relation()
{
    current_parts="$(release_version_parts "$1")" || return 1
    available_parts="$(release_version_parts "$2")" || return 1
    old_ifs="$IFS"
    IFS="$(printf '\t')"
    read -r c1 c2 c3 c4 <<EOF_CURRENT
$current_parts
EOF_CURRENT
    read -r a1 a2 a3 a4 <<EOF_AVAILABLE
$available_parts
EOF_AVAILABLE
    IFS="$old_ifs"
    for pair in "$a1:$c1" "$a2:$c2" "$a3:$c3" "$a4:$c4"
    do
        available_part="${pair%%:*}"
        current_part="${pair#*:}"
        [ "$available_part" -le "$current_part" ] || { printf '%s\n' newer; return 0; }
        [ "$available_part" -ge "$current_part" ] || { printf '%s\n' older; return 0; }
    done
    printf '%s\n' same
}

installed_release_version()
{
    for manifest_relative in /opt/broray/current/release.json /opt/broray/share/release/manifest.json
    do
        manifest="$(installer_path "$manifest_relative")" || return 1
        [ -f "$manifest" ] && [ ! -L "$manifest" ] || continue
        bytes="$("$BB" wc -c <"$manifest" | "$BB" tr -d ' ')" || return 1
        case "$bytes" in ''|*[!0-9]*) return 1 ;; esac
        [ "$bytes" -gt 1 ] && [ "$bytes" -le 262144 ] || return 1
        release="$(jq -ser 'select(length == 1 and (.[0] | type == "object") and (.[0].releaseId | type == "string")) | .[0].releaseId' "$manifest")" || return 1
        release_version_parts "$release" >/dev/null || return 1
        printf '%s\n' "$release"
        return 0
    done
    return 1
}

installer_fetch()
{
    url="$1"
    output="$2"
    maximum="$3"
    if [ -n "${BRORAY_INSTALLER_FETCH_HOOK:-}" ]; then
        "$BRORAY_INSTALLER_FETCH_HOOK" "$url" "$output" "$maximum"
    else
        curl -fL --retry 3 --retry-delay 1 --connect-timeout 15 --max-time 180 --max-filesize "$maximum" \
            -H 'Accept-Encoding: identity' -H 'Cache-Control: no-cache, no-store, max-age=0' \
            "$url" -o "$output"
    fi
}

bootstrap_minisign()
{
    if [ -n "${BRORAY_INSTALLER_MINISIGN:-}" ]; then
        MINISIGN_BIN="$BRORAY_INSTALLER_MINISIGN"
        expected="${BRORAY_INSTALLER_MINISIGN_SHA256:-$MINISIGN_AARCH64_SHA256}"
        [ -f "$MINISIGN_BIN" ] && [ ! -L "$MINISIGN_BIN" ] && [ -x "$MINISIGN_BIN" ] || fail 'provided signature verifier is unsafe'
        [ "$("$BB" sha256sum "$MINISIGN_BIN" | "$BB" awk 'NR==1{print $1;exit}')" = "$expected" ] || fail 'provided signature verifier SHA-256 mismatch'
        return 0
    fi
    command -v curl >/dev/null 2>&1 || fail 'curl unavailable for signature verifier bootstrap'
    [ -d "$TMP_ROOT" ] && [ ! -L "$TMP_ROOT" ] && [ -w "$TMP_ROOT" ] || fail 'temporary directory unavailable for signature verifier bootstrap'
    MINISIGN_ARCHIVE="$("$BB" mktemp "$TMP_ROOT/broray-r0051-minisign.XXXXXX")" || fail 'cannot create signature verifier archive'
    MINISIGN_ROOT="$("$BB" mktemp -d "$TMP_ROOT/broray-r0051-minisign-root.XXXXXX")" || fail 'cannot create signature verifier directory'
    installer_fetch "$MINISIGN_ARCHIVE_URL" "$MINISIGN_ARCHIVE" "$MINISIGN_ARCHIVE_BYTES" || fail 'signature verifier download failed'
    [ "$("$BB" wc -c <"$MINISIGN_ARCHIVE" | "$BB" tr -d ' ')" = "$MINISIGN_ARCHIVE_BYTES" ] || fail 'signature verifier archive size mismatch'
    [ "$("$BB" sha256sum "$MINISIGN_ARCHIVE" | "$BB" awk 'NR==1{print $1;exit}')" = "$MINISIGN_ARCHIVE_SHA256" ] || fail 'signature verifier archive SHA-256 mismatch'
    "$BB" tar -xzf "$MINISIGN_ARCHIVE" -C "$MINISIGN_ROOT" minisign-linux/aarch64/minisign || fail 'signature verifier extraction failed'
    MINISIGN_BIN="$MINISIGN_ROOT/minisign-linux/aarch64/minisign"
    [ -f "$MINISIGN_BIN" ] && [ ! -L "$MINISIGN_BIN" ] || fail 'signature verifier payload missing'
    [ "$("$BB" wc -c <"$MINISIGN_BIN" | "$BB" tr -d ' ')" = "$MINISIGN_AARCH64_BYTES" ] || fail 'signature verifier size mismatch'
    [ "$("$BB" sha256sum "$MINISIGN_BIN" | "$BB" awk 'NR==1{print $1;exit}')" = "$MINISIGN_AARCH64_SHA256" ] || fail 'signature verifier SHA-256 mismatch'
    "$BB" chmod 0700 "$MINISIGN_BIN" || fail 'cannot enable signature verifier'
}

load_signed_release()
{
    [ -n "${BRORAY_INSTALLER_FETCH_HOOK:-}" ] || command -v curl >/dev/null 2>&1 || fail 'curl unavailable'
    command -v jq >/dev/null 2>&1 || fail 'jq unavailable'
    bootstrap_minisign
    SIGNED_INDEX="$("$BB" mktemp "$TMP_ROOT/broray-r0051-release.XXXXXX")" || fail 'cannot create release index file'
    SIGNED_INDEX_SIGNATURE="$("$BB" mktemp "$TMP_ROOT/broray-r0051-release-signature.XXXXXX")" || fail 'cannot create release signature file'
    signature_url="${BRORAY_INSTALLER_RELEASE_SIGNATURE_URL:-${TARGET_RELEASE_INDEX_URL}.minisig}"
    case "$TARGET_RELEASE_INDEX_URL:$signature_url" in https://*:https://*) ;; *) fail 'release index URLs must use HTTPS' ;; esac
    installer_fetch "$TARGET_RELEASE_INDEX_URL" "$SIGNED_INDEX" 262144 || fail 'release index download failed'
    installer_fetch "$signature_url" "$SIGNED_INDEX_SIGNATURE" 4096 || fail 'release signature download failed'
    "$MINISIGN_BIN" -Vm "$SIGNED_INDEX" -x "$SIGNED_INDEX_SIGNATURE" -P "$RELEASE_PUBLIC_KEY" -q >/dev/null 2>&1 || fail 'release index signature invalid'
    jq -se '
      length == 1 and (.[0] | type == "object") and
      .[0].schemaVersion == 1 and .[0].lifecycleContract == "compact-app-rename/1" and
      (.[0].candidate | type == "object") and
      (.[0].candidate.candidateId | type == "string") and
      (.[0].candidate.releaseId | type == "string") and
      (.[0].candidate.packageVersion | type == "string") and
      (.[0].candidate.webUIBuild | type == "string") and
      .[0].candidate.architecture == "aarch64-3.10" and
      (.[0].candidate.bundle.sha256 | type == "string") and
      (.[0].candidate.bundle.sizeBytes | type == "number") and
      (.[0].candidate.bundle.url | type == "string") and
      (.[0].platform.bundle.sha256 | type == "string") and
      (.[0].platform.bundle.sizeBytes | type == "number") and
      (.[0].platform.bundle.url | type == "string") and
      (.[0].platform.updaterSha256 | type == "string") and
      (.[0].cleanBootstrap | type == "object") and
      .[0].cleanBootstrap.kind == "broray-direct-clean-bootstrap/1" and
      (.[0].cleanBootstrap.url | type == "string") and
      (.[0].cleanBootstrap.sha256 | type == "string") and
      (.[0].cleanBootstrap.sizeBytes | type == "number") and
      (.[0].cleanBootstrap.releaseId | type == "string") and
      (.[0].cleanBootstrap.candidateId | type == "string") and
      (.[0].cleanBootstrap.packageVersion | type == "string")
    ' "$SIGNED_INDEX" >/dev/null || fail 'signed release index structure invalid'
    TARGET_CANDIDATE="$(jq -r '.candidate.candidateId' "$SIGNED_INDEX")"
    TARGET_RELEASE="$(jq -r '.candidate.releaseId' "$SIGNED_INDEX")"
    TARGET_PACKAGE="$(jq -r '.candidate.packageVersion' "$SIGNED_INDEX")"
    TARGET_WEBUI="$(jq -r '.candidate.webUIBuild' "$SIGNED_INDEX")"
    PLATFORM_URL="$(jq -r '.platform.bundle.url' "$SIGNED_INDEX")"
    PLATFORM_SHA256="$(jq -r '.platform.bundle.sha256' "$SIGNED_INDEX")"
    PLATFORM_BYTES="$(jq -r '.platform.bundle.sizeBytes' "$SIGNED_INDEX")"
    TARGET_UPDATER_SHA256="$(jq -r '.platform.updaterSha256' "$SIGNED_INDEX")"
    CLEAN_BOOTSTRAP_URL="$(jq -r '.cleanBootstrap.url' "$SIGNED_INDEX")"
    CLEAN_BOOTSTRAP_SHA256="$(jq -r '.cleanBootstrap.sha256' "$SIGNED_INDEX")"
    CLEAN_BOOTSTRAP_BYTES="$(jq -r '.cleanBootstrap.sizeBytes' "$SIGNED_INDEX")"
    CLEAN_BOOTSTRAP_RELEASE="$(jq -r '.cleanBootstrap.releaseId' "$SIGNED_INDEX")"
    CLEAN_BOOTSTRAP_CANDIDATE="$(jq -r '.cleanBootstrap.candidateId' "$SIGNED_INDEX")"
    CLEAN_BOOTSTRAP_PACKAGE="$(jq -r '.cleanBootstrap.packageVersion' "$SIGNED_INDEX")"
    release_version_parts "$TARGET_RELEASE" >/dev/null || fail 'signed target release version invalid'
    case "$TARGET_CANDIDATE:$TARGET_PACKAGE:$TARGET_WEBUI:$PLATFORM_SHA256:$TARGET_UPDATER_SHA256" in *[!A-Za-z0-9._:-]*) fail 'signed release identity contains unsafe characters' ;; esac
    case "$PLATFORM_URL:$CLEAN_BOOTSTRAP_URL" in https://*:https://*) ;; *) fail 'signed platform and clean bootstrap URLs must use HTTPS' ;; esac
    case "$CLEAN_BOOTSTRAP_SHA256" in *[!0-9a-f]*|'') fail 'signed clean bootstrap SHA-256 is invalid' ;; esac
    [ "${#CLEAN_BOOTSTRAP_SHA256}" -eq 64 ] || fail 'signed clean bootstrap SHA-256 length is invalid'
    case "$CLEAN_BOOTSTRAP_BYTES" in ''|*[!0-9]*) fail 'signed clean bootstrap size is invalid' ;; esac
    [ "$CLEAN_BOOTSTRAP_BYTES" -gt 0 ] && [ "$CLEAN_BOOTSTRAP_BYTES" -le 262144 ] || fail 'signed clean bootstrap size is outside bounds'
    [ "$CLEAN_BOOTSTRAP_RELEASE" = "$TARGET_RELEASE" ] && [ "$CLEAN_BOOTSTRAP_CANDIDATE" = "$TARGET_CANDIDATE" ] && [ "$CLEAN_BOOTSTRAP_PACKAGE" = "$TARGET_PACKAGE" ] || fail 'signed direct clean bootstrap target differs'
}

installed_registration()
{
    status="$(opkg status broray 2>/dev/null || true)"
    [ -n "$status" ] || return 1
    printf '%s\n' "$status" | "$BB" awk -F ': ' '
      $1=="Package"{p++;pv=$2}
      $1=="Status"{s++;sv=$2}
      END{exit !(p==1&&pv=="broray"&&s==1&&sv=="install user installed")}
    '
}

compact_updater_available()
{
    installed_registration || return 1
    [ -x "$UPD" ] && [ ! -L "$UPD" ] || return 1
    [ -x "$SYS" ] && [ ! -L "$SYS" ] || return 1
    [ -x "$INIT" ] && [ ! -L "$INIT" ] || return 1
    [ -d /opt/broray/current ] && [ ! -L /opt/broray/current ] || return 1
    [ "$("$UPD" version 2>/dev/null || true)" = 'broray-updater/5' ]
}


clean_install_boundary()
{
    status="$(opkg status broray 2>/dev/null || true)"
    [ -z "$status" ] || return 1
    for relative in \
        /opt/broray \
        /opt/bin/broray-updaterctl \
        /opt/libexec/broray-updater \
        /opt/var/lib/broray \
        /opt/var/lib/broray-updater \
        /opt/var/lock/broray \
        /opt/etc/init.d/S22broray-updater \
        /opt/etc/init.d/S23broray-monitor \
        /opt/etc/init.d/S24broray \
        /opt/etc/init.d/S25broray-web \
        /opt/etc/init.d/S27broray-auto-switch \
        /opt/etc/init.d/S28broray-subscriptions \
        /opt/etc/opkg/broray.conf
    do
        object="$(installer_path "$relative")" || return 1
        [ ! -e "$object" ] && [ ! -L "$object" ] || return 1
    done
    info_root="$(installer_path /opt/lib/opkg/info)" || return 1
    if [ -e "$info_root" ] || [ -L "$info_root" ]; then
        [ -d "$info_root" ] && [ ! -L "$info_root" ] || return 1
        for object in "$info_root"/broray.*
        do
            [ ! -e "$object" ] && [ ! -L "$object" ] || return 1
        done
    fi
    return 0
}

run_clean_bootstrap()
{
    clean_install_boundary || fail 'BROray is partially present or unhealthy; clean bootstrap refused without changes'
    [ -d "$TMP_ROOT" ] && [ ! -L "$TMP_ROOT" ] && [ -w "$TMP_ROOT" ] || fail 'temporary directory unavailable for clean bootstrap'
    CLEAN_BOOTSTRAP="$("$BB" mktemp "$TMP_ROOT/broray-r0082-clean.XXXXXX")" || fail 'cannot create clean bootstrap file'
    installer_fetch "$CLEAN_BOOTSTRAP_URL" "$CLEAN_BOOTSTRAP" "$CLEAN_BOOTSTRAP_BYTES" || fail 'clean bootstrap download failed'
    [ "$("$BB" wc -c <"$CLEAN_BOOTSTRAP" | "$BB" tr -d ' ')" = "$CLEAN_BOOTSTRAP_BYTES" ] || fail 'clean bootstrap size mismatch'
    [ "$("$BB" sha256sum "$CLEAN_BOOTSTRAP" | "$BB" awk 'NR==1{print $1;exit}')" = "$CLEAN_BOOTSTRAP_SHA256" ] || fail 'clean bootstrap SHA-256 mismatch'
    "$BB" sh -n "$CLEAN_BOOTSTRAP" >/dev/null 2>&1 || fail 'clean bootstrap shell syntax invalid'
    "$BB" chmod 0700 "$CLEAN_BOOTSTRAP" || fail 'cannot enable clean bootstrap'
    if [ -n "${BRORAY_INSTALLER_CLEAN_BOOTSTRAP_HOOK:-}" ]; then
        "$BRORAY_INSTALLER_CLEAN_BOOTSTRAP_HOOK" "$CLEAN_BOOTSTRAP" || fail 'clean bootstrap hook failed'
    else
        "$BB" sh "$CLEAN_BOOTSTRAP" || fail 'clean package bootstrap failed'
    fi
    installed_registration || fail 'clean bootstrap completed without an installed BROray registration'
    bootstrapped_release="$(installed_release_version)" || fail 'clean bootstrap release identity is unavailable'
    [ "$bootstrapped_release" = "$CLEAN_BOOTSTRAP_RELEASE" ] || fail 'clean bootstrap release identity differs'
    echo 'CLEAN_BOOTSTRAP=PASS'
    echo "CLEAN_BOOTSTRAP_RELEASE=$bootstrapped_release"
}

# Public transport only; authenticated coordinator owns lifecycle and recovery.
prepare_exact_target()
{
    [ "$TARGET_CANDIDATE" = '3.2.0-r02c01' ] || fail 'download the installer for the selected release'
    [ "$(readlink -f /tmp)" = /tmp ] && [ ! -L /tmp ] || fail 'unsafe RAM workspace'
    awk '$2=="/tmp" && ($3=="tmpfs" || $3=="ramfs"){ok=1} END{exit !ok}' /proc/mounts || fail '/tmp must be RAM-backed'
    APP_STAGE="$("$BB" mktemp -d /tmp/broray-public-preflight.XXXXXXXX)" || fail 'cannot create RAM workspace'
    APP_SLOT="$APP_STAGE/slot"
    mkdir -m 700 "$APP_SLOT" || fail 'cannot create target slot'
    APP_SHA="$(jq -r '.candidate.bundle.sha256' "$SIGNED_INDEX")"
    app_bytes="$(jq -r '.candidate.bundle.sizeBytes' "$SIGNED_INDEX")"
    app_url="$(jq -r '.candidate.bundle.url' "$SIGNED_INDEX")"
    [ "$APP_SHA" = '6690867def03a59386609a2a9ccd2174ef21cc3eccb5daeb5e44a81dacd70647' ] && [ "$app_bytes" = 1597811 ] || fail 'accepted r02c01 archive identity differs'
    installer_fetch "$app_url" "$APP_STAGE/app.tar.gz" "$app_bytes" || fail 'application download failed'
    [ "$(wc -c <"$APP_STAGE/app.tar.gz" | tr -d ' ')" = "$app_bytes" ] && [ "$(sha256sum "$APP_STAGE/app.tar.gz" | awk '{print $1}')" = "$APP_SHA" ] || fail 'application bytes differ'
    # This exact authenticated archive passed path/type/mode acceptance.
    /opt/bin/tar -xzf "$APP_STAGE/app.tar.gz" -C "$APP_SLOT" || fail 'application extraction failed'
    RUNTIME_SHA='88f96720f764941d0ebd167d235c8ea1fcc618a89590378e65d16e7733e14ca4'
    PLATFORM_MANIFEST_SHA='e1bb65e7740fbfe46cf1c458ee74200fb5cf46463e867e29c441a7d4536eaff9'
    PREFLIGHT_HELPER="$APP_STAGE/prepare-persistent-updater.sh"
    installer_fetch 'https://api.brovibe.cloud/releases/stable/broray/3.2.0-r02/prepare-persistent-updater.sh' "$PREFLIGHT_HELPER" 3002 || fail 'preflight helper download failed'
    [ "$(sha256sum "$PREFLIGHT_HELPER" | awk '{print $1}')" = '2896e85cfa355eab3c92e0ec02e76994f9de934c46b329eadd62c9cc2283fa9e' ] || fail 'preflight helper differs'
}

ensure_target_platform()
{
    rc=0
    /opt/bin/ash "$PREFLIGHT_HELPER" "$APP_SLOT" "$RUNTIME_SHA" "$PLATFORM_MANIFEST_SHA" >"$APP_STAGE/preflight.json" || rc=$?
    cat "$APP_STAGE/preflight.json"
    if [ "$rc" = 75 ] && jq -e '.errorCode=="UPDATER_LEGACY_REBOOT_REQUIRED"' "$APP_STAGE/preflight.json" >/dev/null; then
        printf '%s\n' 'REBOOT_REQUIRED=YES' 'Перезагрузите Keenetic через его интерфейс управления, затем повторите эту же команду установки.'
        exit 75
    fi
    [ "$rc" = 0 ] || fail 'protected updater preflight failed; evidence retained'
}

run_prepared_update()
{
    current="$(jq -r '.candidateId // empty' /opt/broray/current/release.json)" || fail 'installed candidate unavailable'
    if [ "$current" = "$TARGET_CANDIDATE" ]; then
        printf '%s\n' 'UPDATE_DECISION=already-current'
        return 0
    fi
    relation="$(release_relation "$INSTALLED_RELEASE" "$TARGET_RELEASE")" || fail 'release comparison failed'
    case "$relation" in newer) action=update ;; same) action=reinstall ;; *) fail 'downgrade refused' ;; esac
    REQUEST="$(BRORAY_RELEASE_INDEX_URL="$TARGET_RELEASE_INDEX_URL" "$UPD" request "$action" --prepared-target "$TARGET_CANDIDATE" "$APP_SHA")" || fail 'exact-target request rejected'
    printf '%s\n' "$REQUEST" | jq -e '.ok==true and .accepted==true and (.operationId|type)=="string" and (.operationId|length)>0' >/dev/null || fail 'invalid updater acceptance'
    oid="$(printf '%s\n' "$REQUEST" | jq -r .operationId)"
    printf 'OPERATION_ID=%s\n' "$oid"
    count=0
    while [ "$count" -lt 300 ]; do
        sleep 2
        STATUS="$("$UPD" status)" || fail 'status read failed; request is not replayed'
        printf '%s\n' "$STATUS" | jq -e --arg id "$oid" '.operationId==$id' >/dev/null || fail 'operation identity changed'
        state="$(printf '%s\n' "$STATUS" | jq -r .state)"
        printf 'UPDATE_STATE=%s\n' "$state"
        case "$state" in
            success) return 0 ;;
            error|recovery-required) printf '%s\n' "$STATUS" >&2; fail 'updater failed; evidence retained' ;;
            queued|running|rolling-back) ;;
            *) fail 'unexpected updater state' ;;
        esac
        count=$((count+1))
    done
    fail 'completion not confirmed; request is not replayed'
}

if [ "${BRORAY_INSTALLER_LIBRARY_ONLY:-0}" = 1 ]; then
    trap - EXIT HUP INT TERM
    return 0 2>/dev/null || exit 0
fi

load_signed_release
prepare_exact_target
if installed_registration; then
    INSTALLED_RELEASE="$(installed_release_version)" || fail 'installed release unavailable'
    [ "$(release_relation "$INSTALLED_RELEASE" "$TARGET_RELEASE")" != older ] || fail 'downgrade refused'
else
    clean_install_boundary || fail 'partial or foreign installation; refused without changes'
    run_clean_bootstrap
    CLEAN_INSTALL_PERFORMED=true
    INSTALLED_RELEASE="$(installed_release_version)" || fail 'installed release unavailable'
fi
ensure_target_platform
if ! compact_updater_available; then
    converter=/opt/libexec/broray-updater/broray-migrate-legacy.sh
    [ -f "$converter" ] && [ ! -L "$converter" ] && [ -x "$converter" ] || fail 'structural converter unavailable'
    BRORAY_UPDATER_INSTALLED_RELEASE="$INSTALLED_RELEASE" /opt/bin/ash "$converter" || fail 'structural conversion failed'
    compact_updater_available || fail 'converted registration not confirmed'
fi
run_prepared_update
actual="$(sha256sum /opt/broray/current/SHA256SUMS)" || fail 'installed manifest missing'
[ "${actual%% *}" = "$RUNTIME_SHA" ] || fail 'installed manifest differs'
(cd /opt/broray/current && sha256sum -c SHA256SUMS >/dev/null) || fail 'installed application bytes differ'
info="$("$SYS" info)" || fail 'installed health unavailable'
printf '%s\n' "$info" | jq -e --arg c "$TARGET_CANDIDATE" --arg w "$TARGET_WEBUI" '.ok==true and .candidateId==$c and .webUIBuild==$w and .installedWebUIBuild==$w and .installationHealthy==true and .versionsConsistent==true and .opkgRegistrationHealthy==true' >/dev/null || fail 'installed health contract failed'
TARGET_RELEASE_INDEX_URL='https://api.brovibe.cloud/releases/stable/broray/release.json'
persist_release_channel || fail 'cannot persist stable release channel'
retire_legacy_opkg_feed || fail 'legacy feed cannot be safely retired'
printf 'BRORAY_INSTALL=PASS\nCANDIDATE=%s\nRELEASE=%s\n' "$TARGET_CANDIDATE" "$TARGET_RELEASE"
