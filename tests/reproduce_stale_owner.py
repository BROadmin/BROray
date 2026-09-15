"""Execute exact 3.1.0 lock functions against synthetic owners, never a router."""
import json
import os
import subprocess
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
WORKSPACE = REPO.parent
BASE = WORKSPACE / '.local/baseline'
BB = WORKSPACE / '.local/bin/busybox64u.exe'

def main():
    updater = (WORKSPACE / '.local/platform/opt/libexec/broray-updater/broray-updater.sh').read_text(encoding='utf-8')
    classifier = updater[updater.index('global_operation_lock_classify()'):updater.index('conflicting_operation_admission_clear()')]
    results = []
    cases = [
        ('subscriptions_system_dead', 'system', 'subscriptions:scheduler', 'dead', 1, 'stale-route-owner'),
        ('auto_switch_system_dead', 'system', 'auto-switch', 'dead', 1, 'stale-route-owner'),
        ('routes_dead', 'routes', 'servers:check', 'dead', 0, 'absent'),
        ('routes_live', 'routes', 'servers:check', 'live', 1, 'live-route-owner'),
        ('routes_unreadable_owner', 'routes', 'servers:check', 'denied', 0, 'absent'),
    ]
    for name, scope, action, owner_mode, reclaim_expected, classification_expected in cases:
        folder = Path(tempfile.mkdtemp(prefix='repro-', dir=WORKSPACE / '.local'))
        lock = folder / 'global.lock'
        lock.mkdir()
        for key, value in {'pid': '900001', 'scope': scope, 'action': action, 'bundle': '', 'startedAt': '2026-09-15T00:00:00Z'}.items():
            (lock / key).write_text(value + '\n', encoding='utf-8')
        script = folder / 'test.sh'
        script.write_text('''#!/bin/sh
set -u
kill() {
    [ "$#" = 2 ] && [ "$1" = -0 ] && [ "$2" = 900001 ] || { echo forbidden-signal >&2; exit 99; }
    [ "$TEST_OWNER_MODE" = live ]
}
. "$TEST_LIBRARY"
''' + classifier + '''
reclaim_rc=0
broray_routes_api_lock_reclaim_stale || reclaim_rc=$?
classification_rc=0
global_operation_lock_classify || classification_rc=$?
printf '%s|%s|%s\\n' "$reclaim_rc" "$GLOBAL_OPERATION_LOCK_STATE" "$classification_rc"
''', encoding='utf-8')
        env = os.environ.copy()
        env.update({
            'PATH': str(WORKSPACE / '.local/bin') + os.pathsep + env.get('PATH', ''),
            'TEST_LIBRARY': (BASE / 'app/lib/routes-api-operation.sh').as_posix(),
            'TEST_OWNER_MODE': owner_mode,
            'BRORAY_ROOT': folder.as_posix(),
            'BRORAY_ROUTES_ROOT': (folder / 'routes').as_posix(),
            'BRORAY_ROUTES_API_LOCK': lock.as_posix(),
            'BRORAY_ROUTES_API_PROGRESS_DIR': (folder / 'operations').as_posix(),
            'BRORAY_UPDATER_REQUEST_LOCK': (folder / 'updater.lock').as_posix(),
            'BRORAY_LEGACY_GLOBAL_LOCK': (folder / 'legacy.lock').as_posix(),
            'BRORAY_ROUTES_API_STALE_LOCK_ROOT': (folder / 'quarantine').as_posix(),
            'GLOBAL_OPERATION_LOCK': lock.as_posix(),
        })
        p = subprocess.run([str(BB), 'ash', str(script)], env=env, capture_output=True, timeout=15)
        output = p.stdout.decode('utf-8', errors='replace').strip()
        expected = f'{reclaim_expected}|{classification_expected}|{0 if classification_expected == "absent" else 1}'
        ok = p.returncode == 0 and output == expected
        row = {'name': name, 'passed': ok, 'expected': expected, 'actual': output, 'stderr': p.stderr.decode('utf-8', errors='replace'), 'ownerModel': 'synthetic kill -0 only', 'evidenceDir': str(folder)}
        if name == 'routes_unreadable_owner':
            row['interpretation'] = 'Additional baseline weakness: failed kill -0 is treated as dead without process identity; no real permission failure was induced.'
        results.append(row)
        if not ok:
            break
    report = {'status': 'PASS' if len(results) == len(cases) and all(x['passed'] for x in results) else 'FAIL', 'baselineDefectReproduced': len(results) >= 2 and all(x['passed'] for x in results[:2]), 'source': 'exact archived 3.1.0-r09c02 function bodies', 'environment': 'Windows BusyBox ash; owner liveness simulated; filesystem operations real', 'routerAccessed': False, 'results': results}
    out = WORKSPACE / 'docs/evidence/stage00-reproducer.json'
    out.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(json.dumps({'status': report['status'], 'cases': len(results), 'baselineDefectReproduced': report['baselineDefectReproduced']}))
    if report['status'] != 'PASS':
        print(json.dumps(results[-1], ensure_ascii=True)); raise SystemExit(1)

if __name__ == '__main__':
    main()
