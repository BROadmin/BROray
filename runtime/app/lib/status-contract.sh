#!/opt/bin/ash

# BROray 3.0.0 unified module status contract.
# The contract deliberately separates API availability, operational state,
# consistency, required user action, freshness and the last operation.

BRORAY_STATUS_SCHEMA_VERSION=1

broray_status_bool()
{
    case "${1:-false}" in
        true|1|yes|on) printf '%s\n' true ;;
        *) printf '%s\n' false ;;
    esac
}

broray_status_reason()
{
    reason_code="${1:-UNKNOWN}"
    reason_message="${2:-Состояние не определено.}"
    reason_details="${3:-}"

    jq -nc \
        --arg code "$reason_code" \
        --arg message "$reason_message" \
        --arg details "$reason_details" '
        {
            code: $code,
            message: $message,
            details: (if $details == "" then null else $details end)
        }
    '
}

broray_status_contract()
{
    status_module="${1:-unknown}"
    status_availability="${2:-unavailable}"
    status_severity="${3:-unknown}"
    status_operational="$(broray_status_bool "${4:-false}")"
    status_consistent="$(broray_status_bool "${5:-false}")"
    status_action_required="$(broray_status_bool "${6:-true}")"
    status_freshness="${7:-unknown}"
    status_checked_at="${8:-}"
    status_reasons="${9:-[]}"
    status_facts="${10-}"
    status_last_operation="${11-}"
    [ -n "$status_facts" ] || status_facts='{}'
    [ -n "$status_last_operation" ] || status_last_operation='null'

    case "$status_availability" in
        available|partial|unavailable) ;;
        *) status_availability="unavailable" ;;
    esac

    case "$status_severity" in
        ok|warning|error|busy|unknown) ;;
        *) status_severity="unknown" ;;
    esac

    case "$status_freshness" in
        fresh|stale|expired|unknown) ;;
        *) status_freshness="unknown" ;;
    esac

    printf '%s\n' "$status_reasons" | jq -e 'type == "array"' >/dev/null 2>&1 || status_reasons='[]'
    printf '%s\n' "$status_facts" | jq -e 'type == "object"' >/dev/null 2>&1 || status_facts='{}'
    printf '%s\n' "$status_last_operation" | jq -e 'type == "object" or . == null' >/dev/null 2>&1 || status_last_operation='null'

    jq -nc \
        --argjson schemaVersion "$BRORAY_STATUS_SCHEMA_VERSION" \
        --arg module "$status_module" \
        --arg availability "$status_availability" \
        --arg severity "$status_severity" \
        --argjson operational "$status_operational" \
        --argjson consistent "$status_consistent" \
        --argjson actionRequired "$status_action_required" \
        --arg freshness "$status_freshness" \
        --arg checkedAt "$status_checked_at" \
        --argjson reasons "$status_reasons" \
        --argjson facts "$status_facts" \
        --argjson lastOperation "$status_last_operation" '
        {
            schemaVersion: $schemaVersion,
            module: $module,
            availability: $availability,
            severity: $severity,
            operational: $operational,
            consistent: $consistent,
            actionRequired: $actionRequired,
            freshness: {
                state: $freshness,
                checkedAt: (if $checkedAt == "" then null else $checkedAt end)
            },
            reasons: $reasons,
            facts: $facts,
            lastOperation: $lastOperation
        }
    '
}

broray_status_rank()
{
    case "${1:-unknown}" in
        error) printf '%s\n' 50 ;;
        busy) printf '%s\n' 40 ;;
        warning) printf '%s\n' 30 ;;
        unknown) printf '%s\n' 20 ;;
        ok) printf '%s\n' 10 ;;
        *) printf '%s\n' 20 ;;
    esac
}

broray_status_worst()
{
    worst="ok"
    worst_rank="$(broray_status_rank "$worst")"

    for candidate in "$@"; do
        candidate_rank="$(broray_status_rank "$candidate")"
        if [ "$candidate_rank" -gt "$worst_rank" ]; then
            worst="$candidate"
            worst_rank="$candidate_rank"
        fi
    done

    printf '%s\n' "$worst"
}

broray_status_file_epoch()
{
    status_file="$1"
    [ -e "$status_file" ] || return 1

    find -P "$status_file" -maxdepth 0 -printf '%T@\n' 2>/dev/null |
        awk -F. 'NR==1{print $1;exit}'
}

broray_status_age_seconds()
{
    status_epoch="${1:-0}"
    status_now="${2:-$(date '+%s')}"

    case "$status_epoch:$status_now" in
        *[!0-9:]*|'') return 1 ;;
    esac

    if [ "$status_now" -lt "$status_epoch" ]; then
        printf '%s\n' 0
    else
        printf '%s\n' $((status_now - status_epoch))
    fi
}

broray_status_freshness_from_age()
{
    status_age="${1:-}"
    status_stale_after="${2:-1800}"
    status_expired_after="${3:-10800}"

    case "$status_age:$status_stale_after:$status_expired_after" in
        *[!0-9:]*|'') printf '%s\n' unknown; return 0 ;;
    esac

    if [ "$status_age" -le "$status_stale_after" ]; then
        printf '%s\n' fresh
    elif [ "$status_age" -le "$status_expired_after" ]; then
        printf '%s\n' stale
    else
        printf '%s\n' expired
    fi
}

broray_status_freshness_rank()
{
    case "${1:-unknown}" in
        expired) printf '%s\n' 40 ;;
        stale) printf '%s\n' 30 ;;
        unknown) printf '%s\n' 20 ;;
        fresh) printf '%s\n' 10 ;;
        *) printf '%s\n' 20 ;;
    esac
}

broray_status_freshness_worst()
{
    worst=fresh
    worst_rank="$(broray_status_freshness_rank "$worst")"
    for candidate in "$@"; do
        candidate_rank="$(broray_status_freshness_rank "$candidate")"
        if [ "$candidate_rank" -gt "$worst_rank" ]; then
            worst="$candidate"
            worst_rank="$candidate_rank"
        fi
    done
    printf '%s\n' "$worst"
}
