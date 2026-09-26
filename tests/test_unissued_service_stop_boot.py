"""Actual public STOP interruption and real persistent-disk kernel reboot.

Called by the NIC-less, two-boot fixture; no router or fabricated bootId.
The baseline is retained separately; every production ledger stays exact.
"""
import hashlib,json,os,subprocess,time,unittest,shutil,fcntl
from pathlib import Path
from test_public_stop_trace import PublicStopTrace

sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()

def prepare(case,home,root,up,generation,origin,command):
    trace=PublicStopTrace(case,['/bin/ash',str(root/'opt/etc/init.d/S22broray-updater'),'stop'],
                         {**os.environ,'BRORAY_UPDATER_ROOT_PREFIX':str(root)})
    lock=root/'opt/var/lock/broray/global-operation.lock'
    try:
        trace.wait_exec(lambda a:len(a)>1 and a[1]==b'--version',time.monotonic()+120)
        case.assertTrue(lock.is_symlink());op=lock.readlink().parent
        state=json.loads((op/'state.json').read_bytes())
        case.assertEqual(state['platformPreflight']['phase'],'STOP_INTENT')
        case.assertNotIn('generationStop',state['platformPreflight'])
        case.assertEqual(state['serviceStop']['generationId'],generation)
        case.assertEqual(state['serviceStop']['originOperationId'],origin.name)
        cut=trace.kill_command();case.assertNotEqual(cut.returncode,0)
        case.assertTrue(lock.is_symlink())
        domain=up/'generations'/generation
        last=max(domain.glob('revision-*.json'));ledger=json.loads(last.read_bytes())
        case.assertEqual(ledger['state'],'RUNNING');case.assertEqual(ledger['stopOperationId'],'')
        row=dict(operation=str(op),generation=generation,origin=str(origin),bootId=ledger['bootId'],
                 ledger={p.name:sha(p) for p in domain.glob('*.json')},stateSha256=sha(op/'state.json'))
        with (home/'unissued-stop.json').open('x') as f:json.dump(row,f);f.flush();os.fsync(f.fileno())
        print('UNISSUED_STOP_PREPARED '+json.dumps(row),flush=True)
    finally:trace.close()

