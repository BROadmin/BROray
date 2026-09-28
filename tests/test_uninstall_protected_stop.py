"""Canonical S22 stop under inherited uninstall exclusion; real Linux processes."""
import json,os,subprocess,time,unittest
from pathlib import Path
import test_installed_generation_stop as installed
ROOT=Path(__file__).resolve().parents[2]
GUARD=ROOT/'.local/bin/linux-guard'
class UninstallProtectedStop(unittest.TestCase):
    def test_canonical_stop_then_legacy_fence_excludes_restart(self):
        f=installed.InstalledGenerationStop(methodName='runTest')
        try:
            f.setUp()
            try:f.installed()
            except Exception:
                probe=f.inspect()
                print('FIXTURE_BOOT_CONTEXT_DIAGNOSTIC',probe.returncode,probe.stdout,probe.stderr,flush=True)
                raise
            started=f.init('start');self.assertEqual(started.returncode,0,started.stdout+started.stderr)
            generation=json.loads(started.stdout)['generationId']
            domain=f.updater/'generations'/generation
            anchor=(domain/'state.json').read_bytes()
            print('UNINSTALL_SCOPE_PHASE=STARTED',generation,flush=True)
            live=f.root/'router';state=live/'opt/var/lib/broray'
            legacy=live/'tmp/broray-global-operation.lock';ready=live/'tmp/stop-ready';release=live/'tmp/release'
            script=live/'opt/etc/init.d/S22broray-updater'
            shell='''set -e
/bin/ash "$1" stop >"$2"
jq -e '.ok and .phase=="SERVICE_STOP_COMPLETED" and .serviceStopped and (.platformReady==false)' "$2" >/dev/null
mkdir "$3"
echo KEEP >"$3/owned"
touch "$4"
while [ ! -e "$5" ]; do sleep 0.1; done
'''
            env=os.environ|{'BRORAY_UPDATER_ROOT_PREFIX':str(live)}
            p=subprocess.Popen([str(GUARD),'--scope',str(state/'operations.guard'),'/bin/ash','-c',shell,
                'stop',str(script),str(live/'tmp/stop.json'),str(legacy),str(ready),str(release)],
                env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            try:
                deadline=time.monotonic()+120
                while not ready.exists() and p.poll() is None and time.monotonic()<deadline:time.sleep(.05)
                self.assertTrue(ready.exists(),p.communicate(timeout=1) if p.poll() is not None else 'stop timed out')
                stopped=json.loads((live/'tmp/stop.json').read_bytes());self.assertEqual(stopped['generationId'],generation)
                print('UNINSTALL_SCOPE_PHASE=STOPPED',stopped,flush=True)
                denied=f.init('start');self.assertNotEqual(denied.returncode,0,denied.stdout+denied.stderr)
                self.assertEqual((legacy/'owned').read_text(),'KEEP\n')
                # state.json is the immutable generation birth anchor. Existing
                # InstalledGenerationStop uses revision records for final state.
                self.assertEqual((domain/'state.json').read_bytes(),anchor)
                revisions=sorted(domain.glob('revision-*.json'));self.assertTrue(revisions)
                ledger=json.loads(revisions[-1].read_bytes())
                self.assertEqual(ledger['state'],'STOPPED');self.assertEqual(ledger['children'],[])
                print('UNINSTALL_SCOPE_PHASE=RESTART_EXCLUDED',flush=True)
            finally:
                release.touch();out,err=p.communicate(timeout=10)
                if (legacy/'owned').exists():(legacy/'owned').unlink();legacy.rmdir()
            self.assertEqual(p.returncode,0,(out,err))
            restored=f.init('start');self.assertEqual(restored.returncode,0,restored.stdout+restored.stderr)
            self.assertNotEqual(json.loads(restored.stdout)['generationId'],generation)
            print('UNINSTALL_SCOPE_PHASE=RESTORED',flush=True)
            self.assertEqual(f.init('stop').returncode,0)
        finally:
            f.doCleanups()
if __name__=='__main__':unittest.main(verbosity=2)
