"""Capture actual Xray release HTTP arguments; no network or router access."""
from pathlib import Path
import hashlib,json,os,shutil,subprocess,tempfile,unittest

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
 def test_candidate_transition_retains_exact_component_evidence(self):
  records=json.loads((ROOT/'runtime/app/share/xray-compatibility.json').read_bytes())['records']
  proof=next(r for r in records if r['candidateId']=='3.2.0-r01c23' and r['xrayTag']=='v26.9.9')['testedSourceSha256']
  result=self.resolve(candidate='3.2.0-r01c999999',components=proof)
  self.assertEqual(result['status'],'compatible',result)
  self.assertEqual(result['testedSourceSha256'],proof)
  self.assertNotEqual(result['candidateId'],'3.2.0-r01c999999','retain evidence provenance')
 def test_registry_requires_exact_installed_manifest(self):
  with tempfile.TemporaryDirectory(prefix='xray-registry-') as tmp:
   home=Path(tmp);(home/'share').mkdir();(home/'current').mkdir();(home/'lib').mkdir()
   shutil.copyfile(ROOT/'runtime/app/lib/xray-releases.jq',home/'lib/xray-releases.jq')
   body=(ROOT/'runtime/app/share/xray-compatibility.json').read_bytes()
   registry=home/'share/xray-compatibility.json';registry.write_bytes(body)
   manifest=home/'current/SHA256SUMS'
   manifest.write_text(hashlib.sha256(body).hexdigest()+'  app/share/xray-compatibility.json\n')
   def load():
    r=subprocess.run(['/bin/ash','-c','. "$1"; broray_xray_registry','test',str(ROOT/'runtime/app/lib/xray-releases.sh')],env={**os.environ,'BRORAY_BASE':str(home)},capture_output=True,text=True,timeout=5)
    self.assertEqual(r.returncode,0,r.stderr);return json.loads(r.stdout)
   self.assertTrue(any(r['candidateId']=='3.2.0-r01c19' and r['status']=='compatible' for r in load()))
   registry.write_bytes(body+b'\n');self.assertEqual(load(),[],'unverified live registry must not supply compatibility')
   registry.write_bytes(body);manifest.unlink();self.assertEqual(load(),[])
   manifest.write_text(hashlib.sha256(body).hexdigest()+'  app/share/xray-compatibility.json\n')
   self.assertTrue(load())
 def component_identity(self):
  return {n:hashlib.sha256((ROOT/'runtime/app'/n).read_bytes()).hexdigest() for n in ['lib/server-config-generator.sh','lib/xray.sh','lib/parser-vless.sh']}
 def resolve(self,candidate='3.2.0-r01c19',architecture='arm64',digest='3e38d72dfc5eb65c91df0e5583e9b6676c32232041da47de6ae73946b526d66c',tag='v26.9.9',components='current',records=None):
  registry=json.loads((ROOT/'runtime/app/share/xray-compatibility.json').read_bytes())
  release=dict(tag_name=tag,assets=[dict(digest='sha256:'+digest)])
  context=dict(candidateId=candidate,architecture=architecture)
  if components=='current':components=self.component_identity()
  if components is not None:context['sourceSha256']=components
  r=subprocess.run(['jq','-nc','-L',str(ROOT/'runtime/app/lib'),'--argjson','records',json.dumps(registry['records'] if records is None else records),'--argjson','context',json.dumps(context),'--argjson','release',json.dumps(release),'include "xray-releases"; $release|compatibility($records;$context)'],capture_output=True,text=True,timeout=5)
  self.assertEqual(r.returncode,0,r.stderr);return json.loads(r.stdout)
 def test_current_candidate_exposes_proven_xray_result(self):
  record=self.resolve();self.assertEqual(record['status'],'compatible',record)
  self.assertEqual(record['candidateId'],'3.2.0-r01c19')
  self.assertIn('CP06-XRAY-CURRENT-PROFILE',record['evidence'])
 def test_other_candidate_architecture_or_archive_remains_untested(self):
  for changed in [dict(candidate='3.2.0-r01c999999',components=None),dict(architecture='amd64'),dict(digest='0'*64),dict(tag='v26.9.7')]:
   with self.subTest(changed=changed):self.assertEqual(self.resolve(**changed)['status'],'untested')
 def test_priority_candidate_retains_exact_compatibility_scope(self):
  records=json.loads((ROOT/'runtime/app/share/xray-compatibility.json').read_bytes())['records']
  for candidate in ['3.2.0-r01c20','3.2.0-r01c21','3.2.0-r01c22','3.2.0-r01c23']:
   current=[r for r in records if r['candidateId']==candidate]
   self.assertEqual(len(current),7)
   for row in current:
    with self.subTest(tag=row['xrayTag']):
     old=next(r for r in records if r['candidateId']=='3.2.0-r01c19' and r['xrayTag']==row['xrayTag'])
     self.assertEqual(row['status'],old['status'])
     self.assertEqual(row.get('configurationGate'),old.get('configurationGate'))
     self.assertEqual(row['testedSourceSha256'],old['testedSourceSha256'])
     for rel,digest in row['testedSourceSha256'].items():
      self.assertEqual(hashlib.sha256((ROOT/'runtime/app'/rel).read_bytes()).hexdigest(),digest)
     result=self.resolve(candidate=candidate,tag=row['xrayTag'],digest=row['archiveSha256'])
     self.assertEqual(result['status'],row['status'])
 def test_historical_evidence_remains_available(self):
  self.assertEqual(self.resolve(candidate='3.1.1-r12c01')['status'],'compatible')
 def test_component_drift_invalidates_even_same_candidate(self):
  for path in self.component_identity():
   changed=self.component_identity();changed[path]='0'*64
   for candidate in ['3.2.0-r01c19','3.2.0-r01c999999']:
    with self.subTest(path=path,candidate=candidate):self.assertEqual(self.resolve(candidate=candidate,components=changed)['status'],'untested')
 def test_incomplete_or_invalid_component_proof_is_not_accepted(self):
  proof=self.component_identity()
  invalid=[None,{},[],{**proof,'lib/unknown.sh':'a'*64}]
  for path in proof:
   invalid += [{k:v for k,v in proof.items() if k!=path},{**proof,path:'z'*64},{**proof,path:None}]
  for value in invalid:
   with self.subTest(value=value):self.assertEqual(self.resolve(components=value)['status'],'untested')
  row=next(r for r in json.loads((ROOT/'runtime/app/share/xray-compatibility.json').read_bytes())['records'] if r['candidateId']=='3.2.0-r01c19' and r['xrayTag']=='v26.9.9')
  for value in invalid:
   with self.subTest(record=value):self.assertEqual(self.resolve(records=[{**row,'testedSourceSha256':value}])['status'],'untested')
 def test_negative_evidence_and_scope_survive_candidate_transition(self):
  records=json.loads((ROOT/'runtime/app/share/xray-compatibility.json').read_bytes())['records']
  for row in [r for r in records if r['candidateId']=='3.2.0-r01c23']:
   result=self.resolve(candidate='3.2.0-r01c999999',tag=row['xrayTag'],digest=row['archiveSha256'])
   self.assertEqual(result['status'],row['status'])
   self.assertEqual(result.get('configurationGate'),row.get('configurationGate'))
   self.assertEqual(result['testedAt'],row['testedAt'])
 def test_legacy_without_hashes_cannot_prove_other_candidate(self):
  records=[r for r in json.loads((ROOT/'runtime/app/share/xray-compatibility.json').read_bytes())['records'] if 'testedSourceSha256' not in r]
  self.assertTrue(records)
  self.assertEqual(self.resolve(candidate='3.2.0-r01c999999',records=records)['status'],'untested')
 def test_generator_only_proof_requires_rejection_evidence(self):
  row=next(r for r in json.loads((ROOT/'runtime/app/share/xray-compatibility.json').read_bytes())['records'] if r['candidateId']=='3.2.0-r01c23' and r['xrayTag']=='v26.9.8')
  for update in [dict(status='compatible'),dict(configurationGate={}),dict(configurationGate={**row['configurationGate'],'failed':0}),dict(testedSourceSha256={'lib/server-config-generator.sh':'0'*64})]:
   result=self.resolve(candidate='3.2.0-r01c999999',tag=row['xrayTag'],digest=row['archiveSha256'],records=[{**row,**update}])
   self.assertEqual(result['status'],'untested')
 def test_selected_admission_uses_same_component_evidence_as_catalog(self):
  with tempfile.TemporaryDirectory(prefix='xray-admission-') as tmp:
   home=Path(tmp);(home/'lib').mkdir();(home/'share/release').mkdir(parents=True);(home/'current').mkdir();(home/'bin').mkdir()
   for path in [*self.component_identity(),'lib/xray-releases.jq','share/xray-compatibility.json']:shutil.copyfile(ROOT/'runtime/app'/path,home/path)
   registry=home/'share/xray-compatibility.json';(home/'current/SHA256SUMS').write_text(hashlib.sha256(registry.read_bytes()).hexdigest()+'  app/share/xray-compatibility.json\n')
   (home/'share/release/manifest.json').write_text('{"candidateId":"3.2.0-r01c999999","version":"3.2.0"}')
   uname=home/'bin/uname';uname.write_text('#!/bin/sh\necho aarch64\n');uname.chmod(0o755)
   digest='3e38d72dfc5eb65c91df0e5583e9b6676c32232041da47de6ae73946b526d66c'
   (home/'official.json').write_text(json.dumps(dict(tag_name='v26.9.9',prerelease=False,assets=[dict(digest='sha256:'+digest),{}])))
   request=home/'request.json';request.write_text(json.dumps(dict(tag='v26.9.9',currentVersion='26.9.9',archiveSha256=digest,allowUntested=False,allowPrerelease=False,allowDowngrade=False)))
   script='''. "$1"
broray_xray_version_number() { echo 26.9.9; }
broray_xray_version_key() { echo 260909; }
broray_xray_release_resolve() { cp "$BRORAY_BASE/official.json" "$2"; }
broray_xray_update_error() { echo "$*" >&2; }
broray_xray_selected_check "$BRORAY_BASE/request.json"
'''
   def run():return subprocess.run(['/bin/ash','-c',script,'test',str(ROOT/'runtime/app/lib/xray-releases.sh')],env={**os.environ,'PATH':str(home/'bin')+':/usr/bin:/bin','BRORAY_BASE':str(home),'BRORAY_XRAY_UPDATE_WORK':str(home)},capture_output=True,text=True,timeout=8)
   result=run();self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(json.loads(result.stdout)['compatibility']['status'],'compatible')
   path=home/'lib/parser-vless.sh';path.write_bytes(path.read_bytes()+b'\n');result=run()
   self.assertNotEqual(result.returncode,0,'drift must require fresh untested consent');self.assertIn('Подтвердите',result.stderr)
 def test_live_context_hashes_actual_files_and_rejects_missing_or_symlink(self):
  with tempfile.TemporaryDirectory(prefix='xray-context-') as tmp:
   home=Path(tmp);(home/'share/release').mkdir(parents=True);(home/'lib').mkdir()
   (home/'share/release/manifest.json').write_text(json.dumps(dict(candidateId='3.2.0-r01c999999',version='3.2.0')))
   for path in self.component_identity():shutil.copyfile(ROOT/'runtime/app'/path,home/path)
   def load():
    r=subprocess.run(['/bin/ash','-c','. "$1"; broray_xray_context','test',str(ROOT/'runtime/app/lib/xray-releases.sh')],env={**os.environ,'BRORAY_BASE':str(home)},capture_output=True,text=True,timeout=5)
    self.assertEqual(r.returncode,0,r.stderr);return json.loads(r.stdout)['sourceSha256']
   self.assertEqual(load(),self.component_identity())
   path=home/'lib/parser-vless.sh';path.write_bytes(path.read_bytes()+b'\n');changed=load()
   self.assertEqual(changed['lib/parser-vless.sh'],hashlib.sha256(path.read_bytes()).hexdigest())
   self.assertEqual(self.resolve(components=changed)['status'],'untested')
   path.unlink();self.assertEqual(load(),{})
   path.symlink_to(ROOT/'runtime/app/lib/parser-vless.sh');self.assertEqual(load(),{})
 def test_older_cores_expose_proven_configuration_failures(self):
  records=json.loads((ROOT/'runtime/app/share/xray-compatibility.json').read_bytes())['records']
  expected={'v26.9.8':2,'v26.7.28':2,'v26.7.11':2,'v26.6.27':2,'v26.3.27':4,'v26.2.6':10}
  for tag,failed in expected.items():
   with self.subTest(tag=tag):
    record=next(r for r in records if r['candidateId']=='3.2.0-r01c19' and r['xrayTag']==tag)
    result=self.resolve(tag=tag,digest=record['archiveSha256'])
    self.assertEqual(result['status'],'incompatible');self.assertEqual(result['configurationGate']['failed'],failed)
    self.assertEqual(len(result['configurationGate']['rejectedConfigurationSha256']),failed)
    self.assertEqual(self.resolve(tag=tag,digest='1'*64)['status'],'untested')

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
