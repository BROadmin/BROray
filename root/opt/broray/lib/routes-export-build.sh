#!/opt/bin/ash

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_ROUTES_EXPORT_ACTIVE_WORK=""
BRORAY_ROUTES_EXPORT_LOCK="$BRORAY_ROUTES_ROOT/locks/operation.lock"

broray_routes_export_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_export_error()
{
    printf '%s\n' "$*" >&2
    exit 1
}

broray_routes_export_bundle_id_valid()
{
    local value

    value="${1:-}"

    [ -n "$value" ] || return 1

    case "$value" in
        *[!a-z0-9_-]*) return 1 ;;
    esac

    return 0
}

broray_routes_export_is_pid()
{
    case "${1:-}" in
        ''|*[!0-9]*) return 1 ;;
    esac

    [ "$1" -gt 1 ] 2>/dev/null
}

broray_routes_export_lock_acquire()
{
    local lock_parent lock_pid

    lock_parent="$(dirname "$BRORAY_ROUTES_EXPORT_LOCK")"
    mkdir -p "$lock_parent" || return 1

    if mkdir "$BRORAY_ROUTES_EXPORT_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_ROUTES_EXPORT_LOCK/pid"
        printf '%s\n' "build-export" >"$BRORAY_ROUTES_EXPORT_LOCK/action"
        printf '%s\n' "$BRORAY_ROUTES_EXPORT_BUNDLE" >"$BRORAY_ROUTES_EXPORT_LOCK/bundle"
        return 0
    fi

    lock_pid="$(sed -n '1p' "$BRORAY_ROUTES_EXPORT_LOCK/pid" 2>/dev/null)"

    if broray_routes_export_is_pid "$lock_pid" &&
       kill -0 "$lock_pid" 2>/dev/null
    then
        return 2
    fi

    rm -rf "$BRORAY_ROUTES_EXPORT_LOCK" 2>/dev/null || return 1

    if mkdir "$BRORAY_ROUTES_EXPORT_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_ROUTES_EXPORT_LOCK/pid"
        printf '%s\n' "build-export" >"$BRORAY_ROUTES_EXPORT_LOCK/action"
        printf '%s\n' "$BRORAY_ROUTES_EXPORT_BUNDLE" >"$BRORAY_ROUTES_EXPORT_LOCK/bundle"
        return 0
    fi

    return 1
}

broray_routes_export_lock_release()
{
    rm -rf "$BRORAY_ROUTES_EXPORT_LOCK" 2>/dev/null || true
}

broray_routes_export_cleanup()
{
    if [ -n "$BRORAY_ROUTES_EXPORT_ACTIVE_WORK" ]; then
        rm -rf "$BRORAY_ROUTES_EXPORT_ACTIVE_WORK" 2>/dev/null || true
    fi

    broray_routes_export_lock_release
}

