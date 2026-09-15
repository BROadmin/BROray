#!/opt/bin/ash

BRORAY_RELEASE_MANIFEST="${BRORAY_RELEASE_MANIFEST:-${BRORAY_ROOT:-/opt/broray}/share/release/manifest.json}"

broray_release_manifest_valid()
{
    [ -r "$BRORAY_RELEASE_MANIFEST" ] &&
        jq -e '
            type == "object" and
            (.schemaVersion == 3) and
            (((.version // "") | type) == "string") and
            (((.webUIBuild // "") | type) == "string") and
            (((.releaseId // "") | type) == "string")
        ' "$BRORAY_RELEASE_MANIFEST" >/dev/null 2>&1
}

broray_release_value()
{
    key="$1"
    fallback="${2:-}"

    if broray_release_manifest_valid; then
        value="$(jq -r --arg key "$key" '.[$key] // empty' "$BRORAY_RELEASE_MANIFEST" 2>/dev/null)"
        [ -n "$value" ] && { printf '%s\n' "$value"; return 0; }
    fi

    printf '%s\n' "$fallback"
}

broray_release_json()
{
    if broray_release_manifest_valid; then
        cat "$BRORAY_RELEASE_MANIFEST"
    else
        jq -nc '{schemaVersion:3,version:"unknown",releaseId:"unknown",webUIBuild:"unknown",buildTrack:"unknown",source:"unknown",commit:null,builtAt:null}'
    fi
}
