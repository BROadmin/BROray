#!/opt/bin/ash

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"

. "$BRORAY_BASE/lib/util.sh"
. "$BRORAY_BASE/lib/server.sh"

BRORAY_SETTINGS="$BRORAY_BASE/config/system/settings.json"
BRORAY_SERVER_TMP_CONFIG="${BRORAY_SERVER_TMP_CONFIG:-$BRORAY_BASE/tmp/server-config.new.json}"

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
                    ($s.tls.alpn // []),
                echConfigList: ($s.tls.echConfigList // ""),
                verifyPeerCertByName: ($s.tls.verifyPeerCertByName // ""),
                pinnedPeerCertSha256:
                    ($s.tls.pinnedPeerCertSha256 // "")
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
                    ($s.reality.spiderX // ""),
                mldsa65Verify: ($s.reality.mldsa65Verify // "")
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
                    # VLESS import already stores Host in transport.host.
                    # Keep it for XHTTP too; otherwise URI and JSON both lose it.
                    if (($s.transport.host // "") | length) > 0
                    then {host: $s.transport.host}
                    else {} end
                )
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
            if $s.transport.header != null then {header:$s.transport.header} else {
                header: {
                    type: ($s.transport.headerType // "none")
                } + (if ($s.transport.headerType // "none") == "http" then
                    {request: {path: [($s.transport.path // "/")]} +
                      (if ($s.transport.host // "") != "" then
                        {headers: {Host: ($s.transport.host | split(","))}} else {} end)}
                  else {} end)
            } end;

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

        def kcp_finalmask($s):
            if ($s.transport.kcpLegacy // false) then
                {udp: ([{type:"mkcp-legacy",settings:(if ($s.transport.kcpSeed // "") == "" then {} else {value:$s.transport.kcpSeed} end)}] +
                    (if ($s.transport.headerType // "none") != "none" then
                        [{type:"mkcp-legacy",settings:({header:(if $s.transport.headerType=="wechat-video" then "wechat" else $s.transport.headerType end)} +
                           (if $s.transport.headerType=="dns" and ($s.transport.host // "") != "" then {value:$s.transport.host} else {} end))}]
                     else [] end))}
            else ($s.transport.finalMask // {}) end;

        def hysteria_finalmask($s):
            ($s.hysteria.finalMask // {}) as $fm |
            (if ($s.hysteria.obfs // "") != "" then
                [{type:"salamander",settings:({password:$s.hysteria.obfsPassword} +
                    (if $s.hysteria.obfs == "gecko" then {packetSize:"512-1200"} else {} end))}]
             else [] end) as $obfs |
            (if ($s.hysteria.ports // "") != "" then
                [{type:"udphop",settings:{mode:"intervalremote",interval:30,remotePorts:$s.hysteria.ports}}]
             else [] end) as $hop |
            if ($obfs + $hop | length) == 0 then $fm
            else $fm + {udp:($obfs + ($fm.udp // []) + $hop)} end;

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
                elif $s.network == "kcp"
                then {kcpSettings: ($s.transport.kcp // {})}
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
                elif $s.network == "kcp" and (kcp_finalmask($s) | length) > 0 then
                    {finalmask: kcp_finalmask($s)}
                elif (($s.transport.finalMask // {}) | length) > 0 then
                    {finalmask: $s.transport.finalMask}
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
                                ({
                                    id: $s.uuid,
                                    encryption:
                                        (
                                            $s.encryption //
                                            "none"
                                        )
                                } +
                                (if (($s.flow // "") | length) > 0 then
                                    {flow:$s.flow}
                                 else {} end))
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

        def shadowsocks_outbound($s):
            {
                tag: "proxy",
                protocol: "shadowsocks",
                settings: {
                    servers: [
                        {
                            address: $s.address,
                            port: $s.port,
                            method: $s.method,
                            password: $s.password
                        }
                    ]
                }
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
                    listen: $cfg.listenAddress,
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
                    elif $s.protocol == "shadowsocks"
                    then
                        shadowsocks_outbound($s)
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
