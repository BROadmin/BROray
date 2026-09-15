#!/opt/bin/ash

# One-time conversion of a legacy /opt/broray tree into compact app slots.
# User data stays in place. The existing Xray binary is moved once into the
# shared runtime and is neither copied into app slots nor replaced.

set -u

PATH="${BRORAY_UPDATER_PATH:-/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin}"
LC_ALL=C
export PATH LC_ALL

ROOT_PREFIX="${BRORAY_UPDATER_ROOT_PREFIX:-}"
SERVICE_HOOK="${BRORAY_UPDATER_SERVICE_HOOK:-}"
ARCHITECTURE="${BRORAY_UPDATER_ARCHITECTURE:-aarch64-3.10}"
TEST_MODE="${BRORAY_UPDATER_TEST_MODE:-0}"

root_path()
{
    case "$1" in /*) ;; *) return 2 ;; esac
    if [ -n "$ROOT_PREFIX" ]; then
        printf '%s%s\n' "${ROOT_PREFIX%/}" "$1"
    else
        printf '%s\n' "$1"
    fi
}

APP_ROOT="${BRORAY_UPDATER_APP_ROOT:-$(root_path /opt/broray)}"
INIT_ROOT="${BRORAY_UPDATER_INIT_ROOT:-$(root_path /opt/etc/init.d)}"
STATE_ROOT="${BRORAY_UPDATER_STATE_ROOT:-$(root_path /opt/var/lib/broray-updater)}"
RELEASES_ROOT="$APP_ROOT/releases"
CURRENT_PATH="$APP_ROOT/current"
RUNTIME_ROOT="$APP_ROOT/runtime"
XRAY_RUNTIME="$RUNTIME_ROOT/xray"
XRAY_WRAPPER="$(root_path /opt/libexec/broray-updater/xray-wrapper)"
SLOT_META_ROOT="$STATE_ROOT/slots"
ASH_BIN="${BRORAY_UPDATER_ASH:-$(root_path /opt/bin/ash)}"

CODE_DIRS="bin lib web-new share"
SERVICES="S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions"
MIGRATION_STARTED=false
MIGRATION_COMPLETE=false
SLOT=""
SLOT_ROOT=""
STAGING_ROOT=""
SERVICE_STATE=""

now()
{
    date -u '+%Y-%m-%dT%H:%M:%SZ'
}

valid_id()
{
    case "${1:-}" in ''|.*|-*|*[!A-Za-z0-9._-]*) return 1 ;; esac
    return 0
}

executable_regular_file()
{
    [ -f "$1" ] && [ ! -L "$1" ] && [ -s "$1" ] || return 1
    [ "$TEST_MODE" = 1 ] || [ -x "$1" ]
}

link_create()
{
    local target link
    target="$1"
    link="$2"
    if [ "$TEST_MODE" = 1 ] && [ -n "${BRORAY_UPDATER_LINK_HOOK:-}" ]; then
        "$ASH_BIN" "$BRORAY_UPDATER_LINK_HOOK" "$target" "$link"
    else
        ln -s "$target" "$link"
    fi
}

valid_release_version()
{
    local value version revision old_ifs
    value="$1"
    case "$value" in *-r*) ;; *) return 1 ;; esac
    version="${value%%-r*}"
    revision="${value#*-r}"
    old_ifs="$IFS"
    IFS=.
    set -- $version
    IFS="$old_ifs"
    [ "$#" -eq 3 ] || return 1
    case "$1$2$3$revision" in ''|*[!0-9]*) return 1 ;; esac
    [ "$revision" -gt 0 ]
}

atomic_json_file()
{
    local target temporary
    target="$1"
    temporary="$target.tmp.$$"
    cat >"$temporary" || return 1
    jq -e 'type == "object"' "$temporary" >/dev/null 2>&1 || {
        rm -f "$temporary"
        return 1
    }
    chmod 600 "$temporary" 2>/dev/null || true
    mv -f "$temporary" "$target"
}

service_call()
{
    local action service
    action="$1"
    service="$2"
    if [ -n "$SERVICE_HOOK" ]; then
        "$SERVICE_HOOK" "$action" "$service"
    else
        "$ASH_BIN" "$INIT_ROOT/$service" "$action"
    fi
}

capture_services()
{
    local service
    : >"$SERVICE_STATE" || return 1
    for service in $SERVICES
    do
        if service_call status "$service" >/dev/null 2>&1; then
            printf '%s\trunning\n' "$service" >>"$SERVICE_STATE" || return 1
        else
            printf '%s\tstopped\n' "$service" >>"$SERVICE_STATE" || return 1
        fi
    done
}

service_was_running()
{
    local service
    service="$1"
    [ "$(awk -F '\t' -v service="$service" '$1 == service {print $2; exit}' "$SERVICE_STATE")" = running ]
}

stop_services()
{
    local service
    for service in S28broray-subscriptions S27broray-auto-switch S25broray-web S24broray S23broray-monitor
    do
        service_was_running "$service" || continue
        service_call stop "$service" >/dev/null 2>&1 || return 1
    done
}

start_services()
{
    local service
    for service in $SERVICES
    do
        service_was_running "$service" || continue
        service_call start "$service" >/dev/null 2>&1 || return 1
    done
}

remove_exact_link()
{
    local path expected
    path="$1"
    expected="$2"
    if [ -L "$path" ] && [ "$(readlink "$path" 2>/dev/null || true)" = "$expected" ]; then
        rm -f "$path"
    fi
}

rollback()
{
    local rc service directory source_root
    rc=$?
    trap - EXIT HUP INT TERM

    if [ "$MIGRATION_STARTED" = true ] && [ "$MIGRATION_COMPLETE" != true ]; then
        stop_services >/dev/null 2>&1 || true

        source_root="$STAGING_ROOT"
        if [ -d "$CURRENT_PATH" ] && [ ! -L "$CURRENT_PATH" ] && \
           [ "$(sed -n '1p' "$CURRENT_PATH/.broray-slot" 2>/dev/null || true)" = "$SLOT" ]; then
            source_root="$CURRENT_PATH"
        fi

        if [ -f "$XRAY_RUNTIME" ] && [ ! -L "$XRAY_RUNTIME" ]; then
            if [ -f "$source_root/app/bin/xray" ] && [ ! -L "$source_root/app/bin/xray" ] && \
               cmp -s "$source_root/app/bin/xray" "$XRAY_WRAPPER"; then
                rm -f "$source_root/app/bin/xray"
            fi
            if [ ! -e "$source_root/app/bin/xray" ] && [ ! -L "$source_root/app/bin/xray" ]; then
                mv "$XRAY_RUNTIME" "$source_root/app/bin/xray" 2>/dev/null || true
            fi
        fi

        for service in $SERVICES
        do
            remove_exact_link "$INIT_ROOT/$service" "$APP_ROOT/current/init/$service"
            if [ -f "$source_root/init/$service" ] && [ ! -e "$INIT_ROOT/$service" ]; then
                mv "$source_root/init/$service" "$INIT_ROOT/$service" 2>/dev/null || true
            fi
        done

        for directory in $CODE_DIRS
        do
            remove_exact_link "$APP_ROOT/$directory" "current/app/$directory"
            if [ -d "$source_root/app/$directory" ] && [ ! -e "$APP_ROOT/$directory" ]; then
                mv "$source_root/app/$directory" "$APP_ROOT/$directory" 2>/dev/null || true
            fi
        done

        rm -f "$source_root/.broray-slot" "$source_root/release.json" "$source_root/SHA256SUMS"
        rmdir "$source_root/app" "$source_root/init" "$source_root" 2>/dev/null || true
        rmdir "$RUNTIME_ROOT" 2>/dev/null || true
        rm -f "$SLOT_META_ROOT/$SLOT.json"
        start_services >/dev/null 2>&1 || true
    fi

    exit "$rc"
}

already_migrated()
{
    local slot directory
    [ -d "$CURRENT_PATH" ] && [ ! -L "$CURRENT_PATH" ] || return 1
    slot="$(sed -n '1p' "$CURRENT_PATH/.broray-slot" 2>/dev/null || true)"
    valid_id "$slot" || return 1
    [ -d "$RUNTIME_ROOT" ] && [ ! -L "$RUNTIME_ROOT" ] || return 1
    executable_regular_file "$XRAY_RUNTIME" || return 1
    [ -f "$CURRENT_PATH/app/bin/xray" ] && [ ! -L "$CURRENT_PATH/app/bin/xray" ] || return 1
    cmp -s "$CURRENT_PATH/app/bin/xray" "$XRAY_WRAPPER" || return 1

    for directory in $CODE_DIRS
    do
        [ -L "$APP_ROOT/$directory" ] || return 1
        [ "$(readlink "$APP_ROOT/$directory")" = "current/app/$directory" ] || return 1
    done
    return 0
}

main()
{
    local command_name directory unsafe service version safe_version package_version safe_package installed_release
    for command_name in awk chmod cmp cp date find jq ln mkdir mv readlink rm sha256sum sort sync
    do
        command -v "$command_name" >/dev/null 2>&1 || {
            printf 'ERROR: missing command: %s\n' "$command_name" >&2
            return 1
        }
    done

    if already_migrated; then
        printf '%s\n' 'LEGACY_MIGRATION=ALREADY_COMPLETE'
        printf 'CURRENT_SLOT=%s\n' "$(sed -n '1p' "$CURRENT_PATH/.broray-slot")"
        return 0
    fi

    [ -d "$APP_ROOT" ] && [ ! -L "$APP_ROOT" ] || {
        printf '%s\n' 'ERROR: /opt/broray is absent or is a symlink' >&2
        return 1
    }
    [ ! -e "$CURRENT_PATH" ] && [ ! -L "$CURRENT_PATH" ] || {
        printf '%s\n' 'ERROR: partial current release exists' >&2
        return 1
    }
    [ ! -e "$RUNTIME_ROOT" ] && [ ! -L "$RUNTIME_ROOT" ] || {
        printf '%s\n' 'ERROR: partial shared runtime exists' >&2
        return 1
    }
    executable_regular_file "$XRAY_WRAPPER" || {
        printf '%s\n' 'ERROR: verified Xray launcher is missing' >&2
        return 1
    }

    for directory in $CODE_DIRS
    do
        [ -d "$APP_ROOT/$directory" ] && [ ! -L "$APP_ROOT/$directory" ] || {
            printf 'ERROR: unsafe legacy code directory: %s\n' "$directory" >&2
            return 1
        }
    done
    executable_regular_file "$APP_ROOT/bin/xray" || {
        printf '%s\n' 'ERROR: unsafe or missing legacy Xray binary' >&2
        return 1
    }

    unsafe="$(find "$APP_ROOT/bin" "$APP_ROOT/lib" "$APP_ROOT/web-new" "$APP_ROOT/share" -xdev \( -type l -o -type b -o -type c -o -type p -o -type s \) -print -quit 2>/dev/null)"
    [ -z "$unsafe" ] || {
        printf 'ERROR: unsafe object in legacy code tree: %s\n' "$unsafe" >&2
        return 1
    }

    for service in $SERVICES
    do
        [ -f "$INIT_ROOT/$service" ] && [ ! -L "$INIT_ROOT/$service" ] || {
            printf 'ERROR: unsafe or missing init script: %s\n' "$service" >&2
            return 1
        }
    done

    version="$(sed -n '1p' "$APP_ROOT/config/version" 2>/dev/null || true)"
    [ -n "$version" ] || version=unknown
    safe_version="$(printf '%s' "$version" | tr -c 'A-Za-z0-9._-' '-')"
    [ -n "$safe_version" ] || safe_version=unknown

    package_version="$(awk -F ': *' '$1 == "Version" {print $2; exit}' "$(root_path /opt/lib/opkg/info/broray.control)" 2>/dev/null || true)"
    [ -n "$package_version" ] || package_version="$version"
    safe_package="$(printf '%s' "$package_version" | tr -c 'A-Za-z0-9._-' '-')"
    installed_release="${BRORAY_UPDATER_INSTALLED_RELEASE:-}"
    if [ -z "$installed_release" ]; then
        installed_release="$(jq -ser '
          select(length == 1 and (.[0] | type == "object") and
                 (.[0].releaseId | type == "string")) |
          .[0].releaseId
        ' "$APP_ROOT/share/release/manifest.json" 2>/dev/null)" || return 1
    fi
    valid_release_version "$installed_release" || return 1

    SLOT="legacy-$safe_version-$(date -u '+%Y%m%d%H%M%S')"
    valid_id "$SLOT" || return 1
    STAGING_ROOT="$APP_ROOT/.current-migration-$SLOT"
    SLOT_ROOT="$STAGING_ROOT"
    SERVICE_STATE="$STATE_ROOT/migration-services.tsv"

    [ ! -e "$STAGING_ROOT" ] && [ ! -L "$STAGING_ROOT" ] || {
        printf '%s\n' 'ERROR: legacy migration staging already exists' >&2
        return 1
    }
    [ ! -e "$RELEASES_ROOT/$SLOT" ] && [ ! -L "$RELEASES_ROOT/$SLOT" ] || return 1

    mkdir -p "$STAGING_ROOT/app" "$STAGING_ROOT/init" "$SLOT_META_ROOT" "$RELEASES_ROOT" || return 1
    chmod 700 "$STATE_ROOT" "$SLOT_META_ROOT" 2>/dev/null || true
    capture_services || return 1

    MIGRATION_STARTED=true
    trap rollback EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM

    stop_services || return 1

    for directory in $CODE_DIRS
    do
        mv "$APP_ROOT/$directory" "$SLOT_ROOT/app/$directory" || return 1
    done

    mkdir "$RUNTIME_ROOT" || return 1
    mv "$SLOT_ROOT/app/bin/xray" "$XRAY_RUNTIME" || return 1
    cp -p "$XRAY_WRAPPER" "$SLOT_ROOT/app/bin/xray" || return 1
    chmod 755 "$SLOT_ROOT/app/bin/xray" "$XRAY_RUNTIME" || return 1

    for service in $SERVICES
    do
        mv "$INIT_ROOT/$service" "$SLOT_ROOT/init/$service" || return 1
    done

    jq -nc \
        --arg candidateId "legacy-structure-$safe_package" \
        --arg releaseId "$installed_release" \
        --arg appVersion "$version" \
        --arg packageVersion "$package_version" \
        --arg architecture "$ARCHITECTURE" \
        --arg createdAt "$(now)" '
        {
          schemaVersion:1,
          lifecycleContract:"compact-app-rename/1",
          layout:"broray-compact-app-slot/1",
          candidateId:$candidateId,
          releaseId:$releaseId,
          appVersion:$appVersion,
          packageVersion:$packageVersion,
          architecture:$architecture,
          sharedRuntime:{xray:{path:"/opt/broray/runtime/xray",mode:"preserve-installed",bundled:false}},
          legacy:true,
          createdAt:$createdAt
        }' >"$SLOT_ROOT/release.json" || return 1

    (
        cd "$SLOT_ROOT" || exit 1
        find . -type f ! -name SHA256SUMS -print | sed 's#^\./##' | sort |
            while IFS= read -r relative
            do
                sha256sum "$relative" || exit 1
            done >SHA256SUMS
    ) || return 1

    printf '%s\n' "$SLOT" >"$SLOT_ROOT/.broray-slot" || return 1
    chmod 600 "$SLOT_ROOT/.broray-slot" 2>/dev/null || true
    mv "$STAGING_ROOT" "$CURRENT_PATH" || return 1
    sync
    SLOT_ROOT="$CURRENT_PATH"

    for directory in $CODE_DIRS
    do
        link_create "current/app/$directory" "$APP_ROOT/$directory" || return 1
    done

    for service in $SERVICES
    do
        link_create "$APP_ROOT/current/init/$service" "$INIT_ROOT/$service" || return 1
    done

    jq -nc \
        --arg slot "$SLOT" \
        --arg candidateId "legacy-structure-$safe_package" \
        --arg releaseId "$installed_release" \
        --arg appVersion "$version" \
        --arg packageVersion "$package_version" \
        --arg architecture "$ARCHITECTURE" \
        --arg createdAt "$(now)" '
        {
          schemaVersion:1,
          slot:$slot,
          lifecycleContract:"compact-app-rename/1",
          candidateId:$candidateId,
          releaseId:$releaseId,
          appVersion:$appVersion,
          packageVersion:$packageVersion,
          architecture:$architecture,
          source:null,
          legacy:true,
          createdAt:$createdAt
        }' | atomic_json_file "$SLOT_META_ROOT/$SLOT.json" || return 1

    [ -d "$CURRENT_PATH" ] && [ ! -L "$CURRENT_PATH" ] || return 1
    [ "$(sed -n '1p' "$CURRENT_PATH/.broray-slot")" = "$SLOT" ] || return 1
    executable_regular_file "$APP_ROOT/bin/broray" || return 1
    executable_regular_file "$APP_ROOT/bin/broray-system" || return 1
    executable_regular_file "$XRAY_RUNTIME" || return 1
    cmp -s "$CURRENT_PATH/app/bin/xray" "$XRAY_WRAPPER" || return 1
    [ -f "$APP_ROOT/web-new/index.html" ] || return 1

    start_services || return 1

    MIGRATION_COMPLETE=true
    trap - EXIT HUP INT TERM

    printf '%s\n' \
        'LEGACY_MIGRATION=PASS' \
        "LEGACY_SLOT=$SLOT" \
        "CURRENT_SLOT=$SLOT" \
        'USER_DATA=MOVED_NONE' \
        'SHARED_XRAY=MOVED_ONCE_NO_COPY' \
        'OPKG_MUTATION=NONE'
}

main "$@"
