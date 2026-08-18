#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
export LC_ALL=C TZ=UTC
OUT="${1:-$PWD/out}"
R18='https://api.brovibe.cloud/releases/staging/broray/3.0.0-r14c18'
R1='https://api.brovibe.cloud/releases/staging/broray/3.0.0-r1'
R1_IPK_SHA='c5a114ab13804ca06ee53348c9fc94ee8bd8df604fea84a939fc6e32def8a959'
R1_IPK_SIZE='14069204'
R1_RELEASE_SHA='51bc50ea57308577524a75271592a1aa64471873959b6fcb303a29de5da790d5'
R18_MANIFEST_SHA='46ae4a8fd09eddf8aaea1b7f31c041642545bd45b169d2d1c52f8742d89e3c7e'
sha(){ sha256sum "$1"|awk 'NR==1{print $1;exit}'; }
fetch(){ curl -q --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 20 --max-time 900 --retry 4 --retry-delay 2 --retry-connrefused -fsSL -H 'Accept-Encoding: identity' "$1" -o "$2"; }
rm -rf "$OUT"; mkdir -p "$OUT/r18-publication" "$OUT/r1/outer" "$OUT/r1/control" "$OUT/r1/data"
fetch "$R18/SHA256SUMS?r14c19=baseline" "$OUT/r18-publication/SHA256SUMS"
[ "$(sha "$OUT/r18-publication/SHA256SUMS")" = "$R18_MANIFEST_SHA" ]
while read -r h rel; do mkdir -p "$OUT/r18-publication/$(dirname "$rel")"; fetch "$R18/$rel?r14c19=baseline" "$OUT/r18-publication/$rel"; done < "$OUT/r18-publication/SHA256SUMS"
(cd "$OUT/r18-publication"; sha256sum -c SHA256SUMS)
fetch "$R1/packages/broray_3.0.0-r1_aarch64-3.10.ipk?r14c19=functional" "$OUT/r1/broray_3.0.0-r1_aarch64-3.10.ipk"
fetch "$R1/release.json?r14c19=functional" "$OUT/r1/release.json"
[ "$(sha "$OUT/r1/broray_3.0.0-r1_aarch64-3.10.ipk")" = "$R1_IPK_SHA" ]
[ "$(wc -c < "$OUT/r1/broray_3.0.0-r1_aarch64-3.10.ipk" | tr -d ' ')" = "$R1_IPK_SIZE" ]
[ "$(sha "$OUT/r1/release.json")" = "$R1_RELEASE_SHA" ]
tar -xzf "$OUT/r1/broray_3.0.0-r1_aarch64-3.10.ipk" -C "$OUT/r1/outer"
tar -xzf "$OUT/r1/outer/control.tar.gz" -C "$OUT/r1/control"
tar -xzf "$OUT/r1/outer/data.tar.gz" -C "$OUT/r1/data"
sha256sum "$OUT/r1/data/opt/broray/lib/package-transaction.sh" "$OUT/r1/data/opt/broray/lib/broray-page.sh" > "$OUT/r1/core-hashes.txt"
cat > "$OUT/BASELINE-RESULT.txt" <<RESULT
R14C18_PUBLICATION=PASS_EXACT
R1_FROM_EXACT_R26=PASS_EXACT
R1_IPK_SHA256=$R1_IPK_SHA
R1_RELEASE_SHA256=$R1_RELEASE_SHA
RESULT