broray_routes_export_build_run()
{
    local bundle_id bundles config catalog routes_file version_file state_file
    local managed_interface route_comment catalog_interface catalog_comment
    local content_sha version_sha source_commit source_date expected_count actual_count
    local lock_result work routes_tsv bat_new plan_new bat_sha now state_new
    local unique_count plan_count bat_count catalog_normalized_sha

    bundle_id="${1:-}"

    broray_routes_export_bundle_id_valid "$bundle_id" ||
        broray_routes_export_error "Некорректный идентификатор набора."

    BRORAY_ROUTES_EXPORT_BUNDLE="$bundle_id"
    export BRORAY_ROUTES_EXPORT_BUNDLE

    bundles="$BRORAY_ROUTES_ROOT/bundles.json"
    config="$BRORAY_ROUTES_ROOT/config.json"
    catalog="$BRORAY_ROUTES_ROOT/catalog/$bundle_id"
    routes_file="$catalog/routes.json"
    version_file="$catalog/version.json"
    state_file="$BRORAY_ROUTES_ROOT/state/$bundle_id.json"

    [ -r "$bundles" ] ||
        broray_routes_export_error "Реестр разрешённых наборов недоступен."

    jq -e --arg bundle_id "$bundle_id" '
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (.bundles | index($bundle_id) != null)
    ' "$bundles" >/dev/null ||
        broray_routes_export_error "Набор маршрутов не разрешён: $bundle_id"

    [ -r "$config" ] ||
        broray_routes_export_error "Конфигурация маршрутов недоступна."

    [ -r "$routes_file" ] ||
        broray_routes_export_error "Проверенная версия набора не скачана."

    [ -r "$version_file" ] ||
        broray_routes_export_error "Сведения о скачанной версии отсутствуют."

    [ -r "$state_file" ] ||
        broray_routes_export_error "Состояние набора недоступно."

    managed_interface="$(jq -r '.managedInterface // empty' "$config")"
    route_comment="$(jq -r '.routeComment // empty' "$config")"

    case "$managed_interface" in
        Proxy[0-9]*) ;;
        *) broray_routes_export_error "Некорректный управляемый интерфейс ProxyN." ;;
    esac
    case "${managed_interface#Proxy}" in
        ''|*[!0-9]*) broray_routes_export_error "Некорректный управляемый интерфейс ProxyN." ;;
    esac

    [ "$route_comment" = "BROray" ] ||
        broray_routes_export_error "Некорректная метка маршрутов BROray."

    jq -e --arg target_interface "$managed_interface" '
        (.schemaVersion == 1) and
        (.managedInterface == $target_interface) and
        (.managedMetric == 1200) and
        (.routeComment == "BROray") and
        (.ownershipPolicy.touchOtherInterfaces == false) and
        (.ownershipPolicy.modifyExternalRoutes == false) and
        (.ownershipPolicy.deleteExternalRoutes == false)
    ' "$config" >/dev/null ||
        broray_routes_export_error "Политика защиты пользовательских маршрутов повреждена."

    catalog_interface="$(jq -r '.targetInterface // empty' "$routes_file")"
    catalog_comment="$(jq -r '.routeComment // empty' "$routes_file")"
    content_sha="$(jq -r '.contentSha256 // empty' "$routes_file")"
    version_sha="$(jq -r '.contentSha256 // empty' "$version_file")"
    source_commit="$(jq -r '.sourceCommit // empty' "$version_file")"
    source_date="$(jq -r '.sourceDate // empty' "$version_file")"
    expected_count="$(jq -r '.routeCount // 0' "$routes_file")"

    [ "$catalog_interface" = "$managed_interface" ] ||
        broray_routes_export_error "Каталог предназначен не для $managed_interface."

    [ "$catalog_comment" = "$route_comment" ] ||
        broray_routes_export_error "Метка каталога не совпадает с политикой BROray."

    [ -n "$content_sha" ] &&
    [ "$content_sha" = "$version_sha" ] ||
        broray_routes_export_error "Версия каталога не прошла сверку SHA-256."

    case "$content_sha" in
        *[!0-9a-fA-F]*|'')
            broray_routes_export_error "Некорректный SHA-256 каталога."
            ;;
    esac

    [ "${#content_sha}" -eq 64 ] ||
        broray_routes_export_error "Некорректная длина SHA-256 каталога."

    case "$expected_count" in
        ''|*[!0-9]*)
            broray_routes_export_error "Некорректное количество маршрутов."
            ;;
    esac

    [ "$expected_count" -gt 0 ] ||
        broray_routes_export_error "Каталог маршрутов пуст."

    jq -e \
        --arg bundle_id "$bundle_id" \
        --arg target_interface "$managed_interface" \
        --arg route_comment "$route_comment" \
        --arg content_sha "$content_sha" \
        --argjson route_count "$expected_count" \
        '
            (.schemaVersion == 1) and
            (.bundleId == $bundle_id) and
            (.targetInterface == $target_interface) and
            (.routeComment == $route_comment) and
            (.contentSha256 == $content_sha) and
            (.routeCount == $route_count) and
            ((.routes | type) == "array") and
            ((.routes | length) == $route_count) and
            (all(.routes[];
                (.family == "ipv4") and
                ((.network | type) == "string") and
                ((.prefix | type) == "number") and
                (.prefix >= 1) and
                (.prefix <= 32)
            ))
        ' "$routes_file" >/dev/null ||
        broray_routes_export_error "Каталог маршрутов повреждён."

    lock_result=0
    broray_routes_export_lock_acquire || lock_result=$?

    case "$lock_result" in
        0) ;;
        2) broray_routes_export_error "Другая операция с маршрутами уже выполняется." ;;
        *) broray_routes_export_error "Не удалось установить блокировку операции." ;;
    esac

    work="$BRORAY_ROUTES_ROOT/tmp/build-export-$bundle_id.$$"
    BRORAY_ROUTES_EXPORT_ACTIVE_WORK="$work"
    routes_tsv="$work/routes.tsv"
    bat_new="$work/keenetic-routes.bat"
    plan_new="$work/export-plan.json"

    mkdir -p "$work" || {
        broray_routes_export_lock_release
        broray_routes_export_error "Не удалось создать временный каталог."
    }

    trap 'broray_routes_export_cleanup' EXIT HUP INT TERM

    jq -r '
        .routes[] |
        [
            .network,
            (.prefix | tostring)
        ] |
        @tsv
    ' "$routes_file" >"$routes_tsv" ||
        broray_routes_export_error "Не удалось подготовить маршруты к экспорту."

    actual_count="$(wc -l <"$routes_tsv" | tr -d ' ')"

    [ "$actual_count" = "$expected_count" ] ||
        broray_routes_export_error "Число маршрутов изменилось при подготовке экспорта."

    LC_ALL=C sort -u "$routes_tsv" >"$work/routes.unique.tsv" ||
        broray_routes_export_error "Не удалось проверить дубли маршрутов."

    unique_count="$(wc -l <"$work/routes.unique.tsv" | tr -d ' ')"

    [ "$unique_count" = "$expected_count" ] ||
        broray_routes_export_error "В каталоге обнаружены дубли маршрутов."

    awk -F '\t' \
        -v comment="$route_comment" \
        -v plan_tsv="$work/plan.tsv" \
        '
        BEGIN {
            mask[1]="128.0.0.0"
            mask[2]="192.0.0.0"
            mask[3]="224.0.0.0"
            mask[4]="240.0.0.0"
            mask[5]="248.0.0.0"
            mask[6]="252.0.0.0"
            mask[7]="254.0.0.0"
            mask[8]="255.0.0.0"
            mask[9]="255.128.0.0"
            mask[10]="255.192.0.0"
            mask[11]="255.224.0.0"
            mask[12]="255.240.0.0"
            mask[13]="255.248.0.0"
            mask[14]="255.252.0.0"
            mask[15]="255.254.0.0"
            mask[16]="255.255.0.0"
            mask[17]="255.255.128.0"
            mask[18]="255.255.192.0"
            mask[19]="255.255.224.0"
            mask[20]="255.255.240.0"
            mask[21]="255.255.248.0"
            mask[22]="255.255.252.0"
            mask[23]="255.255.254.0"
            mask[24]="255.255.255.0"
            mask[25]="255.255.255.128"
            mask[26]="255.255.255.192"
            mask[27]="255.255.255.224"
            mask[28]="255.255.255.240"
            mask[29]="255.255.255.248"
            mask[30]="255.255.255.252"
            mask[31]="255.255.255.254"
            mask[32]="255.255.255.255"
        }

        function valid_octet(value) {
            return value ~ /^[0-9][0-9]*$/ && value + 0 >= 0 && value + 0 <= 255
        }

        function network_aligned(ip, prefix, ip_part, mask_part, i, partial, block, ip_count, mask_count) {
            ip_count = split(ip, ip_part, ".")
            mask_count = split(mask[prefix], mask_part, ".")

            if (ip_count != 4 || mask_count != 4) {
                return 0
            }

            partial = 0

            for (i = 1; i <= 4; i += 1) {
                if (!valid_octet(ip_part[i])) {
                    return 0
                }

                if (partial) {
                    if ((ip_part[i] + 0) != 0) {
                        return 0
                    }
                    continue
                }

                if ((mask_part[i] + 0) == 255) {
                    continue
                }

                if ((mask_part[i] + 0) == 0) {
                    partial = 1
                    if ((ip_part[i] + 0) != 0) {
                        return 0
                    }
                    continue
                }

                block = 256 - (mask_part[i] + 0)

                if (((ip_part[i] + 0) % block) != 0) {
                    return 0
                }

                partial = 1
            }

            return 1
        }

        {
            network=$1
            prefix=$2 + 0

            if (prefix < 1 || prefix > 32 || !(prefix in mask)) {
                print "Некорректный префикс: " $0 > "/dev/stderr"
                exit 21
            }

            if (!network_aligned(network, prefix)) {
                print "Некорректная сеть: " $0 > "/dev/stderr"
                exit 22
            }

            printf "route add %s mask %s 0.0.0.0 metric 1200 :: rem %s\r\n", network, mask[prefix], comment
            printf "%s\t%s\t%s\n", network, prefix, mask[prefix] >> plan_tsv
        }
        ' "$routes_tsv" >"$bat_new" ||
        broray_routes_export_error "Не удалось сформировать файл Keenetic."

    bat_count="$(grep -c '^route add ' "$bat_new" 2>/dev/null || true)"

    [ "$bat_count" = "$expected_count" ] ||
        broray_routes_export_error "Файл Keenetic содержит неверное число маршрутов."

    if ! awk '
        {
            line=$0
            sub(/\r$/, "", line)

            if (line !~ /^route add [0-9][0-9.]* mask [0-9][0-9.]* 0\.0\.0\.0 metric 1200 :: rem BROray$/) {
                exit 1
            }
        }
    ' "$bat_new"
    then
        broray_routes_export_error "Файл Keenetic содержит недопустимые строки."
    fi

    bat_sha="$(sha256sum "$bat_new" | awk '{print $1}')"
    now="$(broray_routes_export_now)"

    jq -Rn \
        --arg bundle_id "$bundle_id" \
        --arg source_commit "$source_commit" \
        --arg source_date "$source_date" \
        --arg content_sha "$content_sha" \
        --arg target_interface "$managed_interface" \
        --arg route_comment "$route_comment" \
        --arg bat_sha "$bat_sha" \
        --arg prepared_at "$now" \
        --argjson route_count "$expected_count" \
        '
            [
                inputs |
                select(length > 0) |
                split("\t") |
                {
                    key: (
                        "ipv4|" + .[0] + "/" + .[1] +
                        "|" + $target_interface +
                        "|gateway:none|metric:1200" +
                        "|automatic:unspecified|exclusive:unspecified"
                    ),
                    family: "ipv4",
                    network: .[0],
                    prefix: (.[1] | tonumber),
                    mask: .[2],
                    targetInterface: $target_interface,
                    gatewayToken: "0.0.0.0",
                    metric: 1200,
                    automatic: null,
                    exclusive: null,
                    comment: $route_comment
                }
            ] as $routes |
            {
                schemaVersion: 1,
                bundleId: $bundle_id,
                sourceCommit: $source_commit,
                sourceDate: $source_date,
                contentSha256: $content_sha,
                targetInterface: $target_interface,
                managedMetric: 1200,
                routeComment: $route_comment,
                routeFile: "keenetic-routes.bat",
                routeFileSha256: $bat_sha,
                routeCount: $route_count,
                preparedAt: $prepared_at,
                routerApplied: false,
                routes: $routes
            }
        ' <"$work/plan.tsv" >"$plan_new" ||
        broray_routes_export_error "Не удалось сформировать план экспорта."

    jq -e \
        --arg bundle_id "$bundle_id" \
        --arg content_sha "$content_sha" \
        --arg target_interface "$managed_interface" \
        --arg route_comment "$route_comment" \
        --arg bat_sha "$bat_sha" \
        --argjson route_count "$expected_count" \
        '
            (.schemaVersion == 1) and
            (.bundleId == $bundle_id) and
            (.contentSha256 == $content_sha) and
            (.targetInterface == $target_interface) and
            (.managedMetric == 1200) and
            (.routeComment == $route_comment) and
            (.routeFileSha256 == $bat_sha) and
            (.routeCount == $route_count) and
            (.routerApplied == false) and
            ((.routes | type) == "array") and
            ((.routes | length) == $route_count) and
            (all(.routes[];
                (.targetInterface == $target_interface) and
                (.metric == 1200) and
                (.comment == "BROray") and
                (.gatewayToken == "0.0.0.0")
            ))
        ' "$plan_new" >/dev/null ||
        broray_routes_export_error "План экспорта не прошёл проверку."

    plan_count="$(jq -r '.routes | length' "$plan_new")"

    [ "$plan_count" = "$expected_count" ] ||
        broray_routes_export_error "План экспорта содержит неверное число маршрутов."

    mv "$bat_new" "$catalog/keenetic-routes.bat" ||
        broray_routes_export_error "Не удалось сохранить файл Keenetic."

    mv "$plan_new" "$catalog/export-plan.json" ||
        broray_routes_export_error "Не удалось сохранить план экспорта."

    chmod 644 "$catalog/keenetic-routes.bat" "$catalog/export-plan.json" ||
        broray_routes_export_error "Не удалось установить права файлов экспорта."

    catalog_normalized_sha="$(sha256sum "$catalog/normalized.txt" | awk '{print $1}')"

    [ "$catalog_normalized_sha" = "$content_sha" ] ||
        broray_routes_export_error "Нормализованный каталог изменился во время операции."

    state_new="$state_file.new.$$"

    jq \
        --arg now "$now" \
        --arg source_commit "$source_commit" \
        --arg content_sha "$content_sha" \
        --arg bat_sha "$bat_sha" \
        --arg file_path "$catalog/keenetic-routes.bat" \
        --arg plan_path "$catalog/export-plan.json" \
        --arg target_interface "$managed_interface" \
        --arg route_comment "$route_comment" \
        --arg message "Маршруты готовы к экспорту" \
        --argjson route_count "$expected_count" \
        '
            .exportBuild = {
                result: "prepared",
                message: $message,
                sourceCommit: $source_commit,
                contentSha256: $content_sha,
                routeFileSha256: $bat_sha,
                routeFile: $file_path,
                planFile: $plan_path,
                routeCount: $route_count,
                targetInterface: $target_interface,
                managedMetric: 1200,
                routeComment: $route_comment,
                routerApplied: false,
                preparedAt: $now
            } |
            .lastError = null |
            .updatedAt = $now
        ' "$state_file" >"$state_new" || {
            rm -f "$state_new"
            broray_routes_export_error "Не удалось обновить локальное состояние."
        }

    mv "$state_new" "$state_file" ||
        broray_routes_export_error "Не удалось сохранить локальное состояние."

    broray_routes_export_lock_release
    BRORAY_ROUTES_EXPORT_ACTIVE_WORK=""
    rm -rf "$work"
    trap - EXIT HUP INT TERM

    echo "Маршруты готовы к экспорту"
    echo "Набор: $bundle_id"
    echo "Интерфейс будущего экспорта: $managed_interface"
    echo "Маршрутов: $expected_count"
    echo "Метка: $route_comment"
    echo "Файл: $catalog/keenetic-routes.bat"
    echo "План: $catalog/export-plan.json"
    echo "Маршруты Keenetic не изменялись."
}
