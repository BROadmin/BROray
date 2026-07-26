#!/opt/bin/ash

# BROray 2.1.1 — safe local BAT route importer.
# Uploaded BAT files are treated strictly as text and are never executed.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_USER_ROUTES_INDEX="${BRORAY_USER_ROUTES_INDEX:-$BRORAY_ROUTES_ROOT/custom.json}"
BRORAY_USER_ROUTES_PREVIEWS="${BRORAY_USER_ROUTES_PREVIEWS:-$BRORAY_ROUTES_ROOT/tmp/user-previews}"
BRORAY_USER_ROUTES_MAX_BYTES="${BRORAY_USER_ROUTES_MAX_BYTES:-2097152}"
BRORAY_USER_ROUTES_MAX_FILES="${BRORAY_USER_ROUTES_MAX_FILES:-16}"
BRORAY_USER_ROUTES_MAX_ROUTES="${BRORAY_USER_ROUTES_MAX_ROUTES:-100000}"
BRORAY_USER_ROUTES_PREVIEW_TTL="${BRORAY_USER_ROUTES_PREVIEW_TTL:-1800}"
BRORAY_USER_ROUTES_LOCK="${BRORAY_USER_ROUTES_LOCK:-$BRORAY_ROUTES_ROOT/locks/operation.lock}"
BRORAY_USER_ROUTES_ACTIVE_WORK=""
BRORAY_USER_ROUTES_LOCK_HELD=false

broray_user_routes_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_user_routes_epoch()
{
    date '+%s'
}

broray_user_routes_stamp()
{
    date '+%Y%m%d-%H%M%S'
}

broray_user_routes_fail()
{
    code="$1"
    shift
    printf 'BRORAY_ERROR:%s:%s\n' "$code" "$*" >&2
    return 1
}

broray_user_routes_cleanup()
{
    if [ -n "$BRORAY_USER_ROUTES_ACTIVE_WORK" ]; then
        rm -rf "$BRORAY_USER_ROUTES_ACTIVE_WORK" 2>/dev/null || true
    fi

    if [ "$BRORAY_USER_ROUTES_LOCK_HELD" = true ]; then
        rm -rf "$BRORAY_USER_ROUTES_LOCK" 2>/dev/null || true
        BRORAY_USER_ROUTES_LOCK_HELD=false
    fi
}

broray_user_routes_require_command()
{
    command -v "$1" >/dev/null 2>&1 ||
        broray_user_routes_fail "DEPENDENCY_MISSING" "Не найдена команда: $1"
}

broray_user_routes_id_valid()
{
    value="${1:-}"

    case "$value" in
        user-*) id_suffix="${value#user-}" ;;
        *) return 1 ;;
    esac

    [ -n "$id_suffix" ] || return 1
    case "$id_suffix" in
        *[!a-z0-9_-]*) return 1 ;;
    esac

    [ "${#value}" -le 63 ] || return 1
    return 0
}

broray_user_routes_token_valid()
{
    value="${1:-}"
    [ "${#value}" -eq 64 ] || return 1

    case "$value" in
        *[!0-9a-f]*) return 1 ;;
    esac

    return 0
}

broray_user_routes_lock_acquire()
{
    lock_pid=""
    mkdir -p "$(dirname "$BRORAY_USER_ROUTES_LOCK")" || return 1

    if mkdir "$BRORAY_USER_ROUTES_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_USER_ROUTES_LOCK/pid"
        printf '%s\n' "user-import" >"$BRORAY_USER_ROUTES_LOCK/action"
        BRORAY_USER_ROUTES_LOCK_HELD=true
        return 0
    fi

    lock_pid="$(sed -n '1p' "$BRORAY_USER_ROUTES_LOCK/pid" 2>/dev/null)"
    case "$lock_pid" in
        ''|*[!0-9]*) lock_pid="" ;;
    esac

    if [ -n "$lock_pid" ] && kill -0 "$lock_pid" 2>/dev/null; then
        return 2
    fi

    rm -rf "$BRORAY_USER_ROUTES_LOCK" 2>/dev/null || return 1

    if mkdir "$BRORAY_USER_ROUTES_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_USER_ROUTES_LOCK/pid"
        printf '%s\n' "user-import" >"$BRORAY_USER_ROUTES_LOCK/action"
        BRORAY_USER_ROUTES_LOCK_HELD=true
        return 0
    fi

    return 1
}

broray_user_routes_prepare_runtime()
{
    mkdir -p \
        "$BRORAY_ROUTES_ROOT/catalog" \
        "$BRORAY_ROUTES_ROOT/manifests" \
        "$BRORAY_ROUTES_ROOT/state" \
        "$BRORAY_ROUTES_ROOT/installed/bundles" \
        "$BRORAY_ROUTES_ROOT/backup" \
        "$BRORAY_ROUTES_ROOT/tmp" \
        "$BRORAY_USER_ROUTES_PREVIEWS" \
        "$BRORAY_ROOT/tmp" ||
        broray_user_routes_fail \
            "RUNTIME_PREPARE_FAILED" \
            "Не удалось подготовить каталоги пользовательских маршрутов."

    chmod 700 "$BRORAY_USER_ROUTES_PREVIEWS" 2>/dev/null || true

    if [ ! -f "$BRORAY_USER_ROUTES_INDEX" ]; then
        index_new="$BRORAY_USER_ROUTES_INDEX.new.$$"
        cat >"$index_new" <<'JSON'
{
  "schemaVersion": 1,
  "bundles": []
}
JSON
        chmod 600 "$index_new" 2>/dev/null || true
        mv "$index_new" "$BRORAY_USER_ROUTES_INDEX" ||
            broray_user_routes_fail \
                "INDEX_CREATE_FAILED" \
                "Не удалось создать реестр пользовательских наборов."
    fi

    jq -e '
        def valid_bundle_id:
            (. | type) == "string" and
            startswith("user-") and
            (length <= 63) and
            (ltrimstr("user-") | length > 0) and
            all(
                ltrimstr("user-") | explode[];
                (. >= 48 and . <= 57) or
                (. >= 97 and . <= 122) or
                (. == 45) or
                (. == 95)
            );

        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (all(.bundles[];
            (.id | valid_bundle_id) and
            ((.name | type) == "string") and
            ((.name | length) > 0)
        )) and
        (([.bundles[].id] | length) == ([.bundles[].id] | unique | length))
    ' "$BRORAY_USER_ROUTES_INDEX" >/dev/null 2>&1 ||
        broray_user_routes_fail \
            "INDEX_INVALID" \
            "Реестр пользовательских маршрутов повреждён."
}

