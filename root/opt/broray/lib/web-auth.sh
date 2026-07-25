#!/opt/bin/ash
PATH=/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH


BRORAY_BASE="/opt/broray"
BRORAY_SESSION_DIR="$BRORAY_BASE/run/web-new/sessions"
BRORAY_SESSION_TTL=1800
BRORAY_KEENETIC_HOST="$(
    jq -r '.listenAddress // empty' \
        "$BRORAY_BASE/config/system/settings.json" \
        2>/dev/null
)"
BRORAY_KEENETIC_HOST="${BRORAY_KEENETIC_HOST:-192.168.1.1}"
BRORAY_KEENETIC_URL="http://$BRORAY_KEENETIC_HOST"

broray_json_escape() {
    jq -Rn --arg value "$1" '$value' |
        sed '1s/^"//; $s/"$//'
}

broray_json_response() {
    status="$1"
    body="$2"

    printf 'Status: %s\r\n' "$status"
    printf 'Content-Type: application/json; charset=utf-8\r\n'
    printf 'Cache-Control: no-store, no-cache, must-revalidate\r\n'
    printf 'Pragma: no-cache\r\n'
    printf 'X-Content-Type-Options: nosniff\r\n'
    printf 'X-Frame-Options: DENY\r\n'
    printf 'Referrer-Policy: no-referrer\r\n'
    printf '\r\n'
    printf '%s\n' "$body"
}

broray_no_content_response() {
    status="$1"

    printf 'Status: %s\r\n' "$status"
    printf 'Cache-Control: no-store, no-cache, must-revalidate\r\n'
    printf 'X-Content-Type-Options: nosniff\r\n'
    printf '\r\n'
}

broray_random_token() {
    hexdump \
        -n 32 \
        -e '32/1 "%02x"' \
        /dev/urandom 2>/dev/null
}

broray_cookie_value() {
    requested_name="$1"
    cookie_header="${HTTP_COOKIE:-}"

    [ -n "$cookie_header" ] || return 1

    old_ifs="$IFS"
    IFS=";"

    for cookie_part in $cookie_header; do
        IFS="$old_ifs"

        cookie_part="$(
            printf "%s" "$cookie_part" |
                sed "s/^[[:space:]]*//;s/[[:space:]]*$//"
        )"

        case "$cookie_part" in
            "$requested_name"=*)
                cookie_value="${cookie_part#*=}"
                printf "%s\n" "$cookie_value"
                IFS="$old_ifs"
                return 0
                ;;
        esac

        IFS=";"
    done

    IFS="$old_ifs"
    return 1
}

broray_valid_token() {
    token="$1"

    [ -n "$token" ] || return 1

    case "$token" in
        *[!0-9a-f]*)
            return 1
            ;;
    esac

    [ "${#token}" -eq 64 ]
}

broray_session_file() {
    token="$1"
    printf '%s/%s\n' "$BRORAY_SESSION_DIR" "$token"
}

broray_session_create() {
    username="$1"
    token="$(broray_random_token)"

    broray_valid_token "$token" || return 1

    now="$(date +%s)"
    expires="$((now + BRORAY_SESSION_TTL))"
    session_file="$(broray_session_file "$token")"
    temporary="$session_file.tmp.$$"

    umask 077

    jq -n \
        --arg username "$username" \
        --argjson createdAt "$now" \
        --argjson lastActivity "$now" \
        --argjson expiresAt "$expires" \
        '{
            schemaVersion: 1,
            username: $username,
            createdAt: $createdAt,
            lastActivity: $lastActivity,
            expiresAt: $expiresAt
        }' > "$temporary" || {
            rm -f "$temporary"
            return 1
        }

    chmod 600 "$temporary"
    mv "$temporary" "$session_file"

    printf '%s\n' "$token"
}

