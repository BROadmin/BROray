"""Export a confirmed stopped ordinary cycle for a real, later VM boot."""
import base64,hashlib,json,os,stat,unittest
from pathlib import Path
from test_installed_service_cycles import ServiceCycles

class CycleBootPrepare(ServiceCycles):
 def test_restart_then_export_confirmed_stop(self):
  self.installed();first=self.success('start')
  origin=self.files(self.op);platform=self.bytes_now()
  repeated=self.success('start');self.assertEqual(first['generationId'],repeated['generationId'])
  following=self.success('restart');self.assertNotEqual(first['generationId'],following['generationId'])
  self.one_live(following['generationId']);self.success('stop')
  self.assertEqual(self.files(self.op),origin);self.assertEqual(self.bytes_now(),platform)
  self.export_stopped(following['generationId'],origin)
 def export_stopped(self,generation,origin):
  rows=[];sockets=[]
  for p in sorted(self.root.rglob('*')):
   rel=p.relative_to(self.root).as_posix();mode=stat.S_IMODE(p.lstat().st_mode)
   if p.is_symlink():
    self.assertEqual(p.name,'retired-lock');target=str(p.parent/'fence')
    self.assertEqual(os.readlink(p),target)
    rows.append({'path':rel,'kind':'symlink','mode':mode,'target':target,'targetSha256':hashlib.sha256(target.encode()).hexdigest()})
   elif p.is_dir():rows.append({'path':rel,'kind':'directory','mode':mode})
   elif p.is_file():
    body=p.read_bytes();rows.append({'path':rel,'kind':'file','mode':mode,'sha256':hashlib.sha256(body).hexdigest(),'base64':base64.b64encode(body).decode()})
   elif stat.S_ISSOCK(p.lstat().st_mode):
    parts=rel.split('/')
    self.assertEqual(parts[:6],['router','opt','var','lib','broray-updater',parts[5]])
    self.assertIn(parts[5],['generations','hosts']);self.assertEqual(len(parts),8);self.assertEqual(parts[7],'control')
    self.assertTrue((self.updater/'generations'/parts[6]/'retirement.receipt').is_file())
    self.assertTrue((self.updater/'hosts'/parts[6]/'retirement.receipt').is_file())
    sockets.append({'path':rel,'mode':mode})
   else:self.fail('non-transferable object '+rel)
  value=dict(self.e)
  value.update(fixture='canonical-boot-context/1',serviceCycleBoot=True,root=str(self.root),rows=rows,socketPaths=sockets,
   oldBootId=Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
   stoppedGeneration=generation,completedOriginFiles=origin,legacyOwnerLiveAtExport=False,
   scope='Confirmed public restart and stopped service; exact durable files across actual offline Linux VM boots')
  for launch in (self.updater/'starts').glob('*/launch.record'):
   domain=Path(launch.read_text().splitlines()[2])
   self.assertTrue((domain/'retirement.receipt').is_file())
   terminal=json.loads(sorted(domain.glob('revision-*.json'))[-1].read_bytes())
   self.assertEqual(terminal['state'],'STOPPED');self.assertEqual(terminal['children'],[])
  raw=json.dumps(value,separators=(',',':')).encode()
  print('MIGRATION_BOOT_EXPORT_BEGIN\n'+base64.b64encode(raw).decode()+'\nMIGRATION_BOOT_EXPORT_END',flush=True)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([CycleBootPrepare('test_restart_then_export_confirmed_stop')]))
 raise SystemExit(not result.wasSuccessful())
