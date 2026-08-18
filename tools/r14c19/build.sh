#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
export LC_ALL=C TZ=UTC

MODE="${1:-diagnostic}"
OUT="${2:-$PWD/out}"
BASE='https://api.brovibe.cloud/releases/staging/broray/3.0.0-r14c18'
R26_BASE='https://api.brovibe.cloud/releases/staging/broray/2.2.7-r26'
EXPECTED_PUBLIC_MANIFEST='46ae4a8fd09eddf8aaea1b7f31c041642545bd45b169d2d1c52f8742d89e3c7e'
EXPECTED_APP='40db147e707b3cc929b8ff221413fbc587fdef4adcf108eea070b5c0c7af5ec1'
EXPECTED_IPK='d5e873d121a69be5882330a85b0c4644d79d84c715d563d382637f19535f0a9a'
EXPECTED_RELEASE='992536fb50a37f0023f72b39b44ea1aa2d74bed2c8e5a9add224f68c6c3c2d67'
EXPECTED_R26_IPK='0709fa8bd371afa75b19e9a34c895fb9f7dd260851ae7bc2f882da3bd12329a0'
EXPECTED_R26_RELEASE='776b477a76a6795ec1c057991ca073b1d29ccae3c8f611c7d9290f92b84992c5'

fail(){ printf 'ERROR: %s\n' "$*" >&2; exit 1; }
sha(){ sha256sum "$1" | awk 'NR==1{print $1;exit}'; }

rm -rf -- "$OUT"
mkdir -p \
  "$OUT/publication" "$OUT/extracted/app" "$OUT/extracted/ipk" \
  "$OUT/r26/publication" "$OUT/r26/outer" "$OUT/r26/control" "$OUT/r26/data" \
  "$OUT/evidence"

fetch_url(){
  local url="$1" dst="$2"
  curl -q --proto '=https' --proto-redir '=https' --tlsv1.2 \
    --connect-timeout 20 --max-time 900 --retry 4 --retry-delay 2 \
    --retry-connrefused -fsSL -H 'Accept-Encoding: identity' \
    "$url" -o "$dst"
}
fetch(){ fetch_url "$BASE/$1?build=r14c19" "$2"; }

fetch SHA256SUMS "$OUT/publication/SHA256SUMS"
[[ "$(sha "$OUT/publication/SHA256SUMS")" == "$EXPECTED_PUBLIC_MANIFEST" ]] || fail 'r14c18 SHA256SUMS identity mismatch'
while read -r expected rel; do
  [[ -n "$rel" ]] || continue
  mkdir -p "$OUT/publication/$(dirname "$rel")"
  fetch "$rel" "$OUT/publication/$rel"
done < "$OUT/publication/SHA256SUMS"
(
  cd "$OUT/publication"
  sha256sum -c SHA256SUMS
)

APP="$OUT/publication/broray-app-3.0.0-r14c18.tar.gz"
IPK="$OUT/publication/opkg/aarch64-3.10/broray_3.0.0-r14_aarch64-3.10.ipk"
REL="$OUT/publication/release.json"
[[ "$(sha "$APP")" == "$EXPECTED_APP" ]] || fail 'r14c18 app identity mismatch'
[[ "$(sha "$IPK")" == "$EXPECTED_IPK" ]] || fail 'r14c18 IPK identity mismatch'
[[ "$(sha "$REL")" == "$EXPECTED_RELEASE" ]] || fail 'r14c18 release identity mismatch'

gzip -t "$APP"
python3 - "$APP" <<'PY'
import sys, tarfile
from pathlib import PurePosixPath
p=sys.argv[1]
with tarfile.open(p,'r:gz') as tf:
    for m in tf.getmembers():
        x=PurePosixPath(m.name)
        if x.is_absolute() or '..' in x.parts or not (m.isdir() or m.isfile()):
            raise SystemExit(f'unsafe app member: {m.name}')
PY
tar -xzf "$APP" --no-same-owner -C "$OUT/extracted/app"

if ar t "$IPK" >/dev/null 2>&1; then
  (cd "$OUT/extracted/ipk" && ar x "$OLDPWD/$IPK")
else
  tar -xf "$IPK" --no-same-owner -C "$OUT/extracted/ipk"
fi

# Exact field-proven 2.2.7-r26 functional baseline.
R26_IPK="$OUT/r26/publication/broray_2.2.7-r26_aarch64-3.10.ipk"
R26_REL="$OUT/r26/publication/release.json"
fetch_url "$R26_BASE/packages/broray_2.2.7-r26_aarch64-3.10.ipk?build=r14c19" "$R26_IPK"
fetch_url "$R26_BASE/release.json?build=r14c19" "$R26_REL"
[[ "$(sha "$R26_IPK")" == "$EXPECTED_R26_IPK" ]] || fail 'r26 IPK identity mismatch'
[[ "$(sha "$R26_REL")" == "$EXPECTED_R26_RELEASE" ]] || fail 'r26 release identity mismatch'