def resume(home,root,up,command):
    case=unittest.TestCase();row=json.loads((home/'unissued-stop.json').read_bytes())
    op=Path(row['operation']);origin=Path(row['origin']);gen=row['generation']
    state=json.loads((op/'state.json').read_bytes());s=state['serviceStop'];boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    case.assertNotEqual(boot,row['bootId']);case.assertEqual(sha(op/'state.json'),row['stateSha256'])
    domain=up/'generations'/gen;lock=root/'opt/var/lock/broray/global-operation.lock'
    native=Path('/work/.local/bin/linux-generation');guard=Path('/work/.local/bin/linux-guard')
    fd=os.open(root/'opt/var/lib/broray/operations.guard',os.O_RDWR);fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
    args=[str(native),'verify-unissued-stop-boot',str(root),origin.name,s['originProofSha256'],s['originStopNonce'],gen,op.name,state['platformPreflight']['stopNonce'],sha(op/'state.json')]
    def run(a):return subprocess.run(a,capture_output=True,text=True,pass_fds=(fd,),timeout=30)
    def snapshot():return {str(p):p.read_bytes() for folder in [domain,op] for p in folder.rglob('*') if p.is_file() and not p.is_symlink()}
    foreign=subprocess.Popen(['/bin/sleep','1000'])
    try:
        before=snapshot();ok=run(args);case.assertEqual(ok.returncode,0,ok.stdout+ok.stderr)
        proof=json.loads(ok.stdout);case.assertEqual(proof['phase'],'UNISSUED_STOP_BOOT_ENDED_VERIFIED')
        for k in ['serviceStopped','platformReady','signalsAuthorized','mutationAuthorized']:case.assertFalse(proof[k])
        case.assertEqual(before,snapshot());case.assertIsNone(foreign.poll())
        negatives=0
        for index,value in [(4,'0'*64),(5,'0'*32),(6,'g-'+'0'*22),(8,'0'*32),(9,'0'*64)]:
            bad=args.copy();bad[index]=value;r=run(bad)
            case.assertNotEqual(r.returncode,0,(index,r.stdout,r.stderr));case.assertEqual(before,snapshot());negatives+=1
        for file in [max(domain.glob('revision-*.json')),up/'starts'/gen/'ledger-witnesses/state.json']:
            original=file.read_bytes()
            try:
                file.write_bytes(b'{corrupt unissued-stop fixture')
                r=run(args);case.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
                case.assertEqual(file.read_bytes(),b'{corrupt unissued-stop fixture');negatives+=1
            finally:file.write_bytes(original)
        print('UNISSUED_STOP_NATIVE_PASS '+json.dumps(dict(positive=1,negatives=negatives,unchanged=before==snapshot(),foreignAlive=foreign.poll() is None)),flush=True)
    finally:fcntl.flock(fd,fcntl.LOCK_UN);os.close(fd)
    try:
        code=home/'current-recovery-code';shutil.copytree('/work/implementation/runtime/app',code)
        for n in ['operation-platform-generation.sh','operation-coordinator.sh','universal-platform-handoff.sh']:
            current=Path('/work/upgrade-'+n)
            if current.is_file():shutil.copyfile(current,code/'lib'/n)
        shutil.copyfile(native,code/'bin/broray-updater-generation');(code/'bin/broray-updater-generation').chmod(0o700)
        old=home/'baseline-recovery-code';shutil.copytree(code,old)
        for n in ['operation-coordinator.sh','operation-platform-generation.sh']:shutil.copyfile('/work/old-'+n,old/'lib'/n)
        env={**os.environ,'BRORAY_ROOT':str(root/'opt/broray'),'BRORAY_STATE_ROOT':str(root/'opt/var/lib/broray'),
             'BRORAY_ROUTES_API_LOCK':str(lock),'BRORAY_OPS_UPDATER_ROOT':str(up),'BRORAY_LEGACY_GLOBAL_LOCK':str(root/'tmp/broray-global-operation.lock'),
             'BRORAY_OPS_RAM_ROOT':str(home/'ram'),'BRORAY_OPS_GUARD':str(guard),'BRORAY_OPS_ASH':'/bin/ash','BRORAY_OPS_CODE_ROOT':str(code)}
        call=['/bin/ash','-c','. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"; broray_ops_call initialize']
        before=snapshot();baseline=subprocess.run(call,env={**env,'BRORAY_OPS_CODE_ROOT':str(old)},capture_output=True,text=True,timeout=60)
        case.assertNotEqual(baseline.returncode,0);case.assertEqual(before,snapshot());case.assertEqual(lock.readlink(),op/'fence')
        print('BASELINE_EXPECTED_FAIL '+json.dumps(dict(rc=baseline.returncode,stdout=baseline.stdout,stderr=baseline.stderr)),flush=True)
        # Lost reply before fence retirement, then after retirement/before START.
        for boundary in ['before-retire','after-retire']:
            traced=PublicStopTrace(case,call,env)
            try:
                pred=(lambda a: a and a[0].rsplit(b'/',1)[-1]==b'mv' and any(x.endswith(b'/retired-lock') for x in a)) if boundary=='before-retire' else (lambda a:len(a)>1 and a[1]==b'replacement-service-current')
                traced.wait_exec(pred,time.monotonic()+120)
                saved=json.loads((op/'state.json').read_bytes());case.assertEqual(saved['state'],'aborted')
                case.assertEqual(saved['errorCode'],'STOP_INTERRUPTED_BY_REBOOT');case.assertNotIn('generationStop',saved['platformPreflight'])
                case.assertEqual(lock.is_symlink(),boundary=='before-retire')
                traced.kill_command();case.assertIsNone(foreign.poll())
                print('UNISSUED_STOP_CRASH_BOUNDARY '+boundary,flush=True)
            finally:traced.close()
        fixed=subprocess.run(call,env=env,capture_output=True,text=True,timeout=180)
        print('UNISSUED_STOP_INITIALIZE '+json.dumps(dict(rc=fixed.returncode,stdout=fixed.stdout,stderr=fixed.stderr)),flush=True)
        case.assertEqual(fixed.returncode,0,fixed.stdout+fixed.stderr);case.assertFalse(lock.exists())
        current=command('status');case.assertEqual(current.returncode,0,current.stdout+current.stderr)
        ready=json.loads(current.stdout);case.assertTrue(ready['platformReady']);case.assertNotEqual(ready['generationId'],gen)
        for name,h in row['ledger'].items():case.assertEqual(sha(domain/name),h)
        case.assertEqual(json.loads(max(domain.glob('revision-*.json')).read_bytes())['state'],'RUNNING')
        case.assertTrue((domain/'boot-ended.receipt').is_file());case.assertIsNone(foreign.poll())
        # Ordinary stop/start must skip only the proven interrupted old request.
        for verb in ['stop','start','status']:
            r=command(verb);case.assertEqual(r.returncode,0,r.stdout+r.stderr)
        repeat=subprocess.run(call,env=env,capture_output=True,text=True,timeout=60)
        case.assertEqual(repeat.returncode,0,repeat.stdout+repeat.stderr)
        print('UNISSUED_STOP_RESUME_PASS actualKernelBoundary=true crashes=2 nativeNegatives=7 foreignPreserved=true startStopStart=true',flush=True)
    finally:foreign.terminate();foreign.wait(timeout=5)
