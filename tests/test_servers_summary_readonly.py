import unittest,tempfile,subprocess,os,json,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
class SummaryReadOnly(unittest.TestCase):
 def test_summary_does_not_invoke_keenetic_refresh(self):
  with tempfile.TemporaryDirectory() as td:
   p=Path(td);common=p/'common.sh';marker=p/'router-probe'
   source=(ROOT/'runtime/app/lib/server-service.sh').read_text()
   prefix=source.split('broray_server_summary()\n{',1)[1].split('    active_server_id=',1)[0]
   common.write_text('''broray_api_require_method(){ [ "$1" = GET ] || exit 31; printf 'METHOD_OK\\n'; }
broray_api_require_session(){ printf 'SESSION_OK\\n'; }
broray_server_refresh_keenetic_status(){ : > "$MARKER"; }
broray_servers_api_run(){ "$@"; }
broray_server_summary(){
'''+prefix+'\nreturn 0\n}\n')
   cgi=(ROOT/'runtime/app/web-new/api/servers/summary.cgi').read_text().replace('. /opt/broray/web-new/api/servers/common.sh','. '+str(common))
   env=os.environ.copy();env.pop('BRORAY_SERVER_SKIP_KEENETIC_REFRESH',None);env.update(BRORAY_BASE=td,BRORAY_QUALITY_DIR=td+'/quality',MARKER=str(marker))
   r=subprocess.run(['sh','-s'],input=cgi,text=True,capture_output=True,env=env)
   self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(r.stdout.splitlines(),['METHOD_OK','SESSION_OK'])
   self.assertFalse(marker.exists(),'Read-only summary unexpectedly invoked live Keenetic refresh')
 def test_explicit_refresh_remains_available(self):
  source=(ROOT/'runtime/app/lib/server-service.sh').read_text()
  function='broray_server_refresh_keenetic_status()\n{'+source.split('broray_server_refresh_keenetic_status()\n{',1)[1].split('\nbroray_server_quality_path()',1)[0]
  with tempfile.TemporaryDirectory() as td:
   p=Path(td);(p/'lib').mkdir();marker=p/'probe';(p/'lib/keenetic-page.sh').write_text('broray_keenetic_status_json(){ : > "$MARKER"; }\n')
   env=dict(os.environ,BRORAY_BASE=td,BRORAY_KEENETIC_STATUS=td+'/absent',MARKER=str(marker))
   r=subprocess.run(['sh','-s'],input=function+'\nbroray_server_refresh_keenetic_status force\n',text=True,capture_output=True,env=env)
   self.assertEqual(r.returncode,0,r.stderr);self.assertTrue(marker.exists())
class ConnectionProjection(unittest.TestCase):
 """Run the actual summary projection; replace only process/probe/mtime inputs."""
 def summary(self,age=10,router=True,healthy=True,consistent=True,probe='healthy',xray=True,socks=True,active=True):
  source=(ROOT/'runtime/app/lib/server-service.sh').read_text()
  tail=source.split('    active_json=null\n',1)[1].split('\nbroray_server_details()',1)[0]
  with tempfile.TemporaryDirectory() as td:
   p=Path(td);(p/'servers.json').write_text(json.dumps([{'id':'s1'}] if active else []))
   if router:(p/'keenetic.json').write_text(json.dumps({'exists':True,'healthy':healthy,'matchesExpected':consistent}))
   script='''set -eu
. "$CONTRACT"
pidof(){ [ "$XRAY" = true ]; }
broray_xray_socks_address(){ printf '127.0.0.1'; }
broray_xray_socks_port(){ printf '2080'; }
broray_xray_socks_active(){ [ "$SOCKS" = true ]; }
broray_active_proxy_cached(){ printf '{"status":"%s"}' "$PROBE"; }
broray_status_file_epoch(){ printf '%s' "$FIXTURE_EPOCH"; }
broray_server_now(){ date -u '+%Y-%m-%dT%H:%M:%SZ'; }
project(){
servers_array="$BRORAY_BASE/servers.json"
active_server_id=s1
total=1;available=1;unavailable=0
quality_fresh=1;quality_stale=0;quality_expired=0;quality_unknown=0
active_json=null
'''+tail+'\nproject\n'
   env=dict(os.environ,CONTRACT=str(ROOT/'runtime/app/lib/status-contract.sh'),BRORAY_BASE=td,
    BRORAY_QUALITY_DIR=td,BRORAY_AUTO_SWITCH_FILE=td+'/auto.json',BRORAY_KEENETIC_STATUS=td+'/keenetic.json',
    BRORAY_KEENETIC_STALE_SECONDS='60',BRORAY_KEENETIC_EXPIRED_SECONDS='300',
    FIXTURE_EPOCH=str(int(time.time())-age),XRAY=str(xray).lower(),SOCKS=str(socks).lower(),PROBE=probe)
   r=subprocess.run(['sh','-s'],input=script,text=True,capture_output=True,env=env)
   self.assertEqual(r.returncode,0,r.stderr)
   return json.loads(r.stdout)
 def test_fresh_connection_survives_stale_router_snapshot_with_warning(self):
  s=self.summary(age=90)
  self.assertEqual(s['activeConnection'],{'method':'current-socks-https','available':True,'up':True,'freshness':'fresh'})
  self.assertEqual(s['connectionState'],'connected')
  self.assertEqual(s['keenetic']['freshness'],'stale')
  self.assertEqual(s['health']['severity'],'warning');self.assertFalse(s['health']['operational'])
  self.assertFalse(s['health']['consistent']);self.assertTrue(s['health']['actionRequired'])
  self.assertEqual([r['code'] for r in s['health']['reasons']],['PROXY0_STATUS_STALE'])
 def test_expired_router_snapshot_retains_warning(self):
  s=self.summary(age=400)
  self.assertEqual(s['connectionState'],'connected');self.assertEqual(s['keenetic']['freshness'],'expired')
  self.assertFalse(s['health']['operational']);self.assertEqual(s['health']['severity'],'warning')
  self.assertIn('PROXY0_STATUS_STALE',[r['code'] for r in s['health']['reasons']])
 def test_fresh_healthy_router_and_connection(self):
  s=self.summary();self.assertEqual(s['connectionState'],'connected')
  self.assertTrue(s['health']['operational']);self.assertTrue(s['health']['consistent'])
  self.assertEqual(s['health']['severity'],'ok');self.assertEqual(s['health']['reasons'],[])
 def test_router_faults_are_not_hidden_by_successful_connection(self):
  for args,code in [({'router':False},'PROXY0_MISSING'),({'healthy':False},'PROXY0_NOT_READY'),({'consistent':False},'PROXY0_MISMATCH')]:
   with self.subTest(args=args):
    s=self.summary(**args);self.assertEqual(s['connectionState'],'connected')
    self.assertEqual(s['health']['severity'],'warning');self.assertFalse(s['health']['consistent'])
    self.assertIn(code,[r['code'] for r in s['health']['reasons']])
 def test_unknown_or_failed_connection_never_connected(self):
  for probe in ['unknown','unhealthy']:
   with self.subTest(probe=probe):
    s=self.summary(probe=probe);self.assertEqual(s['connectionState'],'degraded')
    self.assertFalse(s['health']['operational']);self.assertFalse(s['activeConnection']['up'])
 def test_stopped_path_and_missing_active_never_connected(self):
  for args,state in [({'xray':False},'error'),({'socks':False},'error'),({'active':False},'disabled')]:
   with self.subTest(args=args):
    s=self.summary(**args);self.assertEqual(s['connectionState'],state);self.assertFalse(s['health']['operational'])

if __name__=='__main__':unittest.main()
