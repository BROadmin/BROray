#!/opt/bin/ash

# Read-only Home snapshots. Expensive module checks write here out of band;
# the Home CGI never starts collectors and never owns a global operation lock.

BRORAY_HOME_ROOT="${BRORAY_HOME_ROOT:-${BRORAY_ROOT:-/opt/broray}}"
BRORAY_HOME_SNAPSHOT_DIR="${BRORAY_HOME_SNAPSHOT_DIR:-$BRORAY_HOME_ROOT/run/home-snapshots}"
BRORAY_HOME_SNAPSHOT_FRESH_SECONDS="${BRORAY_HOME_SNAPSHOT_FRESH_SECONDS:-90}"
BRORAY_HOME_SNAPSHOT_EXPIRE_SECONDS="${BRORAY_HOME_SNAPSHOT_EXPIRE_SECONDS:-600}"

case "$BRORAY_HOME_SNAPSHOT_FRESH_SECONDS" in
    ''|*[!0-9]*) BRORAY_HOME_SNAPSHOT_FRESH_SECONDS=90 ;;
esac
case "$BRORAY_HOME_SNAPSHOT_EXPIRE_SECONDS" in
    ''|*[!0-9]*) BRORAY_HOME_SNAPSHOT_EXPIRE_SECONDS=600 ;;
esac
[ "$BRORAY_HOME_SNAPSHOT_FRESH_SECONDS" -ge 15 ] || BRORAY_HOME_SNAPSHOT_FRESH_SECONDS=90
[ "$BRORAY_HOME_SNAPSHOT_EXPIRE_SECONDS" -gt "$BRORAY_HOME_SNAPSHOT_FRESH_SECONDS" ] ||
    BRORAY_HOME_SNAPSHOT_EXPIRE_SECONDS=600

broray_home_snapshot_module_valid()
{
    case "${1:-}" in
        xray|keenetic|servers|subscriptions|dns|routes|broray) return 0 ;;
    esac
    return 1
}

broray_home_snapshot_path()
{
    broray_home_snapshot_module_valid "${1:-}" || return 1
    printf '%s/%s.json\n' "$BRORAY_HOME_SNAPSHOT_DIR" "$1"
}

broray_home_snapshot_dir_safe()
{
    mkdir -p "$BRORAY_HOME_SNAPSHOT_DIR" || return 1
    [ -d "$BRORAY_HOME_SNAPSHOT_DIR" ] && [ ! -L "$BRORAY_HOME_SNAPSHOT_DIR" ]
}

broray_home_snapshot_write()
{
    snapshot_module="${1:-}"
    snapshot_payload="${2:-}"
    broray_home_snapshot_module_valid "$snapshot_module" || return 1
    [ -s "$snapshot_payload" ] && [ ! -L "$snapshot_payload" ] || return 1
    snapshot_data="$(jq -ce 'select(type == "object")' "$snapshot_payload" 2>/dev/null)" || return 1
    broray_home_snapshot_dir_safe || return 1

    snapshot_target="$(broray_home_snapshot_path "$snapshot_module")" || return 1
    [ ! -L "$snapshot_target" ] || return 1
    snapshot_temporary="$BRORAY_HOME_SNAPSHOT_DIR/.${snapshot_module}.new.$$"
    [ ! -e "$snapshot_temporary" ] && [ ! -L "$snapshot_temporary" ] || return 1
    snapshot_epoch="$(date '+%s')"
    case "$snapshot_epoch" in ''|*[!0-9]*) return 1 ;; esac
    snapshot_time="$(date -u '+%Y-%m-%dT%H:%M:%SZ')" || return 1

    jq -n \
        --arg module "$snapshot_module" \
        --arg capturedAt "$snapshot_time" \
        --argjson capturedEpoch "$snapshot_epoch" \
        --argjson data "$snapshot_data" '
          {
            schemaVersion:1,
            module:$module,
            capturedAt:$capturedAt,
            capturedEpoch:$capturedEpoch,
            data:$data
          }
        ' >"$snapshot_temporary" || {
            rm -f "$snapshot_temporary"
            return 1
        }
    jq -e \
        --arg module "$snapshot_module" '
          .schemaVersion == 1 and .module == $module and
          (.capturedAt | type) == "string" and
          (.capturedEpoch | type) == "number" and
          (.data | type) == "object"
        ' "$snapshot_temporary" >/dev/null 2>&1 || {
            rm -f "$snapshot_temporary"
            return 1
        }
    chmod 0600 "$snapshot_temporary" 2>/dev/null || true
    mv -f "$snapshot_temporary" "$snapshot_target" || {
        rm -f "$snapshot_temporary"
        return 1
    }
}

broray_home_snapshot_read()
{
    snapshot_module="${1:-}"
    broray_home_snapshot_module_valid "$snapshot_module" || return 1
    snapshot_target="$(broray_home_snapshot_path "$snapshot_module")" || return 1
    [ -s "$snapshot_target" ] && [ ! -L "$snapshot_target" ] || return 1
    snapshot_now="$(date '+%s')"
    case "$snapshot_now" in ''|*[!0-9]*) return 1 ;; esac

    jq -ce \
        --arg module "$snapshot_module" \
        --argjson now "$snapshot_now" \
        --argjson fresh "$BRORAY_HOME_SNAPSHOT_FRESH_SECONDS" \
        --argjson expire "$BRORAY_HOME_SNAPSHOT_EXPIRE_SECONDS" '
          select(
            .schemaVersion == 1 and .module == $module and
            (.capturedAt | type) == "string" and
            (.capturedEpoch | type) == "number" and
            (.data | type) == "object"
          ) |
          (if ($now - .capturedEpoch) < 0 then 0 else ($now - .capturedEpoch) end) as $age |
          (if $age <= $fresh then "fresh"
           elif $age <= $expire then "stale"
           else "expired" end) as $state |
          .data + {
            _snapshot:{
              schemaVersion:1,
              module:$module,
              capturedAt:.capturedAt,
              capturedEpoch:.capturedEpoch,
              ageSeconds:$age,
              freshness:$state
            }
          }
        ' "$snapshot_target" 2>/dev/null
}
