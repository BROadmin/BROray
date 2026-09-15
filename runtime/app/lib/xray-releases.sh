#!/opt/bin/ash
# Official Xray release selection. Never accepts a caller-supplied download URL.
BRORAY_XRAY_GITHUB='https://api.github.com/repos/XTLS/Xray-core'
BRORAY_XRAY_CATALOG_JQ="$BRORAY_BASE/lib/xray-releases.jq"

broray_xray_tag_valid() {
    jq -ne -L "$BRORAY_BASE/lib" --arg tag "$1" 'include "xray-releases"; $tag|valid_tag' >/dev/null
}

broray_xray_github_get() {
    curl -q -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
        --connect-timeout 8 --max-time 20 --max-filesize 8388608 \
        -H 'Accept: application/vnd.github+json' -H 'User-Agent: BROray-Xray/3.1.0' \
        "$BRORAY_XRAY_GITHUB/$1" -o "$2"
}

broray_xray_context() {
    local architecture
    case "$(uname -m)" in aarch64|arm64) architecture=arm64 ;; *) architecture=unsupported ;; esac
    jq -c --arg arch "$architecture" \
        '{candidateId,releaseId,version,architecture:$arch}' \
        "$BRORAY_BASE/share/release/manifest.json"
}

broray_xray_registry() {
    local registry expected actual
    registry="$BRORAY_BASE/share/xray-compatibility.json"
    expected="$(awk '$2=="app/share/xray-compatibility.json"{print $1}' "$BRORAY_BASE/current/SHA256SUMS" 2>/dev/null)"
    actual="$(sha256sum "$registry" 2>/dev/null | awk '{print $1}')"
    if [ -f "$registry" ] && [ ! -L "$registry" ] && [ -n "$expected" ] && [ "$expected" = "$actual" ] &&
       jq -e '.schemaVersion==1 and (.records|type)=="array"' "$registry" >/dev/null 2>&1; then
        jq -c -L "$BRORAY_BASE/lib" 'include "xray-releases"; [.records[]|select(valid_record)]' "$registry"
    else
        printf '[]\n'
    fi
}

broray_xray_release_resolve() {
    local tag destination raw normalized sha
    tag="$1"; destination="$2"
    broray_xray_tag_valid "$tag" || return 1
    raw="$destination.raw"
    broray_xray_github_get "releases/tags/$tag" "$raw" || return 1
    normalized="$(jq -ce -L "$BRORAY_BASE/lib" --arg tag "$tag" \
        --arg asset "$BRORAY_XRAY_RELEASE_ASSET" --arg digest "$BRORAY_XRAY_RELEASE_DIGEST" \
        'include "xray-releases"; select(.tag_name==$tag) | normalize_release($asset;$digest)' "$raw")" || return 1
    sha="$(printf '%s' "$normalized" | jq -r '.assets[0].digest // ""')"
    if ! printf '%s' "$sha" | grep -Eq '^sha256:[a-f0-9]{64}$'; then
        curl -q -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
            --connect-timeout 8 --max-time 20 --max-filesize 16384 \
            "https://github.com/XTLS/Xray-core/releases/download/$tag/$BRORAY_XRAY_RELEASE_DIGEST" -o "$destination.dgst" || return 1
        sha="$(broray_xray_update_expected_sha256 "$destination.dgst")"
        broray_xray_update_validate_sha256 "$sha" || return 1
        normalized="$(printf '%s' "$normalized" | jq -c --arg sha "sha256:$sha" '.assets[0].digest=$sha')" || return 1
    fi
    printf '%s\n' "$normalized" >"$destination"
}

