#!/usr/bin/env bash

set -euo pipefail

REPOSITORY_ROOT="$(
    cd "$(dirname "${BASH_SOURCE[0]}")/.." &&
        pwd
)"
MIGRATOR="$REPOSITORY_ROOT/scripts/migrate-manual-to-opkg.sh"
PACKAGE="${BRORAY_TEST_PACKAGE:-$REPOSITORY_ROOT/dist/opkg/aarch64-3.10/broray_2.1.0-2_aarch64-3.10.ipk}"
PACKAGE_SHA="${BRORAY_TEST_PACKAGE_SHA256:-7c47d5b45a4f5627aa3efd8c0780b5e881907b0f0f8914e609012b19f97519ff}"
WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/broray-manual-opkg-test.XXXXXX")"

cleanup()
{
    rm -rf "$WORK_ROOT"
}

fail()
{
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

create_fixture()
{
    local fixture="$1"
    local mode="$2"
    local opt="$fixture/opt"
    local tmp="$fixture/tmp"
    local mock="$fixture/mock-bin"
    local archive="$opt/broray/backups/manual-before-upgrade-20260726-093543.tar.gz"

    mkdir -p \
        "$opt/bin" \
        "$opt/broray/backup" \
        "$opt/broray/backups" \
        "$opt/broray/bin" \
        "$opt/broray/config/system" \
        "$opt/broray/routes" \
        "$opt/broray/run" \
        "$opt/etc/init.d" \
        "$opt/etc/opkg" \
        "$opt/lib/opkg/info" \
        "$opt/var/opkg-lists" \
        "$tmp" \
        "$mock"

    printf 'BROray 2.1.0\n' >"$opt/broray/config/version"
    printf '{"marker":"manual-settings"}\n' \
        >"$opt/broray/config/system/settings.json"
    printf '{"marker":"manual-auto"}\n' \
        >"$opt/broray/config/system/server-auto-switch.json"
    printf '{"marker":"manual-routes"}\n' \
        >"$opt/broray/routes/config.json"
    printf '192.0.2.1\n' >"$opt/broray/run/lan-ip"
    printf 'Package: existing\nVersion: 1\nStatus: install ok installed\n\n' \
        >"$opt/lib/opkg/status"

    cat >"$opt/broray/bin/broray" <<'EOF'
#!/bin/sh
printf 'BROray 2.1.0\n'
EOF
    chmod 755 "$opt/broray/bin/broray"

    tar -xzOf "$PACKAGE" ./data.tar.gz |
        tar -xzOf - ./opt/broray/bin/xray \
            >"$opt/broray/bin/xray"
    chmod 755 "$opt/broray/bin/xray"

    if [ "$mode" = xray-mismatch ]; then
        printf 'mismatch\n' >>"$opt/broray/bin/xray"
    fi

    for service_name in \
        S23broray-monitor \
        S24broray \
        S25broray-web \
        S27broray-auto-switch \
        S28broray-subscriptions
    do
        cat >"$opt/etc/init.d/$service_name" <<'EOF'
#!/bin/sh

if [ "${BRORAY_TEST_MODE:-}" = health-failure ] &&
   [ "${1:-}" = status ] &&
   grep -q '^Package: broray$' "$BRORAY_OPT_ROOT/lib/opkg/status"
then
    exit 1
fi

exit 0
EOF
        chmod 755 "$opt/etc/init.d/$service_name"
    done

    for command_name in \
        broray \
        broray-routes \
        broray-server \
        broray-servers \
        broray-subscriptions \
        broray-system
    do
        ln -s "../broray/bin/broray" "$opt/bin/$command_name"
    done

    tar \
        --exclude='broray/backups' \
        -czf "$fixture/manual-backup.tar.gz" \
        -C "$opt" \
        broray
    mv "$fixture/manual-backup.tar.gz" "$archive"
    sha256sum "$archive" >"$archive.sha256"

    cat >"$mock/uname" <<'EOF'
#!/bin/sh
printf 'aarch64\n'
EOF

    cat >"$mock/ndmc" <<'EOF'
#!/bin/sh
exit 0
EOF

    cat >"$mock/curl" <<'EOF'
#!/bin/sh
case "$*" in
    *192.0.2.1:8080*)
        exit 0
        ;;
esac
exec /usr/bin/curl "$@"
EOF

    cat >"$mock/wget" <<'EOF'
#!/bin/sh
printf '%s\n' 'GNU Wget test'
printf '%s\n' '+https'
EOF

    cat >"$mock/df" <<'EOF'
#!/bin/sh
printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n'

if [ "${BRORAY_TEST_MODE:-}" = space-check ] &&
   [ "${2:-}" = "$BRORAY_OPT_ROOT" ]
then
    printf 'fixture 500000 472616 27384 95%% /fixture\n'
else
    printf 'fixture 500000 1000 499000 1%% /fixture\n'
fi
EOF

    cat >"$mock/opkg" <<'EOF'
#!/bin/sh

status_file="$BRORAY_OPT_ROOT/lib/opkg/status"
mode="$BRORAY_TEST_MODE"
force_space=false

if [ "${1:-}" = "--help" ]; then
    if [ "$mode" = unsupported-force ]; then
        printf 'Usage: opkg [options] sub-command\n'
    else
        printf '  --force-space  Disable free space checks\n'
    fi
    exit 0
fi

if [ "${1:-}" = "--force-space" ]; then
    force_space=true
    shift
fi

case "$1" in
    list-installed)
        package="${2:-}"

        case "$package" in
            broray)
                awk '
                    $1 == "Package:" && $2 == "broray" {
                        wanted = 1
                    }
                    wanted && $1 == "Version:" {
                        print "broray - " $2
                        exit
                    }
                ' "$status_file"
                ;;
            *)
                printf '%s - 1\n' "$package"
                ;;
        esac
        ;;
    install)
        if [ "$mode" = space-check ] && [ "$force_space" != true ]; then
            printf '%s\n' \
                'verify_pkg_installable: Only have 27384kb available, pkg broray needs 36926' \
                >&2
            exit 1
        fi

        cp "$status_file" "$status_file.before-install"
        {
            cat "$status_file.before-install"
            printf 'Package: broray\n'
            printf 'Version: 2.1.0-2\n'
            printf 'Status: install ok installed\n\n'
        } >"$status_file"

        printf '{"marker":"package-settings"}\n' \
            >"$BRORAY_OPT_ROOT/broray/config/system/settings.json"
        printf '{"marker":"package-auto"}\n' \
            >"$BRORAY_OPT_ROOT/broray/config/system/server-auto-switch.json"
        printf '{"marker":"package-routes"}\n' \
            >"$BRORAY_OPT_ROOT/broray/routes/config.json"

        if [ "$mode" = failure ]; then
            exit 1
        fi
        ;;
    remove)
        awk '
            BEGIN {
                RS = ""
                ORS = "\n\n"
            }
            $0 !~ /(^|\n)Package:[[:space:]]*broray(\n|$)/ {
                print
            }
        ' "$status_file" >"$status_file.new"
        mv "$status_file.new" "$status_file"
        ;;
    update)
        if [ "$mode" = feed-failure ] &&
           grep -q '^Package: broray$' "$status_file"
        then
            exit 1
        fi
        exit 0
        ;;
    *)
        exit 1
        ;;
