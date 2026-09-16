"""Reproduce unchanged updater admission on a complete abandoned legacy fence."""
import hashlib, json, subprocess, tarfile, tempfile, unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
ARCHIVE=ROOT/'references/3.1.0-r09c02/broray-compact-updater-platform-5.tar.gz'

class LegacyBaseline(unittest.TestCase):
    def test_old_updater_blocks_even_when_legacy_owner_is_absent(self):
        # This reproduces the historical updater. Current source can receive
        # endpoint fixes without silently changing the baseline under test.
        self.assertEqual(hashlib.sha256(ARCHIVE.read_bytes()).hexdigest(),
                         'eaf2eafb62d1b108fe576d1fda09ae7a5d15ad90f71272abc23f321d397611b0')
        with tarfile.open(ARCHIVE) as archive:
            raw=archive.extractfile('opt/libexec/broray-updater/broray-updater.sh').read()
        self.assertEqual(hashlib.sha256(raw).hexdigest(),
                         'a3c094b3a5e82ac82be7ec8b53c90f4023351a94eb6ba945b99501c0876e2c4a')
        source=raw.decode()
        function=source[source.index('global_operation_lock_classify()'):source.index('\nconflicting_operation_admission_clear()')]
        directory=Path(tempfile.mkdtemp(prefix='legacy-admission-'))
        lock=directory/'global-operation.lock';lock.mkdir()
        owner='2147483646'
        self.assertFalse(Path('/proc',owner).exists())
        for name,value in {'pid':owner,'scope':'system','action':'auto-switch','bundle':'','startedAt':'2026-09-15T01:00:00Z'}.items():
            (lock/name).write_text(value+'\n')
        before={p.name:p.read_bytes() for p in lock.iterdir()}
        script=directory/'probe.sh'
        script.write_text('GLOBAL_OPERATION_LOCK="'+str(lock)+'"\n'+function+
                          '\nrc=0; global_operation_lock_classify || rc=$?\nprintf "%s %s\\n" "$rc" "$GLOBAL_OPERATION_LOCK_STATE"\n')
        p=subprocess.run(['/bin/ash',str(script)],capture_output=True,timeout=5)
        self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(p.stdout,b'1 stale-route-owner\n')
        self.assertEqual({p.name:p.read_bytes() for p in lock.iterdir()},before)

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(LegacyBaseline))
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
            'finding':'Verified original updater refuses complete PID-only fence with absent owner',
            'rootCause':'ROOT_CAUSE_NOT_PROVEN','environment':'Linux, exact original classifier, isolated five-file fixture',
            'routerAccessed':False}
    (ROOT/'docs/evidence/legacy-recovery-baseline-tests.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
