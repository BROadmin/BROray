# Реестр клиентских схем Xray 26.11.9
> Исторический реестр: расширенный импорт 3.5.7 описан отдельно в [актуальном реестре 70 схем / 456 записей](IMPORT-XRAY-FIELDS-3.5.7.md) и [форматах импорта](PROTOCOL-SUPPORT.md). Старые строки ниже не являются полной приёмкой 3.5.7.

> Поправка 3.5.6: прежние отметки об отклонении Hysteria masquerade ниже описывают исторический аудит. Теперь известный серверный блок проверяется и пропускается с предупреждением, не применяется на клиенте. Остальные строки аудита не пересматривались. [Приёмка](ACCEPTANCE-3.5.6.md).


Учтены **468** деклараций. Серверные и неподдержанные поля тоже перечислены; общие структуры описаны один раз. Это конечная область проверки, а не обещание работы любого набора значений или любого сервера.

Для каждой строки [JSON-реестр](XRAY-26.11.9-SCHEMA.json) содержит источник с SHA-256, правило обработки и группы тестов. Контрольные суммы квитанций исполнения приведены отдельно. Групповой PASS не выдаётся за исчерпывающий перебор значений каждого поля.

В новом импорте FinalMask проверяются рекурсивно: поля, типы, null, выбранные границы и условия Build. PEM, TLS fingerprint/cipher/curve, криптографические связи и часть семантики XDNS проверяет официальный Xray перед запуском. Исходные значения сохраняются; старые записи не переимпортируются.

Основной TLS/socket и вложенный TLS/socket масок имеют разные разрешённые подмножества. Непустые локальные пути, команды и чужое управление маршрутизацией не исполняются. Реальный Google Drive/WARP и WireGuard на MIPS имеют отдельные ограничения.

