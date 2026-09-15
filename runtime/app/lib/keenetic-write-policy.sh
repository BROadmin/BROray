#!/opt/bin/ash

# Immutable R14C01 gate shared by every Keenetic CLI write path. Source bytes
# are development/fail-closed. The candidate builder may replace only the
# common stage/enabled/SHA tuple before manifests are generated. Runtime
# compatibility is selected exclusively by functional capability probes and
# bounded parsers; router model, hw_id and firmware text are never selectors.

case "${BRORAY_KEENETIC_WRITE_POLICY_LOADED:-}" in
    true)
        command -v broray_keenetic_write_policy_check >/dev/null 2>&1 &&
        command -v broray_keenetic_write_policy_sha256 >/dev/null 2>&1 &&
            return 0
        return 1
        ;;
    '') ;;
    *) return 1 ;;
esac

BRORAY_KEENETIC_WRITE_POLICY_SCHEMA_VERSION=1
BRORAY_KEENETIC_WRITE_POLICY_CANDIDATE_ID='3.0.0-r15c16'
BRORAY_KEENETIC_WRITE_POLICY_STAGE='staged'
BRORAY_KEENETIC_WRITE_POLICY_ENABLED=true
BRORAY_KEENETIC_WRITE_POLICY_SHA256='dc8501856b27260738828e9959e10512496410ff44adfceaa8564056bed1ddfa'
BRORAY_KEENETIC_WEB_PUBLISH_SERIALIZATION='dynamic-bounded-v1'
BRORAY_KEENETIC_PROXY_INTERFACE_SERIALIZATION='dynamic-bounded-v1'
BRORAY_KEENETIC_STATIC_ROUTES_SERIALIZATION='dynamic-bounded-v1'
BRORAY_KEENETIC_DOT_SERIALIZATION='dynamic-bounded-v1'

readonly BRORAY_KEENETIC_WRITE_POLICY_SCHEMA_VERSION
readonly BRORAY_KEENETIC_WRITE_POLICY_CANDIDATE_ID
readonly BRORAY_KEENETIC_WRITE_POLICY_STAGE
readonly BRORAY_KEENETIC_WRITE_POLICY_ENABLED
readonly BRORAY_KEENETIC_WRITE_POLICY_SHA256
readonly BRORAY_KEENETIC_WEB_PUBLISH_SERIALIZATION
readonly BRORAY_KEENETIC_PROXY_INTERFACE_SERIALIZATION
readonly BRORAY_KEENETIC_STATIC_ROUTES_SERIALIZATION
readonly BRORAY_KEENETIC_DOT_SERIALIZATION

broray_keenetic_write_policy_path_valid()
{
    case "${1:-}" in
        web-publish|proxy-interface|static-routes|dot) return 0 ;;
        *) return 1 ;;
    esac
}

broray_keenetic_write_policy_contract_valid()
{
    [ "$BRORAY_KEENETIC_WRITE_POLICY_SCHEMA_VERSION" = 1 ] || return 1
    [ "$BRORAY_KEENETIC_WRITE_POLICY_CANDIDATE_ID" = '3.0.0-r15c16' ] || return 1

    case "$BRORAY_KEENETIC_WRITE_POLICY_ENABLED:$BRORAY_KEENETIC_WRITE_POLICY_STAGE" in
        false:development)
            [ -z "$BRORAY_KEENETIC_WRITE_POLICY_SHA256" ]
            ;;
        true:staged)
            case "$BRORAY_KEENETIC_WRITE_POLICY_SHA256" in
                ''|*[!0-9a-f]*) return 1 ;;
            esac
            [ "${#BRORAY_KEENETIC_WRITE_POLICY_SHA256}" -eq 64 ]
            ;;
        *)
            return 1
            ;;
    esac
    [ "$BRORAY_KEENETIC_WEB_PUBLISH_SERIALIZATION" = dynamic-bounded-v1 ] || return 1
    [ "$BRORAY_KEENETIC_PROXY_INTERFACE_SERIALIZATION" = dynamic-bounded-v1 ] || return 1
    [ "$BRORAY_KEENETIC_STATIC_ROUTES_SERIALIZATION" = dynamic-bounded-v1 ] || return 1
    [ "$BRORAY_KEENETIC_DOT_SERIALIZATION" = dynamic-bounded-v1 ] || return 1
}

# Return codes are part of the R14C01 caller contract:
#   0: staged policy enabled for a known path
#   2: byte-valid development policy is deliberately disabled
#   3: immutable policy constants are invalid or inconsistent
#   4: caller supplied an unknown write-path identifier
broray_keenetic_write_policy_check()
{
    local path

    path="${1:-}"
    broray_keenetic_write_policy_path_valid "$path" || return 4
    broray_keenetic_write_policy_contract_valid || return 3
    [ "$BRORAY_KEENETIC_WRITE_POLICY_ENABLED" = true ] || return 2
    return 0
}

broray_keenetic_write_policy_sha256()
{
    broray_keenetic_write_policy_contract_valid || return 1
    [ "$BRORAY_KEENETIC_WRITE_POLICY_ENABLED" = true ] || return 1
    printf '%s\n' "$BRORAY_KEENETIC_WRITE_POLICY_SHA256"
}

# Dynamic-bounded profiles never predict a model-specific serialization. Each
# caller must accept only its closed command grammar and prove the resulting
# scoped state with its bounded parser before committing ownership.
broray_keenetic_write_policy_web_publish_profile_check()
{
    local rc

    rc=0
    broray_keenetic_write_policy_check web-publish || rc=$?
    [ "$rc" -eq 0 ] || return "$rc"

    [ "$BRORAY_KEENETIC_WEB_PUBLISH_SERIALIZATION" = dynamic-bounded-v1 ] || return 3
}

broray_keenetic_write_policy_web_publish_serialization()
{
    broray_keenetic_write_policy_web_publish_profile_check || return $?
    [ "${1:-}" = profile ] || return 4
    printf '%s\n' "$BRORAY_KEENETIC_WEB_PUBLISH_SERIALIZATION"
}

broray_keenetic_write_policy_proxy_interface_profile_check()
{
    local rc

    rc=0
    broray_keenetic_write_policy_check proxy-interface || rc=$?
    [ "$rc" -eq 0 ] || return "$rc"
    [ "$BRORAY_KEENETIC_PROXY_INTERFACE_SERIALIZATION" = dynamic-bounded-v1 ] || return 3
}

broray_keenetic_write_policy_static_routes_profile_check()
{
    local rc

    rc=0
    broray_keenetic_write_policy_check static-routes || rc=$?
    [ "$rc" -eq 0 ] || return "$rc"
    [ "$BRORAY_KEENETIC_STATIC_ROUTES_SERIALIZATION" = dynamic-bounded-v1 ] || return 3
}

broray_keenetic_write_policy_dot_profile_check()
{
    local rc

    rc=0
    broray_keenetic_write_policy_check dot || rc=$?
    [ "$rc" -eq 0 ] || return "$rc"
    [ "$BRORAY_KEENETIC_DOT_SERIALIZATION" = dynamic-bounded-v1 ] || return 3
}

BRORAY_KEENETIC_WRITE_POLICY_LOADED=true
readonly BRORAY_KEENETIC_WRITE_POLICY_LOADED
