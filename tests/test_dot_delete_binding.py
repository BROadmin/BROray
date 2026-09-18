"""Focused fail-closed tests for DoT delete confirmation binding. No router/network."""
import json, os, shutil, subprocess, tempfile, unittest
from pathlib import Path

ROOT=Path(os.environ.get("BRORAY_TEST_ROOT",Path(__file__).resolve().parents[1]))

class DeleteBinding(unittest.TestCase):
 def setUp(self):
  self.t=tempfile.TemporaryDirectory(prefix="broray-dot-delete-binding-");self.addCleanup(self.t.cleanup)
  self.app=Path(self.t.name)/"app";(self.app/"lib").mkdir(parents=True);(self.app/"routes/dot").mkdir(parents=True);(self.app/"tmp").mkdir()
  shutil.copyfile(ROOT/"runtime/app/lib/routes-dot.sh",self.app/"lib/routes-dot.sh")
  self.config=self.app/"routes/dot/config.json";self.state=self.app/"routes/dot/state.json"
  self.config.write_text(json.dumps({"schemaVersion":3,"requestedIds":["google-primary"],"selectedIds":["google-primary"]}),encoding="utf-8")
  self.state.write_text(json.dumps({"schemaVersion":1,"tests":[]}),encoding="utf-8")
  self.selected=self.app/"tmp/selected.json";self.observed=self.app/"tmp/observed.json"
  self.selected.write_text(json.dumps([self.endpoint("8.8.8.8","dns.google")]),encoding="utf-8")
  self.write_observed("8.8.8.8","dns.google")
  self.env={**os.environ,"BRORAY_ROOT":str(self.app),"PATH":"/usr/bin:/bin:/usr/sbin:/sbin"}

 def endpoint(self,address,sni):
  return {"id":"google-primary","address":address,"effectivePort":853,"sni":sni,"spki":"","interface":"","domain":""}
 def write_observed(self,address,sni):
  entry={**self.endpoint(address,sni),"valid":True,"unknownTokenCount":0,"deleteEligible":True,"catalogMatchIds":["google-primary"]}
  self.observed.write_text(json.dumps({"determinate":True,"runtimeReconciled":True,"dot":[entry],"dohCount":0,"totalSecure":1}),encoding="utf-8")

 def fixture_status(self,address="8.8.8.8",sni="dns.google"):
  live={**self.endpoint(address,sni),"valid":True,"unknownTokenCount":0,"deleteEligible":True,"catalogMatchIds":["google-primary"]}
  server={**self.endpoint(address,sni),"present":True}
  return {"deleteEligible":True,"selectedIds":["google-primary"],"servers":[server],"actual":{"dot":[live]}}

 def shell(self,body,*args):
  prefix='''set -u
. "$BRORAY_ROOT/lib/routes-dot.sh"
broray_dot_transaction_require_clear() { return 0; }
broray_dot_require_write_protocol() { return 0; }
broray_dot_ensure_files() { return 0; }
broray_dot_validate_request() { return 0; }
broray_dot_entries_for_request() { cp "$BRORAY_TEST_SELECTED" "$2"; }
broray_dot_fetch_observed() { cp "$BRORAY_TEST_OBSERVED" "$1"; }
broray_dot_transaction_arm() { printf "armed\\n" >"$BRORAY_ROOT/tmp/armed"; return 1; }
broray_dot_command() { printf "mutation\\n" >>"$BRORAY_ROOT/tmp/mutations"; return 0; }
'''
  env={**self.env,"BRORAY_TEST_SELECTED":str(self.selected),"BRORAY_TEST_OBSERVED":str(self.observed)}
  return subprocess.run(["/bin/ash","-c",prefix+body,"qa",*map(str,args)],env=env,capture_output=True,text=True,timeout=20)

 def request(self,fingerprint,ids=None):
  p=self.app/"tmp/request.json";p.write_text(json.dumps({"schemaVersion":1,"serverIds":ids or ["google-primary"],"expectedFingerprint":fingerprint}),encoding="utf-8");return p

 def assert_no_mutation(self):
  self.assertFalse((self.app/"tmp/armed").exists())
  self.assertFalse((self.app/"tmp/mutations").exists())
 def test_preview_fingerprint_is_bound_to_exact_entries(self):
  status=self.app/"tmp/status.json";status.write_text(json.dumps(self.fixture_status()),encoding="utf-8")
  code='broray_dot_status() { cat "$BRORAY_PREVIEW_FIXTURE"; }; broray_dot_delete_preview'
  env={**self.env,"BRORAY_PREVIEW_FIXTURE":str(status),"BRORAY_TEST_SELECTED":str(self.selected),"BRORAY_TEST_OBSERVED":str(self.observed)}
  prefix='. "$BRORAY_ROOT/lib/routes-dot.sh"; broray_dot_transaction_require_clear(){ return 0; }; broray_dot_require_write_protocol(){ return 0; }; broray_dot_ensure_files(){ return 0; }; '
  p=subprocess.run(["/bin/ash","-c",prefix+code],env=env,capture_output=True,text=True,timeout=20)
  self.assertEqual(p.returncode,0,p.stderr)
  preview=json.loads(p.stdout);self.assertEqual(preview["schemaVersion"],1);self.assertEqual(preview["serverIds"],["google-primary"])
  self.assertEqual(preview["entries"],[self.endpoint("8.8.8.8","dns.google")]);self.assertEqual(len(preview["expectedFingerprint"]),64)

 def test_changed_selection_rejected_before_transaction(self):
  req=self.request("0"*64,["cloudflare-primary"])
  p=self.shell('broray_dot_delete "$1"',req)
  self.assertNotEqual(p.returncode,0);self.assertIn("DOT_DELETE_CONFIRMATION_STALE",p.stderr);self.assert_no_mutation()

 def preview_token(self):
  status=self.app/"tmp/status.json";status.write_text(json.dumps(self.fixture_status()),encoding="utf-8")
  env={**self.env,"BRORAY_PREVIEW_FIXTURE":str(status),"BRORAY_TEST_SELECTED":str(self.selected),"BRORAY_TEST_OBSERVED":str(self.observed)}
  preview_script='. "$BRORAY_ROOT/lib/routes-dot.sh"; broray_dot_transaction_require_clear(){ return 0; }; broray_dot_require_write_protocol(){ return 0; }; broray_dot_ensure_files(){ return 0; }; broray_dot_status(){ cat "$BRORAY_PREVIEW_FIXTURE"; }; broray_dot_delete_preview'
  preview=subprocess.run(["/bin/ash","-c",preview_script],env=env,capture_output=True,text=True,timeout=20)
  self.assertEqual(preview.returncode,0,preview.stderr)
  return json.loads(preview.stdout)

 def test_live_semantic_change_rejected_before_transaction(self):
  token=self.preview_token()
  self.write_observed("8.8.8.8","changed.invalid")
  req=self.request(token["expectedFingerprint"])
  p=self.shell('broray_dot_delete "$1"',req)
  self.assertNotEqual(p.returncode,0);self.assertIn("DOT_DELETE_CONFIRMATION_STALE",p.stderr);self.assert_no_mutation()

 def test_matching_preview_reaches_transaction_boundary_only(self):
  token=self.preview_token()
  req=self.request(token["expectedFingerprint"])
  p=self.shell('broray_dot_delete "$1"',req)
  self.assertNotEqual(p.returncode,0)
  self.assertTrue((self.app/"tmp/armed").exists(),"matching token did not reach transaction arm")
  self.assertFalse((self.app/"tmp/mutations").exists(),"test arm failure must precede mutation")

 def test_missing_fingerprint_rejected(self):
  req=self.app/"tmp/request.json";req.write_text(json.dumps({"schemaVersion":1,"serverIds":["google-primary"]}),encoding="utf-8")
  p=self.shell('broray_dot_delete "$1"',req)
  self.assertNotEqual(p.returncode,0);self.assertIn("REQUEST_INVALID",p.stderr);self.assert_no_mutation()

 def test_changed_shell_files_parse_with_busybox_ash(self):
  files=[
   ROOT/"runtime/app/lib/routes-dot.sh",
   ROOT/"runtime/app/bin/broray-routes-dot",
   ROOT/"runtime/app/web-new/api/routes/dot-common.sh",
   ROOT/"runtime/app/web-new/api/routes/dot-delete.cgi",
   ROOT/"runtime/app/web-new/api/routes/dot-delete-preview.cgi"]
  for path in files:
   p=subprocess.run(["/bin/ash","-n",str(path)],capture_output=True,text=True,timeout=10)
   self.assertEqual(p.returncode,0,f"{path}: {p.stderr}")

if __name__=="__main__":
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(DeleteBinding))
 print("DOT_DELETE_BINDING_REPORT="+json.dumps({"testsRun":result.testsRun,"failures":len(result.failures),"errors":len(result.errors),"routerAccessed":False,"networkEnabled":False}))
 raise SystemExit(not result.wasSuccessful())
