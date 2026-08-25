#!/bin/sh

set -eu

SITE_ROOT="${BROVIBE_DOCS_ROOT:-/var/www/docs.brovibe.cloud}"
STATE_ROOT="${BROVIBE_DOCS_STATE_ROOT:-/var/lib/brovibe-docs-deploy}"
BACKUP_ROOT="${BROVIBE_DOCS_BACKUP_ROOT:-/var/backups/brovibe-docs}"
BASE_URL="${BROVIBE_DOCS_BASE_URL:-}"
GITHUB_API="${BROVIBE_DOCS_GITHUB_API:-https://api.github.com}"
GITHUB_REPOSITORY="${BROVIBE_DOCS_GITHUB_REPOSITORY:-BROadmin/BROray}"
SOURCE_REF="${BROVIBE_DOCS_SOURCE_REF:-main}"
INSTALL_PATH="/usr/local/sbin/deploy-brovibe-docs"
SERVICE_PATH="/etc/systemd/system/brovibe-docs-deploy.service"
TIMER_PATH="/etc/systemd/system/brovibe-docs-deploy.timer"
LOCK_FILE="${BROVIBE_DOCS_LOCK_FILE:-/run/lock/brovibe-docs-deploy.lock}"
SITE_OWNER="${BROVIBE_DOCS_OWNER-www-data:www-data}"
FILES="index.html styles.css broray/index.html"

fail()
{
    echo "ОШИБКА: $*" >&2
    exit 1
}

require_command()
{
    command -v "$1" >/dev/null 2>&1 ||
        fail "не найдена команда $1"
}

resolve_source()
{
    work_root="$1"
    source_url_file="$work_root/source-url"
    source_commit_file="$work_root/source-commit"

    if [ -n "$BASE_URL" ]; then
        printf '%s\n' "$BASE_URL" >"$source_url_file"
        printf '%s\n' 'custom-base-url' >"$source_commit_file"
        return 0
    fi

    metadata="$work_root/source.json"

    curl \
        --proto '=https' \
        --tlsv1.2 \
        -fsSL \
        --connect-timeout 15 \
        --max-time 120 \
        -H 'Accept: application/vnd.github+json' \
        -H 'User-Agent: brovibe-docs-deploy' \
        -o "$metadata" \
        "$GITHUB_API/repos/$GITHUB_REPOSITORY/commits/$SOURCE_REF"

    source_commit="$(
        awk -F '"' '
            /"sha"[[:space:]]*:/ {
                for (field = 2; field <= NF; field++) {
                    if ($field == "sha") {
                        print $(field + 2)
                        exit
                    }
                }
            }
        ' "$metadata"
    )"

    [ "${#source_commit}" = "40" ] ||
        fail "GitHub вернул некорректный commit SHA"

    case "$source_commit" in
        *[!0-9a-f]*)
            fail "GitHub вернул некорректный commit SHA"
            ;;
    esac

    printf '%s\n' \
        "https://raw.githubusercontent.com/$GITHUB_REPOSITORY/$source_commit/site/docs.brovibe.cloud" \
        >"$source_url_file"
    printf '%s\n' "$source_commit" >"$source_commit_file"
}

validate_manifest()
{
    manifest="$1"

    [ "$(wc -l <"$manifest" | tr -d ' ')" = "3" ] ||
        fail "манифест должен содержать ровно три файла"

    for file in $FILES
    do
        matches="$(
            awk -v name="$file" '$2 == name { count++ } END { print count + 0 }' \
                "$manifest"
        )"
        expected="$(
            awk -v name="$file" '$2 == name { print $1 }' "$manifest"
        )"

        [ "$matches" = "1" ] ||
            fail "в манифесте отсутствует $file"

        [ "${#expected}" = "64" ] ||
            fail "неправильная SHA-256 для $file"

        case "$expected" in
            *[!0-9a-f]*)
                fail "неправильная SHA-256 для $file"
                ;;
        esac
    done
}

current_site_matches()
{
    manifest="$1"

    for file in $FILES
    do
        target="$SITE_ROOT/$file"
        [ -f "$target" ] || return 1

        expected="$(
            awk -v name="$file" '$2 == name { print $1 }' "$manifest"
        )"
        actual="$(
            sha256sum "$target" |
                awk '{print $1}'
        )"

        [ "$actual" = "$expected" ] || return 1
    done

    return 0
}