broray_session_validate() {
    session_token="$1"

    broray_valid_token "$session_token" || return 1

    session_file="$BRORAY_SESSION_DIR/$session_token"

    [ -f "$session_file" ] || return 1

    now="$(date +%s)"
    expires="$(
        jq -r ".expiresAt // 0" "$session_file" 2>/dev/null
    )"

    case "$now" in
        ""|*[!0-9]*)
            return 1
            ;;
    esac

    case "$expires" in
        ""|*[!0-9]*)
            rm -f "$session_file"
            return 1
            ;;
    esac

    if [ "$expires" -le "$now" ]; then
        rm -f "$session_file"
        return 1
    fi

    username="$(
        jq -r ".username // empty" "$session_file" 2>/dev/null
    )"

    [ -n "$username" ] || {
        rm -f "$session_file"
        return 1
    }

    new_expires="$((now + BRORAY_SESSION_TTL))"
    temporary="$session_file.tmp.$$"

    jq \
        --argjson lastActivity "$now" \
        --argjson expiresAt "$new_expires" \
        ".lastActivity = \$lastActivity |
         .expiresAt = \$expiresAt" \
        "$session_file" > "$temporary" 2>/dev/null || {
            rm -f "$temporary"
            return 1
        }

    chmod 600 "$temporary"
    mv "$temporary" "$session_file"

    BRORAY_SESSION_USERNAME="$username"
    BRORAY_SESSION_EXPIRES="$new_expires"

    export BRORAY_SESSION_USERNAME
    export BRORAY_SESSION_EXPIRES

    return 0
}

broray_session_require() {
    token="$(broray_cookie_value BRORAY_SESSION)"

    if ! broray_session_validate "$token"; then
        broray_json_response \
            "401 Unauthorized" \
            '{"ok":false,"error":"SESSION_REQUIRED","message":"Требуется авторизация."}'
        exit 0
    fi

    BRORAY_SESSION_TOKEN="$token"
    export BRORAY_SESSION_TOKEN
}

broray_session_delete() {
    token="$1"

    broray_valid_token "$token" || return 0

    session_file="$(broray_session_file "$token")"
    rm -f "$session_file"
}

broray_sessions_cleanup() {
    now="$(date +%s)"

    for session_file in "$BRORAY_SESSION_DIR"/*; do
        [ -f "$session_file" ] || continue

        expires="$(
            jq -r '.expiresAt // 0' "$session_file" 2>/dev/null
        )"

        case "$expires" in
            ''|*[!0-9]*)
                rm -f "$session_file"
                continue
                ;;
        esac

        if [ "$expires" -le "$now" ]; then
            rm -f "$session_file"
        fi
    done
}

broray_keenetic_authenticate() {
    login="$1"
    password="$2"

    auth_dir="$BRORAY_BASE/run/web-new/auth.$$"
    headers="$auth_dir/headers"
    body="$auth_dir/body"
    cookies="$auth_dir/cookies"
    post_body="$auth_dir/post.json"
    post_headers="$auth_dir/post-headers"

    umask 077
    mkdir -p "$auth_dir" || return 1

    curl \
        --silent \
        --show-error \
        --connect-timeout 5 \
        --max-time 10 \
        --dump-header "$headers" \
        --output "$body" \
        --cookie-jar "$cookies" \
        "$BRORAY_KEENETIC_URL/auth" >/dev/null 2>&1 || true

    realm="$(
        sed -n \
            's/^[Xx]-[Nn][Dd][Mm]-[Rr]ealm:[[:space:]]*//p' \
            "$headers" |
        tr -d '\r' |
        head -n 1
    )"

    challenge="$(
        sed -n \
            's/^[Xx]-[Nn][Dd][Mm]-[Cc]hallenge:[[:space:]]*//p' \
            "$headers" |
        tr -d '\r' |
        head -n 1
    )"

    if [ -z "$realm" ] || [ -z "$challenge" ]; then
        rm -rf "$auth_dir"
        return 2
    fi

    md5_hash="$(
        printf '%s' "$login:$realm:$password" |
            md5sum |
            awk '{print $1}'
    )"

    response_hash="$(
        printf '%s' "$challenge$md5_hash" |
            sha256sum |
            awk '{print $1}'
    )"

    jq -n \
        --arg login "$login" \
        --arg password "$response_hash" \
        '{
            login: $login,
            password: $password
        }' > "$post_body" || {
            rm -rf "$auth_dir"
            return 1
        }

    http_code="$(
        curl \
            --silent \
            --show-error \
            --connect-timeout 5 \
            --max-time 10 \
            --cookie "$cookies" \
            --cookie-jar "$cookies" \
            --header 'Content-Type: application/json' \
            --request POST \
            --data-binary "@$post_body" \
            --dump-header "$post_headers" \
            --output "$body" \
            --write-out '%{http_code}' \
            "$BRORAY_KEENETIC_URL/auth" 2>/dev/null ||
            printf '000'
    )"

    password=""
    md5_hash=""
    response_hash=""

    rm -rf "$auth_dir"

    [ "$http_code" = "200" ]
}
