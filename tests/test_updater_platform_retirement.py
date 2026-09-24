"""The retained terminal-domain verifier refuses live or corrupt evidence.

This is a read-only prerequisite for replacement, not permission to install.
This fixture uses one validated native build. Cross-native replacement needs
separate acceptance and is not inferred from this component test.
"""
import hashlib,json,subprocess,unittest
from pathlib import Path
from test_updater_preflight import PreflightAfterBoot
from test_installed_generation_stop import InstalledGenerationStop

class PreviousPlatformRetirement(PreflightAfterBoot):
 fixture_type=InstalledGenerationStop
 def test_live_refused_and_exact_stopped_domain_verified(self):
  first=self.completed(self.bootstrap(self.slot,self.slot_sha))
  probe=Path('/work/.local/bin/replacement-generation')
  self.assertTrue(probe.is_file())
  updater=self.parent_fixture.updater
  host=updater/'hosts'/first['generationId']/'host.record'
  rows=host.read_text().splitlines()
  self.assertEqual(rows[0],'BROray-independent-app-service/1')
  self.assertEqual(rows[1],str(host.parent))
  self.assertEqual(rows[2],str(updater/'generations'/first['generationId']))
  self.assertEqual(rows[3],first['generationId'])
  self.assertEqual(rows[4],self.sha)
  self.assertEqual(rows[5],str(self.root))
  host_sha=hashlib.sha256(host.read_bytes()).hexdigest()
  def check():
   return subprocess.run([str(probe),'service-retired',*rows[1:8],host_sha],
                         capture_output=True,text=True,timeout=15)
  before=self.snapshot();r=check()
  self.assertNotEqual(r.returncode,0,'live generation must not authorize replacement')
  self.assertEqual(self.snapshot(),before)
  status=self.parent_fixture.init('status');self.assertEqual(status.returncode,0,status.stdout+status.stderr)
  self.assertEqual(json.loads(status.stdout)['generationId'],first['generationId'])
  r=self.parent_fixture.init('stop');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  # Socket paths carry no bytes; all evidence and platform files are compared.
  saved={str(p):p.read_bytes() for p in updater.rglob('*') if p.is_file()}
  r=check();print('REPLACEMENT_RETIREMENT_PROBE '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  proof=json.loads(r.stdout)
  self.assertTrue(proof['ok']);self.assertTrue(proof['hostRetired'])
  self.assertEqual(proof['hostRecordSha256'],host_sha)
  self.assertEqual(proof['generationId'],first['generationId'])
  self.assertFalse(proof['signalsAuthorized']);self.assertFalse(proof['appActionsAuthorized'])
  self.assertEqual([p.name for p in (updater/'generations').glob('g-*')],[first['generationId']])
  self.assertEqual(saved,{str(p):p.read_bytes() for p in updater.rglob('*') if p.is_file()})
  self.assertEqual(self.snapshot(),before)
  ledger=updater/'generations'/first['generationId']/'state.json'
  original=ledger.read_bytes()
  try:
   ledger.write_bytes(b'{broken')
   self.assertNotEqual(check().returncode,0)
   self.assertEqual(ledger.read_bytes(),b'{broken')
  finally:ledger.write_bytes(original)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  PreviousPlatformRetirement('test_live_refused_and_exact_stopped_domain_verified')]))
 raise SystemExit(not result.wasSuccessful())
