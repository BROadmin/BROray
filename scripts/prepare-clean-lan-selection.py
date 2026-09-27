"""Build-time edit of a verified clean postinst; never execute/install/sign it.

The clean bootstrap embeds postinst separately from the app archive. Updating
package-setup alone cannot fix its earlier LAN gate. Preserve the historical
transaction byte-for-byte outside this single pre-mutation selection block.
"""
import argparse,hashlib,json
from pathlib import Path

OLD=b'''mkdir -m 700 "$TMP/network" || fail 'cannot create private LAN-detection workspace'
PREMUTATION_LAN="$({
    BRORAY_ROOT="$APP_TREE/app" \\
    BRORAY_NETWORK_ROOT="$APP_TREE/app" \\
    BRORAY_NETWORK_TMP_ROOT="$TMP/network" \\
    /opt/bin/ash -c '. "$1"; broray_detect_lan_ip' _ "$APP_TREE/app/lib/network.sh"
} 2>"$TMP/network-preflight.err")" || {
    sed -n '/^BRORAY_LAN_DIAG /p' "$TMP/network-preflight.err" >&2 || true
    fail 'cannot prove a unique private-interface LAN-IP before mutation'
}
[ -n "$PREMUTATION_LAN" ] || fail 'empty LAN-IP preflight result'
'''
NEW=b'''mkdir -m 700 "$TMP/network" || fail 'cannot create private LAN-detection workspace'
# LAN-INSTALL-01: explicit install-only dialog before persistent mutation.
# stdout contains only the admitted address; the menu uses the controlling tty.
PREMUTATION_LAN="$(
    BRORAY_ROOT="$APP_TREE/app" \\
    BRORAY_NETWORK_ROOT="$APP_TREE/app" \\
    BRORAY_NETWORK_TMP_ROOT="$TMP/network" \\
    /opt/bin/ash -c '. "$1"; broray_network_select install' _ "$APP_TREE/app/lib/network.sh"
)" || fail 'LAN selection cancelled or unavailable before mutation'
[ -n "$PREMUTATION_LAN" ] || fail 'empty LAN-IP preflight result'
# Every subsequent network read revalidates this choice. Existing setup writes
# settings.listenAddress and uses the same address for Xray/ProxyN/WebUI.
BRORAY_LAN_IP_OVERRIDE="$PREMUTATION_LAN"
export BRORAY_LAN_IP_OVERRIDE
'''

def integrate(source,expected_sha256):
    if hashlib.sha256(source).hexdigest()!=expected_sha256:raise ValueError('CLEAN_POSTINST_SOURCE_MISMATCH')
    if source.count(OLD)!=1 or b'LAN-INSTALL-01' in source:raise ValueError('CLEAN_POSTINST_LAN_BOUNDARY_MISMATCH')
    if source.index(OLD)>source.index(b'MUTATION_STARTED=true'):raise ValueError('CLEAN_POSTINST_LAN_TOO_LATE')
    return source.replace(OLD,NEW,1)

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--postinst',type=Path,required=True);p.add_argument('--source-sha256',required=True);p.add_argument('--output',type=Path,required=True)
    a=p.parse_args()
    if not a.postinst.is_file() or a.postinst.is_symlink():raise ValueError('CLEAN_POSTINST_INPUT_UNSAFE')
    result=integrate(a.postinst.read_bytes(),a.source_sha256)
    with a.output.open('xb') as f:f.write(result)
    print(json.dumps(dict(status='PREPARED_UNSIGNED',sourceSha256=a.source_sha256,postinstSha256=hashlib.sha256(result).hexdigest(),runtimeRequirement='network.sh with broray_network_select install; include before calculating IPK/bootstrap hashes and signatures',installed=False,published=False)))

if __name__=='__main__':main()