esac
EOF

    chmod 755 \
        "$mock/uname" \
        "$mock/ndmc" \
        "$mock/curl" \
        "$mock/wget" \
        "$mock/df" \
        "$mock/opkg"
    ln -s /bin/sh "$mock/ash"

    printf '%s\n' "$mode" >"$fixture/mode"
}

run_fixture()
{
    local fixture="$1"
    local expected_result="$2"
    local expected_failure_phase="${3:-rollback}"
    local output="$fixture/output"
    local opt="$fixture/opt"
    local tmp="$fixture/tmp"
    local mock="$fixture/mock-bin"
    local archive="$opt/broray/backups/manual-before-upgrade-20260726-093543.tar.gz"
    local result=0

    if [ "$(<"$fixture/mode")" = auto-backup ]; then
        rm -f "$archive" "$archive.sha256"
    fi

    PATH="$mock:$PATH" \
    BRORAY_OPT_ROOT="$opt" \
    BRORAY_TMP_ROOT="$tmp" \
    BRORAY_TEST_MODE="$(<"$fixture/mode")" \
    BRORAY_PACKAGE_FILE="$PACKAGE" \
    BRORAY_PACKAGE_SHA256="$PACKAGE_SHA" \
    BRORAY_MANUAL_BACKUP="$archive" \
    BRORAY_MANUAL_BACKUP_SUM="$archive.sha256" \
    BRORAY_FEED_FILE="$opt/etc/opkg/broray.conf" \
    BRORAY_BACKUP_MARKER="$tmp/broray-opkg-existing-backup" \
        /bin/sh "$MIGRATOR" >"$output" 2>&1 ||
        result=$?

    if [ "$result" -ne "$expected_result" ]; then
        cat "$output" >&2
        fail "unexpected result $result for $(<"$fixture/mode") fixture"
    fi

    jq -e '.marker == "manual-settings"' \
        "$opt/broray/config/system/settings.json" >/dev/null ||
        fail "manual settings were not preserved"
    jq -e '.marker == "manual-auto"' \
        "$opt/broray/config/system/server-auto-switch.json" >/dev/null ||
        fail "manual auto-switch settings were not preserved"
    jq -e '.marker == "manual-routes"' \
        "$opt/broray/routes/config.json" >/dev/null ||
        fail "manual route settings were not preserved"

    if [ "$expected_result" -eq 0 ]; then
        grep -q '^Version: 2.1.0-2$' "$opt/lib/opkg/status" ||
            fail "successful migration did not register package"
        grep -q '^src/gz broray ' "$opt/etc/opkg/broray.conf" ||
            fail "successful migration did not configure feed"
        grep -q 'Миграция завершена успешно.' "$output" ||
            fail "successful migration was not reported"

        if [ "$(<"$fixture/mode")" = auto-backup ]; then
            [ -s "$archive" ] ||
                fail "automatic manual backup was not created"
            [ -s "$archive.sha256" ] ||
                fail "automatic backup checksum was not created"
            grep -q 'Создан проверенный снимок:' "$output" ||
                fail "automatic manual backup was not reported"
        fi
    else
        ! grep -q '^Package: broray$' "$opt/lib/opkg/status" ||
            fail "failed migration left package registered"
        [ ! -e "$opt/etc/opkg/broray.conf" ] ||
            fail "failed migration left feed configured"

        if [ "$expected_failure_phase" = rollback ]; then
            grep -q 'Ручная установка BROray 2.1.0 восстановлена: OK' "$output" ||
                fail "manual rollback was not reported"
        else
            ! grep -q 'Восстанавливается ручная установка' "$output" ||
                fail "preflight failure unexpectedly started rollback"
        fi
    fi

    case "$(<"$fixture/mode")" in
        unsupported-force)
            grep -q 'OPKG не поддерживает безопасный обход проверки места' "$output" ||
                fail "unsupported force-space option was not rejected"
            ;;
        xray-mismatch)
            grep -q 'действующий Xray отличается от Xray в пакете' "$output" ||
                fail "mismatched Xray was not rejected"
            ;;
    esac
}

