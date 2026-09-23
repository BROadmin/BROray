"""Restore exact durable files into a genuinely new, network-free Linux boot."""
import hashlib,json,os,socket,stat,unittest
from pathlib import Path
from test_installed_service_cycles import ServiceCycles

class CycleBootResume(ServiceCycles):
 def setUp(self):
  self.e=json.loads(Path('/work/migration-transfer.json').read_bytes())
  self.assertIs(self.e.get('serviceCycleBoot'),True)
  self.root=Path(self.e['root']);self.updater=self.root/'router/opt/var/lib/broray-updater'
  self.op=self.root/'router/opt/var/lib/broray/operations'/self.e['operationId']
  self.native=Path('/work/.local/bin/linux-generation')
  self.assertEqual(hashlib.sha256(self.native.read_bytes()).hexdigest(),self.e['nativeSha256'])
  self.boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
  self.assertNotEqual(self.boot,self.e['oldBootId'],'must be a real new Linux boot')
  for row in self.e['rows']:
   p=self.root/row['path'];self.assertEqual(stat.S_IMODE(p.lstat().st_mode),row['mode'])
   if row['kind']=='file':self.assertEqual(hashlib.sha256(p.read_bytes()).hexdigest(),row['sha256'])
   elif row['kind']=='symlink':self.assertEqual(os.readlink(p),row['target'])
   else:self.assertTrue(p.is_dir())
  for row in self.e.get('socketPaths',[]):
   parts=row['path'].split('/');self.assertEqual(parts[:5],['router','opt','var','lib','broray-updater'])
   self.assertEqual(len(parts),8);self.assertIn(parts[5],['hosts','generations']);self.assertEqual(parts[7],'control')
   self.assertTrue((self.updater/'generations'/parts[6]/'retirement.receipt').is_file())
   self.assertTrue((self.updater/'hosts'/parts[6]/'retirement.receipt').is_file())
   p=self.root/row['path'];self.assertFalse(p.exists() or p.is_symlink())
   # A persisted socket pathname has no live kernel peer after reboot.
   with socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET) as s:s.bind(str(p))
   p.chmod(row['mode']);self.assertTrue(p.is_socket())
  self.fixed_links=[];self.fixed_dirs=[];self.addCleanup(self.remove_fixed_aliases)
  for destination,source in [('/opt/bin/ash',self.root/'router/opt/bin/ash'),('/opt/libexec/broray-updater',self.root/'router/opt/libexec/broray-updater'),('/opt/var/lib/broray-updater',self.updater)]:
   p=Path(destination);missing=[];parent=p.parent
   while not parent.exists():missing.append(parent);parent=parent.parent
   for parent in reversed(missing):parent.mkdir();self.fixed_dirs.append(parent)
   if p.exists() or p.is_symlink():self.assertEqual(p.resolve(),source.resolve())
   else:p.symlink_to(source,target_is_directory=source.is_dir());self.fixed_links.append((p,source))
  curl=Path('/usr/bin/curl');self.assertFalse(curl.exists())
  self.fetch=self.root/'unexpected-boot-fetch'
  curl.write_text('#!/bin/ash\nprintf called >"'+str(self.fetch)+'"\nexit 97\n');curl.chmod(0o755)
  self.addCleanup(curl.unlink)
  self.addCleanup(lambda:self.assertFalse(self.fetch.exists(),'startup must not fetch'))
  self.addCleanup(self.stop_created_generation)
 def test_new_boot_starts_fresh_generation_preserving_retired_history(self):
  origin={k:tuple(v) for k,v in self.e['completedOriginFiles'].items()}
  self.assertEqual(self.files(self.op),origin)
  historical={p.name:self.files(p) for p in (self.updater/'generations').iterdir() if p.is_dir()}
  platform=self.bytes_now();before=self.files(self.updater)
  r=self.init('status');self.assertNotEqual(r.returncode,0,'old boot cannot prove live readiness')
  self.assertEqual(self.files(self.updater),before)
  current=self.success('start');self.assertNotEqual(current['generationId'],self.e['stoppedGeneration'])
  self.one_live(current['generationId']);self.assertEqual(self.success('start')['generationId'],current['generationId'])
  self.assertEqual(self.files(self.op),origin);self.assertEqual(self.bytes_now(),platform)
  for name,files in historical.items():self.assertEqual(self.files(self.updater/'generations'/name),files)
  self.success('stop');self.assertEqual(self.success('stop')['generationId'],current['generationId'])
  self.assertEqual(self.files(self.op),origin)
  print('SERVICE_CYCLE_BOOT_RECEIPT '+json.dumps({'oldBootId':self.e['oldBootId'],'newBootId':self.boot,'oldGeneration':self.e['stoppedGeneration'],'newGeneration':current['generationId'],'completedMigrationUnchanged':True,'retiredHistoryUnchanged':True}),flush=True)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([CycleBootResume('test_new_boot_starts_fresh_generation_preserving_retired_history')]))
 raise SystemExit(not result.wasSuccessful())
