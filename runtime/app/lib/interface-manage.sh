#!/opt/bin/ash

# R14C01 transactional ProxyN mutation layer. Production mutation is admitted
# only by the shared policy, closed command grammar and bounded live/startup/
# runtime convergence. Model and firmware identifiers never select behavior.

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_INTERFACE="${BRORAY_INTERFACE:-Proxy0}"
BRORAY_PROXY_CONVERGENCE_ATTEMPTS="${BRORAY_PROXY_CONVERGENCE_ATTEMPTS:-10}"
BRORAY_PROXY_CONVERGENCE_DELAY="${BRORAY_PROXY_CONVERGENCE_DELAY:-1}"
BRORAY_PROXY_CONFIG_SETTLE_DELAY="${BRORAY_PROXY_CONFIG_SETTLE_DELAY:-5}"
BRORAY_INTERFACE_FAILURE_EVIDENCE="${BRORAY_INTERFACE_FAILURE_EVIDENCE:-$BRORAY_BASE/run/interface-last-failure.json}"

broray_interface_description_valid()
{
    broray_interface_description_value_valid "${1:-}"
}

broray_interface_failure_preserve()
{
    local dir temporary

    [ -f "$BRORAY_INTERFACE_LAST_EVIDENCE" ] && [ ! -L "$BRORAY_INTERFACE_LAST_EVIDENCE" ] || return 1
    dir="${BRORAY_INTERFACE_FAILURE_EVIDENCE%/*}"
    mkdir -p "$dir" || return 1
    temporary="$BRORAY_INTERFACE_FAILURE_EVIDENCE.new.$$"
    cp -p "$BRORAY_INTERFACE_LAST_EVIDENCE" "$temporary" || { rm -f "$temporary"; return 1; }
    mv -f "$temporary" "$BRORAY_INTERFACE_FAILURE_EVIDENCE"
}

broray_interface_control_failure_write()
{
    local stage reason empty now dir temporary

    stage="$1"
    reason="$2"
    empty="$(mktemp "${TMPDIR:-/tmp}/broray-proxy-empty.XXXXXX")" || return 1
    : >"$empty"
    broray_interface_command_evidence_write "$stage" "$reason" 1 "$empty" "$empty" || {
        rm -f "$empty"
        return 1
    }
    rm -f "$empty"
    broray_interface_failure_preserve
}

broray_interface_save()
{
    sleep "$BRORAY_PROXY_CONFIG_SETTLE_DELAY"
    broray_interface_ndmc "system configuration save" >/dev/null || {
        echo "ОШИБКА: конфигурация KeeneticOS не сохранена" >&2
        return 1
    }
}

broray_interface_wait_present()
{
    local name host port description expected_baseline attempt

    name="$1"
    host="$2"
    port="$3"
    description="$4"
    expected_baseline="${5:-}"
    attempt=0
    while [ "$attempt" -lt "$BRORAY_PROXY_CONVERGENCE_ATTEMPTS" ]; do
        attempt=$((attempt + 1))
        BRORAY_INTERFACE="$name"
        export BRORAY_INTERFACE
        if broray_interface_source_exact running "$name" "$host" "$port" "$description" &&
           broray_interface_source_exact startup "$name" "$host" "$port" "$description" &&
           broray_interface_runtime_ready "$description" &&
           { [ -z "$expected_baseline" ] ||
             broray_interface_config_pair_present_matches_sha256 \
               "$name" "$host" "$port" "$description" "$expected_baseline"; }; then
            return 0
        fi
        [ "$attempt" -ge "$BRORAY_PROXY_CONVERGENCE_ATTEMPTS" ] || sleep "$BRORAY_PROXY_CONVERGENCE_DELAY"
    done
    return 1
}