broray_xray_catalog_fetch() {
    local work page count complete tag current context records extra row
    work="$BRORAY_XRAY_CATALOG_WORK"
    current="$(broray_xray_version_number "$BRORAY_XRAY_BINARY")"
    context="$(broray_xray_context)" || return 1
    [ "$(printf '%s' "$context" | jq -r '.architecture')" = arm64 ] || return 1
    records="$(broray_xray_registry)"
    printf '[]\n' >"$work/all.json"
    complete=false
    page=1
    while [ "$page" -le 10 ]; do
        broray_xray_github_get "releases?per_page=20&page=$page" "$work/page.json" || return 1
        jq -e 'type=="array"' "$work/page.json" >/dev/null || return 1
        count="$(jq length "$work/page.json")"
        jq -c -L "$BRORAY_BASE/lib" --arg asset "$BRORAY_XRAY_RELEASE_ASSET" --arg digest "$BRORAY_XRAY_RELEASE_DIGEST" \
            'include "xray-releases"; [.[]|try normalize_release($asset;$digest) catch empty]' "$work/page.json" >"$work/normalized.json" || return 1
        jq -sc 'add|unique_by(.tag_name)|sort_by(.published_at)|reverse' "$work/all.json" "$work/normalized.json" >"$work/merged.json" || return 1
        mv "$work/merged.json" "$work/all.json" || return 1
        if jq -e '([.[]|select(.prerelease==false)]|length)>=2 and ([.[]|select(.prerelease)]|length)>=5' "$work/all.json" >/dev/null; then complete=true; break; fi
        if [ "$count" -lt 20 ]; then complete=true; break; fi
        page=$((page + 1))
    done
    # Fetch exact installed and last compatible entries even outside the recent window.
    extra="$(printf '%s' "$records" | jq -r --argjson context "$context" \
        '[.[]|select(.status=="compatible" and .candidateId==$context.candidateId and .architecture==$context.architecture)]|sort_by(.testedAt)|last|.xrayTag // empty')"
    jq -c '([.[]|select(.prerelease==false)][:2]+[.[]|select(.prerelease)][:5])|unique_by(.tag_name)' "$work/all.json" >"$work/selected.json" || return 1
    for tag in "v$current" "$extra"; do
        [ -n "$tag" ] || continue
        if ! jq -e --arg tag "$tag" 'any(.[];.tag_name==$tag)' "$work/selected.json" >/dev/null; then
            if broray_xray_release_resolve "$tag" "$work/exact.json"; then
                jq -sc '.[0]+[.[1]]' "$work/selected.json" "$work/exact.json" >"$work/merged.json" && mv "$work/merged.json" "$work/selected.json" || return 1
            fi
        fi
    done
    # Old GitHub assets may lack the API digest; resolve their official .dgst.
    jq -r -L "$BRORAY_BASE/lib" 'include "xray-releases"; .[]|select((.assets[0].digest // ""|ltrimstr("sha256:")|sha256)|not)|.tag_name' "$work/selected.json" >"$work/missing.txt"
    while IFS= read -r tag; do
        if broray_xray_release_resolve "$tag" "$work/exact.json"; then
            jq -sc --arg tag "$tag" '(.[0]|map(select(.tag_name!=$tag)))+[.[1]]' "$work/selected.json" "$work/exact.json" >"$work/merged.json" && mv "$work/merged.json" "$work/selected.json" || return 1
        fi
    done <"$work/missing.txt"
    jq -c -L "$BRORAY_BASE/lib" --argjson context "$context" --argjson records "$records" \
        'include "xray-releases"; map(. + {brorayCompatibility:compatibility($records;$context)})|sort_by(.published_at)|reverse' \
        "$work/selected.json" >"$BRORAY_XRAY_RELEASE_CACHE" || return 1
    printf '%s' "$context" >"$work/context.json"
    printf '%s' "$complete" >"$work/complete"
}

broray_xray_update_check() (
    umask 077
    BRORAY_XRAY_CATALOG_WORK="$(mktemp -d "${BRORAY_XRAY_UPDATE_TMP_ROOT:-/tmp}/broray-xray-catalog.XXXXXX")" || return 1
    trap 'rm -rf "$BRORAY_XRAY_CATALOG_WORK"' 0
    BRORAY_XRAY_RELEASE_CACHE="$BRORAY_XRAY_CATALOG_WORK/releases.json"
    if ! broray_xray_update_check_impl >"$BRORAY_XRAY_CATALOG_WORK/check.json"; then
        cat "$BRORAY_XRAY_CATALOG_WORK/check.json"
        return 1
    fi
    jq -n -L "$BRORAY_BASE/lib" \
        --slurpfile check "$BRORAY_XRAY_CATALOG_WORK/check.json" \
        --slurpfile rows "$BRORAY_XRAY_RELEASE_CACHE" \
        --slurpfile context "$BRORAY_XRAY_CATALOG_WORK/context.json" \
        --argjson complete "$(cat "$BRORAY_XRAY_CATALOG_WORK/complete")" '
        include "xray-releases";
        $check[0].currentVersion as $current |
        ($rows[0]|map(summarize($current))) as $releases |
        $check[0] + {context:$context[0],catalogComplete:$complete,
          releases:($releases + (if any($releases[];.installed) then [] else
            [{tagName:("v"+$current),version:$current,installed:true,available:false,prerelease:null,
              compatibility:{status:"untested",label:"Не проверялась на совместимость с BROray"}}] end))}'
)

broray_xray_install_request_valid() {
    jq -se -L "$BRORAY_BASE/lib" 'include "xray-releases"; length==1 and (.[0] | type=="object" and
        (keys|sort)==["allowDowngrade","allowPrerelease","allowUntested","archiveSha256","currentVersion","tag"] and
        (.tag|valid_tag) and (.currentVersion|type)=="string" and
        (.archiveSha256|sha256) and
        ([.allowDowngrade,.allowPrerelease,.allowUntested]|all(.[];type=="boolean")))' "$1" >/dev/null 2>&1 &&
        broray_xray_tag_valid "$(jq -r .tag "$1")"
}

