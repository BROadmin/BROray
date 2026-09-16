"""Real archived-source policy + native barrier; fake daemons, isolated VM only."""
import hashlib, json, os, shutil, subprocess, tarfile, unittest
from pathlib import Path
from test_legacy_recovery_prototype import LegacyPrototype, ROOT, BIN, proc

class LegacyPolicy(LegacyPrototype):
    # Inherit lifecycle helpers, not the prototype-only fixture test cases.
    def setUp(self):
        super().setUp()
        self.platform_files=[]
        slot=self.app/'current';slot.mkdir()
        with tarfile.open(ROOT/'references/3.1.0-r09c02/broray-app-3.1.0-r09c02.tar.gz') as tf:
            tf.extractall(slot,filter='data')
        (slot/'.broray-slot').write_text('3.1.0-r09c02--legacy-policy-fixture\n')
        for name in ['bin','lib','share','web-new']:
            path=self.app/name
            if path.exists():path.rmdir()
            path.symlink_to('current/app/'+name,target_is_directory=True)
        with tarfile.open(ROOT/'references/3.1.0-r09c02/broray-compact-updater-platform-5.tar.gz') as tf:
            for member in tf:
                if not member.isfile() or not member.name.startswith('opt/'):continue
                path=Path('/')/member.name;self.assertFalse(path.exists());path.parent.mkdir(parents=True,exist_ok=True)
                path.write_bytes(tf.extractfile(member).read());path.chmod(member.mode)
                self.platform_files.append(path)
        for service in (slot/'init').iterdir():
            path=Path('/opt/etc/init.d')/service.name
            self.assertFalse(path.exists());path.symlink_to(service);self.platform_files.append(path)
        self.updater=Path('/opt/var/lib/broray-updater');self.updater.mkdir()
        (self.updater/'queue').mkdir()
        bundle=ROOT/'.local/legacy-recovery-bundle-linux'
        for path in bundle.iterdir():
            if path.name=='build.json':continue
            target=self.session/path.name;shutil.copy2(path,target);target.chmod(0o700 if path.name in ['legacy-recovery-guard','state-writer'] else 0o600)
        (self.app/'config/config.json').write_text('{"inbounds":[],"outbounds":[{"protocol":"blackhole"}]}\n')
        (self.app/'routes/operations').mkdir(parents=True)
        (self.app/'routes/operations/retained.json').write_text('{"kind":"routes","resumable":true,"running":false,"bundleId":"fixture"}\n')
        self.native('broray-lighttpd','hold-web');self.xray=self.native('xray','preserve-xray')
        self.pause=Path('/opt/var/lib/broray/background-automation.json')

    def refusal(self,error):
        result=self.invoke(75)
        self.assertTrue(self.lock.is_dir());self.assertIsNone(self.xray.poll())
        self.assertFalse(self.pause.exists())
        self.assertIn(error,(self.session/'check.stderr').read_text())
        return result

    def test_policy_exact_archive_passes_and_preserves_business_and_runtime(self):
        before=proc(self.xray.pid);route=self.app/'routes/operations/retained.json';data=route.read_bytes()
        self.invoke(0)
        self.assertEqual(proc(self.xray.pid)['ticks'],before['ticks'])
        self.assertTrue(json.loads(self.pause.read_bytes())['paused'])
        self.assertEqual(route.read_bytes(),data)
        self.assertEqual((self.session/'business.before').read_bytes(),(self.session/'business.after').read_bytes())

    def test_policy_changed_source_is_refused(self):
        source=self.app/'current/app/bin/broray-home-snapshotd';source.write_bytes(source.read_bytes()+b'\n# changed\n')
        self.refusal('SOURCE_CHANGED')

    def test_policy_extra_source_file_is_refused(self):
        (self.app/'current/app/bin/extra-worker').write_text('exit 0\n')
        self.refusal('SOURCE_CHANGED')

    def test_policy_init_override_is_refused(self):
        path=Path('/opt/etc/init.d/S24broray');path.unlink();path.write_text('exit 0\n')
        self.refusal('INIT_LINK_CHANGED')

    def test_policy_pending_updater_request_is_refused(self):
        (self.updater/'request.lock').mkdir();self.refusal('PROTECTED_TRANSACTION_PRESENT')

    def test_policy_pending_queue_is_refused(self):
        (self.updater/'queue/update-fixture.json').write_text('{}\n');self.refusal('UPDATER_QUEUE_PENDING')

    def test_policy_fifo_pointer_is_refused_without_blocking(self):
        os.mkfifo('/opt/var/lib/broray/last-operation');self.refusal('UPDATER_POINTER_UNSAFE')

    def test_policy_interrupted_updater_is_refused(self):
        state=Path('/opt/var/lib/broray/operations/update-fixture');state.mkdir(parents=True)
        (state/'state.json').write_text('{"schemaVersion":1,"engine":"broray-updater/5","running":true,"state":"running"}\n')
        Path('/opt/var/lib/broray/last-operation').write_text('update-fixture\n')
        self.refusal('UPDATER_TRANSACTION_PENDING')

    def test_policy_xray_install_is_preserved_for_domain_recovery(self):
        (self.lock/'scope').write_text('routes\n');(self.lock/'action').write_text('xray:install\n');(self.lock/'bundle').write_text('xray\n')
        self.refusal('PROTECTED_OR_UNSUPPORTED_ACTION')

    def test_policy_existing_route_progress_is_retained(self):
        (self.lock/'scope').write_text('routes\n');(self.lock/'action').write_text('resume\n');(self.lock/'bundle').write_text('fixture\n')
        route=self.app/'routes/operations/retained.json';before=route.read_bytes()
        self.invoke(0);self.assertEqual(route.read_bytes(),before)

    def tearDown(self):
        # Only our own disposable-VM files; no router or host filesystem paths.
        for path in reversed(self.platform_files):path.unlink()
        if self.updater.exists():shutil.rmtree(self.updater)
        state=Path('/opt/var/lib/broray')
        for name in ['last-operation','background-automation.json']:
            path=state/name
            if path.exists() or path.is_symlink():path.unlink()
        operations=state/'operations'
        if operations.exists():shutil.rmtree(operations)
        super().tearDown()

if __name__=='__main__':
    assert ROOT==Path('/work')
    Path('/opt/bin').mkdir(parents=True,exist_ok=True)
    subprocess.run(['/bin/busybox','--install','-s','/opt/bin'],check=True)
    suite=unittest.TestSuite(LegacyPolicy(name) for name in dir(LegacyPolicy) if name.startswith('test_policy_'))
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'prototypeOnly':True,
        'helperSha256':hashlib.sha256(BIN.read_bytes()).hexdigest(),
        'environment':'Archived 3.1.0 source and real callback, real Linux ptrace; fake native daemons',
        'routerAccessed':False,'legacyFullApplicationRecoveryTested':False}
    (ROOT/'docs/evidence/legacy-recovery-policy-tests.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
