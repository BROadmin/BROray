"""Selected Xray admission; official metadata fixtures, no downloads or router."""
import json,os,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))

class PartialAdmission(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
  self.home=Path(self.tmp.name);self.digest='a'*64
  self.release={'tag_name':'v26.7.28','prerelease':False,'assets':[{'digest':'sha256:'+self.digest},{}]}
  self.record={'candidateId':'test','architecture':'arm64','xrayTag':'v26.7.28','archiveSha256':self.digest,'testedAt':'2026-09-25T12:23:00Z','evidence':'fixture','status':'incompatible','limitations':['Переключение UDP-портов (udphop).']}
  self.request={'tag':'v26.7.28','currentVersion':'26.9.9','archiveSha256':self.digest,'allowUntested':False,'allowPrerelease':False,'allowDowngrade':True,'allowPartial':True}
 def run_request(self):
  for name,data in [('official',self.release),('records',[self.record]),('request',self.request)]:
   (self.home/(name+'.json')).write_text(json.dumps(data),encoding='utf-8')
  script='''. "$1"
broray_xray_version_number() { echo 26.9.9; }
broray_xray_version_key() { case "$1" in 26.9.9) echo 260909;; *) echo 260728;; esac; }
broray_xray_context() { echo '{"candidateId":"test","architecture":"arm64"}'; }
broray_xray_registry() { cat "$BRORAY_XRAY_UPDATE_WORK/records.json"; }
broray_xray_release_resolve() { cp "$BRORAY_XRAY_UPDATE_WORK/official.json" "$2"; }
broray_xray_update_error() { echo "$*" >&2; }
broray_xray_selected_check "$BRORAY_XRAY_UPDATE_WORK/request.json"
'''
  return subprocess.run(['/bin/ash','-c',script,'test',str(ROOT/'runtime/app/lib/xray-releases.sh')],env={**os.environ,'BRORAY_BASE':str(ROOT/'runtime/app'),'BRORAY_XRAY_UPDATE_WORK':str(self.home)},capture_output=True,text=True,timeout=8)
 def test_confirmed_partial_release_is_allowed_with_limitations(self):
  r=self.run_request();self.assertEqual(r.returncode,0,r.stderr)
  data=json.loads(r.stdout);self.assertEqual(data['compatibility']['status'],'incompatible');self.assertEqual(data['compatibility']['limitations'],self.record['limitations'])
 def test_partial_requires_explicit_confirmation(self):
  for value in [False,None]:
   if value is None:self.request.pop('allowPartial',None)
   else:self.request['allowPartial']=value
   r=self.run_request();self.assertNotEqual(r.returncode,0);self.assertIn('Подтвердите',r.stderr)
 def test_untyped_confirmation_is_rejected(self):
  self.request['allowPartial']='true';r=self.run_request();self.assertNotEqual(r.returncode,0);self.assertIn('Некорректный',r.stderr)
 def test_partial_consent_does_not_bypass_other_gates(self):
  original=dict(self.request)
  for change in [{'archiveSha256':'b'*64},{'currentVersion':'26.9.8'},{'allowDowngrade':False}]:
   self.request={**original,**change};self.assertNotEqual(self.run_request().returncode,0)
  self.request=original;self.release['prerelease']=True;self.assertNotEqual(self.run_request().returncode,0)
  self.request['allowPrerelease']=True;self.assertEqual(self.run_request().returncode,0)
 def test_unknown_release_still_requires_untested_consent(self):
  self.record['archiveSha256']='b'*64;r=self.run_request();self.assertNotEqual(r.returncode,0)
  self.request['allowUntested']=True;r=self.run_request();self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(json.loads(r.stdout)['compatibility']['status'],'untested')
 def test_old_request_shape_still_accepts_compatible_release(self):
  self.request.pop('allowPartial');self.record['status']='compatible'
  r=self.run_request();self.assertEqual(r.returncode,0,r.stderr)

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
