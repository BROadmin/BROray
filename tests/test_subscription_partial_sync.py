"""Actual sync/filesystem in disposable Linux; owner and live Xray hooks are explicit doubles."""
import hashlib,json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
CORE_RESULTS=[]
def node(key, *, id=None, sub='test', password='uuid-old'):
 return {'schemaVersion':2,'id':id or 'subscription-'+sub+'-'+key,'name':'QA '+key,'uri':'vless://synthetic-'+key,'source':{'type':'subscription','subscriptionId':sub,'importKey':hashlib.sha256(key.encode()).hexdigest(),'nodeIndex':1,'updatedAt':'old'},'protocol':'vless','address':key+'.example.invalid','port':443,'uuid':password,'flow':None,'network':'raw','security':'tls'}
class Sync(unittest.TestCase):
 def setUp(self):
  self.temp=Path(tempfile.mkdtemp(prefix='broray-stage08-sync-'));self.app=self.temp/'app'
  shutil.copytree(ROOT/'runtime/app/lib',self.app/'lib')
  for d in ['servers','config/disabled-subscription-servers/test','tmp','run/server-quality','stage','config/system']: (self.app/d).mkdir(parents=True,exist_ok=True)
  self.env={**os.environ,'BRORAY_ROOT':str(self.app),'BRORAY_BASE':str(self.app),'BRORAY_SERVER_SUB_BASE':str(self.app),'PATH':'/usr/bin:/bin:/usr/sbin:/sbin'}
 def tearDown(self):
  assert self.temp.parent==Path('/tmp') and self.temp.name.startswith('broray-stage08-sync-');shutil.rmtree(self.temp)
 def store(self,n,where='servers'):
  f=self.app/where/(n['id']+'.json');f.parent.mkdir(parents=True,exist_ok=True);f.write_bytes((json.dumps(n,sort_keys=True)+'\n').encode());f.chmod(0o600);return f
 def catalog(self):return {p.relative_to(self.app).as_posix():p.read_bytes() for d in ['servers','config/disabled-subscription-servers','run/server-quality'] for p in (self.app/d).rglob('*.json')}
 def sync(self,policy='retain-unmatched',enabled=True,ok=True):
  script='''. "$BRORAY_ROOT/lib/server-subscription-service.sh"
broray_job_require_owner() { return 0; }
broray_job_checkpoint() { return 0; }
broray_xray_test_file() { printf test >> "$BRORAY_ROOT/unexpected-live-hook"; return 1; }
broray_xray_apply_server() { printf apply >> "$BRORAY_ROOT/unexpected-live-hook"; return 1; }
broray_interface_sync_description() { printf interface >> "$BRORAY_ROOT/unexpected-live-hook"; return 1; }
if [ "${QA_ACTIVE_ROTATION:-0}" = 1 ]; then
 broray_xray_test_file() {
  "$BRORAY_TEST_XRAY" run -test -config "$1" > "$BRORAY_ROOT/xray-config-test.log" 2>&1
  rc=$?
  jq -nc --argjson returncode "$rc" --arg configurationSha256 "$(sha256sum "$1" | awk '{print $1}')" '{returncode:$returncode,configurationSha256:$configurationSha256}' >> "$BRORAY_ROOT/xray-config-tests.jsonl"
  return "$rc"
 }
 broray_xray_apply_server() { printf '%s' "$1" > "$BRORAY_ROOT/applied-id"; return 0; }
fi
if [ "${QA_FAIL_COMMIT:-0}" = 1 ]; then
 cp() { case "$2" in "$BRORAY_ROOT/servers/".*.new.*)
   if [ "${QA_FAIL_SECOND:-0}" = 1 ] && [ ! -f "$BRORAY_ROOT/fault-first-written" ]; then
     : > "$BRORAY_ROOT/fault-first-written"; command cp "$@"; return $?
   fi
   return 1 ;; *)
   if [ "${QA_FAIL_RESTORE:-0}" = 1 ]; then
     case "$1" in */backup/live/*.json|*/backup/disabled/*.json) return 1 ;; esac
   fi
   command cp "$@" ;; esac; }
fi
BRORAY_ACTIVE_SERVER_FILE="$BRORAY_ROOT/active"
broray_server_subscription_sync test "$BRORAY_ROOT/stage" "$1" qa-update "$2"
'''
  p=subprocess.run(['/bin/ash','-c',script,'qa',str(enabled).lower(),policy],env=self.env,capture_output=True,timeout=30)
  core=self.app/'xray-config-tests.jsonl'
  if core.exists():
   CORE_RESULTS.extend(dict(json.loads(line),case=self.id(),output=(self.app/'xray-config-test.log').read_text()) for line in core.read_text().splitlines());core.unlink()
  if ok:self.assertEqual(p.returncode,0,p.stderr.decode(errors='replace')+((self.app/'xray-config-test.log').read_text() if (self.app/'xray-config-test.log').exists() else ''));return json.loads(p.stdout)
  self.assertNotEqual(p.returncode,0,p.stdout);self.assertEqual(p.stdout,b'');return p.stderr.decode(errors='replace')
 def test_unchanged_refresh_preserves_exact_server_and_queue_context(self):
  self.store(node('same'),'stage');self.sync('replace')
  live=next((self.app/'servers').glob('*.json'));before=live.read_bytes()
  def context():
   p=subprocess.run(['/bin/ash','-c','. "$BRORAY_ROOT/lib/server-service.sh"; broray_quality_context all USER'],env=self.env,capture_output=True,timeout=30)
   self.assertEqual(p.returncode,0,p.stderr);return p.stdout
  old_context=context();changed=json.loads(before);changed['source']['updatedAt']='new-observation'
  self.store(changed,'stage');r=self.sync('replace')
  self.assertEqual((r['unchanged'],r['updated']),(1,0))
  self.assertEqual(live.read_bytes(),before,'Unchanged subscription node must not invalidate an admitted quality snapshot')
  self.assertEqual(context(),old_context)
 def test_non_timestamp_metadata_change_is_still_applied(self):
  self.store(node('same'),'stage');self.sync('replace')
  live=next((self.app/'servers').glob('*.json'));changed=json.loads(live.read_bytes())
  changed['name']='Renamed provider node';changed['source']['nodeIndex']=7;changed['source']['updatedAt']='new-observation'
  self.store(changed,'stage');r=self.sync('replace')
  actual=json.loads(live.read_bytes());self.assertEqual(actual['name'],changed['name']);self.assertEqual(actual['source'],changed['source']);self.assertEqual(r['updated'],1)
 def test_regression_preserve_missing_in_partial(self):
  old=self.store(node('old'));before=old.read_bytes();self.store(node('new'),'stage');r=self.sync();self.assertTrue(old.exists());self.assertEqual(old.read_bytes(),before);self.assertEqual(r['retained'],1);self.assertEqual(r['removed'],0)
 def test_regression_keep_active_missing_in_partial(self):
  n=node('active');old=self.store(n);before=old.read_bytes();(self.app/'active').write_text(n['id']+'\n');self.store(node('new'),'stage');r=self.sync();self.assertEqual(old.read_bytes(),before);self.assertEqual(r['activeServerImpact'],'none');self.assertFalse((self.app/'unexpected-live-hook').exists())
 def test_regression_unknown_policy_fails_before_mutation(self):
  self.store(node('old'));self.store(node('new'),'stage');before=self.catalog();self.sync('invented',ok=False);self.assertEqual(self.catalog(),before)
 def test_complete_response_removes_missing(self):
  old=self.store(node('old'));quality=self.store({'id':old.stem},'run/server-quality');self.store(node('new'),'stage');r=self.sync('replace');self.assertFalse(old.exists());self.assertFalse(quality.exists());self.assertEqual(r['removed'],1);self.assertEqual(r['retained'],0)
 def test_partial_preserves_quality_and_foreign_sources(self):
  old=self.store(node('old'));self.store({'id':old.stem,'score':25},'run/server-quality');manual=node('manual');manual['source']={'type':'manual'};self.store(manual);self.store(node('other',sub='other'));before=self.catalog();self.store(node('new'),'stage');self.sync();self.assertTrue(all(self.catalog()[p]==b for p,b in before.items()))
 def test_partial_updates_matching_server(self):
  n=node('same');f=self.store(n);n['uuid']='uuid-new';self.store(n,'stage');self.store(node('old'));r=self.sync();self.assertEqual(json.loads(f.read_bytes())['uuid'],'uuid-new');self.assertEqual((r['updated'],r['retained'],r['accepted']),(1,1,1))
 def test_disabled_partial_stays_disabled(self):
  f=self.store(node('old'),'config/disabled-subscription-servers/test');b=f.read_bytes();self.store(node('new'),'stage');r=self.sync(enabled=False);self.assertEqual(f.read_bytes(),b);self.assertEqual(len(list((self.app/'servers').glob('*.json'))),0);self.assertEqual(r['retained'],1)
 def test_retained_disabled_to_enabled(self):
  f=self.store(node('old'),'config/disabled-subscription-servers/test');b=f.read_bytes();self.store(node('new'),'stage');self.sync();self.assertFalse(f.exists());self.assertEqual((self.app/'servers'/f.name).read_bytes(),b)
 def test_retained_enabled_to_disabled(self):
  f=self.store(node('old'));b=f.read_bytes();self.store(node('new'),'stage');self.sync(enabled=False);self.assertFalse(f.exists());self.assertEqual((self.app/'config/disabled-subscription-servers/test'/f.name).read_bytes(),b)
 def test_empty_stage_keeps_catalog(self):
  self.store(node('old'));before=self.catalog();self.assertIn('NO_VALID_NODES',self.sync(ok=False));self.assertEqual(self.catalog(),before)
 def test_wrong_subscription_keeps_catalog(self):
  self.store(node('old'));self.store(node('foreign',sub='other'),'stage');before=self.catalog();self.sync(ok=False);self.assertEqual(self.catalog(),before)
 def test_stage_immutable(self):
  self.store(node('old'));self.store(node('new'),'stage');before={p.name:p.read_bytes() for p in (self.app/'stage').iterdir()};self.sync();self.assertEqual(before,{p.name:p.read_bytes() for p in (self.app/'stage').iterdir()})
 def test_repeated_partial_does_not_duplicate(self):
  old=self.store(node('old'));b=old.read_bytes();self.store(node('new'),'stage');self.sync();r=self.sync();self.assertEqual((r['added'],r['retained'],r['removed']),(0,1,0));self.assertEqual(len(list((self.app/'servers').glob('*.json'))),2);self.assertEqual(old.read_bytes(),b)
 def test_full_missing_active_still_conflicts(self):
  n=node('active');self.store(n);(self.app/'active').write_text(n['id']);self.store(node('new'),'stage');before=self.catalog();self.assertIn('ACTIVE_SERVER_CONFLICT',self.sync('replace',ok=False));self.assertEqual(before,self.catalog())
 def test_partial_malformed_old_fails_closed(self):
  n=node('old');n.pop('uuid');self.store(n);self.store(node('new'),'stage');before=self.catalog();self.sync(ok=False);self.assertEqual(before,self.catalog())
 def test_retained_id_collision_fails_before_write(self):
  self.store(node('old',id='same-id'));self.store(node('new',id='same-id'),'stage');before=self.catalog();self.assertIn('SERVER_SYNC_CONFLICT',self.sync(ok=False));self.assertEqual(before,self.catalog())
 def test_retained_symlink_is_not_followed(self):
  external=self.temp/'keep.json';external.write_text(json.dumps(node('old')));(self.app/'servers/subscription-test-old.json').symlink_to(external);self.store(node('new'),'stage');b=external.read_bytes();self.sync(ok=False);self.assertEqual(external.read_bytes(),b);self.assertTrue((self.app/'servers/subscription-test-old.json').is_symlink())
 def test_retained_filename_mismatch_is_rejected(self):
  f=self.store(node('old'));f.rename(f.with_name('different.json'));self.store(node('new'),'stage');before=self.catalog();self.sync(ok=False);self.assertEqual(before,self.catalog())
 def test_all_unmatched_are_counted_not_accepted(self):
  for key in ['one','two','three']:self.store(node(key))
  self.store(node('new'),'stage');r=self.sync();self.assertEqual((r['accepted'],r['retained'],r['total'],r['removed']),(1,3,4,0));self.assertEqual(r['deletionPolicy'],'retain-unmatched');self.assertEqual(len(r['warnings']),1)
 def test_next_complete_update_reconciles_retained(self):
  f=self.store(node('old'));self.store(node('new'),'stage');self.sync();r=self.sync('replace');self.assertFalse(f.exists());self.assertEqual(r['removed'],1);self.assertEqual(r['retained'],0)
 def test_nonempty_legacy_lock_untouched(self):
  self.store(node('old'));self.store(node('new'),'stage');lock=self.app/'run/server-subscription.lock';lock.mkdir();(lock/'foreign').write_text('keep');before=self.catalog();self.sync(ok=False);self.assertTrue((lock/'foreign').exists());self.assertEqual(before,self.catalog())
 def test_no_retention_when_all_keys_present(self):
  self.store(node('same'));self.store(node('same'),'stage');r=self.sync();self.assertEqual(r['retained'],0);self.assertEqual(r['warnings'],[]);self.assertEqual(r['total'],1)
 def test_new_source_import_partial(self):
  self.store(node('new'),'stage');r=self.sync();self.assertEqual((r['added'],r['retained'],r['accepted']),(1,0,1))
 def test_union_limit_refuses_before_live_write(self):
  self.store(node('old'));self.store(node('new'),'stage');self.env['BRORAY_SUB_MAX_NODES']='1';before=self.catalog();self.assertIn('PARTIAL_UPDATE_LIMIT',self.sync(ok=False));self.assertEqual(before,self.catalog())
 def test_duplicate_old_keys_are_not_guessed(self):
  self.store(node('old'));self.store(node('old',id='legacy-old'));self.store(node('new'),'stage');before=self.catalog();self.sync(ok=False);self.assertEqual(before,self.catalog())
 def test_foreign_filename_collision_is_not_overwritten(self):
  manual=node('manual',id='collision');manual['source']={'type':'manual'};self.store(manual);self.store(node('new',id='collision'),'stage');before=self.catalog();self.sync(ok=False);self.assertEqual(before,self.catalog())
 def test_commit_failure_restores_exact_catalog(self):
  self.store(node('old'));self.store(node('new'),'stage');before=self.catalog();self.env['QA_FAIL_COMMIT']='1';self.sync(ok=False);self.assertEqual(before,self.catalog());self.assertFalse(list((self.app/'tmp').glob('server-subscription-sync.*')))
 def test_failed_rollback_preserves_backup_and_existing_nodes(self):
  originals={}
  for k in ['aa','bb']:
   f=self.store(node(k,id=k,password='credential-A'));originals[f.name]=f.read_bytes()
   self.store(node(k,id=k,password='credential-B'),'stage')
  self.env.update(QA_FAIL_COMMIT='1',QA_FAIL_SECOND='1',QA_FAIL_RESTORE='1')
  error=self.sync('replace',ok=False)
  self.assertIn('SERVER_SYNC_ROLLBACK_FAILED',error)
  self.assertEqual(len(list((self.app/'servers').glob('*.json'))),2)
  work=list((self.app/'tmp').glob('server-subscription-sync.*'));self.assertEqual(len(work),1)
  self.assertEqual({p.name:p.read_bytes() for p in (work[0]/'backup/live').glob('*.json')},originals)
  self.assertTrue((work[0]/'rollback-required').exists())
  before=self.catalog();self.env.pop('QA_FAIL_COMMIT');self.env.pop('QA_FAIL_SECOND');self.env.pop('QA_FAIL_RESTORE')
  self.assertIn('SERVER_SYNC_RECOVERY_REQUIRED',self.sync('replace',ok=False));self.assertEqual(self.catalog(),before)
 def test_initial_same_topology_distinct_credentials(self):
  for key,credential in [('a','credential-A'),('b','credential-B')]:self.store(node('same',id=key,password=credential),'stage')
  r=self.sync('replace');self.assertEqual(r['added'],2)
  self.assertEqual({json.loads(p.read_bytes())['uuid'] for p in (self.app/'servers').glob('*.json')},{'credential-A','credential-B'})
  r=self.sync('replace');self.assertEqual(r['unchanged'],2)
 def test_removed_topology_variants_are_not_rotation_ambiguity(self):
  for key,credential in [('a','credential-A'),('b','credential-B')]:self.store(node('same',id=key,password=credential))
  self.store(node('other'),'stage');r=self.sync('replace')
  self.assertEqual(r['removed'],2);self.assertEqual(r['added'],1)
 def test_private_work_removed_after_success(self):
  self.store(node('old'));self.store(node('new'),'stage');self.sync();self.assertFalse(list((self.app/'tmp').glob('server-subscription-sync.*')))
 def test_second_commit_failure_restores_already_changed_node(self):
  n=node('a');self.store(n);self.store(node('b'));before=self.catalog();n['uuid']='uuid-new';self.store(n,'stage');self.store(node('z'),'stage');self.env.update(QA_FAIL_COMMIT='1',QA_FAIL_SECOND='1');self.sync(ok=False);self.assertTrue((self.app/'fault-first-written').exists());self.assertEqual(before,self.catalog());self.assertFalse(list((self.app/'tmp').glob('server-subscription-sync.*')))
 def test_credential_rotation_preserves_id(self):
  old=node('same',id='stable-old',password='credential-A');self.store(old)
  new=node('same',id='provisional-new',password='credential-B');self.store(new,'stage')
  r=self.sync('replace');rows=[json.loads(p.read_bytes()) for p in (self.app/'servers').glob('*.json')]
  self.assertEqual(len(rows),1);self.assertEqual(rows[0]['id'],'stable-old');self.assertEqual(rows[0]['uuid'],'credential-B');self.assertEqual(r['updated'],1)
 def test_exact_old_plus_credential_variant(self):
  self.store(node('same',id='stable-old',password='credential-A'))
  self.store(node('same',id='provisional-exact',password='credential-A'),'stage')
  self.store(node('same',id='provisional-new',password='credential-B'),'stage')
  r=self.sync('replace');rows={json.loads(p.read_bytes())['uuid']:json.loads(p.read_bytes())['id'] for p in (self.app/'servers').glob('*.json')}
  self.assertEqual(rows,{'credential-A':'stable-old','credential-B':'provisional-new'})
  self.assertEqual((r['added'],r['removed']),(1,0))
 def test_ambiguous_rotation_preserves_catalog_and_active(self):
  self.store(node('same',id='stable-old',password='credential-A'));(self.app/'active').write_bytes(b'stable-old\n')
  self.store(node('same',id='candidate-b',password='credential-B'),'stage');self.store(node('same',id='candidate-c',password='credential-C'),'stage')
  before=self.catalog();error=self.sync('replace',ok=False)
  self.assertIn('SERVER_SYNC_AMBIGUOUS_IDENTITY',error);self.assertEqual(self.catalog(),before)
  self.assertEqual((self.app/'active').read_bytes(),b'stable-old\n');self.assertFalse((self.app/'unexpected-live-hook').exists())
 def test_active_rotation_uses_remapped_file(self):
  old=node('same',id='stable-active',password='credential-A');self.store(old);(self.app/'active').write_bytes(b'stable-active\n')
  self.store(node('same',id='provisional-new',password='credential-B'),'stage')
  (self.app/'config/system/settings.json').write_text(json.dumps({'listenAddress':'127.0.0.1','socksPort':2080,'logLevel':'warning'}))
  (self.app/'logs').mkdir()
  self.env['QA_ACTIVE_ROTATION']='1'
  r=self.sync('replace')
  self.assertEqual((self.app/'applied-id').read_text(),'stable-active');self.assertEqual((self.app/'active').read_bytes(),b'stable-active\n')
  self.assertEqual(json.loads((self.app/'servers/stable-active.json').read_bytes())['uuid'],'credential-B')
  self.assertEqual(r['activeServerImpact'],'configuration-changed');self.assertIn('Configuration OK',(self.app/'xray-config-test.log').read_text())
if __name__=='__main__':
 assert os.name!='nt', 'Use disposable Linux guest'
 suite=unittest.defaultTestLoader.loadTestsFromTestCase(Sync)
 if os.environ.get('STAGE08_REPRO'):suite=unittest.TestSuite(t for t in suite if 'test_regression_' in t.id())
 result=unittest.TextTestRunner(verbosity=2,failfast=not bool(os.environ.get('STAGE08_REPRO'))).run(suite)
 print('STAGE08_SYNC_REPORT='+json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped),'mocked':['owner checkpoint','Xray live hooks'],'routerAccessed':False}))
 raise SystemExit(0 if result.wasSuccessful() else 1)