if ar t "$R26_IPK" >/dev/null 2>&1; then
  (cd "$OUT/r26/outer" && ar x "$OLDPWD/$R26_IPK")
else
  tar -xf "$R26_IPK" --no-same-owner -C "$OUT/r26/outer"
fi
gzip -t "$OUT/r26/outer/control.tar.gz"
gzip -t "$OUT/r26/outer/data.tar.gz"
tar -xzf "$OUT/r26/outer/control.tar.gz" --no-same-owner -C "$OUT/r26/control"
tar -xzf "$OUT/r26/outer/data.tar.gz" --no-same-owner -C "$OUT/r26/data"

find "$OUT/extracted/app" -printf '%y %m %s %p\n' | sort > "$OUT/evidence/r14c18-app-tree.txt"
find "$OUT/extracted/ipk" -printf '%y %m %s %p\n' | sort > "$OUT/evidence/r14c18-ipk-tree.txt"
find "$OUT/r26/data" -printf '%y %m %s %p\n' | sort > "$OUT/evidence/r26-data-tree.txt"
grep -RIn --binary-files=without-match '/dev/stdin' "$OUT/extracted/app" > "$OUT/evidence/dev-stdin-occurrences.txt" || true
grep -RIn --binary-files=without-match -E 'requestStop|stop\.cgi|DNS-over-TLS|data-page="dns"|href="dns\.html"' "$OUT/extracted/app" > "$OUT/evidence/webui-known-markers.txt" || true

python3 - "$OUT/extracted/app/app" "$OUT/r26/data/opt/broray" "$OUT/evidence/r14c18-r26-functional-diff.json" <<'PY'
import hashlib, json, sys
from pathlib import Path
new, old, out = map(Path, sys.argv[1:])
modules = {
  'home':['web-new/home.html','web-new/assets/js/home.js','web-new/api/home'],
  'servers':['web-new/servers.html','web-new/assets/js/servers.js','web-new/api/servers','lib/servers.sh'],
  'subscriptions':['web-new/subscriptions.html','web-new/assets/js/subscriptions.js','web-new/api/subscriptions'],
  'routes':['web-new/routes.html','web-new/routes-custom.html','web-new/routes-import.html','web-new/assets/js/routes-catalog.js','web-new/api/routes','lib/routes.sh'],
  'dot':['web-new/dns.html','web-new/assets/js/dns.js','lib/routes-dot.sh'],
  'keenetic':['web-new/keenetic.html','web-new/assets/js/keenetic.js','web-new/api/keenetic'],
  'xray':['web-new/xray.html','web-new/assets/js/xray.js','web-new/api/xray'],
  'broray':['web-new/broray.html','web-new/assets/js/broray.js','web-new/api/broray'],
}
def files(root, specs):
    result={}
    for spec in specs:
        p=root/spec
        if p.is_file():
            result[spec]=hashlib.sha256(p.read_bytes()).hexdigest()
        elif p.is_dir():
            for f in sorted(x for x in p.rglob('*') if x.is_file()):
                result[str(f.relative_to(root))]=hashlib.sha256(f.read_bytes()).hexdigest()
    return result
report=[]
for name,specs in modules.items():
    a=files(new,specs); b=files(old,specs)
    report.append({'module':name,'r14c18':a,'r26':b,'commonEqual':sorted(k for k in a.keys()&b.keys() if a[k]==b[k]),'changed':sorted(k for k in a.keys()&b.keys() if a[k]!=b[k]),'r14c18Only':sorted(a.keys()-b.keys()),'r26Only':sorted(b.keys()-a.keys())})
out.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
PY

{
  echo 'MODE='"$MODE"
  echo 'R14C18_PUBLIC_MANIFEST=PASS'
  echo 'R14C18_APP_SHA256='"$(sha "$APP")"
  echo 'R14C18_IPK_SHA256='"$(sha "$IPK")"
  echo 'R14C18_RELEASE_SHA256='"$(sha "$REL")"
  echo 'R26_IPK_SHA256='"$(sha "$R26_IPK")"
  echo 'R26_RELEASE_SHA256='"$(sha "$R26_REL")"
  echo 'PRODUCTION_CGI_DEV_STDIN_OCCURRENCES='"$(wc -l < "$OUT/evidence/dev-stdin-occurrences.txt" | tr -d ' ')"
  echo 'DIAGNOSTIC_EXTRACTION=PASS'
} | tee "$OUT/BUILD-RESULT.txt"

tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner --format=gnu \
  -czf "$OUT/r14c18-extracted-app.tar.gz" -C "$OUT/extracted/app" .
sha256sum "$OUT/r14c18-extracted-app.tar.gz" > "$OUT/r14c18-extracted-app.tar.gz.sha256"
tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner --format=gnu \
  -czf "$OUT/r26-extracted-data.tar.gz" -C "$OUT/r26/data" .
sha256sum "$OUT/r26-extracted-data.tar.gz" > "$OUT/r26-extracted-data.tar.gz.sha256"
