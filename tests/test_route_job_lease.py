"""A route resource lease must be linked to its proven protected job."""
import ctypes,hashlib,json,os,subprocess,time,unittest
from pathlib import Path
from test_route_entry import ROOT,RouteEntry


class RouteJobLease(unittest.TestCase):
    setUp=RouteEntry.setUp
    tearDown=RouteEntry.tearDown
    wait_cli=RouteEntry.wait_cli
    run_cli=RouteEntry.run_cli

    def test_resource_lease_records_proven_job_membership(self):
        (self.app/'lib/routes-download.sh').write_text('''broray_routes_check_run() {
    . "$BRORAY_ROOT/lib/routes-resource-lock.sh"
    broray_route_resource_acquire "$BRORAY_ROOT/routes/locks/operation.lock" check fixture || return $?
    cp "$BRORAY_ROOT/routes/locks/operation.lock/owner.json" "$BRORAY_ROOT/lease.json"
    broray_route_resource_release "$BRORAY_ROOT/routes/locks/operation.lock" "$BRORAY_ROUTE_RESOURCE_TOKEN"
}
''')
        p=self.run_cli();self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        lease=json.loads((self.app/'lease.json').read_bytes())
        records=list((self.state/'operations').glob('*/route-supervision.json'));self.assertEqual(len(records),1)
        supervisor=json.loads(records[0].read_bytes())
        self.assertIn('job',lease)
        self.assertEqual(lease['job']['operationId'],supervisor['operationId'])
        self.assertEqual(lease['job']['supervisorId'],supervisor['supervisorId'])
        self.assertEqual(lease['job']['supervisorOwner'],supervisor['owner'])
        self.assertEqual(len(lease['job']['jobTokenDigest']),64)
        self.assertFalse(self.lock.is_symlink());self.assertFalse(self.lock.exists())

    def test_untraced_copied_token_cannot_publish_a_bound_resource_lease(self):
        (self.app/'lib/routes-download.sh').write_text('''broray_routes_check_run() {
    echo "$$" >"$BRORAY_ROOT/ready"
    n=0; while [ ! -f "$BRORAY_ROOT/finish" ] && [ "$n" -lt 400 ]; do usleep 100000; n=$((n+1)); done
}
''')
        owner=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-routes'),'check','fixture'],env=self.env,
            stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            deadline=time.monotonic()+30
            while not (self.app/'ready').exists():
                if owner.poll() is not None:self.fail(owner.communicate())
                self.assertLess(time.monotonic(),deadline);time.sleep(.02)
            record=json.loads(next((self.state/'operations').glob('*/owner.json')).read_bytes())
            forged={**self.env,'BRORAY_BACKGROUND_OPERATION_ID':record['operationId'],
                'BRORAY_BACKGROUND_OPERATION_TOKEN':record['token'],
                'BRORAY_OPS_SUPERVISED':'ptrace/1','BRORAY_OPS_ROUTE_SUPERVISED':'ptrace/1'}
            script='. "$BRORAY_ROOT/lib/routes-resource-lock.sh"\nbroray_route_resource_acquire "$BRORAY_ROOT/routes/locks/operation.lock" check fixture\n'
            p=subprocess.run(['/bin/ash','-c',script],env=forged,capture_output=True,timeout=15)
            self.assertNotEqual(p.returncode,0)
            self.assertFalse((self.app/'routes/locks/operation.lock').exists())
            # Bypass the convenience wrapper and invoke the serialized
            # publisher with copied context and the real traced caller PID.
            # The publisher itself is outside the tree and must reject it.
            directory=self.state/'operations'/record['operationId']
            supervisor=json.loads((directory/'route-supervision.json').read_bytes())
            context={'ok':True,'operationId':record['operationId'],
                'supervisorId':supervisor['supervisorId'],'supervisorOwner':supervisor['owner'],
                'jobTokenDigest':hashlib.sha256(record['token'].encode()).hexdigest(),
                'action':'check','bundleId':'fixture'}
            parent=self.app/'routes/locks';parent.mkdir(exist_ok=True)
            publisher_env={**forged,'BRORAY_ROUTE_RESOURCE_JOB_CONTEXT':json.dumps(context),
                'BRORAY_ROUTE_RESOURCE_CALLER':(self.app/'ready').read_text().strip()}
            p=subprocess.run([self.env['BRORAY_OPS_GUARD'],str(parent/'resource.control.guard'),
                '/bin/ash',str(self.app/'lib/routes-resource-control.sh'),str(parent/'operation.lock'),
                str(os.getpid()),'acquire','check','fixture'],env=publisher_env,capture_output=True,timeout=15)
            self.assertEqual(p.returncode,73,(p.stdout,p.stderr))
            self.assertFalse((parent/'operation.lock').exists())
        finally:
            (self.app/'finish').touch();p=self.wait_cli(owner)
            self.assertEqual(p.returncode,0,(p.stdout,p.stderr))


if __name__=='__main__':
    if os.name=='nt':raise SystemExit('Run in isolated Linux guest')
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    Path('/opt/bin').mkdir(parents=True,exist_ok=True)
    subprocess.run(['/bin/busybox','--install','-s','/opt/bin'],check=True)
    if not Path('/opt/bin/jq').exists():Path('/opt/bin/jq').symlink_to('/usr/bin/jq')
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RouteJobLease))
    (ROOT/'docs/evidence/route-job-lease-tests.json').write_text(json.dumps({
        'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
        'environment':'Real Linux protected job, original resource lease, harmless backend fixture',
        'routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
