#!/opt/bin/ash
set -eu
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin
export PATH
echo SIZE
du -sk /opt
echo BASE_PACKAGES
for name in entware-release opt-ndmsv2 dropbear busybox opkg; do opkg status "$name" | sed -n '/^Package:/p;/^Version:/p;/^Depends:/p;/^Auto-Installed:/p'; done
echo DIRECTORIES
find /opt/etc /opt/libexec /opt/share /opt/var /opt/tmp -maxdepth 2 -mindepth 1 -type d -print
echo SERVICE_FILES
find /opt/etc -maxdepth 2 -type f -print
echo APP_NAMES
find /opt -xdev -iname '*broray*' -o -iname '*mihomo*' -o -iname '*xray*'
echo PROCESSES
for d in /proc/[0-9]*; do
  [ -L "$d/exe" ] || continue
  exe="$(readlink "$d/exe" 2>/dev/null || true)"
  case "$exe" in /opt/*|*mihomo*|*xray*) printf '%s %s\n' "${d##*/}" "$exe" ;; esac
done
echo FREE_SPACE
df -Pk /opt