broray_interface_wait_absent()
{
    local name expected_baseline attempt

    name="$1"
    expected_baseline="${2:-}"
    attempt=0
    while [ "$attempt" -lt "$BRORAY_PROXY_CONVERGENCE_ATTEMPTS" ]; do
        attempt=$((attempt + 1))
        BRORAY_INTERFACE="$name"
        export BRORAY_INTERFACE
        if broray_interface_source_name_absent running "$name" &&
           broray_interface_source_name_absent startup "$name" &&
           broray_interface_runtime_absent &&
           { [ -z "$expected_baseline" ] ||
             broray_interface_config_pair_absent_matches_sha256 "$name" "$expected_baseline"; }; then
            return 0
        fi
        [ "$attempt" -ge "$BRORAY_PROXY_CONVERGENCE_ATTEMPTS" ] || sleep "$BRORAY_PROXY_CONVERGENCE_DELAY"
    done
    return 1
}

broray_interface_create_stage()
{
    local name stage command_text

    name="$1"
    stage="$2"
    command_text="$3"
    broray_interface_operation_mark "$name" "$stage" attempted || return 1
    broray_interface_ndmc_stage "$stage" "$command_text" || {
        broray_interface_failure_preserve >/dev/null 2>&1 || true
        return 1
    }
    broray_interface_operation_mark "$name" "$stage" accepted
}

broray_interface_provisional_cleanup()
{
    local name baseline_sha exists_rc

    name="$1"
    baseline_sha="$(jq -r '.baselineConfigSha256 // empty' "$BRORAY_INTERFACE_RESERVATION_FILE" 2>/dev/null)"
    case "$baseline_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#baseline_sha}" -eq 64 ] || return 1
    broray_interface_provisional_cleanup_safe "$name" || {
        printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_PROVISIONAL_CLEANUP_REFUSED:live/startup partial block или references не совпадают с attemptedStages' >&2
        return 1
    }
    exists_rc=0
    broray_interface_exists_name "$name" || exists_rc=$?
    case "$exists_rc" in
        0)
            broray_interface_ndmc_stage cleanup-delete "no interface $name" || return 1
            ;;
        1) ;;
        *) return 1 ;;
    esac
    broray_interface_ndmc_stage cleanup-save 'system configuration save' || return 1
    broray_interface_wait_absent "$name" "$baseline_sha" || return 1
    broray_interface_operation_clear "$name" || return 1
    broray_interface_create_reservation_clear "$name" || return 1
    return 0
}

broray_interface_select_for_action()
{
    local selected

    broray_interface_require_write_policy || return 1
    selected="$(broray_interface_select_safe)" || {
        printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_SAFE_SELECTION_FAILED:не удалось доказать owned или свободный ProxyN' >&2
        return 1
    }
    BRORAY_INTERFACE="$selected"
    export BRORAY_INTERFACE
}

broray_interface_create_unchecked()
{
    local description description_command name

    name="$BRORAY_INTERFACE"
    if broray_interface_exists; then
        echo "ОШИБКА: интерфейс $name уже существует" >&2
        return 1
    fi

    description="$(broray_interface_expected_description)" || return 1
    description_command="$(broray_interface_description_command "$description")" || return 1
    echo "Создание интерфейса $name"

    broray_interface_ndmc "interface $name" >/dev/null || {
        echo "ОШИБКА: интерфейс не создан" >&2
        return 1
    }
    broray_interface_exists || {
        echo "ОШИБКА: интерфейс отсутствует после создания" >&2
        return 1
    }
    broray_interface_ndmc "interface $name proxy protocol socks5" >/dev/null || {
        echo "ОШИБКА: протокол SOCKS5 не установлен" >&2
        return 1
    }
    broray_interface_ndmc "interface $name proxy upstream $BRORAY_PROXY_HOST $BRORAY_PROXY_PORT" >/dev/null || {
        echo "ОШИБКА: upstream не установлен" >&2
        return 1
    }
    broray_interface_ndmc "interface $name proxy connect" >/dev/null || {
        echo "ОШИБКА: proxy connect не установлен" >&2
        return 1
    }
    broray_interface_ndmc "$description_command" >/dev/null || {
        echo "ОШИБКА: описание не установлено" >&2
        return 1
    }
    broray_interface_ndmc "interface $name security-level public" >/dev/null || {
        echo "ОШИБКА: security-level не установлен" >&2
        return 1
    }
    broray_interface_ndmc "interface $name up" >/dev/null || {
        echo "ОШИБКА: интерфейс не включён" >&2
        return 1
    }
    broray_interface_save || return 1
    broray_interface_exists || {
        echo "ОШИБКА: интерфейс отсутствует после сохранения" >&2
        return 1
    }
    broray_interface_wait_present \
        "$name" \
        "$BRORAY_PROXY_HOST" \
        "$BRORAY_PROXY_PORT" \
        "$description" || {
        echo "BRORAY_PROXY_ERROR:PROXY_CREATE_CONVERGENCE_TIMEOUT:running/startup/runtime не сошлись после сохранения" >&2
        return 1
    }
    echo "Интерфейс $name создан"
}

