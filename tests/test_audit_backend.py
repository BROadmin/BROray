"""Focused local wiring checks; no router/native ownership claims."""
import os,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
class AuditBackend(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.p=Path(self.tmp.name);(self.p/'lib').mkdir();(self.p/'tmp').mkdir()
  self.env=dict(os.environ,ASH_STANDALONE='1',BRORAY_ROOT=self.p.as_posix(),BRORAY_BASE=self.p.as_posix())
 def tearDown(self):self.tmp.cleanup()
 def shell(self,s):
  b=os.environ.get('BRORAY_TEST_BUSYBOX');cmd=[b,'ash'] if b else ['/bin/ash']
  return subprocess.run(cmd+['-c',s],capture_output=True,text=True,encoding='utf-8',env=self.env,timeout=15)
 def test_cache_guard_inherits_unexported_root_from_cgi(self):
  lib=(ROOT/'runtime/app/lib/routes-router-config.sh').as_posix()
  r=self.shell('. "'+lib+'"; root="$BRORAY_ROOT"; unset BRORAY_ROOT; BRORAY_ROOT="$root"; broray_routes_config_cache_fresh(){ return 1; }; guard(){ env | grep -Fx "BRORAY_ROOT=$root"; }; BRORAY_OPS_GUARD=guard; broray_routes_config_get_cache')
  self.assertEqual(r.returncode,0,r.stderr)
 def test_uninstall_dot_passes_exact_preview_under_existing_owner(self):
  (self.p/'lib/routes-dot.sh').write_text("broray_dot_delete_preview(){ printf '%s\\n' '{\"schemaVersion\":1,\"expectedFingerprint\":\"fixture\",\"serverIds\":[\"a\"]}'; }\nbroray_dot_delete(){ jq -e '.expectedFingerprint==\"fixture\" and .serverIds==[\"a\"]' \"$1\" > /dev/null && echo DELETED; }\n")
  lib=(ROOT/'runtime/app/lib/component-lifecycle.sh').as_posix()
  r=self.shell('. "'+lib+'"; broray_lifecycle_uninstall_owner(){ return 0; }; broray_tx_control_transition_begin(){ :; }; broray_tx_control_transition_assert(){ :; }; broray_tx_control_transition_end(){ :; }; broray_lifecycle_component dot-delete')
  self.assertEqual(r.returncode,0,r.stderr);self.assertIn('DELETED',r.stdout)
 def test_active_health_ignores_icmp_and_requires_current_context(self):
  import json,time
  quality=self.p/'quality.json';auto=self.p/'auto.json';auto.write_text('{}')
  sample=dict(method='current-socks-https',serverId='fixture',context='current',checkedEpoch=int(time.time()),status='healthy')
  lib=(ROOT/'runtime/app/lib/active-proxy-health.sh').as_posix()
  for change,expected in [({},'healthy'),({'context':'old'},'unknown'),({'checkedEpoch':1},'unknown'),({'status':'unhealthy'},'unhealthy')]:
   quality.write_text(json.dumps({'activeHealth':sample|change}))
   r=self.shell('. "'+lib+'"; broray_active_proxy_context(){ echo current; }; broray_active_proxy_cached fixture "'+quality.as_posix()+'" "'+auto.as_posix()+'"')
   self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(json.loads(r.stdout)['status'],expected)
 def test_uninstall_dot_restore_uses_existing_owner(self):
  (self.p/'lib/routes-dot.sh').write_text('broray_dot_apply(){ [ "$BRORAY_DOT_RESTORE_EXACT" = true ] && [ "$1" = exact-request ] && echo RESTORED; }\n')
  lib=(ROOT/'runtime/app/lib/component-lifecycle.sh').as_posix()
  r=self.shell('. "'+lib+'"; broray_lifecycle_uninstall_owner(){ return 0; }; broray_tx_control_transition_begin(){ :; }; broray_tx_control_transition_assert(){ :; }; broray_tx_control_transition_end(){ :; }; BRORAY_DOT_RESTORE_EXACT=true; broray_lifecycle_component dot-restore exact-request')
  self.assertEqual(r.returncode,0,r.stderr);self.assertIn('RESTORED',r.stdout)
 def test_registration_failure_keeps_recovery_snapshot(self):
  src=(ROOT/'runtime/app/lib/broray-page.sh').read_text(encoding='utf-8');a=src.index('    broray_system_uninstall_registration_recovery_required()');b=src.index('    broray_system_uninstall_finalization_marker_write()',a)
  names=['snapshot','bundles','dot_status','dot_request','dot_verify','services','registration_hashes']
  for n in names:(self.p/n).write_text('KEEP')
  setup='\n'.join('uninstall_'+n+'="'+(self.p/n).as_posix()+'"' for n in names)
  r=self.shell(src[a:b]+setup+'\nbroray_system_uninstall_restore(){ return 1; }; broray_system_status_write(){ :; }; broray_system_uninstall_preserved_rollback(){ :; }; uninstall_auth_root_owned=false; operation_id=fixture; broray_system_uninstall_registration_recovery_required unknown 1 remove')
  self.assertEqual(r.returncode,1,r.stderr)
  self.assertTrue((self.p/'snapshot').exists(),'Failed OPKG rollback erased recovery evidence')
 def test_failed_rollback_keeps_evidence(self):
  src=(ROOT/'runtime/app/lib/broray-page.sh').read_text(encoding='utf-8');a=src.index('    broray_system_uninstall_abort()');b=src.index('    broray_system_uninstall_registration_recovery_required()',a)
  for n in ['snapshot','bundles','dot_status','dot_request','dot_verify','services','registration_hashes']:(self.p/n).write_text('KEEP')
  setup='\n'.join('uninstall_'+n+'="'+(self.p/n).as_posix()+'"' for n in ['snapshot','bundles','dot_status','dot_request','dot_verify','services','registration_hashes'])
  r=self.shell(src[a:b]+setup+'\nbroray_system_uninstall_restore(){ return 1; }; broray_system_status_write(){ :; }; broray_system_uninstall_preserved_rollback(){ :; }; broray_system_uninstall_auth_retire(){ :; }; operation_id=fixture; broray_system_uninstall_abort routes failure')
  self.assertEqual(r.returncode,1,r.stderr);self.assertEqual((self.p/'snapshot').read_text(),'KEEP')
if __name__=='__main__':unittest.main(verbosity=2)
