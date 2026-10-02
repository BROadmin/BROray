"""Real route startup/ownership code, isolated filesystem; no router mutations.

Runtime wrapper collaborators (operations, DoT, handoff) are explicit doubles.
Failure injection uses shell cp/mv wrappers, not production test switches.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[1]


class RuntimeRecovery(unittest.TestCase):
    def setUp(self):
        self.t = tempfile.TemporaryDirectory()
        self.addCleanup(self.t.cleanup)
        self.root = Path(self.t.name) / 'broray'
        self.root.mkdir()
        for d in ['lib', 'share', 'bin']:
            shutil.copytree(SOURCE / 'runtime/app' / d, self.root / d)
        shutil.copytree(SOURCE / 'runtime/state-seed/routes', self.root / 'routes')
        for d in ['tmp', 'backup']:
            (self.root / d).mkdir()
        self.routes = self.root / 'routes'
        (self.routes/'locks').mkdir()
        self.guard=Path('/.local/bin/linux-guard')
        self.assertTrue(self.guard.is_file(), 'native test guard required')
        self.env = dict(PATH=os.environ['PATH'], LC_ALL='C', BRORAY_ROOT=str(self.root), BRORAY_OPS_GUARD=str(self.guard))
        self.assertEqual(self.prepare().returncode, 0)
        self.before = self.ownership()

    def prepare(self, inject=''):
        if inject:
            cmd=[str(self.guard),str(self.routes/'locks/resource.control.guard'),'/bin/ash','-c',
                 'set -u\n. "$BRORAY_ROOT/lib/routes-runtime-repair.sh"\n'+inject+'\nbroray_routes_runtime_prepare_locked']
        else:
            cmd=['ash','-c','set -u\n. "$BRORAY_ROOT/lib/routes-runtime-repair.sh"\nbroray_routes_runtime_prepare']
        return subprocess.run(cmd, env=self.env, text=True, capture_output=True, timeout=30)

    def ownership(self):
        return {str(p.relative_to(self.routes)): p.read_bytes() for p in (self.routes / 'installed').rglob('*.json')}

    def state(self, name='telegram'):
        return self.routes / 'state' / (name + '.json')

    def evidence(self):
        return list((self.root / 'backup/routes-runtime-recovery').glob('*/state.json'))

    def assert_recovered(self, raw, name='telegram'):
        s = json.loads(self.state(name).read_bytes())
        self.assertEqual(s['bundleId'], name)
        self.assertEqual(s['schemaVersion'], 1)
        self.assertEqual(s['status'], 'not_checked')
        self.assertIsNone(s['installedVersion'])
        evidence = [p for p in self.evidence() if p.read_bytes() == raw]
        self.assertTrue(evidence, 'Exact corrupt bytes must be preserved')
        receipt = json.loads((evidence[0].parent / 'recovery.json').read_bytes())
        self.assertEqual(receipt['originalSha256'], hashlib.sha256(raw).hexdigest())
        self.assertEqual(receipt['backupSha256'], receipt['originalSha256'])
        self.assertEqual(self.ownership(), self.before)

    def corrupt_and_repair(self, raw):
        self.state().write_bytes(raw)
        p = self.prepare()
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assert_recovered(raw)

    def test_R01_zero(self):
        self.corrupt_and_repair(b'')
        # Exercise the actual runtime command after recovery as well.
        (self.root/'lib/operation-client.sh').write_text('broray_ops_call() { return 0; }\n')
        (self.root/'lib/routes-dot.sh').write_text('broray_dot_ensure_files() { return 0; }\n')
        handoff=self.root/'handoff'; handoff.write_text('#!/bin/ash\nexit 0\n'); handoff.chmod(0o700)
        p=subprocess.run(['ash', str(self.root/'bin/broray-runtime-prepare')], env=dict(self.env, BRORAY_PLATFORM_HANDOFF=str(handoff)),capture_output=True,text=True,timeout=30)
        self.assertEqual(p.returncode,0,p.stderr)
        self.assertIn('BRORAY_RUNTIME_PREPARE=PASS',p.stdout)

    def test_R02_truncated(self):
        self.corrupt_and_repair(b'{"schemaVersion":1,"bundleId":"telegram"')

    def test_R03_schema(self):
        self.corrupt_and_repair(b'{"schemaVersion":2,"status":12}')

    def test_R04_wrong_bundle(self):
        s=json.loads(self.state().read_bytes()); s['bundleId']='youtube'
        self.corrupt_and_repair(json.dumps(s).encode())

    def test_R05_two_states(self):
        self.state().write_bytes(b''); self.state('tiktok').write_bytes(b'{')
        p=self.prepare(); self.assertEqual(p.returncode,0,p.stderr)
        self.assert_recovered(b''); self.assert_recovered(b'{','tiktok')
        self.assertEqual(len(self.evidence()),2)

    def install_evidence(self):
        version={'contentSha256':'a'*64,'sourceCommit':'b'*40,'sourceDate':'2026-10-02','routeCount':1}
        key='ipv4|192.0.2.0|24|Proxy0|0.0.0.0|1200'
        registry=self.routes/'installed/bundles/telegram.json'
        r=json.loads(registry.read_bytes());r.update(installedVersion=version,routeKeys=[key],managedRouteKeys=[key]);registry.write_text(json.dumps(r))
        g=self.routes/'installed/routes.json'; data=json.loads(g.read_bytes())
        data['routes']=[dict(key=key,interface='Proxy0',metric=1200,createdByBROray=True,managed=True,owners=['telegram'])]
        g.write_text(json.dumps(data)); self.before=self.ownership()
        return version

    def test_R06_installed_preserved(self):
        v=self.install_evidence(); self.state().write_bytes(b'{')
        p=self.prepare(); self.assertEqual(p.returncode,0,p.stderr)
        s=json.loads(self.state().read_bytes());self.assertEqual(s['installedVersion'],v)
        self.assertEqual(s['status'],'installed');self.assertEqual(self.ownership(),self.before)
        self.assertEqual(self.evidence()[0].read_bytes(),b'{')

    def test_R07_corrupt_registry(self):
        self.state().write_bytes(b'{');(self.routes/'installed/bundles/telegram.json').write_bytes(b'{')
        before=self.ownership();p=self.prepare();self.assertNotEqual(p.returncode,0)
        self.assertEqual(self.state().read_bytes(),b'{');self.assertEqual(self.ownership(),before)

    def test_R08_symlink(self):
        original=self.state().read_bytes(); outside=self.root/'outside';outside.write_bytes(original)
        self.state().unlink();self.state().symlink_to(outside)
        p=self.prepare();self.assertNotEqual(p.returncode,0)
        self.assertTrue(self.state().is_symlink());self.assertEqual(outside.read_bytes(),original)

    def test_R09_evidence_failure(self):
        self.state().write_bytes(b'{')
        p=self.prepare('cp() { case "$*" in *routes-runtime-recovery*) return 1;; esac; command cp "$@"; }')
        self.assertNotEqual(p.returncode,0);self.assertEqual(self.state().read_bytes(),b'{')
        self.assertEqual(self.ownership(),self.before)

    def test_R10_rename_failure(self):
        self.state().write_bytes(b'{')
        p=self.prepare('mv() { local target; for target; do :; done; [ "$target" != "$BRORAY_ROOT/routes/state/telegram.json" ] || return 1; command mv "$@"; }')
        self.assertNotEqual(p.returncode,0);self.assertEqual(self.state().read_bytes(),b'{')
        self.assertEqual(self.ownership(),self.before)
        self.assertEqual(self.evidence()[0].read_bytes(),b'{')

    def test_R11_idempotent(self):
        self.corrupt_and_repair(b'{');before=self.state().read_bytes();e=self.evidence()
        p=self.prepare();self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(self.state().read_bytes(),before);self.assertEqual(self.evidence(),e)

    def test_R12_corrupt_catalog(self):
        version=self.install_evidence()
        s=json.loads(self.state().read_bytes());s.update(installedVersion=version,downloadedVersion=version,status='installed');self.state().write_text(json.dumps(s))
        c=self.routes/'catalog/telegram';c.mkdir(exist_ok=True)
        (c/'version.json').write_bytes(b'{"schemaVersion":1,')
        before=(c/'version.json').read_bytes()
        p=self.prepare();self.assertEqual(p.returncode,0,p.stderr);self.assertNotIn('parse error',p.stderr)
        s=json.loads(self.state().read_bytes());self.assertIsNone(s['downloadedVersion'])
        self.assertEqual(s['installedVersion'],version)
        self.assertEqual(s['lastError']['code'],'ROUTES_CATALOG_INVALID')
        self.assertEqual((c/'version.json').read_bytes(),before);self.assertEqual(self.ownership(),self.before)
        p=subprocess.run(['ash','-c','. "$BRORAY_ROOT/lib/routes-export-build.sh"; broray_routes_export_build_run telegram'],env=self.env,capture_output=True,text=True,timeout=30)
        self.assertNotEqual(p.returncode,0);self.assertNotIn('parse error',p.stderr)
        self.assertIn('ROUTES_CATALOG_INVALID',p.stderr)

    def test_R13_unsafe_parent(self):
        target=self.routes/'saved-state';self.state().parent.rename(target)
        (self.routes/'state').symlink_to(target)
        p=self.prepare();self.assertNotEqual(p.returncode,0)
        self.assertTrue((self.routes/'state').is_symlink())

    def test_R14_wrong_field_types(self):
        s=json.loads(self.state().read_bytes());s['routeCount']='unknown'
        self.corrupt_and_repair(json.dumps(s).encode())

    def test_R15_rollback_blocks_repair(self):
        self.state().write_bytes(b'{');marker=self.routes/'rollback-required.json';marker.write_bytes(b'pending evidence')
        p=self.prepare();self.assertNotEqual(p.returncode,0)
        self.assertEqual(self.state().read_bytes(),b'{');self.assertEqual(marker.read_bytes(),b'pending evidence')

    def test_R16_missing_state_preserves_installed(self):
        v=self.install_evidence();self.state().unlink()
        p=self.prepare();self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(json.loads(self.state().read_bytes())['installedVersion'],v)
        self.assertEqual(self.ownership(),self.before)

    def test_R17_invalid_config_not_recoverable(self):
        p=self.routes/'config.json'; cfg=json.loads(p.read_bytes());cfg['schemaVersion']=999;p.write_text(json.dumps(cfg))
        raw=p.read_bytes();self.state().write_bytes(b'{')
        r=self.prepare();self.assertNotEqual(r.returncode,0)
        self.assertEqual(p.read_bytes(),raw);self.assertEqual(self.state().read_bytes(),b'{')
        self.assertEqual(self.ownership(),self.before)

    def test_R18_ambiguous_lease_preserved(self):
        lock=self.routes/'locks/operation.lock';lock.mkdir();(lock/'foreign').write_bytes(b'owner evidence')
        self.state().write_bytes(b'{');r=self.prepare();self.assertNotEqual(r.returncode,0)
        self.assertEqual((lock/'foreign').read_bytes(),b'owner evidence');self.assertEqual(self.state().read_bytes(),b'{')

    def test_R19_global_registry_corrupt(self):
        p=self.routes/'installed/routes.json';p.write_bytes(b'{');self.state().write_bytes(b'{')
        before=self.ownership();r=self.prepare();self.assertNotEqual(r.returncode,0)
        self.assertEqual(self.ownership(),before);self.assertEqual(self.state().read_bytes(),b'{')

    def test_R20_conflicting_ownership(self):
        self.install_evidence();p=self.routes/'installed/routes.json';g=json.loads(p.read_bytes());g['routes'][0]['owners']=['youtube'];p.write_text(json.dumps(g))
        self.state().write_bytes(b'{');before=self.ownership();r=self.prepare();self.assertNotEqual(r.returncode,0)
        self.assertEqual(self.ownership(),before);self.assertEqual(self.state().read_bytes(),b'{')

    def test_R21_evidence_hash_mismatch(self):
        self.state().write_bytes(b'{')
        r=self.prepare('cp() { command cp "$@" || return; local target; for target; do :; done; case "$target" in *routes-runtime-recovery*/state.json) printf tampered >"$target";; esac; }')
        self.assertNotEqual(r.returncode,0);self.assertEqual(self.state().read_bytes(),b'{');self.assertEqual(self.ownership(),self.before)

    def test_R22_backup_parent_symlink(self):
        outside=self.root/'foreign-backup';outside.mkdir();(self.root/'backup/routes-runtime-recovery').symlink_to(outside)
        self.state().write_bytes(b'{');r=self.prepare();self.assertNotEqual(r.returncode,0)
        self.assertEqual(self.state().read_bytes(),b'{');self.assertEqual(list(outside.iterdir()),[])

    def test_R23_custom_state_not_recovered(self):
        custom=self.routes/'custom.json';custom.write_text(json.dumps({'schemaVersion':1,'bundles':[{'id':'user-fixture','name':'fixture'}]}))
        state=self.state('user-fixture');state.write_bytes(b'{');r=self.prepare();self.assertNotEqual(r.returncode,0)
        self.assertEqual(state.read_bytes(),b'{');self.assertEqual(self.ownership(),self.before)

    def test_R24_catalog_files_and_cli_refusal(self):
        # Admission is tested by the existing native CLI/API suites. Here the
        # real dispatcher and export validator run after an explicit admission
        # double, before any resource acquisition or router command is allowed.
        (self.root/'lib/route-job.sh').write_text('broray_route_cli_enter() { return 0; }\n')
        catalog=self.routes/'catalog/telegram';catalog.mkdir(exist_ok=True)
        for filename in ['routes.json','source-files.json']:
            with self.subTest(filename=filename):
                (catalog/filename).write_bytes(b'{')
                r=self.prepare();self.assertEqual(r.returncode,0,r.stderr);self.assertNotIn('parse error',r.stderr)
                self.assertEqual((catalog/filename).read_bytes(),b'{')
                for action in ['build-export','plan','export']:
                    p=subprocess.run(['ash',str(self.root/'bin/broray-routes'),action,'telegram'],env=self.env,capture_output=True,text=True,timeout=30)
                    self.assertNotEqual(p.returncode,0);self.assertIn('ROUTES_CATALOG_INVALID',p.stderr);self.assertNotIn('parse error',p.stderr)
                self.assertEqual(self.ownership(),self.before)

    def test_R25_missing_registry_not_reset(self):
        self.install_evidence();(self.routes/'installed/bundles/telegram.json').unlink()
        before=self.ownership();state=self.state().read_bytes();r=self.prepare()
        self.assertNotEqual(r.returncode,0);self.assertEqual(self.state().read_bytes(),state);self.assertEqual(self.ownership(),before)

    def test_R26_missing_global_not_rebuilt(self):
        self.install_evidence();(self.routes/'installed/routes.json').unlink()
        before=self.ownership();r=self.prepare();self.assertNotEqual(r.returncode,0)
        self.assertEqual(self.ownership(),before)

    def test_R27_nonexported_root_crosses_native_guard(self):
        env={k:v for k,v in self.env.items() if k!='BRORAY_ROOT'}
        self.state().write_bytes(b'')
        r=subprocess.run(['ash','-c','set -u; BRORAY_ROOT="$1"; . "$BRORAY_ROOT/lib/routes-runtime-repair.sh"; broray_routes_runtime_prepare','fixture',str(self.root)],env=env,capture_output=True,text=True,timeout=30)
        self.assertEqual(r.returncode,0,r.stderr)
        self.assert_recovered(b'')


if __name__=='__main__':unittest.main()