broray_interface_create()
{
    local create_rc

    broray_interface_select_for_action || return 1
    if broray_interface_exists; then
        if broray_interface_owner_valid "$BRORAY_INTERFACE"; then
            echo "ОШИБКА: интерфейс $BRORAY_INTERFACE уже существует" >&2
            echo "Используйте repair" >&2
        else
            echo "ОШИБКА: интерфейс $BRORAY_INTERFACE занят и не принадлежит BROray" >&2
            echo "Чужой прокси-интерфейс оставлен без изменений" >&2
        fi
        return 1
    fi

    create_rc=0
    broray_interface_create_unchecked || create_rc=$?
    if [ "$create_rc" -ne 0 ]; then
        if broray_interface_exists; then
            broray_interface_delete_unchecked || true
        fi
        return "$create_rc"
    fi

    broray_interface_owner_signature_matches "$BRORAY_INTERFACE" || {
        echo "ОШИБКА: созданный интерфейс не прошёл простую проверку BROray" >&2
        broray_interface_delete_unchecked || true
        return 1
    }
    broray_interface_owner_write "$BRORAY_INTERFACE" created || {
        echo "ОШИБКА: не удалось сохранить локальную запись интерфейса BROray" >&2
        broray_interface_delete_unchecked || true
        return 1
    }
    broray_interface_sync_route_policy "$BRORAY_INTERFACE" || return 1
}

broray_interface_restore_owned()
{
    local name host port description description_command expected_baseline exists_rc

    name="$1"
    host="$2"
    port="$3"
    description="$4"
    description_command="$(broray_interface_description_command "$description")" || return 1
    expected_baseline="${5:-}"
    exists_rc=0
    broray_interface_exists_name "$name" || exists_rc=$?
    case "$exists_rc" in
        0) ;;
        1) broray_interface_ndmc_stage rollback-create-interface "interface $name" || return 1 ;;
        *) return 1 ;;
    esac
    broray_interface_ndmc_stage rollback-protocol "interface $name proxy protocol socks5" &&
    broray_interface_ndmc_stage rollback-upstream "interface $name proxy upstream $host $port" &&
    broray_interface_ndmc_stage rollback-connect "interface $name proxy connect" &&
    broray_interface_ndmc_stage rollback-description "$description_command" &&
    broray_interface_ndmc_stage rollback-security-level "interface $name security-level public" &&
    broray_interface_ndmc_stage rollback-admin-up "interface $name up" &&
    sleep "$BRORAY_PROXY_CONFIG_SETTLE_DELAY" &&
    broray_interface_ndmc_stage rollback-save 'system configuration save' &&
    broray_interface_wait_present "$name" "$host" "$port" "$description" "$expected_baseline"
}

