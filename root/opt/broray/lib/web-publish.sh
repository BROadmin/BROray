#!/opt/bin/ash

# Публичный модуль публикации WebUI через KeenDNS HTTP Proxy.

broray_web_publish_exists() {
    command -v ndmc >/dev/null 2>&1 || return 1
    ndmc -c 'show ip http proxy' 2>/dev/null |
        awk '
            $0 ~ /name:[ \t]*broray/ {found = 1}
            END {exit(found ? 0 : 1)}
        '
}

broray_web_publish_delete() {
    command -v ndmc >/dev/null 2>&1 || return 0
    ndmc -c 'no ip http proxy broray' >/dev/null 2>&1 || true
    ndmc -c 'system configuration save' >/dev/null 2>&1 || return 1
    return 0
}
