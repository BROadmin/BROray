import unittest,tempfile,subprocess,os
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
if __name__=='__main__':unittest.main()