broray_interface_migrate_legacy_owner()
{
    local name old_form desired desired_command failure

    name="$1"
    broray_interface_legacy_owner_migratable "$name" || return 1
    desired="$(broray_interface_expected_description)" || return 1
    desired_command="$(broray_interface_description_command "$desired")" || return 1
    if broray_interface_owner_block "$name" | grep -Fqx '    description null'; then
        old_form=null
    else
        old_form=absent
    fi

    failure=''
    broray_interface_ndmc_stage description "$desired_command" || failure=description
    [ -n "$failure" ] || broray_interface_ndmc_stage connect "interface $name proxy connect" || failure=connect
    [ -n "$failure" ] || broray_interface_ndmc_stage admin-up "interface $name up" || failure=admin-up
    [ -n "$failure" ] || sleep "$BRORAY_PROXY_CONFIG_SETTLE_DELAY"
    [ -n "$failure" ] || broray_interface_ndmc_stage save 'system configuration save' || failure=save
    [ -n "$failure" ] || broray_interface_sync_wait_exact "$name" "$BRORAY_PROXY_HOST" "$BRORAY_PROXY_PORT" "$desired" || failure=convergence-timeout
    if [ -n "$failure" ]; then
        if [ "$old_form" = null ]; then
            broray_interface_ndmc_stage rollback-description "interface $name description null" >/dev/null 2>&1 || true
        else
            broray_interface_ndmc_stage rollback-description "interface $name no description" >/dev/null 2>&1 || true
        fi
        broray_interface_ndmc_stage rollback-save 'system configuration save' >/dev/null 2>&1 || true
        if broray_interface_legacy_owner_migratable "$name"; then
            printf 'BRORAY_PROXY_LEGACY_MIGRATION_ROLLBACK=PASS failedStage=%s\n' "$failure" >&2
        else
            printf 'BRORAY_PROXY_ERROR:PROXY_RECOVERY_REQUIRED:legacy owner migration rollback не доказан failedStage=%s\n' "$failure" >&2
        fi
        return 1
    fi

    broray_interface_owner_write "$name" legacy-schema1-migrated "$desired" || return 1
    printf 'BRORAY_PROXY_OWNER_MIGRATION=PASS schema=2 description=%s\n' "$desired"
}

broray_interface_repair_unchecked()
{
    local name old_host old_port old_description desired_description desired_command baseline_sha changed failure

    name="$BRORAY_INTERFACE"
    broray_interface_require_owned "$name" || return 1
    if jq -e '.schemaVersion==1' "$BRORAY_INTERFACE_OWNER_FILE" >/dev/null 2>&1; then
        broray_interface_migrate_legacy_owner "$name" || return 1
    fi
    old_host="$(jq -r '.upstream.host' "$BRORAY_INTERFACE_OWNER_FILE")" || return 1
    old_port="$(jq -r '.upstream.port' "$BRORAY_INTERFACE_OWNER_FILE")" || return 1
    old_description="$(jq -r '.description' "$BRORAY_INTERFACE_OWNER_FILE")" || return 1
    desired_description="$(broray_interface_expected_description)" || return 1
    broray_interface_description_valid "$desired_description" || return 1
    desired_command="$(broray_interface_description_command "$desired_description")" || return 1
    baseline_sha="$(broray_interface_config_pair_without_proxy_sha256 \
        "$name" "$old_host" "$old_port" "$old_description")" || return 1

    if [ "$old_host" = "$BRORAY_PROXY_HOST" ] && [ "$old_port" = "$BRORAY_PROXY_PORT" ] &&
       [ "$old_description" = "$desired_description" ] && broray_interface_runtime_ready "$desired_description"; then
        printf '%s\n' 'Исправление не требуется'
        return 0
    fi

    changed=false
    failure=''
    if [ "$old_host" != "$BRORAY_PROXY_HOST" ] || [ "$old_port" != "$BRORAY_PROXY_PORT" ]; then
        broray_interface_ndmc_stage upstream "interface $name proxy upstream $BRORAY_PROXY_HOST $BRORAY_PROXY_PORT" || failure=upstream
        [ -n "$failure" ] || changed=true
    fi
    if [ -z "$failure" ] && [ "$old_description" != "$desired_description" ]; then
        broray_interface_ndmc_stage description "$desired_command" || failure=description
        [ -n "$failure" ] || changed=true
    fi
    if [ -z "$failure" ] && [ "$changed" = true ]; then
        broray_interface_save save || failure=save
    fi
    if [ -z "$failure" ]; then
        broray_interface_wait_present "$name" "$BRORAY_PROXY_HOST" "$BRORAY_PROXY_PORT" "$desired_description" "$baseline_sha" || failure=convergence-timeout
    fi

    if [ -n "$failure" ]; then
        broray_interface_failure_preserve >/dev/null 2>&1 || true
        if broray_interface_restore_owned "$name" "$old_host" "$old_port" "$old_description" "$baseline_sha"; then
            printf 'BRORAY_PROXY_REPAIR_ROLLBACK=PASS failedStage=%s\n' "$failure" >&2
        else
            printf 'BRORAY_PROXY_ERROR:PROXY_RECOVERY_REQUIRED:repair rollback не доказан failedStage=%s\n' "$failure" >&2
        fi
        return 1
    fi

    broray_interface_owner_write "$name" repaired "$desired_description" || {
        broray_interface_restore_owned "$name" "$old_host" "$old_port" "$old_description" >/dev/null 2>&1 || true
        return 1
    }
    broray_interface_sync_route_policy "$name" || return 1
    rm -f "$BRORAY_INTERFACE_FAILURE_EVIDENCE" 2>/dev/null || true
    printf '%s\n' 'Интерфейс исправлен и подтверждён exact receipt'
}

