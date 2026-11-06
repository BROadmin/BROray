#!/bin/sh
# Start fakerci + a dev daemon on 127.0.0.1:18080 with a throwaway root.
#   tools/devrun.sh start|stop
# Parallel runs: BRORAY_DEV_ROOT, BRORAY_DEV_BIN, BRORAY_DEV_WEB_PORT,
# BRORAY_DEV_RCI_PORT, BRORAY_DEV_SOCKS_PORT. FAKERCI_IPV6=1 starts fakerci
# with global IPv6; BRORAY_DEV_IPV6_TTL (e.g. 2s) shortens the IPv6 cache.
# «Проверить UDP» without Internet STUN (dev build, cmd/broray/dev_udp.go):
#   BRORAY_DEV_STUN=local BRORAY_DEV_STUN_PROXYN=127.0.0.1:12080 tools/devrun.sh start
set -eu
ROOT=${BRORAY_DEV_ROOT:-/tmp/broray-dev}
BIN=${BRORAY_DEV_BIN:-/tmp/broray-dev-bin}
WEB=${BRORAY_DEV_WEB_PORT:-18080}
RCI=${BRORAY_DEV_RCI_PORT:-17979}
SOCKS=${BRORAY_DEV_SOCKS_PORT:-12080}
case "${1:-start}" in
start)
  . "$(dirname "$0")/../.goenv"
  cd "${BRORAY_SRC:-$(dirname "$0")/..}"
  go build -tags dev -o $BIN/broray ./cmd/broray
  go build -o $BIN/fakerci ./tools/fakerci
  mkdir -p "$ROOT/config"
  cat > "$ROOT/config/config.v1.json" <<JSON
{"schemaVersion":1,"listenAddress":"127.0.0.1","webPort":$WEB,"socksPort":$SOCKS,"logLevel":"warning",
 "keenetic":{"managementPort":80,"rciUrl":"http://127.0.0.1:$RCI","writeTransport":"rci","routeMetric":1200,"lanInterface":"Bridge0"},
 "health":{"probeUrls":["http://cp.cloudflare.com/"],"intervalSec":15,"failureThreshold":3,"cooldownMinutes":10,"selectionRule":"best-quality"},
 "xray":{"version":"","repo":"XTLS/Xray-core"},"routes":{"repository":"RockBlack-VPN/ip-address","commit":"65a895d1e8cf30653e94a00f2e374991a6838e36"}}
JSON
  nohup $BIN/fakerci :$RCI >"$ROOT/fakerci.log" 2>&1 &
  echo $! > "$ROOT/fakerci.pid"
  BRORAY_ROOT="$ROOT" BRORAY_OPT="$ROOT/opt" BRORAY_DEV_LOGIN=admin:dev BRORAY_DEV_RAW_BASE=${BRORAY_DEV_RAW_BASE:-} BRORAY_DEV_IPV6_TTL=${BRORAY_DEV_IPV6_TTL:-} BRORAY_DEV_ROUTES_API=${BRORAY_DEV_ROUTES_API:-} BRORAY_DEV_ROUTES_TICK=${BRORAY_DEV_ROUTES_TICK:-} nohup $BIN/broray daemon >"$ROOT/daemon.log" 2>&1 &
  echo $! > "$ROOT/daemon.pid"
  sleep 1; echo "daemon http://127.0.0.1:$WEB (admin/dev), log $ROOT/daemon.log"
  ;;
stop)
  for p in daemon fakerci; do [ -f "$ROOT/$p.pid" ] && kill "$(cat "$ROOT/$p.pid")" 2>/dev/null || true; rm -f "$ROOT/$p.pid"; done
  ;;
esac
