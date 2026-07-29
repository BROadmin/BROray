#!/usr/bin/env bash
set -euo pipefail

REPOSITORY_ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE="$REPOSITORY_ROOT/scripts/build-opkg-release.sh"
TEMP="$REPOSITORY_ROOT/scripts/.build-opkg-release-2.2.0.$$"

cleanup() { rm -f "$TEMP"; }
trap cleanup EXIT HUP INT TERM

[ -r "$SOURCE" ] || { echo "ERROR: missing $SOURCE" >&2; exit 1; }

python3 - "$SOURCE" "$TEMP" <<'PY'
from pathlib import Path
import sys
source=Path(sys.argv[1]).read_text(encoding='utf-8')
replacements={
    'VERSION="2.1.0"': 'VERSION="2.2.0"',
    'REVISION="${BRORAY_PACKAGE_REVISION:-2}"': 'REVISION="${BRORAY_PACKAGE_REVISION:-1}"',
    'MANUAL_MIGRATOR_NAME="broray-manual-to-opkg-$PACKAGE_VERSION.sh"': 'MANUAL_MIGRATOR_NAME="broray-manual-to-opkg-2.1.0-2.sh"',
    '"$REPOSITORY_ROOT/scripts/safe-opkg-upgrade.sh"': '"$REPOSITORY_ROOT/scripts/safe-opkg-upgrade-2.2.0-1.sh"',
}
for old,new in replacements.items():
    if old not in source:
        raise SystemExit(f'expected build-script token not found: {old}')
    source=source.replace(old,new,1)
Path(sys.argv[2]).write_text(source,encoding='utf-8')
PY
chmod 755 "$TEMP"
exec "$TEMP" "$@"
