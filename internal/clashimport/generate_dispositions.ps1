# Regenerate the field accounting from the pinned schema declaration, never
# from observed successful parses. This file performs no network or build work.
$registry = Get-Content -LiteralPath (Join-Path $PSScriptRoot '../clashschema/registry.go') -Raw
$sourceCommit = [regex]::Match($registry, 'const SourceCommit = "([^"]+)"').Groups[1].Value
$protocolText = [regex]::Match($registry, '(?s)var protocolSchemas = map\[string\]string\{(.*?)\n\}').Groups[1].Value
$protocols = [ordered]@{}
foreach ($match in [regex]::Matches($protocolText, '"([^"]+)":\s+"([^"]+)"')) { $protocols[$match.Groups[1].Value] = $match.Groups[2].Value }
$mappedRoots = @('VlessOption','VmessOption','TrojanOption','ShadowSocksOption','HttpOption','Socks5Option','Hysteria2Option')
$coreRoots = @('AnyTLSOption','EasyTierOption','GostRelayOption','HysteriaOption','MieruOption','OpenVPNOption','ShadowQuicOption','SnellOption','SshOption','ShadowSocksROption','SudokuOption','TailscaleOption','TrustTunnelOption','TuicOption','ZeroTierOption')
$directRoot = 'name type server port uuid encryption flow cipher alterId password username headers udp ip-version tfo mptcp interface-name routing-mark tls servername sni client-fingerprint fingerprint name-cert-verify alpn skip-cert-verify network' -split ' '
$semanticFields = 'name server port uuid password username cipher protocol obfs version psk key network ip ipv6 ipv4 ip-version mode congestion-controller transport port-range udp-over-stream-version udp-relay-mode fingerprint name-cert-verify certificate private-key ca cert host-key tran-window handshake-timeout padding-min padding-max aead-method table-type quic-versions control-url' -split ' '
$mappedNested = @('HTTPOptions','XHTTPReuseSettings','RealityOptions')
$coreNested = @('SingMuxOption','BrutalOption','JLSOptions','RestlsOptions','ShadowTLSOptions','TLSMirrorOptions','TLSMirrorConnectionEnrolment','TLSMirrorTimeSpec','TLSMirrorTrafficGenerator','TLSMirrorTrafficStep','TLSMirrorTrafficHeader','TLSMirrorTrafficTransferCandidate','TLSMirrorTransportLayerPadding','MekyaOptions','TrojanSSOption','kcpTunOption','simpleObfsOption','shadowTLSOption','restlsOption','jlsOption')
$entries = [System.Collections.Generic.List[object]]::new()
foreach ($block in [regex]::Matches($registry, '(?ms)^\t"([^"]+)": \{\r?\n(.*?)^\t\},')) {
    $schema = $block.Groups[1].Value
    foreach ($field in [regex]::Matches($block.Groups[2].Value, '"([^"]+)":\s+"([^"]+)"')) {
        $name = $field.Groups[1].Value
        $declaredType = $field.Groups[2].Value
        $disposition = 'preserved-conversion-unimplemented'
        $validation = 'declared-shape-only'
        $condition = 'No runtime equivalence claimed; complete typed source is retained privately.'
        if ($schema -in $coreRoots -or $schema -in $coreNested) {
            $disposition = 'preserved-core-unsupported'
            $condition = 'Backend or enclosing wire protocol has no implemented Xray equivalent; no runnable projection.'
        }
        if ($schema -in $mappedRoots -and $name -in $directRoot) {
            $disposition = 'conditional-direct-mapping'
            $validation = 'shared-Xray-importer-and-record-validation'
            $condition = 'Applies only when the complete node is convertible. IP preference remains unavailable; insecure TLS is removed by the pinned core. Credentials depend on source protocol.'
        }
        if ($schema -in $mappedRoots -and $name -in @('ws-opts','grpc-opts','xhttp-opts','http-upgrade-opts','http-opts','mkcp-opts','reality-opts')) { $disposition = 'conditional-transport-or-security-container'; $validation = 'active-context-and-nested-source-target-validation'; $condition = 'Container is converted in its matching source network/security context. Nested declarations carry their own conditions; unavailable nested settings disable the original complete node.' }
        if ($schema -in $mappedNested -or ($schema -eq 'XHTTPOptions' -and $name -ne 'download-settings') -or ($schema -eq 'GrpcOptions' -and $name -in @('grpc-service-name','grpc-user-agent')) -or ($schema -eq 'WSOptions' -and $name -in @('path','headers','v2ray-http-upgrade')) -or ($schema -eq 'MKCPOptions' -and $name -in @('mtu','tti','uplink-capacity','downlink-capacity','seed','header'))) {
            $disposition = 'conditional-direct-mapping'
            $validation = 'shared-Xray-importer-and-record-validation'
            $condition = 'Only in the matching mapped transport; inactive/mismatched transport fields reject. Parent capability can disable the complete node.'
        }
        if ($schema -in @('WireGuardOption','WireGuardPeerOption','AmneziaWGOption','IPStackOption')) {
            $disposition = 'source-dependent-WG-or-AWG'
            $validation = 'typed-profile-and-source-projection-consistency'
            $condition = 'Plain WG semantic projection maps supported fields; differing reserved/DNS resolver/socket/stack extensions and all AWG remain private with explicit runtime refusal.'
        }
        if (($schema -in $mappedRoots -or $schema -eq 'WireGuardOption') -and $name -eq 'udp') { $disposition = 'conditional-policy-preservation'; $validation = 'known-source-validation-before-policy-classification'; $condition = 'Explicit false retains the original private node as local-policy unavailable; no runnable UDP-capable projection is emitted.' }
        if ($schema -in $mappedRoots -and $name -in @('interface-name','routing-mark')) { $disposition = 'conditional-local-control'; $validation = 'shared-Xray-source-classification'; $condition = 'Nonempty interface or nonzero mark retains original Clash node with local-control refusal; empty/zero remains convertible.' }
        if ($schema -eq 'ShadowSocksOption' -and $name -in @('cipher','password')) { $validation = 'pinned-source-cipher-registry-and-key-semantics'; $condition = 'Exact 39-name source registry; SS2022 base64/key lengths and AES-only key chains validated. Seven cipher names map, including the XChaCha alias; other 32 source ciphers are core-unsupported.' }
        if ($schema -eq 'ShadowSocksOption' -and $name -in @('plugin','plugin-opts')) { $disposition = 'conditional-plugin-WS'; $validation = 'source-plugin-and-shared-target-validation'; $condition = 'v2ray-plugin websocket with explicit mux=false and mapped TLS/headers converts; default/enabled mux and remaining plugin modes retain complete source unavailable.' }
        if ($schema -eq 'ECHOptions' -or ($schema -in $mappedRoots -and $name -eq 'ech-opts')) { $disposition = 'conditional-static-ECH'; $validation = 'base64-source-shape-and-target-TLS-validation'; $condition = 'Enabled static config maps to echConfigList; disabled source is inactive. Dynamic resolver/query-server-name behavior remains unavailable. Static config overrides query-server-name in source.' }
        if ($schema -eq 'WSOptions' -and $name -in @('max-early-data','early-data-header-name','v2ray-http-upgrade-fast-open')) { $disposition = 'conditional-compatible-WS-early-data'; $validation = 'source-header-mode-and-range'; $condition = 'Sec-WebSocket-Protocol early data maps to the target ed query; arbitrary header/path-suffix and fast-open modes remain unavailable.' }
        if (($schema -eq 'XHTTPOptions' -and $name -eq 'download-settings') -or $schema -eq 'XHTTPDownloadSettings') { $disposition = 'conditional-download-stream'; $validation = 'source-inheritance-and-shared-target-validation'; $condition = 'Explicit address/port and inherited upload HTTP/TLS options become nested Xray download stream; wrappers, dynamic ECH, mTLS and other unavailable context disable the whole original node.' }
        if ($schema -eq 'Hysteria2Option' -and $name -in @('up','down','bbr-profile','ports','hop-interval','obfs','obfs-password','obfs-min-packet-size','obfs-max-packet-size','initial-stream-receive-window','max-stream-receive-window','initial-connection-receive-window','max-connection-receive-window')) { $disposition = 'conditional-HY2-finalmask'; $validation = 'source-units-ranges-and-target-finalmask'; $condition = 'QUIC receive windows/bandwidth/BBR, salamander/gecko and port hopping map through existing finalmask implementations. Remaining HY2 fields remain explicit unavailable.' }
        if ($schema -eq 'Hysteria2Option' -and $name -eq 'sni') { $condition = 'Maps to TLS serverName; source empty/omitted SNI defaults explicitly to server before private TLS fields are attached.' }
        if ($schema -eq 'MKCPOptions' -and $name -in @('congestion','read-buffer','write-buffer')) { $disposition = 'preserved-core-unsupported'; $condition = 'Removed legacy Xray mKCP tuning has no proven current equivalent.' }
        if ($name -in @('certificate','private-key') -and $schema -in $mappedRoots) { $disposition = 'preserved-core-unsupported-or-reference'; $validation = 'inline-PEM-key-agreement-or-unresolved-reference'; $condition = 'Ordinary Xray TLS client mTLS unavailable; source-local file references are not read.' }
        if ($schema -in $coreRoots -and $name -in $semanticFields) { $validation = 'adapter-semantic-checks-see-remote_semantic.go' }
        if ($schema -eq 'MasqueOption') { $condition = 'Basic H3 WARP maps DER keys/addresses, SNI/URI and endpoint through native Xray. Other network, MTU, stack, resolver and congestion contexts remain unavailable, not an absent-core claim.'; if ($name -in @('name','type','server','port','private-key','public-key','ip','ipv6','sni','uri','network','udp','tfo','mptcp','interface-name','routing-mark')) { $disposition = 'conditional-native-MASQUE-WARP'; $validation = 'source-DER-address-and-native-target-validation' } }
        if ($schema -eq 'v2rayObfsOption') { $disposition = 'conditional-plugin-WS'; $validation = 'source-plugin-and-shared-target-validation'; $condition = 'Only websocket with explicit mux=false and disabled upgrade/fast-open maps via the WS transport. TLS, headers, host and path retain semantic identity and private masking; unavailable enclosing context disables the complete source.' }
        if ($schema -in @('v2rayObfsOption','XHTTPDownloadSettings') -and $name -in @('certificate','private-key')) { $disposition = 'preserved-core-unsupported-or-reference'; $validation = 'inline-PEM-key-agreement-or-unresolved-reference'; $condition = 'An active ordinary TLS mTLS pair is unavailable in the pinned core; unresolved source-local file references remain private and are not read.' }
        if ($schema -eq 'XHTTPDownloadSettings' -and $name -in @('shadow-tls-opts','restls-opts','jls-opts')) { $disposition = 'preserved-core-unsupported'; $validation = 'nested-source-shape-and-selected-security-semantics'; $condition = 'An active wrapper disables the entire original node; the ordinary download mapping must not strip it.' }
        if ($schema -in @('v2rayObfsOption','XHTTPDownloadSettings') -and $name -eq 'ech-opts') { $disposition = 'conditional-static-ECH'; $validation = 'base64-source-shape-and-target-TLS-validation'; $condition = 'Static ECH in active TLS maps; dynamic lookup or incompatible security context retains the complete source unavailable.' }
        if ($schema -eq 'MasqueOption' -and $name -eq 'udp') { $disposition = 'conditional-policy-preservation'; $validation = 'known-source-validation-before-policy-classification'; $condition = 'Explicit false retains the original private node as local-policy unavailable; true/omitted is eligible for basic WARP mapping.' }
        if ($schema -eq 'MasqueOption' -and $name -in @('interface-name','routing-mark')) { $disposition = 'conditional-local-control'; $validation = 'shared-Xray-source-classification'; $condition = 'Nonempty interface/nonzero mark retains original Clash source with local-control refusal; empty/zero remains eligible for basic WARP.' }
        if (($schema -in $mappedRoots -or $schema -in @('MasqueOption','v2rayObfsOption','XHTTPDownloadSettings')) -and $name -eq 'skip-cert-verify') { $disposition = 'conditional-core-removed'; $validation = 'source-boolean'; $condition = 'In an active mapped TLS context, true is removed by pinned Xray 26.11.9; false is accepted. No insecure runnable projection.' }
        $entries.Add([ordered]@{schema=$schema;field=$name;declaredType=$declaredType;disposition=$disposition;validation=$validation;condition=$condition})
    }
}
$report = [ordered]@{sourceCommit=$sourceCommit;kind='declaration-dispositions-not-runtime-certification';protocolSchemas=$protocols;fieldCount=$entries.Count;fields=$entries}
$json = $report | ConvertTo-Json -Depth 12
[System.IO.File]::WriteAllText((Join-Path $PSScriptRoot 'field_dispositions.json'), $json + "`n", [System.Text.UTF8Encoding]::new($false))
Write-Output ('Recorded ' + $entries.Count + ' declared fields across ' + $protocols.Count + ' registered server types.')
