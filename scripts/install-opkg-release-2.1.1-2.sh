#!/opt/bin/ash
set -u
cd / || exit 1
OPT_ROOT="${BRORAY_OPT_ROOT:-/opt}"
TMP_ROOT="${BRORAY_TMP_ROOT:-/tmp}"
PATH="$OPT_ROOT/sbin:$OPT_ROOT/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH
TARGET_VERSION="2.1.1-2"
TARGET_APP_VERSION="2.1.1"
PACKAGE_URL="https://api.brovibe.cloud/releases/opkg/aarch64-3.10/broray_2.1.1-2_aarch64-3.10.ipk"
PACKAGE_SHA="c7e627cb8aa73f0ae20a96e028af195037b8f087a4ac4caa98b99cfd580bc3fb"
CACHE_TAG="20260727-official-2.1.1-2-r1"
WORK="$TMP_ROOT/broray-install-2.1.1-2-$$"
IPK="$WORK/broray_2.1.1-2_aarch64-3.10.ipk"

fail() { printf 'ОШИБКА: %s
' "$*" >&2; rm -rf "$WORK"; exit 1; }
download() {
    if [ -x "$OPT_ROOT/bin/curl" ]; then
        "$OPT_ROOT/bin/curl" -fL -H 'Accept-Encoding: identity' -H 'Cache-Control: no-cache, no-store' --connect-timeout 20 --max-time 600 -o "$IPK.part" "$PACKAGE_URL?v=$CACHE_TAG-$(date +%s)-$$" || return 1
    else
        "$OPT_ROOT/bin/wget" -qO "$IPK.part" --header='Cache-Control: no-cache, no-store' "$PACKAGE_URL?v=$CACHE_TAG-$(date +%s)-$$" || return 1
    fi
    mv "$IPK.part" "$IPK"
}
health() {
    opkg list-installed broray 2>/dev/null | grep -Fq "broray - $TARGET_VERSION" || return 1
    "$OPT_ROOT/broray/bin/broray" version 2>/dev/null | grep -Fq "BROray $TARGET_APP_VERSION" || return 1
    for s in "$OPT_ROOT/etc/init.d/S23broray-monitor" "$OPT_ROOT/etc/init.d/S24broray" "$OPT_ROOT/etc/init.d/S25broray-web" "$OPT_ROOT/etc/init.d/S27broray-auto-switch" "$OPT_ROOT/etc/init.d/S28broray-subscriptions"; do [ -x "$s" ] && "$s" status >/dev/null 2>&1 || return 1; done
    for id in telegram whatsapp youtube chatgpt facebook instagram meta tiktok speedtest; do [ -r "$OPT_ROOT/broray/share/routes/manifests/$id.json" ] || return 1; done
}

echo "=================================================="
echo "BROray — чистая установка $TARGET_VERSION"
echo "=================================================="
case "$(uname -m 2>/dev/null)" in aarch64|arm64) ;; *) fail "поддерживается только ARM64";; esac
command -v opkg >/dev/null 2>&1 || fail "Entware OPKG не установлен"
if opkg list-installed broray 2>/dev/null | grep -q '^broray - ' || [ -d "$OPT_ROOT/broray" ]; then fail "BROray уже присутствует; используйте раздел Обновление"; fi
rm -rf "$OPT_ROOT/broray-backups" "$OPT_ROOT/broray-test-backups" "$OPT_ROOT/broray-test-logs" "$OPT_ROOT/var/cache/opkg" "$OPT_ROOT/var/opkg-lists"
mkdir -p "$OPT_ROOT/var/cache/opkg" "$OPT_ROOT/var/opkg-lists" "$WORK" || fail "не удалось подготовить каталоги"
opkg update || fail "не удалось обновить списки Entware"
opkg install ca-bundle ca-certificates curl jq lighttpd lighttpd-mod-cgi tar || fail "не удалось установить зависимости"
download || fail "не удалось скачать точный пакет"
[ "$(sha256sum "$IPK" | awk '{print $1}')" = "$PACKAGE_SHA" ] || fail "SHA-256 пакета не совпала"
free_kb="$(df -Pk "$OPT_ROOT" | awk 'NR==2 {print $4; exit}')"; [ -n "$free_kb" ] || fail "не удалось определить свободное место"
[ "$free_kb" -ge 42000 ] || fail "недостаточно места: $free_kb КБ"
opkg install "$IPK" || { opkg remove broray >/dev/null 2>&1 || true; rm -rf "$OPT_ROOT/broray"; fail "OPKG не установил BROray"; }
health || { opkg remove broray >/dev/null 2>&1 || true; rm -rf "$OPT_ROOT/broray"; fail "установка не прошла проверку"; }
rm -rf "$WORK"
echo "BROray $TARGET_VERSION установлен: OK"
echo "Все пять служб и девять наборов маршрутов, включая Meta: OK"
echo "WebUI: http://192.168.1.1:8080/"
echo "Обсуждение и поддержка: https://t.me/BROvibe_vpn"
