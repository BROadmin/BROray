"""Capture actual Xray release HTTP arguments; no network or router access."""
from pathlib import Path
import json,os,subprocess,tempfile,unittest

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))

class ReleaseMetadata(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='xray-metadata-');self.addCleanup(self.tmp.cleanup)
  self.home=Path(self.tmp.name);self.manifest=self.home/'share/release/manifest.json';self.manifest.parent.mkdir(parents=True)
  self.capture=self.home/'curl-args.json';bin=self.home/'bin';bin.mkdir()
  curl=bin/'curl';curl.write_text('#!/usr/bin/python3\nimport json,os,sys\nfrom pathlib import Path\nPath(os.environ["TEST_CAPTURE"]).write_text(json.dumps(sys.argv[1:]))\n');curl.chmod(0o755)
  self.env={**os.environ,'PATH':str(bin)+':/usr/bin:/bin','BRORAY_BASE':str(self.home),'TEST_CAPTURE':str(self.capture)}
 def request(self,manifest):
  if manifest is not None:self.manifest.write_bytes(manifest)
  p=subprocess.run(['/bin/ash','-c','. "$1"; broray_xray_github_get "releases?per_page=20&page=1" "$2"','test',str(ROOT/'runtime/app/lib/xray-releases.sh'),str(self.home/'out')],env=self.env,capture_output=True,text=True,timeout=5)
  self.assertEqual(p.returncode,0,p.stderr);args=json.loads(self.capture.read_text());print('HTTP_ARGUMENTS '+json.dumps(args),flush=True)
  self.assertIn('https://api.github.com/repos/XTLS/Xray-core/releases?per_page=20&page=1',args)
  self.assertIn('Accept: application/vnd.github+json',args)
  return [args[i+1] for i in range(len(args)-1) if args[i]=='-H' and args[i+1].startswith('User-Agent:')]
 def test_current_build_version_is_used(self):
  self.assertEqual(self.request(b'{"version":"3.2.0"}'),['User-Agent: BROray-Xray/3.2.0'])
 def test_missing_metadata_uses_unknown(self):
  self.assertEqual(self.request(None),['User-Agent: BROray-Xray/unknown'])
 def test_unusable_metadata_cannot_inject_headers(self):
  for value in [b'{broken',b'{"version":12}',json.dumps({'version':'3.2.0\r\nInjected: yes'}).encode()]:
   with self.subTest(value=value):self.assertEqual(self.request(value),['User-Agent: BROray-Xray/unknown'])

class CandidateCompatibility(unittest.TestCase):
 def resolve(self,candidate='3.2.0-r01c15',architecture='arm64',digest='3e38d72dfc5eb65c91df0e5583e9b6676c32232041da47de6ae73946b526d66c',tag='v26.9.9'):
  registry=json.loads((ROOT/'runtime/app/share/xray-compatibility.json').read_bytes())
  release=dict(tag_name=tag,assets=[dict(digest='sha256:'+digest)])
  context=dict(candidateId=candidate,architecture=architecture)
  r=subprocess.run(['jq','-nc','-L',str(ROOT/'runtime/app/lib'),'--argjson','records',json.dumps(registry['records']),'--argjson','context',json.dumps(context),'--argjson','release',json.dumps(release),'include "xray-releases"; $release|compatibility($records;$context)'],capture_output=True,text=True,timeout=5)
  self.assertEqual(r.returncode,0,r.stderr);return json.loads(r.stdout)
 def test_current_candidate_exposes_proven_xray_result(self):
  record=self.resolve();self.assertEqual(record['status'],'compatible',record)
  self.assertEqual(record['candidateId'],'3.2.0-r01c15')
  self.assertIn('CP06-XRAY-CURRENT-PROFILE',record['evidence'])
 def test_other_candidate_architecture_or_archive_remains_untested(self):
  for changed in [dict(candidate='3.2.0-r01c16'),dict(architecture='amd64'),dict(digest='0'*64),dict(tag='v26.9.8')]:
   with self.subTest(changed=changed):self.assertEqual(self.resolve(**changed)['status'],'untested')
 def test_historical_evidence_remains_available(self):
  self.assertEqual(self.resolve(candidate='3.1.1-r12c01')['status'],'compatible')

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
