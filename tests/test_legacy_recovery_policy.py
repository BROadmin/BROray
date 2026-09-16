"""Real archived-source policy + native barrier; fake daemons, isolated VM only."""
import hashlib, json, os, shutil, subprocess, tarfile, time, unittest
from pathlib import Path
from test_legacy_recovery_prototype import LegacyPrototype, ROOT, BIN, proc

class LegacyPolicy(LegacyPrototype):
    # Inherit lifecycle helpers, not the prototype-only fixture test cases.
    def setUp(self):
        super().setUp()
        self.extra_sessions=[]
        self.platform_files=[]
        jq=Path('/opt/bin/jq')
        if not jq.exists():jq.symlink_to('/usr/bin/jq');self.platform_files.append(jq)
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

    def test_policy_archived_daemon_loops_quiesce_with_real_pid_projections(self):
        # These are unchanged archived daemon bodies. The Home health/refresh
        # dependencies return immediately; this is not a full router app test.
        definitions=[
            ('broray-home-snapshotd','stop-home',30,'home-snapshotd.pid',
             {'BRORAY_HOME_SNAPSHOT_REFRESH':'/opt/bin/true','BRORAY_LIGHTTPD_GUARD':'/opt/bin/true','BRORAY_MONITOR_SERVICE':'/opt/bin/true'}),
            ('broray-subscription-scheduler','stop-subscriptions',60,'subscription-scheduler.pid',{}),
            ('broray-server-auto-switch','stop-auto',15,'server-auto-switch.pid',{}),
            ('broray-connection-monitor','stop-monitor',10,'connection-monitor.pid',
             {'BRORAY_MONITOR_MAINTENANCE':'/opt/bin/true'})]
        daemons=[]
        for name,role,seconds,pidfile,env in definitions:
            log=self.session/(name+'.log')
            with log.open('wb') as stream:
                process=subprocess.Popen(['/opt/bin/ash',str(self.app/'bin'/name)],
                    env={**os.environ,'PATH':'/opt/bin:/bin:/usr/bin',**env},
                    stdin=subprocess.DEVNULL,stdout=stream,stderr=stream)
            self.processes.append(process);time.sleep(.1)
            self.assertIsNone(process.poll(),(name,log.read_text()))
            if role!='stop-subscriptions':(self.app/'run'/pidfile).write_text(f'{process.pid}\n')
            deadline=time.monotonic()+8
            while time.monotonic()<deadline:
                children=[]
                for directory in Path('/proc').iterdir():
                    if not directory.name.isdigit():continue
                    try:
                        item=proc(int(directory.name))
                        if item['parent']==process.pid:children.append(item)
                    except (FileNotFoundError,ProcessLookupError):pass
                if len(children)==1 and children[0]['cmd'].split(b'\0')[:2]==[b'sleep',str(seconds).encode()]:break
                self.assertIsNone(process.poll());time.sleep(.05)
            else:self.fail('Archived daemon did not reach the supported idle point: '+name)
            self.register(process,role,seconds);daemons.append((process,pidfile))
        self.invoke(0)
        for process,pidfile in daemons:
            process.wait(timeout=3);self.assertEqual(process.returncode,-9)
            self.assertFalse((self.app/'run'/pidfile).exists())
            self.assertEqual((self.session/('retired-'+pidfile)).read_text(),f'{process.pid}\n')
        self.assertFalse((self.app/'run/subscription-scheduler.starttime').exists())
        self.assertTrue(json.loads(self.pause.read_bytes())['paused']);self.assertIsNone(self.xray.poll())

    def launch_wrapper(self):
        # Architecture-only dependency fixture: execute the identical shell
        # wrapper with the VM's tested x86 helper. Real ARM is a separate gate.
        uname=Path('/opt/bin/uname');self.assertTrue(uname.is_symlink())
        target=os.readlink(uname);uname.unlink()
        uname.write_text('#!/opt/bin/ash\nprintf "aarch64\\n"\n');uname.chmod(0o700)
        before=set(self.session.parent.iterdir())
        try:
            return subprocess.run(['/opt/bin/ash',str(self.session/'recover.sh')],capture_output=True,timeout=45)
        finally:
            self.extra_sessions.extend(set(self.session.parent.iterdir())-before)
            uname.unlink();uname.symlink_to(target)

    def test_policy_wrapper_discovers_and_recovers_verified_bundle(self):
        result=self.launch_wrapper()
        self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        self.assertEqual(json.loads(result.stdout)['result'],'legacy_fence_retired')
        self.assertEqual(len(self.extra_sessions),1);self.assertTrue(json.loads(self.pause.read_bytes())['paused'])

    def test_policy_wrapper_rejects_tampered_bundle_before_session(self):
        manifest=self.session/'source.files';manifest.write_bytes(manifest.read_bytes()+b'extra\n')
        result=self.launch_wrapper();self.assertEqual(result.returncode,73,result.stderr)
        self.assertFalse(self.extra_sessions);self.assertFalse(self.pause.exists());self.assertTrue(self.lock.is_dir())

    def test_policy_wrapper_does_not_retry_after_policy_refusal(self):
        (self.lock/'action').write_text('unsupported\n')
        result=self.launch_wrapper();self.assertEqual(result.returncode,75,(result.stdout,result.stderr))
        self.assertEqual(len(self.extra_sessions),1)
        self.assertEqual(json.loads(result.stdout)['errorCode'],'LEGACY_RECOVERY_REFUSED')
        self.assertTrue(self.lock.is_dir());self.assertFalse(self.pause.exists())

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
        for path in self.extra_sessions:
            assert path.resolve().parent==self.session.parent.resolve() and len(path.name)==32
            shutil.rmtree(path)
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
