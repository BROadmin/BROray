#!/bin/sh

. /opt/broray/lib/util.sh
. /opt/broray/lib/server.sh

BRORAY_BASE="/opt/broray"
BRORAY_SETTINGS="$BRORAY_BASE/config/system/settings.json"
BRORAY_SERVER_TMP_CONFIG="$BRORAY_BASE/tmp/server-config.new.json"

broray_generate_server_config()
{
    server_id="$1"

    [ -n "$server_id" ] ||
        broray_die \
            "не указан идентификатор сервера"

    server_file="$(
        broray_server_path "$server_id"
    )"

    broray_server_validate "$server_file"
    broray_json_validate "$BRORAY_SETTINGS"

    mkdir -p "$BRORAY_BASE/tmp"

    jq -n \
        --slurpfile server "$server_file" \
        --slurpfile settings "$BRORAY_SETTINGS" \
        --arg accessLog "$BRORAY_BASE/logs/access.log" \
        --arg errorLog "$BRORAY_BASE/logs/error.log" \
        '
        def compact_object:
            with_entries(
                select(
                    .value != null and
                    .value != "" and
                    .value != [] and
                    .value != {}
                )
            );

        def tls_settings($s):
            {
                serverName:
                    (
                        $s.tls.serverName //
                        $s.reality.serverName //
                        ""
                    ),
                fingerprint:
                    (
                        $s.tls.fingerprint //
                        $s.reality.fingerprint //
                        "chrome"
                    ),
                alpn:
                    ($s.tls.alpn // [])
            }
            | compact_object;

        def reality_settings($s):
            {
                serverName:
                    ($s.reality.serverName // ""),
                fingerprint:
                    ($s.reality.fingerprint // "chrome"),
                publicKey:
                    ($s.reality.publicKey // ""),
                shortId:
                    ($s.reality.shortId // ""),
                spiderX:
                    ($s.reality.spiderX // "")
            }
            | compact_object;

        def xhttp_settings($s):
            if $s.protocol == "vless" then
                {
                    path: ($s.xhttp.path // "/"),
                    mode: ($s.xhttp.mode // "auto")
                }
                +
                (
                    if (($s.xhttp.extra // {}) | length) > 0
                    then {
                        extra: ($s.xhttp.extra // {})
                    }
                    else {}
                    end
                )
            else
                {
                    path: ($s.transport.path // "/"),
                    mode: ($s.transport.mode // "auto")
                }
                +
                (
                    if (($s.transport.host // "") | length) > 0
                    then {
                        host: $s.transport.host
                    }
                    else {}
                    end
                )
                +
                (
                    if (($s.transport.extra // {}) | length) > 0
                    then {
                        extra: ($s.transport.extra // {})
                    }
                    else {}
                    end
                )
            end;

        def raw_settings($s):
            {
                header: {
                    type:
                        ($s.transport.headerType // "none")
                }
            };

        def websocket_settings($s):
            {
                path:
                    ($s.transport.path // "/")
            }
            +
            (
                if (($s.transport.host // "") | length) > 0
                then {
                    host: $s.transport.host
                }
                else {}
                end
            );

        def grpc_settings($s):
            {
                serviceName:
                    ($s.transport.serviceName // ""),
                authority:
                    ($s.transport.host // ""),
                multiMode:
                    (
                        ($s.transport.mode // "") ==
                        "multi"
                    )
            }
            | compact_object;

        def httpupgrade_settings($s):
            {
                path:
                    ($s.transport.path // "/"),
                host:
                    ($s.transport.host // "")
            }
            | compact_object;

        def hysteria_finalmask($s):
            if
                (
                    ($s.hysteria.finalMask // {}) |
                    type == "object" and length > 0
                )
            then
                $s.hysteria.finalMask
            elif
                ($s.hysteria.obfs // "") == "salamander" and
                (($s.hysteria.obfsPassword // "") | length) > 0
            then
                {
                    udp: [
                        {
                            type: "salamander",
                            settings: {
                                password:
                                    $s.hysteria.obfsPassword
                            }
                        }
                    ]
                }
            else
                {}
            end;

        def stream_settings($s):
            {
                network: $s.network,
                security: $s.security
            }
            +
            (
                if $s.network == "xhttp"
                then {
                    xhttpSettings:
                        xhttp_settings($s)
                }
                elif $s.network == "raw"
                then {
                    rawSettings:
                        raw_settings($s)
                }
                elif $s.network == "ws"
                then {
                    wsSettings:
                        websocket_settings($s)
                }
                elif $s.network == "grpc"
                then {
                    grpcSettings:
                        grpc_settings($s)
                }
                elif $s.network == "httpupgrade"
                then {
                    httpupgradeSettings:
                        httpupgrade_settings($s)
                }
                elif $s.network == "hysteria"
                then {
                    hysteriaSettings: {
                        version: 2,
                        auth: $s.auth
                    }
                }
                else
                    error(
                        "неподдерживаемый транспорт: " +
                        $s.network
                    )
                end
            )
            +
            (
                if $s.security == "tls"
                then {
                    tlsSettings:
                        tls_settings($s)
                }
                elif $s.security == "reality"
                then {
                    realitySettings:
                        reality_settings($s)
                }
                elif $s.security == "none"
                then {}
                else
                    error(
                        "неподдерживаемая защита: " +
                        $s.security
                    )
                end
            )
            +
            (
                if
                    $s.network == "hysteria" and
                    ((hysteria_finalmask($s)) | length) > 0
                then {
                    finalmask:
                        hysteria_finalmask($s)
                }
                else {}
                end
            );

        def vless_outbound($s):
            {
                tag: "proxy",
                protocol: "vless",
                settings: {
                    vnext: [
                        {
                            address: $s.address,
                            port: $s.port,
                            users: [
                                {
                                    id: $s.uuid,
                                    encryption:
                                        (
                                            $s.encryption //
                                            "none"
                                        )
                                }
                            ]
                        }
                    ]
                },
                streamSettings:
                    stream_settings($s)
            };

        def vmess_outbound($s):
            {
                tag: "proxy",
                protocol: "vmess",
                settings: {
                    vnext: [
                        {
                            address: $s.address,
                            port: $s.port,
                            users: [
                                {
                                    id: $s.uuid,
                                    alterId:
                                        ($s.alterId // 0),
                                    security:
                                        (
                                            $s.encryption //
                                            "auto"
                                        )
                                }
                            ]
                        }
                    ]
                },
                streamSettings:
                    stream_settings($s)
            };

        def trojan_outbound($s):
            {
                tag: "proxy",
                protocol: "trojan",
                settings: {
                    servers: [
                        {
                            address: $s.address,
                            port: $s.port,
                            password: $s.password
                        }
                    ]
                },
                streamSettings:
                    stream_settings($s)
            };

        def hysteria2_outbound($s):
            {
                tag: "proxy",
                protocol: "hysteria",
                settings:
                    (
                        {
                            version: 2,
                            address: $s.address,
                            port: $s.port
                        }
                        +
                        (
                            if (($s.hysteria.obfsPassword // "") | length) > 0
                            then
                                {
                                    obfsPassword:
                                        $s.hysteria.obfsPassword
                                }
                            else
                                {}
                            end
                        )
                    ),
                streamSettings:
                    stream_settings($s)
            };
        ($server[0]) as $s |
        ($settings[0]) as $cfg |

        {
            log: {
                access: $accessLog,
                error: $errorLog,
                loglevel:
                    ($cfg.logLevel // "warning")
            },
            inbounds: [
                {
                    tag: "socks",
                    listen:
                        (
                            $cfg.listenAddress //
                            "192.168.1.1"
                        ),
                    port:
                        ($cfg.socksPort // 2080),
                    protocol: "socks",
                    settings: {
                        auth: "noauth",
                        udp: true
                    }
                }
            ],
            outbounds: [
                (
                    if $s.protocol == "vless"
                    then
                        vless_outbound($s)
                    elif $s.protocol == "vmess"
                    then
                        vmess_outbound($s)
                    elif $s.protocol == "trojan"
                    then
                        trojan_outbound($s)
                    elif $s.protocol == "hysteria2"
                    then
                        hysteria2_outbound($s)
                    else
                        error(
                            "генератор не поддерживает протокол: " +
                            $s.protocol
                        )
                    end
                )
            ]
        }
        ' > "$BRORAY_SERVER_TMP_CONFIG" ||
        broray_die \
            "не удалось создать конфигурацию сервера"

    broray_json_validate \
        "$BRORAY_SERVER_TMP_CONFIG"

    printf '%s\n' \
        "$BRORAY_SERVER_TMP_CONFIG"
}