download_site()
{
    work_root="$1"
    manifest="$work_root/SHA256SUMS"
    payload="$work_root/site"

    mkdir -p "$payload/broray"
    resolve_source "$work_root"
    source_url="$(cat "$work_root/source-url")"

    curl \
        --proto '=https' \
        --tlsv1.2 \
        -fsSL \
        --connect-timeout 15 \
        --max-time 120 \
        -o "$manifest" \
        "$source_url/SHA256SUMS"

    validate_manifest "$manifest"

    for file in $FILES
    do
        curl \
            --proto '=https' \
            --tlsv1.2 \
            -fsSL \
            --connect-timeout 15 \
            --max-time 120 \
            -o "$payload/$file" \
            "$source_url/$file"
    done

    (
        cd "$payload"
        sha256sum -c "$manifest"
    )

    echo "Источник GitHub: $(cat "$work_root/source-commit")"
}

backup_site()
{
    [ -d "$SITE_ROOT" ] || return 0

    mkdir -p "$BACKUP_ROOT"
    stamp="$(date '+%Y%m%d-%H%M%S')"
    archive="$BACKUP_ROOT/docs-brovibe-cloud-before-$stamp.tar.gz"

    tar \
        -C "$(dirname "$SITE_ROOT")" \
        -czf "$archive" \
        "$(basename "$SITE_ROOT")"

    chmod 600 "$archive"
    echo "Резервная копия: $archive"
}

publish_file()
{
    source_file="$1"
    target_file="$2"
    target_dir="$(dirname "$target_file")"
    temporary_file="$target_file.new.$$"

    mkdir -p "$target_dir"
    install -m 0644 "$source_file" "$temporary_file"

    if [ -n "$SITE_OWNER" ]; then
        chown "$SITE_OWNER" "$temporary_file"
    fi

    mv -f "$temporary_file" "$target_file"
}

deploy_site()
{
    require_command awk
    require_command curl
    require_command flock
    require_command grep
    require_command install
    require_command sha256sum
    require_command tar

    mkdir -p "$(dirname "$LOCK_FILE")"
    exec 9>"$LOCK_FILE"

    if ! flock -n 9; then
        echo "Обновление уже выполняется."
        return 0
    fi

    work_root="$(mktemp -d /tmp/brovibe-docs-deploy.XXXXXX)"

    cleanup()
    {
        rm -rf -- "$work_root"
    }

    trap cleanup EXIT HUP INT TERM

    download_site "$work_root"

    if current_site_matches "$work_root/SHA256SUMS"; then
        echo "BROvibe Docs уже актуален."
        return 0
    fi

    backup_site

    for file in $FILES
    do
        publish_file "$work_root/site/$file" "$SITE_ROOT/$file"
    done

    current_site_matches "$work_root/SHA256SUMS" ||
        fail "файлы после публикации не прошли контрольную проверку"

    mkdir -p "$STATE_ROOT"
    install -m 0644 \
        "$work_root/SHA256SUMS" \
        "$STATE_ROOT/SHA256SUMS"
    install -m 0644 \
        "$work_root/source-commit" \
        "$STATE_ROOT/SOURCE_COMMIT"

    echo "BROvibe Docs обновлён: OK"
}

install_timer()
{
    [ "$(id -u)" = "0" ] ||
        fail "установка таймера требует права root"

    require_command install
    require_command systemctl

    install -m 0755 "$0" "$INSTALL_PATH"

    temporary_service="$(mktemp /tmp/brovibe-docs-service.XXXXXX)"
    temporary_timer="$(mktemp /tmp/brovibe-docs-timer.XXXXXX)"

    cleanup_units()
    {
        rm -f "$temporary_service" "$temporary_timer"
    }

    trap cleanup_units EXIT HUP INT TERM

    cat >"$temporary_service" <<'EOF'
[Unit]
Description=Publish BROvibe Docs from GitHub
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/deploy-brovibe-docs
EOF

    cat >"$temporary_timer" <<'EOF'
[Unit]
Description=Check BROvibe Docs updates

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min
RandomizedDelaySec=30s
Persistent=true

[Install]
WantedBy=timers.target
EOF

    install -m 0644 "$temporary_service" "$SERVICE_PATH"
    install -m 0644 "$temporary_timer" "$TIMER_PATH"

    systemctl daemon-reload
    "$INSTALL_PATH"
    systemctl enable --now brovibe-docs-deploy.timer

    echo "Автоматическое обновление BROvibe Docs подключено: OK"
    systemctl --no-pager status brovibe-docs-deploy.timer || true
}

case "${1:-}" in
    "")
        deploy_site
        ;;
    --install)
        install_timer
        ;;
    *)
        fail "использование: $0 [--install]"
        ;;
esac