broray_interface_repair()
{
    local create_rc

    broray_interface_select_for_action || return 1
    if broray_interface_exists; then
        broray_interface_require_owned "$BRORAY_INTERFACE" || return 1
        broray_interface_repair_unchecked || return $?
    else
        create_rc=0
        broray_interface_create_unchecked || create_rc=$?
        if [ "$create_rc" -ne 0 ]; then
            if broray_interface_exists; then
                broray_interface_delete_unchecked || true
            fi
            return "$create_rc"
        fi
    fi

    broray_interface_owner_signature_matches "$BRORAY_INTERFACE" || {
        echo "ОШИБКА: интерфейс не прошёл простую проверку BROray" >&2
        return 1
    }
    broray_interface_owner_write "$BRORAY_INTERFACE" repaired || {
        echo "ОШИБКА: не удалось сохранить локальную запись интерфейса BROray" >&2
        return 1
    }
    broray_interface_sync_route_policy "$BRORAY_INTERFACE" || return 1
}

broray_interface_delete_unchecked()
{
    if ! broray_interface_exists; then
        echo "Интерфейс $BRORAY_INTERFACE уже отсутствует"
        return 0
    fi

    echo "Удаление интерфейса $BRORAY_INTERFACE"
    broray_interface_ndmc "no interface $BRORAY_INTERFACE" >/dev/null || {
        echo "ОШИБКА: KeeneticOS отклонила удаление" >&2
        return 1
    }
    broray_interface_save || return 1
    if broray_interface_exists; then
        echo "ОШИБКА: интерфейс остался после удаления" >&2
        return 1
    fi
    echo "Интерфейс $BRORAY_INTERFACE удалён"
}

broray_interface_delete()
{
    local owner_name selected

    owner_name="$(broray_interface_owner_name 2>/dev/null || true)"
    if [ -z "$owner_name" ]; then
        selected="$(broray_interface_selected_name 2>/dev/null || true)"
        if [ -n "$selected" ] &&
           [ ! -e "$BRORAY_INTERFACE_OWNER_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OWNER_FILE" ] &&
           broray_interface_source_name_absent running "$selected" &&
           broray_interface_source_name_absent startup "$selected"; then
            printf 'Интерфейс %s уже отсутствует; delete mutation не выполнялась\n' "$selected"
            return 0
        fi
        printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:полный R14C01 ownership receipt отсутствует' >&2
        return 1
    fi
    broray_interface_require_write_policy || return 1
    BRORAY_INTERFACE="$owner_name"
    export BRORAY_INTERFACE
    broray_interface_delete_unchecked
}

broray_interface_recover_provisional()
{
    local name

    broray_interface_require_write_policy || return 1
    [ -f "$BRORAY_INTERFACE_OPERATION_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OPERATION_FILE" ] || return 1
    name="$(jq -r '.interfaceName // empty' "$BRORAY_INTERFACE_OPERATION_FILE" 2>/dev/null)"
    broray_interface_name_valid "$name" || return 1
    BRORAY_INTERFACE="$name"
    export BRORAY_INTERFACE
    broray_interface_provisional_cleanup "$name"
}