broray_xray_selected_check() {
    local request tag current context records target compatibility old new
    request="$1"
    broray_xray_install_request_valid "$request" || { broray_xray_update_error 'Некорректный запрос установки Xray.'; return 1; }
    tag="$(jq -r .tag "$request")"
    current="$(broray_xray_version_number "$BRORAY_XRAY_BINARY")"
    [ "$current" = "$(jq -r .currentVersion "$request")" ] || { broray_xray_update_error 'Установленная версия изменилась. Обновите список и повторите выбор.'; return 1; }
    context="$(broray_xray_context)" || return 1
    [ "$(printf '%s' "$context" | jq -r .architecture)" = arm64 ] || { broray_xray_update_error 'Архитектура устройства не поддерживается этим пакетом BROray.'; return 1; }
    broray_xray_release_resolve "$tag" "$BRORAY_XRAY_UPDATE_WORK/selected.json" || { broray_xray_update_error 'Не удалось подтвердить выбранный официальный релиз Xray.'; return 1; }
    records="$(broray_xray_registry)"
    target="$(jq -c -L "$BRORAY_BASE/lib" --argjson context "$context" --argjson records "$records" --arg current "$current" \
        'include "xray-releases"; .+{brorayCompatibility:compatibility($records;$context)}|summarize($current)' "$BRORAY_XRAY_UPDATE_WORK/selected.json")" || return 1
    [ "$(printf '%s' "$target"|jq -r .archiveSha256)" = "$(jq -r .archiveSha256 "$request")" ] || { broray_xray_update_error 'Контрольная сумма выбранного релиза изменилась. Повторите проверку версий.'; return 1; }
    compatibility="$(printf '%s' "$target"|jq -r .compatibility.status)"
    [ "$compatibility" != incompatible ] || { broray_xray_update_error 'Выбранная версия несовместима с этой сборкой BROray.'; return 1; }
    if [ "$compatibility" = untested ] && [ "$(jq -r .allowUntested "$request")" != true ]; then broray_xray_update_error 'Подтвердите установку версии, не проверявшейся на совместимость с BROray.'; return 1; fi
    if [ "$(printf '%s' "$target"|jq -r .prerelease)" = true ] && [ "$(jq -r .allowPrerelease "$request")" != true ]; then broray_xray_update_error 'Подтвердите установку предварительного релиза.'; return 1; fi
    old="$(broray_xray_version_key "$current")"; new="$(broray_xray_version_key "${tag#v}")"
    if [ "$new" -lt "$old" ] && [ "$(jq -r .allowDowngrade "$request")" != true ]; then broray_xray_update_error 'Подтвердите понижение версии Xray.'; return 1; fi
    printf '%s' "$target" | jq --arg current "$current" --argjson newer "$([ "$new" -gt "$old" ] && echo true || echo false)" \
        '{success:true,currentVersion:$current,latestVersion:.version,latestTag:.tagName,archiveSha256,asset,digest,compatibility,prerelease,updateAvailable:$newer,installedNewer:false}'
}

# Legacy update/reinstall remains non-interactive and never grants risk consent.
broray_xray_install_check() {
    if [ "$1" = install ]; then
        broray_xray_selected_check "$2"
        return $?
    fi
    broray_xray_update_check >"$BRORAY_XRAY_UPDATE_WORK/catalog.json" || {
        cat "$BRORAY_XRAY_UPDATE_WORK/catalog.json"; return 1;
    }
    jq --arg mode "$1" '
        .currentVersion as $current | .latestTag as $latest |
        [.releases[]|select(if $mode=="reinstall" then .installed else .tagName==$latest end)][0] |
        {tag:.tagName,currentVersion:$current,archiveSha256,allowUntested:false,allowPrerelease:false,allowDowngrade:false}
    ' "$BRORAY_XRAY_UPDATE_WORK/catalog.json" >"$BRORAY_XRAY_UPDATE_WORK/request.json" || return 1
    broray_xray_selected_check "$BRORAY_XRAY_UPDATE_WORK/request.json"
}

broray_xray_selected_runtime_ready() (
    # The CLI loads xray-control.sh; the WebUI status helpers live separately.
    # Import them only here, without replacing the caller's lifecycle helpers.
    . "$BRORAY_BASE/lib/xray.sh" || return 1
    local attempt address port
    address="$(broray_xray_socks_address)"; port="$(broray_xray_socks_port)"
    for attempt in 1 2 3 4 5 6 7 8 9 10 11 12; do
        if broray_xray_is_running && broray_xray_socks_active "$address" "$port"; then return 0; fi
        sleep 1
    done
    return 1
)

broray_xray_release_operation_fence() {
    broray_job_finish "${1:-failed}"
}

broray_xray_install_dispatch() {
    local install_rc
    if [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ]; then
        # Only the actual acknowledged executor can continue this job.
        broray_job_require_owner || return $?
        broray_xray_update_install "$@"
        return $?
    fi
    broray_job_begin routes "xray:$1" xray USER cooperative || return $?
    trap 'broray_xray_job_exit "$?"' EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    install_rc=0
    broray_xray_update_install "$@"
    install_rc=$?
    broray_xray_update_abort_cleanup || return 75
    broray_job_exit "$install_rc" || return 75
    trap - EXIT HUP INT TERM
    return "$install_rc"
}