trap cleanup EXIT

for required_command in jq sha256sum tar; do
    command -v "$required_command" >/dev/null 2>&1 ||
        fail "missing command: $required_command"
done

[ -f "$MIGRATOR" ] || fail "migrator not found"
[ -f "$PACKAGE" ] || fail "package not found"

success_fixture="$WORK_ROOT/success"
install_failure_fixture="$WORK_ROOT/install-failure"
health_failure_fixture="$WORK_ROOT/health-failure"
feed_failure_fixture="$WORK_ROOT/feed-failure"
space_check_fixture="$WORK_ROOT/space-check"
unsupported_force_fixture="$WORK_ROOT/unsupported-force"
xray_mismatch_fixture="$WORK_ROOT/xray-mismatch"
auto_backup_fixture="$WORK_ROOT/auto-backup"

create_fixture "$success_fixture" success
create_fixture "$install_failure_fixture" failure
create_fixture "$health_failure_fixture" health-failure
create_fixture "$feed_failure_fixture" feed-failure
create_fixture "$space_check_fixture" space-check
create_fixture "$unsupported_force_fixture" unsupported-force
create_fixture "$xray_mismatch_fixture" xray-mismatch
create_fixture "$auto_backup_fixture" auto-backup
run_fixture "$success_fixture" 0
run_fixture "$install_failure_fixture" 1
run_fixture "$health_failure_fixture" 1
run_fixture "$feed_failure_fixture" 1
run_fixture "$space_check_fixture" 0
run_fixture "$unsupported_force_fixture" 1 preflight
run_fixture "$xray_mismatch_fixture" 1 preflight
run_fixture "$auto_backup_fixture" 0

printf 'BROray manual-to-OPKG migration success/rollback tests: PASS\n'