broray_user_routes_cleanup_previews()
{
    now_epoch="$(broray_user_routes_epoch)"

    for preview_dir in "$BRORAY_USER_ROUTES_PREVIEWS"/*; do
        [ -d "$preview_dir" ] || continue
        created_epoch="$(jq -r '.createdEpoch // 0' "$preview_dir/report.json" 2>/dev/null)"

        case "$created_epoch" in
            ''|*[!0-9]*) created_epoch=0 ;;
        esac

        age=$((now_epoch - created_epoch))
        if [ "$age" -gt "$BRORAY_USER_ROUTES_PREVIEW_TTL" ] 2>/dev/null; then
            rm -rf "$preview_dir" 2>/dev/null || true
        fi
    done
}

# Parse one BAT file. Output includes every valid route line after network
# normalization; de-duplication is deliberately performed after all files are
# combined so the report can distinguish normalization and duplicate removal.
broray_user_routes_parse_bat()
{
    input="$1"
    output="$2"
    stats="$3"
    errors="$4"

    rm -f "$output" "$stats" "$errors"

    awk \
        -v stats_file="$stats" \
        -v error_file="$errors" '
        function report(message) {
            if (error_count < 20) {
                print "Строка " NR ": " message > error_file
            }
            error_count += 1
            bad = 1
        }

        function valid_octet(value, number) {
            if (value !~ /^[0-9]+$/) return 0
            number = value + 0
            return number >= 0 && number <= 255
        }

        function valid_ipv4(value, parts, count, i) {
            count = split(value, parts, ".")
            if (count != 4) return 0
            for (i = 1; i <= 4; i += 1) {
                if (!valid_octet(parts[i])) return 0
            }
            return 1
        }

        function mask_octet_bits(value) {
            if (value == 255) return 8
            if (value == 254) return 7
            if (value == 252) return 6
            if (value == 248) return 5
            if (value == 240) return 4
            if (value == 224) return 3
            if (value == 192) return 2
            if (value == 128) return 1
            if (value == 0) return 0
            return -1
        }

        function mask_prefix(value, parts, count, i, bits, zero_seen, prefix) {
            count = split(value, parts, ".")
            if (count != 4) return -1
            zero_seen = 0
            prefix = 0
            for (i = 1; i <= 4; i += 1) {
                if (!valid_octet(parts[i])) return -1
                bits = mask_octet_bits(parts[i] + 0)
                if (bits < 0) return -1
                if (zero_seen && bits != 0) return -1
                if (bits < 8) zero_seen = 1
                prefix += bits
            }
            return prefix
        }

        function normalized_network(ip, mask, ip_parts, mask_parts, i, block, octet, result) {
            split(ip, ip_parts, ".")
            split(mask, mask_parts, ".")
            result = ""
            for (i = 1; i <= 4; i += 1) {
                if ((mask_parts[i] + 0) == 255) {
                    octet = ip_parts[i] + 0
                } else {
                    block = 256 - (mask_parts[i] + 0)
                    octet = int((ip_parts[i] + 0) / block) * block
                }
                result = result (i == 1 ? "" : ".") octet
            }
            return result
        }

        {
            sub(/\r$/, "")
            line = $0
            sub(/^[ \t]+/, "", line)
            sub(/[ \t]+$/, "", line)
            lower_line = tolower(line)

            if (line == "" ||
                substr(line, 1, 1) == "#" ||
                substr(line, 1, 2) == "::" ||
                lower_line ~ /^rem([ \t]|$)/ ||
                lower_line ~ /^@?echo([ \t]|$)/ ||
                lower_line ~ /^chcp([ \t]|$)/ ||
                lower_line ~ /^title([ \t]|$)/ ||
                lower_line ~ /^pause([ \t]|$)/ ||
                lower_line ~ /^cls([ \t]|$)/ ||
                lower_line ~ /^setlocal([ \t]|$)/ ||
                lower_line ~ /^endlocal([ \t]|$)/ ||
                lower_line ~ /^exit[ \t]+\/b([ \t]|$)/) {
                next
            }

            count = split(line, fields, /[ \t]+/)
            if (count < 6 ||
                tolower(fields[1]) != "route" ||
                tolower(fields[2]) != "add" ||
                tolower(fields[4]) != "mask") {
                report("разрешена только команда route add <IPv4> mask <маска> 0.0.0.0")
                next
            }

            if (count > 6 &&
                (count < 8 || fields[7] != "&" || tolower(fields[8]) != "rem")) {
                report("после маршрута разрешён только комментарий & rem")
                next
            }

            network_original = fields[3]
            mask = fields[5]
            gateway = fields[6]

            if (!valid_ipv4(network_original)) {
                report("некорректный IPv4-адрес сети")
                next
            }

            prefix = mask_prefix(mask)
            if (prefix < 0) {
                report("некорректная или непоследовательная маска")
                next
            }

            if (gateway != "0.0.0.0") {
                report("разрешён только шлюз 0.0.0.0")
                next
            }

            network = normalized_network(network_original, mask)
            if (network == "0.0.0.0" && prefix == 0) {
                report("маршрут по умолчанию 0.0.0.0/0 запрещён")
                next
            }

            route_lines += 1
            if (network != network_original) normalized_lines += 1
            print network "/" prefix
        }

        END {
            print "routeLines=" (route_lines + 0) > stats_file
            print "normalizedLines=" (normalized_lines + 0) >> stats_file
            print "errorLines=" (error_count + 0) >> stats_file
            if (bad) exit 1
            if ((route_lines + 0) == 0) exit 2
        }
    ' "$input" >"$output"
}

# Lossless CIDR aggregation. It first removes subnets already covered by a
# broader network and then recursively merges exact siblings. IPv4 integers
# are below 2^32 and therefore exactly representable by awk numeric values.
broray_user_routes_collapse()
{
    input="$1"
    output="$2"
    numeric="$output.numeric.$$"

    awk '
        BEGIN {
            pow2[0] = 1
            for (i = 1; i <= 32; i += 1) pow2[i] = pow2[i - 1] * 2
        }

        function ip_to_num(ip, parts) {
            split(ip, parts, ".")
            return (((parts[1] * 256 + parts[2]) * 256 + parts[3]) * 256 + parts[4])
        }

        {
            split($0, parts, "/")
            n = ip_to_num(parts[1])
            p = parts[2] + 0
            candidate[n SUBSEP p] = 1
            net_by_key[n SUBSEP p] = n
            prefix_by_key[n SUBSEP p] = p
        }

        END {
            # Keep broader networks first and discard covered descendants.
            for (p = 0; p <= 32; p += 1) {
                for (key in candidate) {
                    if (prefix_by_key[key] != p) continue
                    n = net_by_key[key]
                    covered = 0
                    for (q = 0; q < p; q += 1) {
                        block = pow2[32 - q]
                        parent = int(n / block) * block
                        if ((parent SUBSEP q) in kept) {
                            covered = 1
                            break
                        }
                    }
                    if (!covered) kept[n SUBSEP p] = 1
                }
            }

            # Merge exact siblings. Newly created parents are processed at the
            # next (broader) prefix during this descending loop.
            for (p = 32; p >= 2; p -= 1) {
                block = pow2[32 - p]
                parent_block = block * 2
                delete parents

                for (key in kept) {
                    split(key, kp, SUBSEP)
                    if ((kp[2] + 0) != p) continue
                    n = kp[1] + 0
                    parent = int(n / parent_block) * parent_block
                    parents[parent] = 1
                }

                for (parent in parents) {
                    left = parent + 0
                    right = left + block
                    if (((left SUBSEP p) in kept) && ((right SUBSEP p) in kept)) {
                        delete kept[left SUBSEP p]
                        delete kept[right SUBSEP p]
                        kept[left SUBSEP (p - 1)] = 1
                    }
                }
            }

            for (key in kept) {
                split(key, kp, SUBSEP)
                print (kp[1] + 0) "\t" (kp[2] + 0)
            }
        }
    ' "$input" | LC_ALL=C sort -n -k1,1 -k2,2n >"$numeric" || {
        rm -f "$numeric"
        return 1
    }

    awk -F '\t' '
        function num_to_ip(value, a, b, c, d, rest) {
            a = int(value / 16777216)
            rest = value - a * 16777216
            b = int(rest / 65536)
            rest -= b * 65536
            c = int(rest / 256)
            d = rest - c * 256
            return a "." b "." c "." d
        }
        { print num_to_ip($1 + 0) "/" ($2 + 0) }
    ' "$numeric" >"$output" || {
        rm -f "$numeric"
        return 1
    }

    rm -f "$numeric"
}

broray_user_routes_verify_source_files()
{
    source_dir="$1"
    metadata_file="$2"
    verify_list="$BRORAY_ROOT/tmp/user-route-source-verify.$$.tsv"
    verify_ok=true
    verify_count=0

    jq -e 'type == "array" and (length > 0) and all(.[];
        ((.storedName | type) == "string") and
        ((.sha256 | type) == "string") and
        ((.sizeBytes | type) == "number") and
        (.sizeBytes > 0)
    )' "$metadata_file" >/dev/null 2>&1 || return 1

    jq -r '.[] | [.storedName, .sha256, (.sizeBytes | tostring)] | @tsv' \
        "$metadata_file" >"$verify_list" || return 1

    while IFS="$(printf '\t')" read -r stored_name expected_sha expected_size
    do
        [ -n "$stored_name" ] || { verify_ok=false; break; }
        case "$stored_name" in
            */*|*\\*|*[!0-9A-Za-z._-]*|.*|*..*)
                verify_ok=false
                break
                ;;
            *.bat) ;;
            *)
                verify_ok=false
                break
                ;;
        esac

        case "$expected_sha" in
            ''|*[!0-9a-f]* ) verify_ok=false; break ;;
        esac
        [ "${#expected_sha}" -eq 64 ] || { verify_ok=false; break; }
        case "$expected_size" in
            ''|*[!0-9]*) verify_ok=false; break ;;
        esac

        source_file="$source_dir/$stored_name"
        [ -r "$source_file" ] || { verify_ok=false; break; }
        actual_size="$(wc -c <"$source_file" | tr -d ' ')"
        actual_sha="$(sha256sum "$source_file" | awk '{print $1}')"
        [ "$actual_size" = "$expected_size" ] || { verify_ok=false; break; }
        [ "$actual_sha" = "$expected_sha" ] || { verify_ok=false; break; }
        verify_count=$((verify_count + 1))
    done <"$verify_list"

    expected_count="$(jq -r 'length' "$metadata_file")"
    rm -f "$verify_list"
    [ "$verify_ok" = true ] && [ "$verify_count" = "$expected_count" ]
}

