#!/bin/sh

broray_detect_lan_ip() {
    LAN_IP=""

    # Основной LAN-мост Keenetic.
    if ip -4 addr show br0 >/dev/null 2>&1; then
        LAN_IP="$(
            ip -4 addr show br0 2>/dev/null |
            awk '/inet / {
                ip=$2
                sub(/\/.*/, "", ip)
                print ip
                exit
            }'
        )"
    fi

    # Адрес роутера, через который установлена текущая SSH-сессия.
    if [ -z "$LAN_IP" ] && [ -n "${SSH_CONNECTION:-}" ]; then
        LAN_IP="$(printf '%s\n' "$SSH_CONNECTION" | awk '{print $3}')"
    fi

    # Первый локальный RFC1918-адрес, исключая VPN и WAN-интерфейсы.
    if [ -z "$LAN_IP" ]; then
        LAN_IP="$(
            ip -4 addr show 2>/dev/null |
            awk '
                /^[0-9]+:/ {
                    iface=$2
                    sub(/:$/, "", iface)
                    sub(/@.*/, "", iface)
                }

                /inet / {
                    ip=$2
                    sub(/\/.*/, "", ip)

                    if (
                        iface != "lo" &&
                        iface !~ /^(ppp|nwg|wg|tun|tap|xray)/ &&
                        (
                            ip ~ /^10\./ ||
                            ip ~ /^192\.168\./ ||
                            ip ~ /^172\.(1[6-9]|2[0-9]|3[01])\./
                        )
                    ) {
                        print ip
                        exit
                    }
                }
            '
        )"
    fi

    [ -n "$LAN_IP" ] || {
        echo "Не удалось определить LAN-IP роутера." >&2
        return 1
    }

    case "$LAN_IP" in
        127.*|0.0.0.0|255.*)
            echo "Определён недопустимый LAN-IP: $LAN_IP" >&2
            return 1
            ;;
    esac

    printf '%s\n' "$LAN_IP"
}

broray_save_lan_ip() {
    LAN_IP="$(broray_detect_lan_ip)" || return 1

    mkdir -p /opt/broray/run
    printf '%s\n' "$LAN_IP" > /opt/broray/run/lan-ip

    printf '%s\n' "$LAN_IP"
}