| Схема | Поле | Тип | Контекст | Правило обработки |
|---|---|---|---|---|
| StreamConfig | address | *Address | client | rejected |
| StreamConfig | port | uint16 | client | rejected |
| StreamConfig | method | *TransportProtocol | client | rejected |
| StreamConfig | network | *TransportProtocol | client | accepted-context-dependent |
| StreamConfig | security | string | client | accepted-context-dependent |
| StreamConfig | finalmask | *FinalMask | client | accepted-context-dependent |
| StreamConfig | tlsSettings | *TLSConfig | client | accepted-context-dependent |
| StreamConfig | realitySettings | *REALITYConfig | client | accepted-context-dependent |
| StreamConfig | rawSettings | *TCPConfig | client | accepted-context-dependent |
| StreamConfig | tcpSettings | *TCPConfig | client | accepted-context-dependent |
| StreamConfig | xhttpSettings | *SplitHTTPConfig | client | accepted-context-dependent |
| StreamConfig | splithttpSettings | *SplitHTTPConfig | client | accepted-context-dependent |
| StreamConfig | kcpSettings | *KCPConfig | client | accepted-context-dependent |
| StreamConfig | grpcSettings | *GRPCConfig | client | accepted-context-dependent |
| StreamConfig | wsSettings | *WebSocketConfig | client | accepted-context-dependent |
| StreamConfig | httpupgradeSettings | *HttpUpgradeConfig | client | accepted-context-dependent |
| StreamConfig | hysteriaSettings | *HysteriaConfig | client | accepted-context-dependent |
| StreamConfig | masqueSettings | *MasqueConfig | client | accepted-context-dependent |
| StreamConfig | xdriveSettings | *XDriveConfig | client | accepted-context-dependent |
| StreamConfig | sockopt | *SocketConfig | client | accepted-context-dependent |
| REALITYConfig | masterKeyLog | string | client | rejected |
| REALITYConfig | show | bool | client | accepted-metadata-warning |
| REALITYConfig | target | json.RawMessage | server-only | excluded-server |
| REALITYConfig | dest | json.RawMessage | server-only | excluded-server |
| REALITYConfig | type | string | server-only | excluded-server |
| REALITYConfig | xver | uint64 | server-only | excluded-server |
| REALITYConfig | serverNames | []string | server-only | excluded-server |
| REALITYConfig | privateKey | string | server-only | excluded-server |
| REALITYConfig | minClientVer | string | server-only | excluded-server |
| REALITYConfig | maxClientVer | string | server-only | excluded-server |
| REALITYConfig | maxTimeDiff | uint64 | server-only | excluded-server |
| REALITYConfig | shortIds | []string | server-only | excluded-server |
| REALITYConfig | mldsa65Seed | string | server-only | excluded-server |
| REALITYConfig | limitFallbackUpload | LimitFallback | server-only | excluded-server |
| REALITYConfig | limitFallbackDownload | LimitFallback | server-only | excluded-server |
| REALITYConfig | fingerprint | string | client | accepted-preserved |
| REALITYConfig | serverName | string | client | accepted-preserved |
| REALITYConfig | password | string | client | accepted-transformed |
| REALITYConfig | publicKey | string | client | accepted-preserved |
| REALITYConfig | shortId | string | client | accepted-preserved |
| REALITYConfig | mldsa65Verify | string | client | accepted-preserved |
| REALITYConfig | spiderX | string | client | accepted-preserved |
| TLSCertConfig | certificateFile | string | client-or-server-by-usage | rejected |
| TLSCertConfig | certificate | []string | client-or-server-by-usage | rejected |
| TLSCertConfig | keyFile | string | client-or-server-by-usage | rejected |
| TLSCertConfig | key | []string | client-or-server-by-usage | rejected |
| TLSCertConfig | usage | string | client-or-server-by-usage | rejected |
| TLSCertConfig | ocspStapling | uint64 | client-or-server-by-usage | rejected |
| TLSCertConfig | oneTimeLoading | bool | client-or-server-by-usage | rejected |
| TLSCertConfig | buildChain | bool | client-or-server-by-usage | rejected |
| TLSConfig | allowInsecure | bool | client | accepted-false-rejected-true |
| TLSConfig | certificates | []*TLSCertConfig | client | rejected |
| TLSConfig | serverName | string | client | accepted-preserved |
| TLSConfig | alpn | *StringList | client | accepted-preserved |
| TLSConfig | enableSessionResumption | bool | client | rejected |
| TLSConfig | disableSystemRoot | bool | client | rejected |
| TLSConfig | minVersion | string | client | rejected |
| TLSConfig | maxVersion | string | client | rejected |
| TLSConfig | cipherSuites | string | client | rejected |
| TLSConfig | fingerprint | string | client | accepted-preserved |
| TLSConfig | rejectUnknownSni | bool | server-only | excluded-server |
| TLSConfig | curvePreferences | *StringList | client | rejected |
| TLSConfig | masterKeyLog | string | client | rejected |
| TLSConfig | pinnedPeerCertSha256 | string | client | accepted-preserved |
| TLSConfig | verifyPeerCertByName | string | client | accepted-preserved |
| TLSConfig | echServerKeys | string | server-only | excluded-server |
| TLSConfig | echConfigList | string | client | accepted-preserved |
| TLSConfig | echSockopt | *SocketConfig | client | rejected |
| TLSConfig | useSystemCA | bool | client | rejected |
| CustomSockoptConfig | system | string | client | rejected |
| CustomSockoptConfig | network | string | client | rejected |
| CustomSockoptConfig | level | string | client | rejected |
| CustomSockoptConfig | opt | string | client | rejected |
| CustomSockoptConfig | value | string | client | rejected |
| CustomSockoptConfig | type | string | client | rejected |
| HappyEyeballsConfig | prioritizeIPv6 | bool | client | rejected |
| HappyEyeballsConfig | tryDelayMs | uint64 | client | rejected |
| HappyEyeballsConfig | interleave | uint32 | client | rejected |
| HappyEyeballsConfig | maxConcurrentTry | uint32 | client | rejected |
| SocketConfig | mark | int32 | client | rejected |
| SocketConfig | tcpFastOpen | interface{} | client | accepted-preserved |
| SocketConfig | tproxy | string | listener/server-policy; excluded from client endpoint import | rejected |
| SocketConfig | acceptProxyProtocol | bool | listener/server-policy; excluded from client endpoint import | rejected |
| SocketConfig | domainStrategy | string | client | accepted-preserved |
| SocketConfig | dialerProxy | string | client | rejected |
| SocketConfig | tcpKeepAliveInterval | int32 | client | accepted-preserved |
| SocketConfig | tcpKeepAliveIdle | int32 | client | accepted-preserved |
| SocketConfig | tcpCongestion | string | client | rejected |
| SocketConfig | tcpWindowClamp | int32 | client | rejected |
| SocketConfig | tcpMaxSeg | int32 | client | rejected |
| SocketConfig | penetrate | bool | client | rejected |
| SocketConfig | tcpUserTimeout | int32 | client | rejected |
| SocketConfig | v6only | bool | listener/server-policy; excluded from client endpoint import | rejected |
| SocketConfig | interface | string | client | rejected |
| SocketConfig | tcpMptcp | bool | client | rejected |
| SocketConfig | customSockopt | []*CustomSockoptConfig | client | rejected |
| SocketConfig | addressPortStrategy | string | client | rejected |
| SocketConfig | happyEyeballs | *HappyEyeballsConfig | client | rejected |
| SocketConfig | trustedXForwardedFor | []string | listener/server-policy; excluded from client endpoint import | rejected |
| Filter | blockedSourceIPs | []string | TCP-or-UDP | rejected-mask-type |
| Filter | allowedSourceIPs | []string | TCP-or-UDP | rejected-mask-type |
| TCPItem | delay | Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| TCPItem | rand | int32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| TCPItem | randRange | *Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| TCPItem | capture | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| TCPItem | type | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| TCPItem | reuse | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| TCPItem | transform | *CustomTransform | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| TCPItem | packet | json.RawMessage | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| HeaderCustomTCP | clients | [][]TCPItem | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| HeaderCustomTCP | servers | [][]TCPItem | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| HeaderCustomTCP | errors | [][]TCPItem | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| FragmentMask | packets | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| FragmentMask | length | Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| FragmentMask | delay | Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| FragmentMask | lengths | []Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| FragmentMask | delays | []Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| FragmentMask | maxSplit | Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| NoiseItem | rand | Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| NoiseItem | randRange | *Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| NoiseItem | type | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| NoiseItem | packet | json.RawMessage | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| NoiseItem | delay | Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| NoiseMask | reset | Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| NoiseMask | noise | []NoiseItem | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| NoiseMask | script | string | mask-specific TCP/UDP context | accepted-empty-only |
| UDPItem | rand | int32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| UDPItem | randRange | *Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| UDPItem | capture | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| UDPItem | type | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| UDPItem | reuse | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| UDPItem | transform | *CustomTransform | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| UDPItem | packet | json.RawMessage | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| CustomTransform | op | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| CustomTransform | args | []CustomTransformArg | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| CustomTransformArg | type | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| CustomTransformArg | bytes | json.RawMessage | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| CustomTransformArg | u64 | *uint64 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| CustomTransformArg | reuse | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| CustomTransformArg | metadata | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| CustomTransformArg | transform | *CustomTransform | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| HeaderCustomUDP | mode | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| HeaderCustomUDP | client | []UDPItem | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| HeaderCustomUDP | server | []UDPItem | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| MkcpLegacy | header | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| MkcpLegacy | value | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Salamander | password | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Salamander | packetSize | Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Sudoku | password | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Sudoku | ascii | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Sudoku | customTable | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Sudoku | custom_table | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Sudoku | customTables | []string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Sudoku | custom_tables | []string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Sudoku | paddingMin | uint32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Sudoku | padding_min | uint32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Sudoku | paddingMax | uint32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Sudoku | padding_max | uint32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNSDomain | names | []string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNSDomain | lenLimit | int32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNSDomain | labelLimit | int32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNSDomain | types | []int32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNSDomain | edns0 | int32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNSResolver | addrs | []string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNSResolver | worker | int32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNSResolver | tlsConfig | *TLSConfig | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNSResolver | quicConfig | *QuicParamsConfig | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNS | domains | []XDNSDomain | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNS | resolvers | []XDNSResolver | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XDNS | extraPoll | int32 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XMC | hostname | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XMC | profiles | []XMCProfile | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XMC | password | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XMCProfile | username | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XMCProfile | uuid | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XMCProfile | texturesValue | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| XMCProfile | texturesSignature | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Xicmp | dgram | bool | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Xicmp | ips | []string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Realm | url | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Realm | stunServers | []string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Realm | tlsConfig | *TLSConfig | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Realm | ipMode | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Realm | portMapping | *realm.PortMapping | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| UDPHop | mode | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| UDPHop | interval | Int32Range | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| UDPHop | remoteIPs | []string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| UDPHop | remotePorts | PortList | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| Mask | type | string | client | fresh-schema-preserved-core-semantics |
| Mask | settings | *json.RawMessage | client | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | congestion | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | debug | bool | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | bbrProfile | string | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | brutalUp | Bandwidth | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | brutalDown | Bandwidth | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | brutalDisableLossCompensation | bool | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | initStreamReceiveWindow | uint64 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | maxStreamReceiveWindow | uint64 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | initConnectionReceiveWindow | uint64 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | maxConnectionReceiveWindow | uint64 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | maxIdleTimeout | int64 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | keepAlivePeriod | int64 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | disablePathMTUDiscovery | bool | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | disableChromeParrot | bool | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | disableGSO | bool | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | maxIncomingStreams | int64 | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| QuicParamsConfig | disableStatelessReset | bool | mask-specific TCP/UDP context | fresh-schema-preserved-core-semantics |
| FinalMask | tcp | []Mask | client | fresh-schema-preserved-core-semantics |
| FinalMask | udp | []Mask | client | fresh-schema-preserved-core-semantics |
| FinalMask | quicParams | *QuicParamsConfig | client | fresh-schema-preserved-core-semantics |
| AuthenticatorRequest | version | string | client-and-server-handshake | accepted-preserved |
| AuthenticatorRequest | method | string | client-and-server-handshake | accepted-preserved |
| AuthenticatorRequest | path | StringList | client-and-server-handshake | accepted-preserved |
| AuthenticatorRequest | headers | map[string]*StringList | client-and-server-handshake | accepted-preserved |
| AuthenticatorResponse | version | string | client-and-server-handshake | accepted-preserved |
| AuthenticatorResponse | status | string | client-and-server-handshake | accepted-preserved |
| AuthenticatorResponse | reason | string | client-and-server-handshake | accepted-preserved |
| AuthenticatorResponse | headers | map[string]*StringList | client-and-server-handshake | accepted-preserved |
| Authenticator | request | AuthenticatorRequest | client-and-server-handshake | accepted-preserved |
| Authenticator | response | AuthenticatorResponse | client-and-server-handshake | accepted-preserved |
| TCPConfig | header | json.RawMessage | client | accepted-preserved |
| TCPConfig | acceptProxyProtocol | bool | server-only | excluded-server |
| SplitHTTPConfig | host | string | client | accepted-transformed |
| SplitHTTPConfig | path | string | client | accepted-transformed |
| SplitHTTPConfig | mode | string | client | accepted-transformed |
| SplitHTTPConfig | headers | map[string]string | client | accepted-preserved |
| SplitHTTPConfig | xPaddingBytes | Int32Range | client | accepted-preserved |
| SplitHTTPConfig | xPaddingObfsMode | bool | client | accepted-preserved |
| SplitHTTPConfig | xPaddingKey | string | client | accepted-preserved |
| SplitHTTPConfig | xPaddingHeader | string | client | accepted-preserved |
| SplitHTTPConfig | xPaddingPlacement | string | client | accepted-preserved |
| SplitHTTPConfig | xPaddingMethod | string | client | accepted-preserved |
| SplitHTTPConfig | uplinkHTTPMethod | string | client | accepted-preserved |
| SplitHTTPConfig | sessionIDPlacement | string | client | accepted-preserved |
| SplitHTTPConfig | sessionIDKey | string | client | accepted-preserved |
| SplitHTTPConfig | sessionIDTable | string | client | accepted-preserved |
| SplitHTTPConfig | sessionIDLength | Int32Range | client | accepted-preserved |
| SplitHTTPConfig | seqPlacement | string | client | accepted-preserved |
| SplitHTTPConfig | seqKey | string | client | accepted-preserved |
| SplitHTTPConfig | uplinkDataPlacement | string | client | accepted-preserved |
| SplitHTTPConfig | uplinkDataKey | string | client | accepted-preserved |
| SplitHTTPConfig | uplinkChunkSize | Int32Range | client | accepted-preserved |
| SplitHTTPConfig | noGRPCHeader | bool | client | accepted-preserved |
| SplitHTTPConfig | noSSEHeader | bool | client | accepted-preserved |
| SplitHTTPConfig | scMaxEachPostBytes | Int32Range | client | accepted-preserved |
| SplitHTTPConfig | scMinPostsIntervalMs | Int32Range | client | accepted-preserved |
| SplitHTTPConfig | scMaxBufferedPosts | int64 | client | accepted-preserved |
| SplitHTTPConfig | scStreamUpServerSecs | Int32Range | client | accepted-preserved |
| SplitHTTPConfig | serverMaxHeaderBytes | int32 | server setting preserved; not client behavior proof | accepted-preserved |
| SplitHTTPConfig | xmux | XmuxConfig | client | accepted-preserved |
| SplitHTTPConfig | downloadSettings | *StreamConfig | client | accepted-preserved |
| SplitHTTPConfig | h2Params | json.RawMessage | server-only | excluded-server |
| SplitHTTPConfig | extra | json.RawMessage | client | accepted-transformed |
| XmuxConfig | maxConcurrency | Int32Range | client | accepted-preserved |
| XmuxConfig | maxConnections | Int32Range | client | accepted-preserved |
| XmuxConfig | cMaxReuseTimes | Int32Range | client | accepted-preserved |
| XmuxConfig | hMaxRequestTimes | Int32Range | client | accepted-preserved |
| XmuxConfig | hMaxReusableSecs | Int32Range | client | accepted-preserved |
| XmuxConfig | hKeepAlivePeriod | int64 | client | accepted-preserved |
| KCPConfig | mtu | *uint32 | client | accepted-preserved |
| KCPConfig | tti | *uint32 | client | accepted-preserved |
| KCPConfig | uplinkCapacity | *uint32 | client | accepted-preserved |
| KCPConfig | downlinkCapacity | *uint32 | client | accepted-preserved |
| KCPConfig | cwndMultiplier | *uint32 | client | accepted-preserved |
| KCPConfig | maxSendingWindow | *uint32 | client | accepted-preserved |
| KCPConfig | header | json.RawMessage | client | accepted-transformed |
| KCPConfig | seed | *string | client | accepted-transformed |
| GRPCConfig | authority | string | client | accepted-preserved |
| GRPCConfig | serviceName | string | client | accepted-preserved |
| GRPCConfig | multiMode | bool | client | accepted-preserved |
| GRPCConfig | idle_timeout | int32 | client | accepted-preserved |
| GRPCConfig | health_check_timeout | int32 | client | accepted-preserved |
| GRPCConfig | permit_without_stream | bool | client | accepted-preserved |
| GRPCConfig | initial_windows_size | int32 | client | accepted-preserved |
| GRPCConfig | user_agent | string | client | accepted-preserved |
| WebSocketConfig | host | string | client | accepted-preserved |
| WebSocketConfig | path | string | client | accepted-preserved |
| WebSocketConfig | headers | map[string]string | client | accepted-preserved |
| WebSocketConfig | acceptProxyProtocol | bool | server-only | excluded-server |
| WebSocketConfig | heartbeatPeriod | uint32 | client | accepted-preserved |
| HttpUpgradeConfig | host | string | client | accepted-preserved |
| HttpUpgradeConfig | path | string | client | accepted-preserved |
| HttpUpgradeConfig | headers | map[string]string | client | accepted-preserved |
| HttpUpgradeConfig | acceptProxyProtocol | bool | server-only | excluded-server |
| Masquerade | type | string | server-only | excluded-server |
| Masquerade | dir | string | server-only | excluded-server |
| Masquerade | url | string | server-only | excluded-server |
| Masquerade | rewriteHost | bool | server-only | excluded-server |
| Masquerade | xForwarded | bool | server-only | excluded-server |
| Masquerade | insecure | bool | server-only | excluded-server |
| Masquerade | content | string | server-only | excluded-server |
| Masquerade | headers | map[string]string | server-only | excluded-server |
| Masquerade | statusCode | int32 | server-only | excluded-server |
| HysteriaConfig | version | int32 | client | accepted-preserved |
| HysteriaConfig | auth | string | client | accepted-preserved |
| HysteriaConfig | udpIdleTimeout | int64 | client | accepted-preserved |
| HysteriaConfig | masquerade | Masquerade | client | excluded-server |
| MasqueWarpConfig | privateKey | string | client | accepted-preserved-native-owned |
| MasqueWarpConfig | publicKey | string | client | accepted-preserved-native-owned |
| MasqueWarpConfig | address | []string | client | accepted-preserved-native-owned |
| MasqueConfig | host | string | client | accepted-preserved-native-owned |
| MasqueConfig | path | string | client | accepted-preserved-native-owned |
| MasqueConfig | user | string | client | accepted-preserved-native-owned |
| MasqueConfig | pass | string | client | accepted-preserved-native-owned |
| MasqueConfig | headers | map[string]string | client | accepted-preserved-native-owned |
| MasqueConfig | warp | *MasqueWarpConfig | client | accepted-preserved-native-owned |
| MasqueConfig | xmux | XmuxConfig | client | accepted-preserved-native-owned |
| XDriveConfig | remoteFolder | string | client | accepted-preserved-native-owned |
| XDriveConfig | service | string | client | accepted-preserved-native-owned |
| XDriveConfig | secrets | []string | client | accepted-preserved-native-owned |
| XDriveConfig | segmentBytes | uint32 | client | accepted-preserved-native-owned |
| XDriveConfig | flushIntervalMs | uint32 | client | accepted-preserved-native-owned |
| XDriveConfig | pollIntervalMs | uint32 | client | accepted-preserved-native-owned |
| XDriveConfig | maxPollIntervalMs | uint32 | client | accepted-preserved-native-owned |
| XDriveConfig | sessionTtlSeconds | uint32 | client | accepted-preserved-native-owned |
| XDriveConfig | concurrency | uint32 | client | accepted-preserved-native-owned |
| XDriveConfig | eagerWindowMs | uint32 | client | accepted-preserved-native-owned |
| XDriveConfig | holeTimeoutMs | uint32 | client | accepted-preserved-native-owned |
| XDriveConfig | template | json.RawMessage | client | accepted-preserved-native-owned |
| XDriveConfig | s3 | *XDriveS3Config | client | accepted-preserved-native-owned |
| XDriveConfig | xmux | XmuxConfig | client | accepted-preserved-native-owned |
| XDriveS3Config | endpoint | string | client | accepted-preserved-native-owned |
| XDriveS3Config | region | string | client | accepted-preserved-native-owned |
| XDriveS3Config | bucket | string | client | accepted-preserved-native-owned |
| XDriveS3Config | pathStyle | *bool | client | accepted-preserved-native-owned |
| authTemplate | type | string | client | accepted-preserved-native-owned |
| authTemplate | header | map[string]string | client | accepted-preserved-native-owned |
| authTemplate | username | string | client | accepted-preserved-native-owned |
| authTemplate | password | string | client | accepted-preserved-native-owned |
| authTemplate | tokenUrl | string | client | accepted-preserved-native-owned |
| authTemplate | form | map[string]string | client | accepted-preserved-native-owned |
| authTemplate | tokenPath | string | client | accepted-preserved-native-owned |
| authTemplate | expiryPath | string | client | accepted-preserved-native-owned |
| opTemplate | method | string | client | accepted-preserved-native-owned |
| opTemplate | url | string | client | accepted-preserved-native-owned |
| opTemplate | headers | map[string]string | client | accepted-preserved-native-owned |
| opTemplate | body | string | client | accepted-preserved-native-owned |
| opTemplate | namesRegex | string | client | accepted-preserved-native-owned |
| opTemplate | hrefPath | string | client | accepted-preserved-native-owned |
| opTemplate | hrefMethod | string | client | accepted-preserved-native-owned |
| retryTemplate | status | []int | client | accepted-preserved-native-owned |
| retryTemplate | rateReason | string | client | accepted-preserved-native-owned |
| storageTemplate | flatten | bool | client | accepted-preserved-native-owned |
| storageTemplate | concurrency | int | client | accepted-preserved-native-owned |
| storageTemplate | auth | authTemplate | client | accepted-preserved-native-owned |
| storageTemplate | put | opTemplate | client | accepted-preserved-native-owned |
| storageTemplate | get | opTemplate | client | accepted-preserved-native-owned |
| storageTemplate | delete | opTemplate | client | accepted-preserved-native-owned |
| storageTemplate | list | opTemplate | client | accepted-preserved-native-owned |
| storageTemplate | retry | retryTemplate | client | accepted-preserved-native-owned |
| LimitFallback | AfterBytes | uint64 | REALITY fallback limiter | excluded-server |
| LimitFallback | BytesPerSec | uint64 | REALITY fallback limiter | excluded-server |
| LimitFallback | BurstBytesPerSec | uint64 | REALITY fallback limiter | excluded-server |
| Realm.PortMapping | enabled | bool | FinalMask realm | accepted-passthrough-unvalidated |
| Realm.PortMapping | timeout | int64 | FinalMask realm | accepted-passthrough-unvalidated |
| Realm.PortMapping | lifetime | int64 | FinalMask realm | accepted-passthrough-unvalidated |
| HTTPAccount | user | string | client | accepted-or-equivalent |
| HTTPAccount | pass | string | client | accepted-or-equivalent |
| HTTPRemoteConfig | address | *Address | client | accepted-or-equivalent |
| HTTPRemoteConfig | port | uint16 | client | accepted-or-equivalent |
| HTTPRemoteConfig | users | []json.RawMessage | client | accepted-or-equivalent |
| HTTPClientConfig | address | *Address | client | accepted-or-equivalent |
| HTTPClientConfig | port | uint16 | client | accepted-or-equivalent |
| HTTPClientConfig | level | uint32 | client | metadata-warning-subset |
| HTTPClientConfig | email | string | client | metadata-warning-subset |
| HTTPClientConfig | user | string | client | accepted-or-equivalent |
| HTTPClientConfig | pass | string | client | accepted-or-equivalent |
| HTTPClientConfig | servers | []*HTTPRemoteConfig | client | accepted-or-equivalent |
| HTTPClientConfig | headers | map[string]string | client | rejected |
| SocksAccount | user | string | client | accepted-or-equivalent |
| SocksAccount | pass | string | client | accepted-or-equivalent |
| SocksRemoteConfig | address | *Address | client | accepted-or-equivalent |
| SocksRemoteConfig | port | uint16 | client | accepted-or-equivalent |
| SocksRemoteConfig | users | []json.RawMessage | client | accepted-or-equivalent |
| SocksClientConfig | address | *Address | client | accepted-or-equivalent |
| SocksClientConfig | port | uint16 | client | accepted-or-equivalent |
| SocksClientConfig | level | uint32 | client | metadata-warning-subset |
| SocksClientConfig | email | string | client | metadata-warning-subset |
| SocksClientConfig | user | string | client | accepted-or-equivalent |
| SocksClientConfig | pass | string | client | accepted-or-equivalent |
| SocksClientConfig | servers | []*SocksRemoteConfig | client | accepted-or-equivalent |
| VLessOutboundVnext | address | *Address | client | accepted-or-equivalent |
| VLessOutboundVnext | port | uint16 | client | accepted-or-equivalent |
| VLessOutboundVnext | users | []json.RawMessage | client | accepted-or-equivalent |
| VLessOutboundConfig | address | *Address | client | accepted-or-equivalent |
| VLessOutboundConfig | port | uint16 | client | accepted-or-equivalent |
| VLessOutboundConfig | level | uint32 | client | metadata-warning-subset |
| VLessOutboundConfig | email | string | client | metadata-warning-subset |
| VLessOutboundConfig | id | string | client | accepted-or-equivalent |
| VLessOutboundConfig | flow | string | client | accepted-or-equivalent |
| VLessOutboundConfig | seed | string | client | rejected |
| VLessOutboundConfig | encryption | string | client | accepted-or-equivalent |
| VLessOutboundConfig | reverse | *VLessReverseConfig | client | rejected |
| VLessOutboundConfig | testpre | uint32 | client | rejected |
| VLessOutboundConfig | testseed | []uint32 | client | rejected |
| VLessOutboundConfig | vnext | []*VLessOutboundVnext | client | accepted-or-equivalent |
| VMessAccount | id | string | client | accepted-or-equivalent |
| VMessAccount | security | string | client | accepted-or-equivalent |
| VMessAccount | experiments | string | client | accepted-empty-only |
| VMessOutboundTarget | address | *Address | client | accepted-or-equivalent |
| VMessOutboundTarget | port | uint16 | client | accepted-or-equivalent |
| VMessOutboundTarget | users | []json.RawMessage | client | accepted-or-equivalent |
| VMessOutboundConfig | address | *Address | client | accepted-or-equivalent |
| VMessOutboundConfig | port | uint16 | client | accepted-or-equivalent |
| VMessOutboundConfig | level | uint32 | client | metadata-warning-subset |
| VMessOutboundConfig | email | string | client | metadata-warning-subset |
| VMessOutboundConfig | id | string | client | accepted-or-equivalent |
| VMessOutboundConfig | security | string | client | accepted-or-equivalent |
| VMessOutboundConfig | experiments | string | client | accepted-empty-only |
| VMessOutboundConfig | vnext | []*VMessOutboundTarget | client | accepted-or-equivalent |
| TrojanServerTarget | address | *Address | client | accepted-or-equivalent |
| TrojanServerTarget | port | uint16 | client | accepted-or-equivalent |
| TrojanServerTarget | level | byte | client | metadata-warning-subset |
| TrojanServerTarget | email | string | client | metadata-warning-subset |
| TrojanServerTarget | password | string | client | accepted-or-equivalent |
| TrojanServerTarget | flow | string | client | accepted-empty-only |
| TrojanClientConfig | address | *Address | client | accepted-or-equivalent |
| TrojanClientConfig | port | uint16 | client | accepted-or-equivalent |
| TrojanClientConfig | level | byte | client | metadata-warning-subset |
| TrojanClientConfig | email | string | client | metadata-warning-subset |
| TrojanClientConfig | password | string | client | accepted-or-equivalent |
| TrojanClientConfig | flow | string | client | accepted-empty-only |
| TrojanClientConfig | servers | []*TrojanServerTarget | client | accepted-or-equivalent |
| ShadowsocksServerTarget | address | *Address | client | accepted-or-equivalent |
| ShadowsocksServerTarget | port | uint16 | client | accepted-or-equivalent |
| ShadowsocksServerTarget | level | byte | client | metadata-warning-subset |
| ShadowsocksServerTarget | email | string | client | metadata-warning-subset |
| ShadowsocksServerTarget | method | string | client | accepted-or-equivalent |
| ShadowsocksServerTarget | password | string | client | accepted-or-equivalent |
| ShadowsocksClientConfig | address | *Address | client | accepted-or-equivalent |
| ShadowsocksClientConfig | port | uint16 | client | accepted-or-equivalent |
| ShadowsocksClientConfig | level | byte | client | metadata-warning-subset |
| ShadowsocksClientConfig | email | string | client | metadata-warning-subset |
| ShadowsocksClientConfig | method | string | client | accepted-or-equivalent |
| ShadowsocksClientConfig | password | string | client | accepted-or-equivalent |
| ShadowsocksClientConfig | servers | []*ShadowsocksServerTarget | client | accepted-or-equivalent |
| WireGuardPeerConfig | publicKey | string | client | accepted-or-equivalent |
| WireGuardPeerConfig | preSharedKey | string | client | accepted-or-equivalent |
| WireGuardPeerConfig | endpoint | string | client | accepted-or-equivalent |
| WireGuardPeerConfig | keepAlive | uint32 | client | accepted-or-equivalent |
| WireGuardPeerConfig | allowedIPs | []string | client | accepted-or-equivalent |
| WireGuardPeerConfig | level | uint32 | client | metadata-warning-subset |
| WireGuardPeerConfig | email | string | client | metadata-warning-subset |
| WireGuardConfig | noKernelTun | bool | client | accepted-or-equivalent |
| WireGuardConfig | secretKey | string | client | accepted-or-equivalent |
| WireGuardConfig | address | []string | client | accepted-or-equivalent |
| WireGuardConfig | peers | []*WireGuardPeerConfig | client | accepted-or-equivalent |
| WireGuardConfig | mtu | int32 | client | accepted-or-equivalent |
| WireGuardConfig | reserved | []byte | client | accepted-or-equivalent |
| WireGuardConfig | remoteDNS | []string | client | accepted-or-equivalent |
| HysteriaClientConfig | version | int32 | client | accepted-or-equivalent |
| HysteriaClientConfig | address | *Address | client | accepted-or-equivalent |
| HysteriaClientConfig | port | uint16 | client | accepted-or-equivalent |
| MasqueClientConfig | address | *Address | client | accepted-or-equivalent |
| MasqueClientConfig | port | uint16 | client | accepted-or-equivalent |
| MasqueClientConfig | remoteDNS | []string | client | accepted-or-equivalent |
| MuxConfig | enabled | bool | client | accepted-xdrive-only |
| MuxConfig | concurrency | int16 | client | accepted-xdrive-only |
| MuxConfig | xudpConcurrency | int16 | client | accepted-xdrive-only |
| MuxConfig | xudpProxyUDP443 | string | client | accepted-xdrive-only |
| OutboundDetourConfig | protocol | string | client | accepted-or-equivalent |
| OutboundDetourConfig | sendThrough | *string | client | rejected |
| OutboundDetourConfig | tag | string | client | accepted-or-equivalent |
| OutboundDetourConfig | settings | *json.RawMessage | client | accepted-or-equivalent |
| OutboundDetourConfig | streamSettings | *StreamConfig | client | accepted-or-equivalent |
| OutboundDetourConfig | proxySettings | *json.RawMessage | client | rejected |
| OutboundDetourConfig | mux | *MuxConfig | client | accepted-xdrive-only |
| OutboundDetourConfig | targetStrategy | string | client | rejected |
