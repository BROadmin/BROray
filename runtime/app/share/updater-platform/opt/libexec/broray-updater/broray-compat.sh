#!/opt/bin/ash

# Compatibility adapter for the BROray information page.  Application
# identity comes from the active immutable release, while installed-package
# identity must come from the real OPKG control/status records.  Keeping those
# sources separate prevents a damaged/deinstall registration from being
# reported as healthy merely because the app slot still exists.

BRORAY_UPDATER_STATE="${BRORAY_UPDATER_STATE:-/opt/var/lib/broray-updater}"
BRORAY_UPDATER_CURRENT="${BRORAY_UPDATER_CURRENT:-/opt/broray/current}"
BRORAY_UPDATER_RELEASE_MANIFEST="${BRORAY_UPDATER_RELEASE_MANIFEST:-/opt/broray/share/release/manifest.json}"
BRORAY_OPKG_INFO_ROOT="${BRORAY_OPKG_INFO_ROOT:-/opt/lib/opkg/info}"
BRORAY_OPKG_CONTROL="${BRORAY_OPKG_CONTROL:-$BRORAY_OPKG_INFO_ROOT/broray.control}"

broray_opkg_control_value()
{
    local field
    field="$1"
    [ -f "$BRORAY_OPKG_CONTROL" ] && [ ! -L "$BRORAY_OPKG_CONTROL" ] || return 1
    awk -F ': ' -v key="$field" '$1 == key {print substr($0, length($1) + 3); found=1; exit} END {exit found ? 0 : 1}' \
        "$BRORAY_OPKG_CONTROL"
}

broray_opkg_registration_valid()
{
    local status control_version status_version hook
    command -v opkg >/dev/null 2>&1 || return 1
    [ -f "$BRORAY_OPKG_CONTROL" ] && [ ! -L "$BRORAY_OPKG_CONTROL" ] || return 1
    for hook in preinst postinst prerm postrm; do
        [ -f "$BRORAY_OPKG_INFO_ROOT/broray.$hook" ] &&
        [ ! -L "$BRORAY_OPKG_INFO_ROOT/broray.$hook" ] &&
        [ -x "$BRORAY_OPKG_INFO_ROOT/broray.$hook" ] || return 1
    done
    [ -f "$BRORAY_OPKG_INFO_ROOT/broray.list" ] &&
    [ ! -L "$BRORAY_OPKG_INFO_ROOT/broray.list" ] &&
    [ ! -s "$BRORAY_OPKG_INFO_ROOT/broray.list" ] || return 1
    awk -F ': ' '
      $1=="Package"{p++; pv=$2}
      $1=="Version"{v++; vv=$2}
      $1=="Architecture"{a++; av=$2}
      $1=="X-BROray-Package-Revision"{r++}
      $1=="X-BROray-Version"{xv++}
      END{exit !(p==1 && pv=="broray" && v==1 && vv!="" && a==1 && av=="aarch64-3.10" && r==1 && xv==1)}
    ' "$BRORAY_OPKG_CONTROL" || return 1
    control_version="$(broray_opkg_control_value Version)" || return 1
    status="$(opkg status broray 2>/dev/null)" || return 1
    printf '%s\n' "$status" | awk -F ': ' '
      $1=="Package"{p++; pv=$2}
      $1=="Version"{v++; vv=$2}
      $1=="Architecture"{a++; av=$2}
      $1=="Status"{s++; sv=$2}
      END{exit !(p==1 && pv=="broray" && v==1 && vv!="" && a==1 && av=="aarch64-3.10" && s==1 && sv=="install user installed")}
    ' || return 1
    status_version="$(printf '%s\n' "$status" | awk -F ': ' '$1=="Version"{print $2;exit}')"
    [ "$status_version" = "$control_version" ] || return 1
    [ "$(broray_opkg_control_value Package 2>/dev/null)" = broray ] || return 1
    [ "$(broray_opkg_control_value Architecture 2>/dev/null)" = aarch64-3.10 ]
}

broray_updater_manifest_value()
{
    key="$1"
    [ -s "$BRORAY_UPDATER_RELEASE_MANIFEST" ] || return 1
    jq -er --arg key "$key" '.[$key] // empty' "$BRORAY_UPDATER_RELEASE_MANIFEST" 2>/dev/null
}

broray_updater_current_slot()
{
    local slot
    [ -d "$BRORAY_UPDATER_CURRENT" ] && [ ! -L "$BRORAY_UPDATER_CURRENT" ] || return 1
    slot="$(sed -n '1p' "$BRORAY_UPDATER_CURRENT/.broray-slot" 2>/dev/null || true)"
    case "$slot" in ''|.*|-*|*[!A-Za-z0-9._-]*) return 1 ;; esac
    printf '%s\n' "$slot"
}

broray_updater_slot_metadata()
{
    slot="$(broray_updater_current_slot)" || return 1
    metadata="$BRORAY_UPDATER_STATE/slots/$slot.json"
    [ -s "$metadata" ] && jq -e 'type == "object"' "$metadata" >/dev/null 2>&1 || return 1
    cat "$metadata"
}

broray_system_installed_package_version()
{
    broray_opkg_control_value Version
}

broray_system_installed_package_revision()
{
    broray_opkg_control_value X-BROray-Package-Revision
}

broray_system_installed_release_id()
{
    broray_updater_manifest_value releaseId
}

broray_system_installed_webui_build()
{
    broray_updater_manifest_value webUIBuild
}

broray_system_installed_app_version()
{
    broray_updater_manifest_value version
}

broray_system_installed_candidate_sha()
{
    broray_updater_slot_metadata | jq -er '.source.bundle.sha256 // empty' 2>/dev/null
}

broray_system_update_cache_read()
{
    cache="$BRORAY_UPDATER_STATE/release-index.json"
    [ -s "$cache" ] || return 1
    metadata="$(broray_updater_slot_metadata 2>/dev/null || printf '{}')"
    current_candidate="$(printf '%s\n' "$metadata" | jq -r '.candidateId // ""')"

    jq -ce \
        --arg currentCandidate "$current_candidate" '
        select(
          .schemaVersion == 1 and
          .lifecycleContract == "compact-app-rename/1" and
          (.candidate | type == "object") and
          ((.updateAvailable|type)=="boolean") and
          (.candidateRelation=="newer" or .candidateRelation=="same" or
           .candidateRelation=="older" or .candidateRelation=="uncomparable") and
          ((.currentCandidateAtCheck // "") == $currentCandidate)
        ) |
        {
          updateAvailable:.updateAvailable,
          candidateRelation:.candidateRelation,
          availableVersion:.candidate.appVersion,
          availablePackageVersion:.candidate.packageVersion,
          checkedAt:(.checkedAt // null),
          checkedEpoch:(.checkedEpoch // 0),
          candidate:.candidate
        }
        ' "$cache"
}
