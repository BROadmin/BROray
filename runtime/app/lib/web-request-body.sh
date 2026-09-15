#!/opt/bin/ash

# Canonical CGI request-body reader for BusyBox ash/lighttpd.
# The request body is read directly from CGI descriptor zero; device aliases are forbidden.
broray_web_request_body_to_file()
{
    broray_web_body_target="$1"
    broray_web_body_limit="$2"
    broray_web_body_length="${CONTENT_LENGTH:-}"

    case "$broray_web_body_length" in
        ''|*[!0-9]*)
            return 2
            ;;
    esac

    [ "$broray_web_body_length" -gt 0 ] || return 3
    [ "$broray_web_body_length" -le "$broray_web_body_limit" ] || return 4

    umask 077
    rm -f "$broray_web_body_target"
    dd of="$broray_web_body_target" bs=1 count="$broray_web_body_length" 2>/dev/null || {
        rm -f "$broray_web_body_target"
        return 5
    }

    broray_web_body_actual="$(wc -c <"$broray_web_body_target" 2>/dev/null | tr -d ' ')"
    [ "$broray_web_body_actual" = "$broray_web_body_length" ] || {
        rm -f "$broray_web_body_target"
        return 6
    }

    return 0
}
