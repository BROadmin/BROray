"""Kernel tests of an UNPUBLISHED legacy prototype in a disposable Linux VM.

Fake daemon bodies and a read-only preflight fixture are not a full 3.1.0 repair.
"""
import ctypes, hashlib, json, os, shutil, signal, subprocess, time, unittest, uuid
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
BIN=ROOT/'.local/bin/linux-legacy-recovery-prototype'
FIXTURE=ROOT/'.local/bin/linux-legacy-process-fixture'

def proc(pid):
    directory=Path('/proc')/str(pid)
    fields=directory.joinpath('stat').read_text().rsplit(') ',1)[1].split()
    return {'pid':pid,'ticks':fields[19],'parent':int(fields[1]),'state':fields[0],
            'exe':os.readlink(directory/'exe'),'cmd':directory.joinpath('cmdline').read_bytes()}

class LegacyPrototype(unittest.TestCase):
    def setUp(self):
        assert ROOT==Path('/work'), 'Disposable VM only; fixed /opt fixtures are forbidden elsewhere.'
        assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
        self.nonce=uuid.uuid4().hex
        self.app=Path('/opt/broray');self.app.mkdir(mode=0o700)
        (self.app/'TEST-OWNER').write_text(self.nonce)
        for name in ['bin','runtime','config','run']:(self.app/name).mkdir()
        self.session=Path('/opt/var/lib/broray/legacy-recovery')/self.nonce
        self.session.mkdir(parents=True,mode=0o700)
        self.parent=Path('/opt/var/lock/broray');self.parent.mkdir(parents=True,exist_ok=True)
        self.marker=self.parent/'TEST-OWNER';self.marker.write_text(self.nonce)
        self.lock=self.parent/'global-operation.lock';self.lock.mkdir()
        for name,value in {'pid':'2147483646','scope':'system','action':'auto-switch','bundle':'','startedAt':'2026-09-16T00:00:00Z'}.items():
            (self.lock/name).write_text(value+'\n')
        self.callback=self.session/'preflight.sh';self.preflight('exit 0')
        self.processes=[];self.targets=[]
        self.boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()

    def preflight(self,body):
        self.callback.write_text('#!/opt/bin/ash\nset -eu\n'+body+'\n')
        self.callback.chmod(0o600)

    def start(self,argv,env=None):
        p=subprocess.Popen(argv,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,
                           env={**os.environ,'PATH':'/opt/bin:/bin:/usr/bin',**(env or {})})
        self.processes.append(p)
        time.sleep(.1);self.assertIsNone(p.poll());return p

    def native(self,name,role,heartbeat=None,register=True):
        file=self.app/'runtime'/name;shutil.copy2(FIXTURE,file);file.chmod(0o700)
        argv=[str(file)]
        if role=='hold-web':argv+=['-f',str(self.app/'config/lighttpd.conf')]
        if role=='preserve-xray':argv+=['run','-c',str(self.app/'config/config.json')]
        p=self.start(argv,{'BRORAY_LEGACY_FIXTURE_HEARTBEAT':str(heartbeat)} if heartbeat else None)
        if register:self.register(p,role,0)
        return p

    def daemon(self,body='sleep 30',role='stop-home'):
        name={'stop-home':'broray-home-snapshotd','stop-auto':'broray-server-auto-switch'}[role]
        script=self.app/'bin'/name
        script.write_text('#!/opt/bin/ash\nwhile :; do '+body+'; done\n')
        p=self.start(['/opt/bin/ash',str(script)])
        self.register(p,role,30 if role=='stop-home' else 15);return p

    def register(self,p,role,seconds):
        record=proc(p.pid);file=self.session/f'cmd-{p.pid}';file.write_bytes(record['cmd']);file.chmod(0o600)
        self.targets.append({'process':p,'record':record,'role':role,'seconds':seconds})

    def manifest(self):
        path=self.session/'targets.tsv'
        path.write_text(''.join(f"{t['role']}\t{t['record']['pid']}\t{t['record']['ticks']}\t{self.boot}\t{t['record']['exe']}\tcmd-{t['record']['pid']}\t{t['seconds']}\n" for t in self.targets))
        path.chmod(0o600)

    def invoke(self,expected):
        self.manifest();p=subprocess.run([str(BIN),str(self.session)],capture_output=True,timeout=20)
        self.assertEqual(p.returncode,expected,(p.stdout,p.stderr));return p

    def assert_preserved(self):
        self.assertTrue(self.lock.is_dir());self.assertFalse((self.session/'retired-global.lock').exists())
        for target in self.targets:self.assertIsNone(target['process'].poll())

    def test_admission_parent_is_not_an_exempt_bootstrap_ancestor(self):
        self.daemon();self.manifest()
        frontend=self.app/'runtime/broray-lighttpd'
        shutil.copy2(FIXTURE,frontend);frontend.chmod(0o700)
        # Simulate an invocation beneath the application's live CGI broker.
        # Exempting that ancestor would leave it able to admit another writer.
        result=subprocess.run([str(frontend),'-f',str(self.app/'config/lighttpd.conf')],
            env={**os.environ,'BRORAY_LEGACY_FIXTURE_CHILD_BINARY':str(BIN),
                 'BRORAY_LEGACY_FIXTURE_SESSION':str(self.session)},capture_output=True,timeout=20)
        self.assertEqual(result.returncode,75,(result.stdout,result.stderr))
        self.assert_preserved()

    def test_idle_recovery_retires_exact_fence_and_preserves_xray(self):
        heartbeat=self.session/'xray-heartbeat'
        web=self.native('broray-lighttpd','hold-web')
        xray=self.native('xray','preserve-xray',heartbeat)
        owner=self.daemon();birth=proc(xray.pid)
        (self.lock/'pid').write_text(str(owner.pid)+'\n')
        before={p.name:p.read_bytes() for p in self.lock.iterdir()}
        self.preflight('sleep 1\nexit 0')
        initial=heartbeat.stat().st_size
        data=json.loads(self.invoke(0).stdout)
        self.assertEqual(data['result'],'legacy_fence_retired')
        owner.wait(timeout=3);self.assertEqual(owner.returncode,-signal.SIGKILL)
        self.assertIsNone(web.poll());self.assertIsNone(xray.poll())
        self.assertEqual(proc(xray.pid)['ticks'],birth['ticks'])
        self.assertGreater(heartbeat.stat().st_size,initial+3)
        self.assertFalse(self.lock.exists())
        retired=self.session/'retired-global.lock'
        self.assertEqual({p.name:p.read_bytes() for p in retired.iterdir()},before)
        self.assertFalse(json.loads((self.session/'quiescent.json').read_bytes())['globalFenceRetired'])
        self.assertTrue(json.loads((self.session/'complete.json').read_bytes())['globalFenceRetired'])

    def test_empty_bundle_from_archived_scheduler_is_supported(self):
        self.native('broray-lighttpd','hold-web');owner=self.daemon()
        # Archived scheduler/auto-switch use ': > bundle', not printf newline.
        (self.lock/'action').write_text('subscriptions:scheduler\n')
        (self.lock/'bundle').write_bytes(b'')
        self.invoke(0);owner.wait(timeout=3)
        self.assertEqual((self.session/'retired-global.lock/bundle').read_bytes(),b'')

    def test_discovery_records_full_identity_without_changing_lock(self):
        self.native('broray-lighttpd','hold-web');owner=self.daemon()
        before={p.name:p.read_bytes() for p in self.lock.iterdir()}
        for target in self.targets:(self.session/f"cmd-{target['record']['pid']}").unlink()
        result=subprocess.run([str(BIN),'--discover',str(self.session)],capture_output=True,timeout=10)
        self.assertEqual(result.returncode,0,result.stderr)
        rows=(self.session/'targets.tsv').read_text().splitlines()
        self.assertEqual(len(rows),2)
        self.assertEqual((self.session/f'cmd-{owner.pid}').read_bytes(),proc(owner.pid)['cmd'])
        self.assertEqual(before,{p.name:p.read_bytes() for p in self.lock.iterdir()})
        result=subprocess.run([str(BIN),str(self.session)],capture_output=True,timeout=20)
        self.assertEqual(result.returncode,0,result.stderr);owner.wait(timeout=3)

    def test_exact_stopped_pid_projection_is_archived_by_native_parent(self):
        self.native('broray-lighttpd','hold-web');owner=self.daemon()
        projection=self.app/'run/home-snapshotd.pid';value=f'{owner.pid}\n'.encode();projection.write_bytes(value)
        self.invoke(0);owner.wait(timeout=3)
        self.assertFalse(projection.exists())
        self.assertEqual((self.session/'retired-home-snapshotd.pid').read_bytes(),value)

    def test_external_native_ip_writer_is_not_exempt(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        directory=Path('/tmp')/('legacy-native-writer-'+self.nonce);directory.mkdir()
        binary=directory/'ip';shutil.copy2(FIXTURE,binary);binary.chmod(0o700)
        writer=self.start([str(binary),'route','add','fixture'])
        try:
            self.invoke(75);self.assert_preserved();self.assertIsNone(writer.poll())
        finally:
            writer.kill();writer.wait(timeout=3);shutil.rmtree(directory)

    def test_transient_xray_probe_is_not_a_preserved_runtime(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        self.native('xray','preserve-xray')
        probe=self.start([str(self.app/'runtime/xray'),'run','-c','/tmp/probe.json'])
        self.register(probe,'preserve-xray',0)
        self.invoke(75);self.assert_preserved()

    def test_live_foreign_pid_projection_is_preserved(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        xray=self.native('xray','preserve-xray')
        projection=self.app/'run/home-snapshotd.pid';projection.write_text(f'{xray.pid}\n')
        self.invoke(75);self.assert_preserved();self.assertTrue(projection.exists())

    def test_replaced_projection_retains_global_fence(self):
        web=self.native('broray-lighttpd','hold-web');owner=self.daemon()
        projection=self.app/'run/home-snapshotd.pid';projection.write_text(f'{owner.pid}\n')
        self.preflight(f'if [ "$2" = finalize ]; then printf "2147483646\\n" >"{projection}"; fi')
        self.invoke(75);owner.wait(timeout=3)
        self.assertTrue(self.lock.is_dir());self.assertIsNone(web.poll())
        self.assertEqual(projection.read_text(),'2147483646\n')

    def test_existing_receipt_is_not_overwritten_or_used_as_authority(self):
        web=self.native('broray-lighttpd','hold-web');owner=self.daemon()
        receipt=self.session/'quiescent.json';receipt.write_text('foreign receipt')
        self.invoke(74)
        owner.wait(timeout=3);self.assertIsNone(web.poll())
        self.assertTrue(self.lock.is_dir());self.assertEqual(receipt.read_text(),'foreign receipt')
        self.assertFalse((self.session/'complete.json').exists())

    def test_unknown_worker_refuses_without_stopping_any_known_daemon(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        unknown=self.native('unknown-worker','unregistered',register=False)
        result=self.invoke(75)
        self.assertIn(b'LEGACY_WRITER_UNCONFIRMED',result.stderr)
        self.assertIsNone(unknown.poll());self.assert_preserved()

    def test_busy_shell_child_is_not_an_idle_witness(self):
        self.native('broray-lighttpd','hold-web')
        self.daemon("/opt/bin/ash -c 'sleep 30; :' & wait $!")
        self.invoke(75);self.assert_preserved()

    def test_changed_birth_is_rejected_before_pin(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        self.targets[-1]['record']['ticks']=str(int(self.targets[-1]['record']['ticks'])+1)
        self.invoke(75);self.assert_preserved()

    def test_readonly_preflight_refusal_preserves_all_tasks(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        self.preflight('exit 75')
        self.invoke(75);self.assert_preserved()

    def test_callback_cannot_leave_an_untracked_background_writer(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        self.preflight(f"'{FIXTURE}' &\nexit 0")
        self.invoke(75);self.assert_preserved()

    def test_bookkeeping_finishes_before_fence_retirement(self):
        self.native('broray-lighttpd','hold-web');owner=self.daemon()
        phases=self.session/'phases'
        self.preflight(f"test -d '{self.lock}'\nprintf '%s\\n' \"${{2:-missing}}\" >>'{phases}'")
        self.invoke(0)
        owner.wait(timeout=3)
        self.assertEqual(phases.read_text().splitlines(),['check','finalize'])

    def test_failed_bookkeeping_keeps_fence_after_daemon_stop(self):
        web=self.native('broray-lighttpd','hold-web');owner=self.daemon()
        self.preflight('if [ "${2:-}" = finalize ]; then exit 75; fi')
        self.invoke(75)
        owner.wait(timeout=3)
        self.assertEqual(owner.returncode,-signal.SIGKILL)
        self.assertTrue(self.lock.is_dir());self.assertIsNone(web.poll())

    def test_changed_generation_is_not_retired(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        # Test-only fault hook deliberately simulates an external generation
        # change. The eventual signed production preflight must be read-only.
        self.preflight(f"printf 'subscriptions:scheduler\\n' >'{self.lock}/action'")
        self.invoke(75);self.assert_preserved()
        self.assertEqual((self.lock/'action').read_text(),'subscriptions:scheduler\n')

    def test_live_unrelated_pid_in_old_record_is_preserved(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        xray=self.native('xray','preserve-xray')
        (self.lock/'pid').write_text(str(xray.pid)+'\n')
        self.invoke(75);self.assert_preserved()

    def test_extra_owner_metadata_prevents_legacy_retirement(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        (self.lock/'owner.json').write_text('{"schemaVersion":2}')
        self.invoke(75);self.assert_preserved()
        self.assertEqual((self.lock/'owner.json').read_text(),'{"schemaVersion":2}')

    def test_padded_nginx_title_is_pinned_and_resumed(self):
        self.native('broray-lighttpd','hold-web');owner=self.daemon()
        binary=self.app/'run/web-new/native-auth/broray-ndm-auth-nginx'
        binary.parent.mkdir(parents=True);shutil.copy2(FIXTURE,binary);binary.chmod(0o700)
        proxy=self.start([str(binary)],{'BRORAY_LEGACY_FIXTURE_TITLE':'nginx: worker process'})
        self.assertGreater(proc(proxy.pid)['cmd'].count(b'\0'),8)
        self.register(proxy,'hold-proxy',0)
        self.preflight(f"tracer=$(awk '/^TracerPid:/ {{print $2}}' /proc/{proxy.pid}/status)\n"+
                       'test "$tracer" = "$BRORAY_LEGACY_BARRIER_PID"')
        self.invoke(0);owner.wait(timeout=3)
        self.assertIsNone(proxy.poll());self.assertNotIn(proc(proxy.pid)['state'],['T','t'])

    def test_symlink_projection_preserves_its_target(self):
        self.native('broray-lighttpd','hold-web');self.daemon()
        foreign=self.session/'foreign';foreign.write_text('auto-switch\n')
        (self.lock/'action').unlink();(self.lock/'action').symlink_to(foreign)
        self.invoke(75);self.assert_preserved()
        self.assertTrue((self.lock/'action').is_symlink())
        self.assertEqual(foreign.read_text(),'auto-switch\n')

    def test_crash_of_guard_detaches_web_and_preserves_fence(self):
        web=self.native('broray-lighttpd','hold-web');self.daemon()
        ready=self.session/'preflight-ready'
        self.preflight(f"echo yes >'{ready}'\nsleep 10")
        self.manifest()
        guard=subprocess.Popen([str(BIN),str(self.session)],stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
        self.processes.append(guard)
        deadline=time.monotonic()+8
        while not ready.exists() and time.monotonic()<deadline:
            self.assertIsNone(guard.poll());time.sleep(.05)
        self.assertTrue(ready.exists());self.assertEqual(proc(web.pid)['state'],'t')
        guard.kill();guard.wait(timeout=5)
        deadline=time.monotonic()+3
        while proc(web.pid)['state'] in ['t','T'] and time.monotonic()<deadline:time.sleep(.05)
        self.assertNotIn(proc(web.pid)['state'],['t','T']);self.assert_preserved()

    def tearDown(self):
        for p in reversed(self.processes):
            if p.poll() is None:p.kill()
            p.wait(timeout=5)
        # We are a child subreaper in an isolated VM. A live unreaped direct
        # child cannot have its PID reused before our own waitpid reaps it.
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            direct=[]
            for directory in Path('/proc').iterdir():
                if not directory.name.isdigit():continue
                try:
                    fields=directory.joinpath('stat').read_text().rsplit(') ',1)[1].split()
                    if int(fields[1])==os.getpid():direct.append(int(directory.name))
                except (FileNotFoundError,ProcessLookupError):pass
            if not direct:break
            for pid in direct:
                try:
                    waited,_=os.waitpid(pid,os.WNOHANG)
                    if waited==0:os.kill(pid,signal.SIGKILL);os.waitpid(pid,0)
                except ChildProcessError:pass
        else:self.fail('A fixture descendant survived; preserve the VM for diagnosis')
        assert self.app.resolve()==Path('/opt/broray') and (self.app/'TEST-OWNER').read_text()==self.nonce
        assert self.marker.read_text()==self.nonce and self.parent.resolve()==Path('/opt/var/lock/broray')
        assert self.session.resolve()==Path('/opt/var/lib/broray/legacy-recovery')/self.nonce
        shutil.rmtree(self.app);shutil.rmtree(self.session)
        if self.lock.exists():shutil.rmtree(self.lock)
        self.marker.unlink()

if __name__=='__main__':
    assert ROOT==Path('/work')
    Path('/opt/bin').mkdir(parents=True,exist_ok=True)
    subprocess.run(['/bin/busybox','--install','-s','/opt/bin'],check=True)
    result=unittest.TextTestRunner(verbosity=2,failfast=False).run(unittest.defaultTestLoader.loadTestsFromTestCase(LegacyPrototype))
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
            'helperSha256':hashlib.sha256(BIN.read_bytes()).hexdigest(),'prototypeOnly':True,
            'environment':'Real Linux ptrace and process inventory; private fake daemon and preflight fixtures',
            'routerAccessed':False,'legacyFullApplicationRecoveryTested':False}
    (ROOT/'docs/evidence/legacy-recovery-prototype-tests.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
