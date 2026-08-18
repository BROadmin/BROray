#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
export LC_ALL=C TZ=UTC

MODE="${1:-diagnostic}"
OUT="${2:-$PWD/out}"
BASE='https://api.brovibe.cloud/releases/staging/broray/3.0.0-r14c18'
EXPECTED_PUBLIC_MANIFEST='46ae4a8fd09eddf8aaea1b7f31c041642545bd45b169d2d1c52f8742d89e3c7e'
EXPECTED_APP='40db147e707b3cc929b8ff221413fbc587fdef4adcf108eea070b5c0c7af5ec1'
EXPECTED_IPK='d5e873d121a69be5882330a85b0c4644d79d84c715d563d382637f19535f0a9a'
EXPECTED_RELEASE='992536fb50a37f0023f72b39b44ea1aa2d74bed2c8e5a9add224f68c6c3c2d67'

fail(){ printf 'ERROR: %s\n' "$*" >&2; exit 1; }
sha(){ sha256sum "$1" | awk 'NR==1{print $1;exit}'; }

rm -rf -- "$OUT"
mkdir -p "$OUT/publication" "$OUT/extracted/app" "$OUT/extracted/ipk" "$OUT/evidence"

fetch(){
  local rel="$1" dst="$2"
  curl -q --proto '=https' --proto-redir '=https' --tlsv1.2 \
    --connect-timeout 20 --max-time 600 --retry 4 --retry-delay 2 \
    --retry-connrefused -fsSL -H 'Accept-Encoding: identity' \
    "$BASE/$rel?build=r14c19" -o "$dst"
}

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

# IPK containers used by Entware may be either ar or tar based. Inspect both safely.
if ar t "$IPK" >/dev/null 2>&1; then
  (cd "$OUT/extracted/ipk" && ar x "$OLDPWD/$IPK")
else
  python3 - "$IPK" <<'PY'
import sys, tarfile
from pathlib import PurePosixPath
p=sys.argv[1]
with tarfile.open(p,'r:*') as tf:
    for m in tf.getmembers():
        x=PurePosixPath(m.name)
        if x.is_absolute() or '..' in x.parts or not (m.isdir() or m.isfile()):
            raise SystemExit(f'unsafe ipk member: {m.name}')
PY
  tar -xf "$IPK" --no-same-owner -C "$OUT/extracted/ipk"
fi

find "$OUT/extracted/app" -printf '%y %m %s %p\n' | sort > "$OUT/evidence/r14c18-app-tree.txt"
find "$OUT/extracted/ipk" -printf '%y %m %s %p\n' | sort > "$OUT/evidence/r14c18-ipk-tree.txt"
grep -RIn --binary-files=without-match '/dev/stdin' "$OUT/extracted/app" > "$OUT/evidence/dev-stdin-occurrences.txt" || true
grep -RIn --binary-files=without-match -E 'requestStop|stop\.cgi|DNS-over-TLS|data-page="dns"|href="dns\.html"' "$OUT/extracted/app" > "$OUT/evidence/webui-known-markers.txt" || true

{
  echo 'MODE='"$MODE"
  echo 'R14C18_PUBLIC_MANIFEST=PASS'
  echo 'R14C18_APP_SHA256='"$(sha "$APP")"
  echo 'R14C18_IPK_SHA256='"$(sha "$IPK")"
  echo 'R14C18_RELEASE_SHA256='"$(sha "$REL")"
  echo 'PRODUCTION_CGI_DEV_STDIN_OCCURRENCES='"$(wc -l < "$OUT/evidence/dev-stdin-occurrences.txt" | tr -d ' ')"
  echo 'DIAGNOSTIC_EXTRACTION=PASS'
} | tee "$OUT/BUILD-RESULT.txt"

# Keep the diagnostic artifact reasonably small while retaining exact final bytes.
tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner --format=gnu \
  -czf "$OUT/r14c18-extracted-app.tar.gz" -C "$OUT/extracted/app" .
sha256sum "$OUT/r14c18-extracted-app.tar.gz" > "$OUT/r14c18-extracted-app.tar.gz.sha256"
