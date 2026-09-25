"""Real Home CGI + jq; isolated snapshots and authentication fixture, no router."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]


class HomeInstalledRelease(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.app = Path(self.tmp.name)
        for folder in ['lib', 'web-new/api', 'share/release', 'run/home-snapshots']:
            (self.app/folder).mkdir(parents=True)
        for name in ['home-snapshot.sh', 'release-manifest.sh']:
            shutil.copyfile(ROOT/'runtime/app/lib'/name, self.app/'lib'/name)
        (self.app/'web-new/api/auth-common.sh').write_text(
            'broray_api_require_method(){ [ "$1" = GET ]; }\n'
            'broray_api_require_session(){ :; }\n'
            'broray_api_success(){ printf "%s\\n" "$1"; }\n'
            'broray_api_error(){ exit 91; }\n', encoding='utf-8')
        self.manifest = self.app/'share/release/manifest.json'
        self.release('3.2.0')
        self.snapshot = self.app/'run/home-snapshots/broray.json'
        self.snapshot.write_text(json.dumps(dict(schemaVersion=1,module='broray',
            capturedAt='2026-09-23T16:30:11Z',capturedEpoch=int(time.time())-1200,
            data=dict(version='3.1.1',installationHealthy=True))), encoding='utf-8')
        self.env = dict(os.environ, BRORAY_HOME_ROOT=self.app.as_posix(), ASH_STANDALONE='1')

    def tearDown(self):
        self.tmp.cleanup()

    def release(self, version):
        self.manifest.write_text(json.dumps(dict(schemaVersion=3,version=version,
            releaseId=version+'-r01',webUIBuild='WebUI-'+version+'-r01c12',
            candidateId=version+'-r01c12')),encoding='utf-8')

    def run_cgi(self):
        before={p.relative_to(self.app).as_posix():hashlib.sha256(p.read_bytes()).hexdigest()
                for p in self.app.rglob('*') if p.is_file()}
        busybox=os.environ.get('BRORAY_TEST_BUSYBOX')
        shell=[busybox,'ash'] if busybox else ['/bin/sh']
        # Add host jq after the CGI's project/Entware search paths.
        self.env['PATH']=os.environ.get('BRORAY_TEST_BIN','')+os.pathsep+self.env['PATH']
        r=subprocess.run(shell+[str(ROOT/'runtime/app/web-new/api/home/summary.cgi')],
                         env=self.env,capture_output=True,text=True,encoding='utf-8',timeout=20)
        self.assertEqual(r.returncode,0,r.stderr)
        after={p.relative_to(self.app).as_posix():hashlib.sha256(p.read_bytes()).hexdigest()
               for p in self.app.rglob('*') if p.is_file()}
        self.assertEqual(before,after,'Home must not write snapshots or state')
        return json.loads(r.stdout)

    def test_current_manifest_beats_old_snapshot_without_rewriting_it(self):
        data=self.run_cgi()
        self.assertEqual(data.get('installedRelease',{}).get('version'),'3.2.0')
        self.assertEqual(data['broray']['version'],'3.1.1')
        self.assertEqual(data['broray']['_snapshot']['freshness'],'expired')
        self.assertEqual(data['snapshotRuntime']['collectorProcessesStarted'],0)

    def test_changed_installed_version_is_read_on_next_request(self):
        self.run_cgi();self.release('3.2.1')
        self.assertEqual(self.run_cgi().get('installedRelease',{}).get('version'),'3.2.1')

    def test_missing_manifest_does_not_reuse_cache(self):
        self.manifest.unlink()
        self.assertIn('installedRelease',self.run_cgi())
        self.assertIsNone(self.run_cgi()['installedRelease'])

    def test_corrupt_manifest_does_not_reuse_cache(self):
        self.manifest.write_text('{broken',encoding='utf-8')
        data=self.run_cgi()
        self.assertIn('installedRelease',data)
        self.assertIsNone(data['installedRelease'])

if __name__=='__main__':unittest.main(verbosity=2)