broray_user_routes_build_routes_json()
{
    bundle_id="$1"
    interface="$2"
    content_sha="$3"
    routes_file="$4"
    output="$5"

    jq -Rn \
        --arg bundle_id "$bundle_id" \
        --arg target_interface "$interface" \
        --arg content_sha256 "$content_sha" '
        [
            inputs |
            select(length > 0) |
            split("/") as $parts |
            {
                family: "ipv4",
                network: $parts[0],
                prefix: ($parts[1] | tonumber)
            }
        ] as $routes |
        {
            schemaVersion: 1,
            bundleId: $bundle_id,
            targetInterface: $target_interface,
            routeComment: "BROray",
            contentSha256: $content_sha256,
            routeCount: ($routes | length),
            routes: $routes
        }
    ' <"$routes_file" >"$output"
}

broray_user_routes_name_from_json()
{
    request_file="$1"

    jq -r '
        if (.name | type) == "string" then
            .name
        else
            ""
        end
    ' "$request_file" |
        awk '
            NR == 1 {
                line = $0
                sub(/^[[:space:]]+/, "", line)
                sub(/[[:space:]]+$/, "", line)
                print line
                next
            }
            {
                exit 2
            }
        '
}

broray_user_routes_validate_name()
{
    name="$1"
    [ -n "$name" ] || return 1
    [ "${#name}" -le 80 ] || return 1
    printf '%s' "$name" | LC_ALL=C grep '[[:cntrl:]]' >/dev/null 2>&1 && return 1
    return 0
}

