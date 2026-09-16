"""Real CLI entry point with harmless business fixtures in a private Linux root."""
import ctypes, json, os, shutil, subprocess, tempfile, time, unittest
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]


class RouteEntry(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='route-entry-',dir=ROOT/'.local'))
        self.app=self.temp/'app'; self.state=self.temp/'state'; self.lock=self.temp/'global.lock'
        shutil.copytree(ROOT/'implementation/runtime/app/lib',self.app/'lib')
        (self.app/'bin').mkdir(); (self.app/'routes/operations').mkdir(parents=True)
        for name in ['broray-routes']:
            shutil.copy2(ROOT/'implementation/runtime/app/bin'/name,self.app/'bin'/name)
        self.env={**os.environ,'BRORAY_ROOT':str(self.app),'BRORAY_STATE_ROOT':str(self.state),
            'BRORAY_ROUTES_API_LOCK':str(self.lock),'BRORAY_LEGACY_GLOBAL_LOCK':str(self.temp/'legacy'),
            'BRORAY_OPS_UPDATER_ROOT':str(self.temp/'updater'),'BRORAY_OPS_RAM_ROOT':str(self.temp/'ram'),
            'BRORAY_OPS_GUARD':str(ROOT/'.local/bin/linux-guard'),'BRORAY_OPS_SUPERVISOR':str(ROOT/'.local/bin/linux-supervisor'),
            'BRORAY_OPS_ASH':'/bin/ash','BRORAY_ASH_BIN':'/bin/ash'}
        (self.app/'lib/routes-download.sh').write_text('broray_routes_check_run() { echo CHANGED >"$BRORAY_ROOT/changed"; }\n')

    def tearDown(self):
        assert self.temp.resolve().parent==(ROOT/'.local').resolve()
        shutil.rmtree(self.temp)

    def wait_cli(self,p):
        deadline=time.monotonic()+60
        while p.poll() is None:
            for file in (self.temp/'ram').rglob('children.json'):
                try: children=json.loads(file.read_text()).get('children',[])
                except FileNotFoundError: continue
                for child in children:
                    try: os.waitpid(child['pid'],os.WNOHANG)
                    except ChildProcessError: pass
            if time.monotonic()>deadline:
                p.kill(); p.communicate(timeout=5); self.fail('CLI owner exceeded test deadline')
            time.sleep(.02)
        out,err=p.communicate(timeout=5)
        return subprocess.CompletedProcess(p.args,p.returncode,out,err)

    def run_cli(self,env=None):
        return self.wait_cli(subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-routes'),'check','fixture'],
            env=env or self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE))

    def test_cli_preserves_existing_foreign_global_fence(self):
        self.lock.mkdir(); (self.lock/'sentinel').write_text('KEEP')
        p=self.run_cli()
        self.assertEqual(p.returncode,2,(p.stdout,p.stderr))
        self.assertFalse((self.app/'changed').exists())
        self.assertEqual((self.lock/'sentinel').read_text(),'KEEP')

    def test_cli_has_a_recorded_non_cancellable_owner(self):
        (self.app/'lib/routes-download.sh').write_text('''broray_routes_check_run() {
    test -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" || return 93
    jq -e '.scope=="routes" and .cancelability=="protected" and .running' \
      "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json" >/dev/null || return 94
    echo CHANGED >"$BRORAY_ROOT/changed"
}
''')
        p=self.run_cli(); self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual((self.app/'changed').read_text(),'CHANGED\n')
        self.assertFalse(self.lock.exists()); self.assertFalse(self.lock.is_symlink())

    def test_cli_drains_a_detached_descendant_before_releasing(self):
        (self.app/'lib/routes-download.sh').write_text('''broray_routes_check_run() {
    /bin/ash -c 'sleep 2; echo LATE >"$BRORAY_ROOT/late"' >/dev/null 2>&1 &
    return 0
}
''')
        p=self.run_cli(); self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        time.sleep(3)
        self.assertFalse((self.app/'late').exists(),'Detached writer outlived the route command')
        self.assertFalse(self.lock.exists()); self.assertFalse(self.lock.is_symlink())

    def test_copied_token_and_flags_do_not_admit_an_untraced_process(self):
        (self.app/'lib/routes-download.sh').write_text('''broray_routes_check_run() {
    echo READY >"$BRORAY_ROOT/ready"
    n=0; while [ ! -f "$BRORAY_ROOT/finish" ] && [ "$n" -lt 400 ]; do usleep 100000; n=$((n+1)); done
}
''')
        owner=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-routes'),'check','fixture'],
            env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            deadline=time.monotonic()+30
            while not (self.app/'ready').exists():
                if owner.poll() is not None:self.fail(owner.communicate())
                self.assertLess(time.monotonic(),deadline); time.sleep(.02)
            files=list((self.state/'operations').glob('*/owner.json')); self.assertEqual(len(files),1)
            record=json.loads(files[0].read_bytes())
            forged={**self.env,'BRORAY_BACKGROUND_OPERATION_ID':record['operationId'],
                'BRORAY_BACKGROUND_OPERATION_TOKEN':record['token'],
                'BRORAY_OPS_SUPERVISED':'ptrace/1','BRORAY_OPS_ROUTE_SUPERVISED':'ptrace/1'}
            p=self.run_cli(forged); self.assertEqual(p.returncode,73,(p.stdout,p.stderr))
            self.assertTrue(self.lock.is_symlink())
        finally:
            (self.app/'finish').touch()
            p=self.wait_cli(owner); self.assertEqual(p.returncode,0,(p.stdout,p.stderr))

    def test_traced_command_cannot_switch_to_a_different_bundle(self):
        (self.app/'lib/routes-download.sh').write_text('''broray_routes_check_run() {
    [ "${TEST_INNER:-0}" = 0 ] || { echo WRONG >"$BRORAY_ROOT/wrong"; return 0; }
    rc=0; TEST_INNER=1 /bin/ash "$BRORAY_ROOT/bin/broray-routes" check other || rc=$?
    [ "$rc" = 73 ]
}
''')
        p=self.run_cli(); self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertFalse((self.app/'wrong').exists())

    def test_custom_cli_cannot_bypass_a_foreign_fence(self):
        shutil.copy2(ROOT/'implementation/runtime/app/bin/broray-routes-user',self.app/'bin')
        (self.app/'lib/routes-user-import.sh').write_text('''broray_user_routes_cleanup() { :; }
broray_user_routes_preview() { echo CHANGED >"$BRORAY_ROOT/changed"; }
''')
        self.lock.mkdir(); (self.lock/'sentinel').write_text('KEEP')
        request=self.temp/'request.json'; request.write_text('{}')
        p=self.wait_cli(subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-routes-user'),'preview',str(request)],
            env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE))
        self.assertEqual(p.returncode,2,(p.stdout,p.stderr))
        self.assertFalse((self.app/'changed').exists()); self.assertEqual((self.lock/'sentinel').read_text(),'KEEP')


if __name__=='__main__':
    if os.name=='nt':raise SystemExit('Run in isolated Linux guest')
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    Path('/opt/bin').mkdir(parents=True,exist_ok=True)
    subprocess.run(['/bin/busybox','--install','-s','/opt/bin'],check=True)
    if not Path('/opt/bin/jq').exists():Path('/opt/bin/jq').symlink_to('/usr/bin/jq')
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RouteEntry))
    (ROOT/'docs/evidence/route-entry-tests.json').write_text(json.dumps({
        'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
        'environment':'Real Linux processes, original CLI entry, harmless private business fixture',
        'routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
