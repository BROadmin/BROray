"""Actual DoT transaction/CLI/fence; only Keenetic I/O and certification are fixtures."""
import copy, json, os, shutil, subprocess, tempfile, time, unittest
from pathlib import Path
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
HELPER=r'''import json,os,sys,time
from pathlib import Path
root=Path(os.environ['BRORAY_ROOT']); obs=root/'observed.json'; cfg=root/'routes/dot/config.json'
data=json.loads(obs.read_text()); action=sys.argv[1]
if action=='status':
 ids=json.loads(cfg.read_text())['selectedIds']
 print(json.dumps({'deleteEligible':bool(ids),'selectedIds':ids,'servers':[], 'actual':data}))
else:
 command=sys.argv[2]
 with (root/'commands').open('a') as f:f.write(command+'\n')
 if (root/'hold').exists():
  (root/'ready').touch(); deadline=time.monotonic()+15
  while (root/'hold').exists():
   if time.monotonic()>deadline:raise SystemExit(2)
   time.sleep(.02)
 words=command.split()
 if command=='system configuration save':
  flag=root/'fail-save-once'
  if flag.exists():flag.unlink();raise SystemExit(1)
 elif words[:4]==['no','dns-proxy','tls','upstream'] and len(words)==6:
  data['dot']=[e for e in data['dot'] if not(e['address']==words[4] and e['effectivePort']==int(words[5]))]
 elif words[:3]==['dns-proxy','tls','upstream']:
  original=json.loads((root/'original-observed.json').read_text())
  data['dot'] += [e for e in original['dot'] if e['address']==words[3] and e['effectivePort']==int(words[4])]
 else:raise SystemExit(3)
 data['totalSecure']=len(data['dot'])+data['dohCount'];obs.write_text(json.dumps(data))
'''
class DeleteIntegration(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='dot-binding-integration-');self.addCleanup(self.tmp.cleanup)
  self.app=Path(self.tmp.name)/'app'
  for d in ['lib','bin','tmp','routes/dot','run','locks','updater','operations']:(self.app/d).mkdir(parents=True,exist_ok=True)
  for name in ['routes-dot.sh','routes-api-operation.sh']:
   shutil.copyfile(ROOT/'runtime/app/lib'/name,self.app/'lib'/name)
  self.cli=self.app/'bin/broray-routes-dot';shutil.copyfile(ROOT/'runtime/app/bin/broray-routes-dot',self.cli)
  self.config=self.app/'routes/dot/config.json';self.state=self.app/'routes/dot/state.json'
  self.set_ids(['google-primary','cloudflare-primary'])
  self.state.write_text(json.dumps({'schemaVersion':1,'tests':[]}))
  self.entries=[self.entry('google-primary','8.8.8.8','dns.google'),self.entry('cloudflare-primary','1.1.1.1','cloudflare-dns.com'),self.entry('foreign','192.0.2.53','foreign.invalid')]
  self.observed=self.app/'observed.json';self.put_observed(self.entries)
  (self.app/'original-observed.json').write_bytes(self.observed.read_bytes())
  self.helper=self.app/'fixture.py';self.helper.write_text(HELPER)
  self.shim=self.app/'lib/fixture.sh';self.shim.write_text('''. "$BRORAY_ROOT/lib/routes-dot.sh"
broray_dot_require_write_protocol() { return 0; }
broray_dot_fetch_observed() { cp "$BRORAY_ROOT/observed.json" "$1"; }
broray_dot_status() { python3 "$BRORAY_ROOT/fixture.py" status; }
broray_dot_command() { python3 "$BRORAY_ROOT/fixture.py" command "$1"; }
''')
  self.env={**os.environ,'BRORAY_ROOT':str(self.app),'BRORAY_DOT_LIB':str(self.shim),'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','BRORAY_ROUTES_API_LOCK':str(self.app/'locks/global'),'BRORAY_UPDATER_REQUEST_LOCK':str(self.app/'updater/request.lock'),'BRORAY_UPDATER_OPERATION_POINTER':str(self.app/'operations/pointer'),'BRORAY_UPDATER_OPERATION_ROOT':str(self.app/'operations'),'BRORAY_LEGACY_GLOBAL_LOCK':str(self.app/'locks/legacy')}
 def entry(self,id,address,sni):
  return dict(id=id,address=address,effectivePort=853,portRaw='853',portState='explicit',sni=sni,spki='',interface='',on='',domain='',valid=True,unknownTokenCount=0,deleteEligible=id!='foreign',catalogMatchIds=[] if id=='foreign' else [id])
 def set_ids(self,ids):
  self.config.write_text(json.dumps(dict(schemaVersion=3,requestedIds=ids,selectedIds=ids,effectiveIds=[],managed=[],quarantinedReceipts=[],migrationState='none',updatedAt=None)))
 def put_observed(self,entries):
  self.observed.write_text(json.dumps(dict(determinate=True,runtimeReconciled=True,dot=entries,dohCount=1,totalSecure=len(entries)+1)))
 def call(self,*args,env=None):
  return subprocess.run(['/bin/ash',str(self.cli),*map(str,args)],env=env or self.env,capture_output=True,text=True,timeout=40)
 def preview(self):
  p=self.call('delete-preview');self.assertEqual(p.returncode,0,p.stderr);return json.loads(p.stdout)
 def request(self,preview=None):
  data=preview or self.preview();p=self.app/'request.json'
  p.write_text(json.dumps({k:data[k] for k in ['schemaVersion','serverIds','expectedFingerprint']}));return p
 def snapshot(self):
  return self.config.read_bytes(),self.state.read_bytes(),self.observed.read_bytes()
 def assert_unchanged(self,before):
  self.assertEqual(self.snapshot(),before);self.assertFalse((self.app/'commands').exists());self.assertFalse((self.app/'locks/global').exists())
 def test_happy_path_deletes_exact_selected_preserves_foreign_and_doh(self):
  p=self.call('delete',self.request());self.assertEqual(p.returncode,0,p.stderr)
  result=json.loads(self.observed.read_text());self.assertEqual(result['dot'],[self.entries[2]]);self.assertEqual(result['dohCount'],1)
  self.assertEqual(json.loads(self.config.read_text())['selectedIds'],[])
  self.assertEqual((self.app/'commands').read_text().splitlines(),['no dns-proxy tls upstream 1.1.1.1 853','no dns-proxy tls upstream 8.8.8.8 853','system configuration save'])
  self.assertFalse((self.app/'locks/global').exists())
 def test_selection_changed_between_preview_and_delete_is_read_only(self):
  request=self.request();self.set_ids(['cloudflare-primary']);before=self.snapshot()
  p=self.call('delete',request);self.assertNotEqual(p.returncode,0);self.assertIn('DOT_DELETE_CONFIRMATION_STALE',p.stderr);self.assert_unchanged(before)
 def test_fresh_preview_of_changed_selection_succeeds(self):
  self.preview();self.set_ids(['cloudflare-primary']);p=self.call('delete',self.request())
  self.assertEqual(p.returncode,0,p.stderr);self.assertEqual([x['id'] for x in json.loads(self.observed.read_text())['dot']],['google-primary','foreign'])
 def test_replay_after_success_does_not_delete_other_records(self):
  request=self.request();first=self.call('delete',request);self.assertEqual(first.returncode,0,first.stderr)
  before=self.snapshot();calls=(self.app/'commands').read_bytes();second=self.call('delete',request)
  self.assertNotEqual(second.returncode,0);self.assertIn('DOT_DELETE_CONFIRMATION_STALE',second.stderr)
  self.assertEqual(self.snapshot(),before);self.assertEqual((self.app/'commands').read_bytes(),calls)
 def test_reordering_does_not_change_fingerprint(self):
  a=self.preview();self.set_ids(['cloudflare-primary','google-primary']);self.put_observed(list(reversed(self.entries)))
  b=self.preview();self.assertEqual(a,b);p=self.call('delete',self.request(a));self.assertEqual(p.returncode,0,p.stderr)
 def test_every_selector_field_change_rejects_old_confirmation(self):
  request=self.request()
  for key,value in [('address','192.0.2.54'),('effectivePort',8853),('sni','changed.invalid'),('spki','changed'),('interface','Proxy2'),('domain','changed.invalid')]:
   with self.subTest(field=key):
    entries=copy.deepcopy(self.entries);entries[0][key]=value;self.put_observed(entries);before=self.snapshot()
    p=self.call('delete',request);self.assertNotEqual(p.returncode,0);self.assert_unchanged(before)
 def test_partial_or_duplicate_preview_is_refused(self):
  for entries in [[self.entries[0],self.entries[2]],self.entries+[self.entries[0]]]:
   with self.subTest(count=len(entries)):
    self.put_observed(entries);before=self.snapshot();p=self.call('delete-preview')
    self.assertNotEqual(p.returncode,0);self.assert_unchanged(before)
 def test_ambiguous_live_selector_refuses_mutation(self):
  request=self.request();other=copy.deepcopy(self.entries[0]);other['sni']='other.invalid';other['deleteEligible']=False
  self.put_observed(self.entries+[other]);before=self.snapshot();p=self.call('delete',request)
  self.assertNotEqual(p.returncode,0);self.assertIn('DOT_SELECTOR_CONFLICT',p.stderr);self.assert_unchanged(before)
 def test_malformed_requests_do_not_mutate(self):
  valid=self.preview()
  values=[{}, {'schemaVersion':1,'serverIds':valid['serverIds']}, {**valid,'expectedFingerprint':'z'*64}, {**valid,'expectedFingerprint':'a'*63},{**valid,'expectedFingerprint':'A'*64},{**valid,'serverIds':[]},{**valid,'serverIds':['google-primary','google-primary']},{**valid,'serverIds':['unknown']},{**valid,'schemaVersion':2}]
  path=self.app/'bad.json'
  for value in values:
   with self.subTest(value=value):
    path.write_text(json.dumps(value));before=self.snapshot();p=self.call('delete',path)
    self.assertNotEqual(p.returncode,0);self.assert_unchanged(before)
  path.write_text(json.dumps(valid)+'\n'+json.dumps(valid));before=self.snapshot()
  self.assertNotEqual(self.call('delete',path).returncode,0);self.assert_unchanged(before)
 def test_missing_argument_and_symlink_are_refused(self):
  before=self.snapshot();p=self.call('delete');self.assertNotEqual(p.returncode,0);self.assertIn('REQUEST_INVALID',p.stderr);self.assert_unchanged(before)
  request=self.request();link=self.app/'link.json';link.symlink_to(request)
  before=self.snapshot();self.assertNotEqual(self.call('delete',link).returncode,0);self.assert_unchanged(before)
 def test_failed_save_rolls_back_actual_transaction(self):
  request=self.request();before=self.snapshot();(self.app/'fail-save-once').touch()
  p=self.call('delete',request);self.assertNotEqual(p.returncode,0);self.assertIn('DOT_DELETE_FAILED',p.stderr)
  self.assertEqual(self.config.read_bytes(),before[0]);self.assertEqual(self.state.read_bytes(),before[1])
  actual=json.loads(self.observed.read_text());expected=json.loads(before[2])
  self.assertEqual(sorted(actual['dot'],key=lambda e:e['id']),sorted(expected['dot'],key=lambda e:e['id']))
  self.assertEqual(actual['dohCount'],expected['dohCount']);self.assertEqual(actual['totalSecure'],expected['totalSecure'])
  self.assertFalse((self.app/'locks/global').exists())
 def test_two_concurrent_deletes_cannot_overlap(self):
  request=self.request();(self.app/'hold').touch()
  p=subprocess.Popen(['/bin/ash',str(self.cli),'delete',str(request)],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
  try:
   deadline=time.monotonic()+10
   while not (self.app/'ready').exists():
    if p.poll() is not None or time.monotonic()>deadline:self.fail('first delete did not reach guarded command')
    time.sleep(.02)
   self.assertTrue((self.app/'locks/global').is_dir())
   second=self.call('delete',request);self.assertNotEqual(second.returncode,0);self.assertIn('ROUTES_OPERATION_BUSY',second.stderr)
   (self.app/'hold').unlink();out,err=p.communicate(timeout=30);self.assertEqual(p.returncode,0,(out,err))
   self.assertEqual(len((self.app/'commands').read_text().splitlines()),3)
  finally:
   (self.app/'hold').unlink(missing_ok=True)
   if p.poll() is None:p.terminate()
   p.communicate(timeout=20)
 def test_inherited_parent_fence_is_kept_until_cgi_releases_it(self):
  request=self.request();script='. "$BRORAY_ROOT/lib/routes-api-operation.sh"; broray_routes_api_lock_acquire dot:delete dns-over-tls || exit 7; trap broray_routes_api_lock_release EXIT; BRORAY_DOT_PARENT_LOCK_PID=$$ /bin/ash "$1" delete "$2" || exit 8; [ -d "$BRORAY_ROUTES_API_LOCK" ] || exit 9'
  p=subprocess.run(['/bin/ash','-c',script,'test',str(self.cli),str(request)],env=self.env,capture_output=True,text=True,timeout=40)
  self.assertEqual(p.returncode,0,p.stderr);self.assertFalse((self.app/'locks/global').exists())
if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(DeleteIntegration))
 raise SystemExit(not result.wasSuccessful())
