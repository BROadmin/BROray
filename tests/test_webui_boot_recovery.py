"""Offline ash tests. Fake config validator/proc; no actual router/process signals."""
import hashlib, os, shutil, subprocess, tempfile, unittest
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
class Recovery(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.app = Path(self.tmp.name)
        for d in ['lib','bin','config','run','logs','tmp','runtime','proc','current/state-seed/config']:
            (self.app/d).mkdir(parents=True,exist_ok=True)
        shutil.copy(ROOT/'runtime/app/lib/operation-owner.sh',self.app/'lib/operation-owner.sh')
        self.conf = self.app/'config/lighttpd.conf'
        self.seed = self.app/'current/state-seed/config/lighttpd.conf'
        self.seed.write_bytes(b'server.port = 8080\nserver.bind = "127.0.0.1"\n')
        self.manifest = self.app/'current/SHA256SUMS'
        self.manifest.write_text(hashlib.sha256(self.seed.read_bytes()).hexdigest()+'  state-seed/config/lighttpd.conf\n')
        self.binary = self.app/'runtime/broray-lighttpd'
        self.binary.write_text('#!/bin/ash\n[ "$1" = -tt ] || exit 99\n[ -s "$3" ] && ! grep -q BROKEN "$3"\n')
        self.binary.chmod(0o755)
        self.defs = self.app/'defs.sh'
        self.defs.write_text((ROOT/'runtime/init/S25broray-web').read_text().split('\ncase "${1:-}" in',1)[0])
        self.env = {**os.environ,'BRORAY_WEB_BASE':str(self.app),'BRORAY_WEB_PROC_ROOT':str(self.app/'proc')}
    def run_shell(self,code):
        return subprocess.run(['/bin/ash','-c','. "$BRORAY_WEB_BASE/defs.sh"\n'+code],env=self.env,capture_output=True,timeout=15)
    def recover(self,ok=True):
        p=self.run_shell('broray_web_recover_config')
        if ok:self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        else:self.assertNotEqual(p.returncode,0)
        return p
    def test_empty_config_preserved_then_trusted_seed(self):
        self.conf.write_bytes(b'');self.recover()
        self.assertEqual(self.conf.read_bytes(),self.seed.read_bytes())
        saved=list((self.app/'logs/webui-recovery').glob('*/before'))
        self.assertEqual(len(saved),1);self.assertEqual(saved[0].read_bytes(),b'')
    def test_missing_config_recovered(self):self.recover();self.assertEqual(self.conf.read_bytes(),self.seed.read_bytes())
    def test_broken_nonempty_bytes_preserved(self):
        self.conf.write_bytes(b'BROKEN\x00conf\n');self.recover()
        self.assertEqual(next((self.app/'logs/webui-recovery').glob('*/before')).read_bytes(),b'BROKEN\x00conf\n')
    def test_good_config_not_changed(self):
        self.conf.write_bytes(b'custom good config\n');self.recover();self.assertEqual(self.conf.read_bytes(),b'custom good config\n')
    def test_target_symlink_refused(self):
        self.conf.symlink_to(self.seed);self.recover(False);self.assertTrue(self.conf.is_symlink())
    def test_seed_symlink_refused(self):
        data=self.seed.read_bytes();self.seed.unlink();other=self.app/'other';other.write_bytes(data);self.seed.symlink_to(other)
        self.conf.write_bytes(b'');self.recover(False);self.assertEqual(self.conf.read_bytes(),b'')
    def test_bad_seed_hash_preserves_target(self):
        self.conf.write_bytes(b'BROKEN');self.seed.write_text('other');self.recover(False);self.assertEqual(self.conf.read_bytes(),b'BROKEN')
    def test_duplicate_manifest_entry_refused(self):
        self.manifest.write_text(self.manifest.read_text()*2);self.conf.write_bytes(b'');self.recover(False);self.assertEqual(self.conf.read_bytes(),b'')
    def test_evidence_failure_preserves_target(self):
        (self.app/'logs/webui-recovery').write_text('foreign');self.conf.write_bytes(b'');self.recover(False);self.assertEqual(self.conf.read_bytes(),b'')
    def test_retention_limit_does_not_discard_evidence(self):
        for i in range(8):(self.app/'logs/webui-recovery'/('incident-'+str(i))).mkdir(parents=True)
        self.conf.write_bytes(b'');self.recover(False);self.assertEqual(self.conf.read_bytes(),b'')
    def test_empty_pid_evidence_and_retirement(self):
        pid=self.app/'run/lighttpd.pid';pid.write_bytes(b'')
        p=self.run_shell('broray_web_retire_invalid_pid');self.assertEqual(p.returncode,0,p.stderr);self.assertFalse(pid.exists())
        self.assertEqual(next((self.app/'logs/webui-recovery').glob('*/before')).read_bytes(),b'')
    def test_orphan_private_process_prevents_pid_retirement(self):
        pid=self.app/'run/lighttpd.pid';pid.write_bytes(b'');proc=self.app/'proc/222';proc.mkdir();(proc/'exe').symlink_to(self.binary)
        p=self.run_shell('broray_web_retire_invalid_pid');self.assertNotEqual(p.returncode,0);self.assertTrue(pid.exists())
    def test_live_foreign_pid_not_retired(self):
        pid=self.app/'run/lighttpd.pid';pid.write_text(str(os.getpid())+'\n')
        p=self.run_shell('broray_web_retire_invalid_pid');self.assertNotEqual(p.returncode,0);self.assertTrue(pid.exists())
    def test_health_rejects_invalid_config_even_with_http(self):
        self.conf.write_bytes(b'')
        p=self.run_shell('broray_web_pid_read(){ echo 222; };broray_web_process_identity_twice(){ return 0; };broray_web_http_healthy(){ return 0; };status')
        self.assertNotEqual(p.returncode,0,p.stdout)
    def publish_fixture(self):
        (self.app/'config/web-publish.json').write_text('{}')
        (self.app/'lib/web-publish.sh').write_text('''BRORAY_WEB_PUBLISH_OWNER="$BRORAY_WEB_PUBLISH_ROOT/config/web-publish.json"
broray_web_publish_owner_record_valid(){ return 0; }
broray_web_publish_ensure(){ echo ensure >>"$BRORAY_WEB_PUBLISH_ROOT/calls"; return "${TEST_PUBLISH_RC:-0}"; }
''')
        (self.app/'lib/routes-api-operation.sh').write_text('''broray_routes_api_lock_acquire(){ echo acquire >>"$BRORAY_ROOT/calls";return "${TEST_BUSY:-0}"; }
broray_routes_api_lock_release(){ echo release >>"$BRORAY_ROOT/calls"; }
''')
    def test_publish_runs_under_existing_global_lock(self):
        self.publish_fixture();p=self.run_shell('broray_web_http_healthy(){ return 0; };broray_web_reconcile_owned_publish')
        self.assertEqual(p.returncode,0,p.stderr);self.assertEqual((self.app/'calls').read_text(),'acquire\nensure\nrelease\n')
    def test_publish_never_creates_absent_owner(self):
        self.publish_fixture();(self.app/'config/web-publish.json').unlink()
        p=self.run_shell('broray_web_http_healthy(){ return 0; };broray_web_reconcile_owned_publish')
        self.assertNotEqual(p.returncode,0);self.assertFalse((self.app/'calls').exists())
    def test_publish_http_failure_zero_mutations(self):
        self.publish_fixture();p=self.run_shell('broray_web_http_healthy(){ return 1; };broray_web_reconcile_owned_publish')
        self.assertNotEqual(p.returncode,0);self.assertFalse((self.app/'calls').exists())
    def test_publish_busy_does_not_ensure_or_release_foreign_lock(self):
        self.publish_fixture();self.env['TEST_BUSY']='2';p=self.run_shell('broray_web_http_healthy(){ return 0; };broray_web_reconcile_owned_publish')
        self.assertNotEqual(p.returncode,0);self.assertEqual((self.app/'calls').read_text(),'acquire\n')
    def test_publish_failure_releases_owned_lock(self):
        self.publish_fixture();self.env['TEST_PUBLISH_RC']='1';p=self.run_shell('broray_web_http_healthy(){ return 0; };broray_web_reconcile_owned_publish')
        self.assertNotEqual(p.returncode,0);self.assertEqual((self.app/'calls').read_text(),'acquire\nensure\nrelease\n')
if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