broray_user_routes_preview()
{
    request_file="$1"

    broray_user_routes_prepare_runtime || return 1
    broray_user_routes_cleanup_previews

    jq -e '
        (type == "object") and
        ((.name | type) == "string") and
        ((.files | type) == "array") and
        ((.files | length) > 0) and
        (all(.files[];
            ((.name | type) == "string") and
            ((.contentBase64 | type) == "string")
        ))
    ' "$request_file" >/dev/null 2>&1 ||
        broray_user_routes_fail \
            "REQUEST_INVALID" \
            "Некорректный запрос загрузки BAT-файлов." || return 1

    name="$(broray_user_routes_name_from_json "$request_file")" || return 1
    broray_user_routes_validate_name "$name" ||
        broray_user_routes_fail \
            "NAME_INVALID" \
            "Название набора должно содержать от 1 до 80 печатных символов." || return 1

    file_count="$(jq -r '.files | length' "$request_file")"
    [ "$file_count" -le "$BRORAY_USER_ROUTES_MAX_FILES" ] ||
        broray_user_routes_fail \
            "TOO_MANY_FILES" \
            "Разрешено не более $BRORAY_USER_ROUTES_MAX_FILES BAT-файлов в одном наборе." || return 1

    work="$BRORAY_ROUTES_ROOT/tmp/user-preview-build-$$"
    BRORAY_USER_ROUTES_ACTIVE_WORK="$work"
    source_dir="$work/source"
    mkdir -p "$source_dir" ||
        broray_user_routes_fail \
            "PREVIEW_PREPARE_FAILED" \
            "Не удалось подготовить временный каталог проверки." || return 1

    combined="$work/combined.txt"
    metadata_tsv="$work/source-files.tsv"
    : >"$combined"
    : >"$metadata_tsv"

    total_bytes=0
    total_route_lines=0
    normalized_lines=0
    index=0

    while [ "$index" -lt "$file_count" ]; do
        original_name="$(jq -r --argjson index "$index" '.files[$index].name' "$request_file")" || return 1
        encoded_file="$work/file-$index.base64"
        raw_file="$source_dir/$(printf '%02d.bat' $((index + 1)))"
        parsed_file="$work/parsed-$index.txt"
        stats_file="$work/stats-$index.txt"
        errors_file="$work/errors-$index.txt"

        [ -n "$original_name" ] && [ "${#original_name}" -le 160 ] ||
            broray_user_routes_fail \
                "FILENAME_INVALID" \
                "Некорректное имя загружаемого файла." || return 1

        case "$original_name" in
            */*|*\\*)
                broray_user_routes_fail \
                    "FILENAME_INVALID" \
                    "Имя файла не должно содержать путь." || return 1
                ;;
        esac

        lower_name="$(printf '%s' "$original_name" | tr '[:upper:]' '[:lower:]')"
        case "$lower_name" in
            *.bat) ;;
            *)
                broray_user_routes_fail \
                    "FILE_EXTENSION_INVALID" \
                    "Разрешены только файлы с расширением .bat." || return 1
                ;;
        esac

        jq -r --argjson index "$index" '.files[$index].contentBase64' \
            "$request_file" >"$encoded_file" || return 1

        base64 -d "$encoded_file" >"$raw_file" 2>/dev/null ||
            broray_user_routes_fail \
                "FILE_DECODE_FAILED" \
                "Не удалось декодировать файл $original_name." || return 1

        rm -f "$encoded_file"
        file_bytes="$(wc -c <"$raw_file" | tr -d ' ')"
        case "$file_bytes" in
            ''|*[!0-9]*)
                broray_user_routes_fail \
                    "FILE_SIZE_FAILED" \
                    "Не удалось определить размер файла $original_name." || return 1
                ;;
        esac

        [ "$file_bytes" -gt 0 ] ||
            broray_user_routes_fail \
                "FILE_EMPTY" \
                "Файл $original_name пуст." || return 1

        total_bytes=$((total_bytes + file_bytes))
        [ "$total_bytes" -le "$BRORAY_USER_ROUTES_MAX_BYTES" ] ||
            broray_user_routes_fail \
                "SOURCE_TOO_LARGE" \
                "Общий размер файлов превышает 2 МБ." || return 1

        if od -An -tu1 "$raw_file" | awk '{for (i=1; i<=NF; i++) if ($i == 0) exit 1}'; then
            :
        else
            broray_user_routes_fail \
                "FILE_NOT_TEXT" \
                "Файл $original_name содержит нулевые байты и не является текстовым BAT-файлом." || return 1
        fi

        parse_rc=0
        broray_user_routes_parse_bat \
            "$raw_file" "$parsed_file" "$stats_file" "$errors_file" || parse_rc=$?

        if [ "$parse_rc" -ne 0 ]; then
            details="$(sed -n '1,12p' "$errors_file" 2>/dev/null)"
            if [ "$parse_rc" -eq 2 ]; then
                details="В файле не найдено ни одной допустимой команды route add."
            fi
            broray_user_routes_fail \
                "FILE_REJECTED" \
                "Файл $original_name не прошёл безопасную проверку. $details" || return 1
        fi

        file_route_lines="$(sed -n 's/^routeLines=//p' "$stats_file")"
        file_normalized_lines="$(sed -n 's/^normalizedLines=//p' "$stats_file")"
        file_sha="$(sha256sum "$raw_file" | awk '{print $1}')"

        total_route_lines=$((total_route_lines + file_route_lines))
        normalized_lines=$((normalized_lines + file_normalized_lines))

        printf '%s\t%s\t%s\t%s\t%s\n' \
            "$((index + 1))" \
            "$(printf '%s' "$original_name" | base64 | tr -d '\n')" \
            "$file_sha" \
            "$file_bytes" \
            "$file_route_lines" >>"$metadata_tsv"

        cat "$parsed_file" >>"$combined" || return 1
        index=$((index + 1))
    done

    [ "$total_route_lines" -le "$BRORAY_USER_ROUTES_MAX_ROUTES" ] ||
        broray_user_routes_fail \
            "TOO_MANY_ROUTES" \
            "Количество строк маршрутов превышает лимит $BRORAY_USER_ROUTES_MAX_ROUTES." || return 1

    normalized="$work/normalized.txt"
    optimized="$work/optimized.txt"
    LC_ALL=C sort -u "$combined" >"$normalized" || return 1

    unique_count="$(wc -l <"$normalized" | tr -d ' ')"
    duplicate_count=$((total_route_lines - unique_count))

    [ "$unique_count" -gt 0 ] ||
        broray_user_routes_fail \
            "NO_ROUTES" \
            "В загруженных файлах не найдено маршрутов." || return 1

    [ "$unique_count" -le "$BRORAY_USER_ROUTES_MAX_ROUTES" ] ||
        broray_user_routes_fail \
            "TOO_MANY_ROUTES" \
            "Количество уникальных маршрутов превышает лимит $BRORAY_USER_ROUTES_MAX_ROUTES." || return 1

    broray_user_routes_collapse "$normalized" "$optimized" ||
        broray_user_routes_fail \
            "CIDR_OPTIMIZE_FAILED" \
            "Не удалось безопасно оптимизировать CIDR для экспорта." || return 1

    optimized_count="$(wc -l <"$optimized" | tr -d ' ')"
    broad_file="$work/broad-routes.txt"
    awk -F/ '$2 <= 8 {print}' "$normalized" >"$broad_file"
    broad_count="$(wc -l <"$broad_file" | tr -d ' ')"
    normalized_sha="$(sha256sum "$normalized" | awk '{print $1}')"
    optimized_sha="$(sha256sum "$optimized" | awk '{print $1}')"
    created_at="$(broray_user_routes_now)"
    created_epoch="$(broray_user_routes_epoch)"
    token="$(
        {
            printf '%s\n' "$created_epoch" "$$" "$name" "$normalized_sha" "$optimized_sha"
            cat "$metadata_tsv"
        } | sha256sum | awk '{print $1}'
    )"

    preview_dir="$BRORAY_USER_ROUTES_PREVIEWS/$token"
    mkdir -p "$preview_dir" || return 1
    chmod 700 "$preview_dir" 2>/dev/null || true

    source_files_json="$work/source-files.json"
    jq -Rn '
        [
            inputs |
            select(length > 0) |
            split("\t") |
            {
                storedName: ((.[0] | tonumber | tostring) + ".bat"),
                originalName: (.[1] | @base64d),
                parser: "windows-route-bat",
                sha256: .[2],
                sizeBytes: (.[3] | tonumber),
                routeLineCount: (.[4] | tonumber)
            }
        ]
    ' <"$metadata_tsv" >"$source_files_json" || return 1

    # Correct the stored names to the zero-padded names actually used on disk.
    jq '
        to_entries |
        map(.value + {storedName: (((.key + 1) | tostring | if length == 1 then "0" + . else . end) + ".bat")})
    ' "$source_files_json" >"$source_files_json.new" &&
        mv "$source_files_json.new" "$source_files_json" || return 1

    broad_json="$work/broad-routes.json"
    jq -R -s 'split("\n") | map(select(length > 0))' \
        "$broad_file" >"$broad_json" || return 1

    report="$work/report.json"
    jq -n \
        --arg token "$token" \
        --arg name "$name" \
        --arg created_at "$created_at" \
        --argjson created_epoch "$created_epoch" \
        --arg normalized_sha "$normalized_sha" \
        --arg optimized_sha "$optimized_sha" \
        --argjson file_count "$file_count" \
        --argjson total_bytes "$total_bytes" \
        --argjson source_line_count "$total_route_lines" \
        --argjson unique_count "$unique_count" \
        --argjson optimized_count "$optimized_count" \
        --argjson normalized_lines "$normalized_lines" \
        --argjson duplicate_count "$duplicate_count" \
        --argjson broad_count "$broad_count" \
        --slurpfile source_files "$source_files_json" \
        --slurpfile broad_routes "$broad_json" '
        {
            schemaVersion: 1,
            token: $token,
            name: $name,
            createdAt: $created_at,
            createdEpoch: $created_epoch,
            expiresInSeconds: 1800,
            fileCount: $file_count,
            totalBytes: $total_bytes,
            sourceRouteLineCount: $source_line_count,
            canonicalRouteCount: $unique_count,
            exportRouteCount: $optimized_count,
            normalizedNetworkCount: $normalized_lines,
            duplicateCount: $duplicate_count,
            rejectedLineCount: 0,
            broadRouteCount: $broad_count,
            broadRoutes: $broad_routes[0],
            normalizedSha256: $normalized_sha,
            exportSha256: $optimized_sha,
            sourceFiles: $source_files[0],
            ready: true,
            warning: (
                if $broad_count > 0 then
                    "Набор содержит очень широкие сети /7–/8. Через Xray может направляться значительная часть интернет-трафика."
                else null end
            )
        }
    ' >"$report" || return 1

    cp "$normalized" "$preview_dir/normalized.txt" &&
    cp "$optimized" "$preview_dir/optimized.txt" &&
    cp "$source_files_json" "$preview_dir/source-files.json" &&
    cp "$report" "$preview_dir/report.json" &&
    cp -R "$source_dir" "$preview_dir/source" || return 1

    chmod 600 "$preview_dir"/*.json "$preview_dir"/*.txt 2>/dev/null || true
    chmod 600 "$preview_dir/source"/* 2>/dev/null || true

    BRORAY_USER_ROUTES_ACTIVE_WORK=""
    rm -rf "$work"
    jq -c . "$preview_dir/report.json"
}

broray_user_routes_preview_valid()
{
    preview_dir="$1"
    [ -d "$preview_dir" ] || return 1
    [ -r "$preview_dir/report.json" ] || return 1
    [ -r "$preview_dir/normalized.txt" ] || return 1
    [ -r "$preview_dir/optimized.txt" ] || return 1
    [ -r "$preview_dir/source-files.json" ] || return 1
    [ -d "$preview_dir/source" ] || return 1

    broray_user_routes_verify_source_files \
        "$preview_dir/source" "$preview_dir/source-files.json" || return 1

    jq -e '
        (.schemaVersion == 1) and
        (.ready == true) and
        ((.token | type) == "string") and
        ((.canonicalRouteCount | type) == "number") and
        ((.exportRouteCount | type) == "number") and
        ((.sourceFiles | type) == "array")
    ' "$preview_dir/report.json" >/dev/null 2>&1 || return 1

    expected_normalized="$(jq -r '.normalizedSha256' "$preview_dir/report.json")"
    expected_optimized="$(jq -r '.exportSha256' "$preview_dir/report.json")"
    [ "$(sha256sum "$preview_dir/normalized.txt" | awk '{print $1}')" = "$expected_normalized" ] || return 1
    [ "$(sha256sum "$preview_dir/optimized.txt" | awk '{print $1}')" = "$expected_optimized" ] || return 1

    return 0
}

broray_user_routes_bundle_exists()
{
    bundle_id="$1"
    jq -e --arg id "$bundle_id" '.bundles | any(.id == $id)' \
        "$BRORAY_USER_ROUTES_INDEX" >/dev/null 2>&1
}

broray_user_routes_empty_registry()
{
    bundle_id="$1"
    interface="$2"
    now="$3"
    output="$4"

    jq -n \
        --arg id "$bundle_id" \
        --arg target_interface "$interface" \
        --arg now "$now" '
        {
            schemaVersion: 1,
            bundleId: $id,
            installedVersion: null,
            routeKeys: [],
            managedRouteKeys: [],
            externalRouteKeys: [],
            targetInterface: $target_interface,
            managedMetric: 1200,
            installedAt: null,
            removedAt: null,
            updatedAt: $now
        }
    ' >"$output"
}

broray_user_routes_commit()
{
    request_file="$1"

    broray_user_routes_prepare_runtime || return 1

    jq -e '
        (type == "object") and
        ((.token | type) == "string") and
        ((.name | type) == "string") and
        ((.bundleId == null) or ((.bundleId | type) == "string"))
    ' "$request_file" >/dev/null 2>&1 ||
        broray_user_routes_fail \
            "REQUEST_INVALID" \
            "Некорректный запрос сохранения пользовательского набора." || return 1

    token="$(jq -r '.token' "$request_file")"
    name="$(broray_user_routes_name_from_json "$request_file")"
    bundle_id="$(jq -r '.bundleId // empty' "$request_file")"

    broray_user_routes_token_valid "$token" ||
        broray_user_routes_fail "TOKEN_INVALID" "Некорректный токен предварительной проверки." || return 1
    broray_user_routes_validate_name "$name" ||
        broray_user_routes_fail "NAME_INVALID" "Некорректное название пользовательского набора." || return 1

    preview_dir="$BRORAY_USER_ROUTES_PREVIEWS/$token"
    broray_user_routes_preview_valid "$preview_dir" ||
        broray_user_routes_fail \
            "PREVIEW_INVALID" \
            "Предварительная проверка отсутствует, истекла или повреждена." || return 1

    created_epoch="$(jq -r '.createdEpoch' "$preview_dir/report.json")"
    now_epoch="$(broray_user_routes_epoch)"
    age=$((now_epoch - created_epoch))
    [ "$age" -le "$BRORAY_USER_ROUTES_PREVIEW_TTL" ] ||
        broray_user_routes_fail \
            "PREVIEW_EXPIRED" \
            "Срок предварительной проверки истёк. Проверьте файлы повторно." || return 1

    replacing=false
    if [ -n "$bundle_id" ]; then
        broray_user_routes_id_valid "$bundle_id" ||
            broray_user_routes_fail "BUNDLE_INVALID" "Некорректный идентификатор набора." || return 1
        broray_user_routes_bundle_exists "$bundle_id" ||
            broray_user_routes_fail "BUNDLE_NOT_FOUND" "Пользовательский набор не найден." || return 1
        replacing=true
    else
        bundle_id="user-$(printf '%s' "$token" | cut -c1-16)"
        suffix=0
        while broray_user_routes_bundle_exists "$bundle_id"; do
            suffix=$((suffix + 1))
            bundle_id="user-$(printf '%s' "$token" | cut -c1-12)-$suffix"
        done
    fi

    lock_rc=0
    broray_user_routes_lock_acquire || lock_rc=$?
    case "$lock_rc" in
        0) ;;
        2)
            broray_user_routes_fail \
                "ROUTES_BUSY" \
                "Другая операция с маршрутами уже выполняется." || return 1
            ;;
        *)
            broray_user_routes_fail \
                "LOCK_FAILED" \
                "Не удалось установить блокировку маршрутов." || return 1
            ;;
    esac

    work="$BRORAY_ROUTES_ROOT/tmp/user-commit-$bundle_id-$$"
    BRORAY_USER_ROUTES_ACTIVE_WORK="$work"
    stage="$work/stage"
    backup="$work/backup"
    catalog_stage="$stage/catalog"
    mkdir -p "$catalog_stage/source" "$backup" || return 1

    now="$(broray_user_routes_now)"
    interface="$(jq -r '.managedInterface // empty' "$BRORAY_ROUTES_ROOT/config.json" 2>/dev/null)"
    case "$interface" in
        Proxy[0-9]*) ;;
        *)
            broray_user_routes_fail \
                "ROUTES_CONFIG_INVALID" \
                "Некорректный управляемый интерфейс ProxyN." || return 1
            ;;
    esac

    report="$preview_dir/report.json"
    canonical_count="$(jq -r '.canonicalRouteCount' "$report")"
    export_count="$(jq -r '.exportRouteCount' "$report")"
    canonical_sha="$(jq -r '.normalizedSha256' "$report")"
    export_sha="$(jq -r '.exportSha256' "$report")"
    source_file_count="$(jq -r '.fileCount' "$report")"

    cp -R "$preview_dir/source/." "$catalog_stage/source/" &&
    # Keep the full canonical list for ownership/audit, while normalized.txt
    # follows the existing BROray export contract and contains the exact
    # losslessly aggregated routes referenced by routes.json/contentSha256.
    cp "$preview_dir/normalized.txt" "$catalog_stage/canonical.txt" &&
    cp "$preview_dir/optimized.txt" "$catalog_stage/normalized.txt" &&
    cp "$preview_dir/source-files.json" "$catalog_stage/source-files.json" &&
    cp "$preview_dir/report.json" "$catalog_stage/import-report.json" || return 1

    manifest_stage="$stage/manifest.json"
    jq -n \
        --arg id "$bundle_id" \
        --arg name "$name" \
        --arg interface "$interface" \
        --arg created_at "$now" \
        --slurpfile source_files "$preview_dir/source-files.json" '
        {
            schemaVersion: 1,
            id: $id,
            name: $name,
            description: "Пользовательский набор из загруженных BAT-файлов.",
            source: {
                provider: "local-upload",
                files: $source_files[0]
            },
            targetInterface: $interface,
            exportComment: "BROray",
            limits: {
                maxSourceBytes: 2097152,
                maxRoutes: 100000
            },
            createdAt: $created_at,
            updatedAt: $created_at
        }
    ' >"$manifest_stage" || return 1
    cp "$manifest_stage" "$catalog_stage/manifest.json" || return 1

    broray_user_routes_build_routes_json \
        "$bundle_id" "$interface" "$export_sha" \
        "$preview_dir/optimized.txt" "$catalog_stage/routes.json" || return 1

    jq -n \
        --arg bundle_id "$bundle_id" \
        --arg source_commit "$canonical_sha" \
        --arg source_date "$now" \
        --arg content_sha256 "$export_sha" \
        --arg downloaded_at "$now" \
        --arg target_interface "$interface" \
        --argjson route_count "$export_count" \
        --argjson canonical_count "$canonical_count" \
        --argjson source_file_count "$source_file_count" \
        --slurpfile source_files "$preview_dir/source-files.json" '
        {
            schemaVersion: 1,
            bundleId: $bundle_id,
            sourceCommit: $source_commit,
            sourceDate: $source_date,
            contentSha256: $content_sha256,
            routeCount: $route_count,
            canonicalRouteCount: $canonical_count,
            sourceFileCount: $source_file_count,
            sourceFiles: $source_files[0],
            downloadedAt: $downloaded_at,
            targetInterface: $target_interface,
            sourceProvider: "local-upload"
        }
    ' >"$catalog_stage/version.json" || return 1

    old_state="$BRORAY_ROUTES_ROOT/state/$bundle_id.json"
    state_stage="$stage/state.json"
    if [ "$replacing" = true ] && [ -r "$old_state" ]; then
        jq \
            --arg id "$bundle_id" \
            --arg now "$now" \
            --arg canonical_sha "$canonical_sha" \
            --arg export_sha "$export_sha" \
            --argjson route_count "$export_count" \
            --argjson canonical_count "$canonical_count" \
            --argjson source_file_count "$source_file_count" \
            --slurpfile source_files "$preview_dir/source-files.json" \
            --slurpfile report "$preview_dir/report.json" '
            .schemaVersion = 1 |
            .bundleId = $id |
            .status = "downloaded" |
            .availableVersion = {
                sourceCommit: $canonical_sha,
                sourceDate: $now,
                contentSha256: $export_sha,
                sourceFiles: $source_files[0]
            } |
            .downloadedVersion = {
                sourceCommit: $canonical_sha,
                sourceDate: $now,
                contentSha256: $export_sha
            } |
            .routeCount = $route_count |
            .lastCheckedAt = $now |
            .lastDownloadedAt = $now |
            .lastError = null |
            .checkResult = {
                result: "local_validated",
                message: "Пользовательские BAT-файлы проверены.",
                sourceFiles: $source_files[0],
                canonicalRouteCount: $canonical_count,
                exportRouteCount: $route_count
            } |
            .downloadResult = {
                result: "local_saved",
                message: "Пользовательский набор сохранён и готов к экспорту.",
                routeCount: $route_count,
                canonicalRouteCount: $canonical_count,
                sourceFileCount: $source_file_count
            } |
            .customImport = $report[0] |
            .updatedAt = $now
        ' "$old_state" >"$state_stage" || return 1
    else
        jq -n \
            --arg id "$bundle_id" \
            --arg now "$now" \
            --arg canonical_sha "$canonical_sha" \
            --arg export_sha "$export_sha" \
            --argjson route_count "$export_count" \
            --argjson canonical_count "$canonical_count" \
            --argjson source_file_count "$source_file_count" \
            --slurpfile source_files "$preview_dir/source-files.json" \
            --slurpfile report "$preview_dir/report.json" '
            {
                schemaVersion: 1,
                bundleId: $id,
                status: "downloaded",
                availableVersion: {
                    sourceCommit: $canonical_sha,
                    sourceDate: $now,
                    contentSha256: $export_sha,
                    sourceFiles: $source_files[0]
                },
                downloadedVersion: {
                    sourceCommit: $canonical_sha,
                    sourceDate: $now,
                    contentSha256: $export_sha
                },
                installedVersion: null,
                routeCount: $route_count,
                lastCheckedAt: $now,
                lastDownloadedAt: $now,
                lastExportedAt: null,
                lastDeletedAt: null,
                lastError: null,
                checkResult: {
                    result: "local_validated",
                    message: "Пользовательские BAT-файлы проверены.",
                    sourceFiles: $source_files[0],
                    canonicalRouteCount: $canonical_count,
                    exportRouteCount: $route_count
                },
                downloadResult: {
                    result: "local_saved",
                    message: "Пользовательский набор сохранён и готов к экспорту.",
                    routeCount: $route_count,
                    canonicalRouteCount: $canonical_count,
                    sourceFileCount: $source_file_count
                },
                exportBuild: null,
                preflight: null,
                exportResult: null,
                deleteResult: null,
                customImport: $report[0],
                updatedAt: $now
            }
        ' >"$state_stage" || return 1
    fi

    custom_stage="$stage/custom.json"
    jq \
        --arg id "$bundle_id" \
        --arg name "$name" \
        --arg now "$now" \
        --argjson canonical_count "$canonical_count" \
        --argjson export_count "$export_count" \
        --argjson source_file_count "$source_file_count" '
        .schemaVersion = 1 |
        .bundles = (
            [(.bundles // [])[] | select(.id != $id)] +
            [{
                id: $id,
                name: $name,
                description: "Пользовательский набор из загруженных BAT-файлов.",
                sourceProvider: "local-upload",
                canonicalRouteCount: $canonical_count,
                exportRouteCount: $export_count,
                sourceFileCount: $source_file_count,
                createdAt: (([.bundles[]? | select(.id == $id) | .createdAt] | first) // $now),
                updatedAt: $now
            }]
        )
    ' "$BRORAY_USER_ROUTES_INDEX" >"$custom_stage" || return 1

    bundles_stage="$stage/bundles.json"
    jq \
        --arg id "$bundle_id" '
        .schemaVersion = 1 |
        .bundles = (
            if ((.bundles // []) | index($id)) == null
            then (.bundles // []) + [$id]
            else (.bundles // [])
            end
        )
    ' "$BRORAY_ROUTES_ROOT/bundles.json" >"$bundles_stage" || return 1

    registry_target="$BRORAY_ROUTES_ROOT/installed/bundles/$bundle_id.json"
    registry_stage="$stage/registry.json"
    if [ -r "$registry_target" ]; then
        cp "$registry_target" "$registry_stage" || return 1
    else
        broray_user_routes_empty_registry \
            "$bundle_id" "$interface" "$now" "$registry_stage" || return 1
    fi

    # Validate the complete staged transaction before changing live files.
    jq -e --arg id "$bundle_id" '.schemaVersion == 1 and .id == $id and .source.provider == "local-upload"' \
        "$manifest_stage" >/dev/null 2>&1 || return 1
    jq -e --arg id "$bundle_id" '.schemaVersion == 1 and .bundleId == $id and (.routes | length) == .routeCount' \
        "$catalog_stage/routes.json" >/dev/null 2>&1 || return 1
    jq -e --arg id "$bundle_id" '.schemaVersion == 1 and .bundleId == $id and .downloadedVersion != null' \
        "$state_stage" >/dev/null 2>&1 || return 1
    jq -e --arg id "$bundle_id" --arg interface "$interface" '
        .schemaVersion == 1 and
        .bundleId == $id and
        .targetInterface == $interface and
        .managedMetric == 1200 and
        ((.routeKeys | type) == "array") and
        ((.managedRouteKeys | type) == "array") and
        ((.externalRouteKeys | type) == "array")
    ' "$registry_stage" >/dev/null 2>&1 || return 1
    jq -e --arg id "$bundle_id" '.bundles | index($id) != null' \
        "$bundles_stage" >/dev/null 2>&1 || return 1
    jq -e --arg id "$bundle_id" '.bundles | any(.id == $id)' \
        "$custom_stage" >/dev/null 2>&1 || return 1

    catalog_target="$BRORAY_ROUTES_ROOT/catalog/$bundle_id"
    manifest_target="$BRORAY_ROUTES_ROOT/manifests/$bundle_id.json"
    state_target="$BRORAY_ROUTES_ROOT/state/$bundle_id.json"
    bundles_target="$BRORAY_ROUTES_ROOT/bundles.json"
    custom_target="$BRORAY_USER_ROUTES_INDEX"

    # Record and backup every target so a failed replacement can be restored.
    : >"$backup/targets.tsv"
    for entry in \
        "catalog|$catalog_target" \
        "manifest|$manifest_target" \
        "state|$state_target" \
        "registry|$registry_target" \
        "bundles|$bundles_target" \
        "custom|$custom_target"
    do
        key="${entry%%|*}"
        target="${entry#*|}"
        if [ -e "$target" ]; then
            printf '%s\t%s\t1\n' "$key" "$target" >>"$backup/targets.tsv"
            cp -a "$target" "$backup/$key" || return 1
        else
            printf '%s\t%s\t0\n' "$key" "$target" >>"$backup/targets.tsv"
        fi
    done

    apply_failed=false
    rm -rf "$catalog_target.new.$$"
    cp -a "$catalog_stage" "$catalog_target.new.$$" || apply_failed=true

    if [ "$apply_failed" = false ]; then
        rm -rf "$catalog_target"
        mv "$catalog_target.new.$$" "$catalog_target" || apply_failed=true
    fi

    if [ "$apply_failed" = false ]; then
        cp "$manifest_stage" "$manifest_target.new.$$" && mv "$manifest_target.new.$$" "$manifest_target" || apply_failed=true
        cp "$state_stage" "$state_target.new.$$" && mv "$state_target.new.$$" "$state_target" || apply_failed=true
        cp "$registry_stage" "$registry_target.new.$$" && mv "$registry_target.new.$$" "$registry_target" || apply_failed=true
        cp "$bundles_stage" "$bundles_target.new.$$" && mv "$bundles_target.new.$$" "$bundles_target" || apply_failed=true
        cp "$custom_stage" "$custom_target.new.$$" && mv "$custom_target.new.$$" "$custom_target" || apply_failed=true
    fi

    if [ "$apply_failed" = true ]; then
        while IFS="$(printf '\t')" read -r key target existed; do
            rm -rf "$target" 2>/dev/null || true
            if [ "$existed" = "1" ]; then
                cp -a "$backup/$key" "$target" 2>/dev/null || true
            fi
        done <"$backup/targets.tsv"
        broray_user_routes_fail \
            "COMMIT_FAILED" \
            "Не удалось атомарно сохранить пользовательский набор; прежнее состояние восстановлено." || return 1
    fi

    chmod 600 "$custom_target" "$catalog_target"/*.json "$catalog_target"/*.txt 2>/dev/null || true
    chmod 644 "$manifest_target" "$state_target" "$registry_target" "$bundles_target" 2>/dev/null || true
    chmod 600 "$catalog_target/source"/* 2>/dev/null || true

    rm -rf "$preview_dir"
    BRORAY_USER_ROUTES_ACTIVE_WORK=""
    rm -rf "$work"
    rm -rf "$BRORAY_USER_ROUTES_LOCK" 2>/dev/null || true
    BRORAY_USER_ROUTES_LOCK_HELD=false

    jq -n \
        --arg id "$bundle_id" \
        --arg name "$name" \
        --arg now "$now" \
        --argjson replaced "$replacing" \
        --argjson canonical_count "$canonical_count" \
        --argjson export_count "$export_count" \
        --argjson source_file_count "$source_file_count" '
        {
            bundleId: $id,
            name: $name,
            replaced: $replaced,
            canonicalRouteCount: $canonical_count,
            exportRouteCount: $export_count,
            sourceFileCount: $source_file_count,
            status: "downloaded",
            readyForExport: true,
            updatedAt: $now
        }
    '
}

broray_user_routes_list()
{
    broray_user_routes_prepare_runtime || return 1

    jq -c \
        --arg routes_root "$BRORAY_ROUTES_ROOT" '
        {
            schemaVersion: 1,
            bundles: (.bundles // [])
        }
    ' "$BRORAY_USER_ROUTES_INDEX"
}

broray_user_routes_validate()
{
    bundle_id="$1"
    broray_user_routes_prepare_runtime || return 1
    broray_user_routes_id_valid "$bundle_id" ||
        broray_user_routes_fail "BUNDLE_INVALID" "Некорректный идентификатор набора." || return 1
    broray_user_routes_bundle_exists "$bundle_id" ||
        broray_user_routes_fail "BUNDLE_NOT_FOUND" "Пользовательский набор не найден." || return 1

    catalog="$BRORAY_ROUTES_ROOT/catalog/$bundle_id"
    report="$catalog/import-report.json"
    normalized="$catalog/canonical.txt"
    optimized="$catalog/normalized.txt"
    routes_json="$catalog/routes.json"
    version_json="$catalog/version.json"
    source_files_json="$catalog/source-files.json"
    source_dir="$catalog/source"
    state="$BRORAY_ROUTES_ROOT/state/$bundle_id.json"

    [ -r "$report" ] && [ -r "$normalized" ] && [ -r "$optimized" ] &&
    [ -r "$routes_json" ] && [ -r "$version_json" ] &&
    [ -r "$source_files_json" ] && [ -d "$source_dir" ] && [ -r "$state" ] ||
        broray_user_routes_fail "BUNDLE_DAMAGED" "Файлы пользовательского набора отсутствуют." || return 1

    broray_user_routes_verify_source_files "$source_dir" "$source_files_json" ||
        broray_user_routes_fail "BUNDLE_DAMAGED" "Исходные BAT-файлы пользовательского набора изменены или отсутствуют." || return 1

    jq -e '
        .schemaVersion == 1 and
        .ready == true and
        ((.sourceFiles | type) == "array") and
        (.fileCount == (.sourceFiles | length)) and
        ((.canonicalRouteCount | type) == "number") and
        ((.exportRouteCount | type) == "number") and
        ((.normalizedSha256 | type) == "string") and
        ((.exportSha256 | type) == "string")
    ' "$report" >/dev/null 2>&1 ||
        broray_user_routes_fail "BUNDLE_DAMAGED" "Отчёт импорта пользовательского набора повреждён." || return 1

    jq -e --arg id "$bundle_id" '
        .schemaVersion == 1 and
        .bundleId == $id and
        ((.downloadedVersion | type) == "object") and
        ((.installedVersion == null) or ((.installedVersion | type) == "object"))
    ' "$state" >/dev/null 2>&1 ||
        broray_user_routes_fail "BUNDLE_DAMAGED" "Состояние пользовательского набора повреждено." || return 1

    expected_normalized="$(jq -r '.normalizedSha256' "$report")"
    expected_optimized="$(jq -r '.exportSha256' "$report")"
    expected_canonical_count="$(jq -r '.canonicalRouteCount' "$report")"
    expected_export_count="$(jq -r '.exportRouteCount' "$report")"
    actual_normalized="$(sha256sum "$normalized" | awk '{print $1}')"
    actual_optimized="$(sha256sum "$optimized" | awk '{print $1}')"
    actual_canonical_count="$(wc -l <"$normalized" | tr -d ' ')"
    actual_export_count="$(wc -l <"$optimized" | tr -d ' ')"

    [ "$actual_normalized" = "$expected_normalized" ] &&
    [ "$actual_optimized" = "$expected_optimized" ] &&
    [ "$actual_canonical_count" = "$expected_canonical_count" ] &&
    [ "$actual_export_count" = "$expected_export_count" ] ||
        broray_user_routes_fail "BUNDLE_DAMAGED" "Контрольная сумма или количество маршрутов пользовательского набора не совпало." || return 1

    jq -e \
        --arg id "$bundle_id" \
        --arg sha "$expected_optimized" \
        --argjson count "$expected_export_count" '
        .schemaVersion == 1 and
        .bundleId == $id and
        .contentSha256 == $sha and
        .routeCount == $count and
        ((.routes | type) == "array") and
        ((.routes | length) == $count)
    ' "$routes_json" >/dev/null 2>&1 ||
        broray_user_routes_fail "BUNDLE_DAMAGED" "Каталог экспорта пользовательского набора повреждён." || return 1

    jq -e \
        --arg id "$bundle_id" \
        --arg sha "$expected_optimized" \
        --argjson count "$expected_export_count" '
        .schemaVersion == 1 and
        .bundleId == $id and
        .contentSha256 == $sha and
        .routeCount == $count and
        .sourceProvider == "local-upload"
    ' "$version_json" >/dev/null 2>&1 ||
        broray_user_routes_fail "BUNDLE_DAMAGED" "Сведения о версии пользовательского набора повреждены." || return 1

    now="$(broray_user_routes_now)"
    state_new="$state.new.$$"
    jq \
        --arg now "$now" \
        --slurpfile report "$report" '
        .lastCheckedAt = $now |
        .lastError = null |
        .checkResult = {
            result: "local_validated",
            message: "Исходные и нормализованные файлы пользовательского набора проверены.",
            sourceFiles: $report[0].sourceFiles,
            canonicalRouteCount: $report[0].canonicalRouteCount,
            exportRouteCount: $report[0].exportRouteCount
        } |
        .customImport = $report[0] |
        .updatedAt = $now
    ' "$state" >"$state_new" && mv "$state_new" "$state" || {
        rm -f "$state_new"
        return 1
    }

    jq -c '. + {validatedAt: $now}' --arg now "$now" "$report"
}

broray_user_routes_remove()
{
    bundle_id="$1"
    broray_user_routes_prepare_runtime || return 1
    broray_user_routes_id_valid "$bundle_id" ||
        broray_user_routes_fail "BUNDLE_INVALID" "Некорректный идентификатор набора." || return 1
    broray_user_routes_bundle_exists "$bundle_id" ||
        broray_user_routes_fail "BUNDLE_NOT_FOUND" "Пользовательский набор не найден." || return 1

    state="$BRORAY_ROUTES_ROOT/state/$bundle_id.json"
    registry="$BRORAY_ROUTES_ROOT/installed/bundles/$bundle_id.json"

    jq -e --arg id "$bundle_id" '
        .schemaVersion == 1 and
        .bundleId == $id and
        ((.installedVersion == null) or ((.installedVersion | type) == "object"))
    ' "$state" >/dev/null 2>&1 ||
        broray_user_routes_fail "BUNDLE_DAMAGED" "Состояние пользовательского набора отсутствует или повреждено." || return 1

    jq -e --arg id "$bundle_id" '
        .schemaVersion == 1 and
        .bundleId == $id and
        ((.installedVersion == null) or ((.installedVersion | type) == "object")) and
        ((.routeKeys | type) == "array") and
        ((.managedRouteKeys | type) == "array") and
        ((.externalRouteKeys | type) == "array")
    ' "$registry" >/dev/null 2>&1 ||
        broray_user_routes_fail "BUNDLE_DAMAGED" "Реестр установки пользовательского набора отсутствует или повреждён." || return 1

    installed="$(jq -nr \
        --slurpfile state "$state" \
        --slurpfile registry "$registry" '
        if (($state[0].installedVersion != null) or
            ($registry[0].installedVersion != null) or
            (($registry[0].routeKeys | length) > 0))
        then "true" else "false" end
    ')"

    if [ "$installed" = "true" ]; then
        "$BRORAY_ROOT/bin/broray-routes" delete "$bundle_id" ||
            broray_user_routes_fail \
                "ROUTER_DELETE_FAILED" \
                "Не удалось безопасно удалить маршруты набора из Keenetic." || return 1
    fi

    lock_rc=0
    broray_user_routes_lock_acquire || lock_rc=$?
    case "$lock_rc" in
        0) ;;
        2) broray_user_routes_fail "ROUTES_BUSY" "Другая операция с маршрутами уже выполняется." || return 1 ;;
        *) broray_user_routes_fail "LOCK_FAILED" "Не удалось установить блокировку маршрутов." || return 1 ;;
    esac

    backup_dir="$BRORAY_ROUTES_ROOT/backup/user-removed-$bundle_id-$(broray_user_routes_stamp)-$$"
    mkdir -p "$backup_dir/files" || return 1

    cp "$BRORAY_ROUTES_ROOT/bundles.json" "$backup_dir/bundles.json" || return 1
    cp "$BRORAY_USER_ROUTES_INDEX" "$backup_dir/custom.json" || return 1

    jq --arg id "$bundle_id" '.bundles = [(.bundles // [])[] | select(. != $id)]' \
        "$BRORAY_ROUTES_ROOT/bundles.json" >"$backup_dir/bundles.new" || return 1
    jq --arg id "$bundle_id" '.bundles = [(.bundles // [])[] | select(.id != $id)]' \
        "$BRORAY_USER_ROUTES_INDEX" >"$backup_dir/custom.new" || return 1
    jq -e --arg id "$bundle_id" '.bundles | index($id) == null' \
        "$backup_dir/bundles.new" >/dev/null 2>&1 || return 1
    jq -e --arg id "$bundle_id" '.bundles | all(.id != $id)' \
        "$backup_dir/custom.new" >/dev/null 2>&1 || return 1

    : >"$backup_dir/targets.tsv"
    for entry in \
        "catalog|$BRORAY_ROUTES_ROOT/catalog/$bundle_id" \
        "manifest|$BRORAY_ROUTES_ROOT/manifests/$bundle_id.json" \
        "state|$BRORAY_ROUTES_ROOT/state/$bundle_id.json" \
        "registry|$BRORAY_ROUTES_ROOT/installed/bundles/$bundle_id.json"
    do
        key="${entry%%|*}"
        target="${entry#*|}"
        [ -e "$target" ] || continue
        printf '%s\t%s\n' "$key" "$target" >>"$backup_dir/targets.tsv"
        mv "$target" "$backup_dir/files/$key" || return 1
    done

    remove_failed=false
    cp "$backup_dir/bundles.new" "$BRORAY_ROUTES_ROOT/bundles.json.new.$$" &&
        mv "$BRORAY_ROUTES_ROOT/bundles.json.new.$$" "$BRORAY_ROUTES_ROOT/bundles.json" || remove_failed=true
    if [ "$remove_failed" = false ]; then
        cp "$backup_dir/custom.new" "$BRORAY_USER_ROUTES_INDEX.new.$$" &&
            mv "$BRORAY_USER_ROUTES_INDEX.new.$$" "$BRORAY_USER_ROUTES_INDEX" || remove_failed=true
    fi

    if [ "$remove_failed" = true ]; then
        cp "$backup_dir/bundles.json" "$BRORAY_ROUTES_ROOT/bundles.json" 2>/dev/null || true
        cp "$backup_dir/custom.json" "$BRORAY_USER_ROUTES_INDEX" 2>/dev/null || true
        while IFS="$(printf '\t')" read -r key target; do
            [ -e "$backup_dir/files/$key" ] || continue
            mv "$backup_dir/files/$key" "$target" 2>/dev/null || true
        done <"$backup_dir/targets.tsv"
        broray_user_routes_fail \
            "REMOVE_FAILED" \
            "Не удалось удалить пользовательский набор; локальное состояние восстановлено." || return 1
    fi

    rm -f "$backup_dir/bundles.new" "$backup_dir/custom.new"
    now="$(broray_user_routes_now)"
    jq -n \
        --arg id "$bundle_id" \
        --arg backup "$backup_dir" \
        --arg removed_at "$now" '
        {
            bundleId: $id,
            removed: true,
            backupPath: $backup,
            removedAt: $removed_at
        }
    '

    rm -rf "$BRORAY_USER_ROUTES_LOCK" 2>/dev/null || true
    BRORAY_USER_ROUTES_LOCK_HELD=false
}
