"""Coordinator file/mode proofs with BusyBox stat but no standalone stat."""
from pathlib import Path
import hashlib,json,os,shutil,subprocess,tempfile,unittest

SOURCE=Path(os.environ.get('BRORAY_TEST_ROOT','/work/implementation'))/'runtime/app/lib/operation-platform-generation.sh'

class EntwareStat(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='entware-stat-');self.addCleanup(self.tmp.cleanup)
  self.root=Path(self.tmp.name);self.bin=self.root/'tools';self.bin.mkdir()
  for name in ['busybox','jq','sha256sum']:(self.bin/name).symlink_to(shutil.which(name))
  self.app=self.root/'opt/broray';self.app.mkdir(parents=True)
  rows=[]
  for i in range(7):
   p=self.root/f'opt/platform{i}';p.write_bytes(b'platform fixture\n');p.chmod(0o755)
   rows.append({'path':p.name,'value':{'executable':True,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()}})
  self.inventory=self.root/'inventory.json';self.inventory.write_text(json.dumps(rows))
  self.sha=hashlib.sha256(''.join(r['value']['sha256']+'  opt/'+r['path']+'\n' for r in rows).encode()).hexdigest()
 def run_inventory(self):
  env={**os.environ,'PATH':str(self.bin),'OPS_APP':str(self.app),'PG_MANIFEST':self.sha,'LIB':str(SOURCE),'INVENTORY':str(self.inventory)}
  return subprocess.run(['/bin/ash','-c','. "$LIB"\nops_platform_service_inventory() { jq -c . "$INVENTORY"; }\nops_platform_generation_files'],env=env,capture_output=True,text=True,timeout=5)
 def test_busybox_applet_preserves_exact_file_modes(self):
  self.assertFalse((self.bin/'stat').exists());r=self.run_inventory();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
 def test_wrong_mode_still_refused(self):
  (self.root/'opt/platform3').chmod(0o775);self.assertNotEqual(self.run_inventory().returncode,0)
 def test_failed_standalone_stat_must_not_fallback(self):
  p=self.bin/'stat';p.write_text('#!/bin/ash\nexit 7\n');p.chmod(0o755)
  self.assertNotEqual(self.run_inventory().returncode,0)
 def terse_only_busybox(self, malformed=False):
  p=self.bin/'busybox';p.unlink()
  real=shutil.which('busybox')
  body='#!/bin/ash\n[ "$1" = stat ] || exit 70\nshift\n'
  body+='for arg in "$@"; do [ "$arg" != -c ] || { echo "stat: invalid option -- c" >&2; exit 1; }; done\n'
  body+=('printf "corrupt record\\n"\n' if malformed else 'exec '+real+' stat "$@"\n')
  p.write_text(body);p.chmod(0o755)
 def test_target_terse_only_stat_inventory(self):
  self.terse_only_busybox();r=self.run_inventory();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
 def helper(self, rel, name, args):
  root=SOURCE.parents[3]
  text=(root/rel).read_text();start=text.index(name+'()')
  # Both original brace body and replacement subshell body are supported;
  # capture the complete helper, not an independently rewritten implementation.
  lines=text[start:].splitlines();body=[]
  for line in lines:
   body.append(line)
   if line in ('}',')'):break
  else:self.fail('missing helper end')
  env={**os.environ,'PATH':str(self.bin)}
  return subprocess.run(['/bin/ash','-c','\n'.join(body)+'\n'+name+' "$@"','test',*args],env=env,capture_output=True,text=True,timeout=5)
 def test_target_terse_formats_in_all_three_helpers(self):
  self.terse_only_busybox()
  helpers=[('runtime/app/lib/operation-platform-generation.sh','ops_platform_stat'),('runtime/app/share/updater-platform/opt/etc/init.d/S22broray-updater','updater_stat'),('runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh','updater_stat')]
  f=self.root/'file with spaces';f.write_text('exact bytes');f.chmod(0o4755)
  link=self.root/'link';link.symlink_to(f)
  for rel,name in helpers:
   for flags,fmt,expected in [([], '%a','4755'),([], '%a:%u','4755:'+str(os.getuid())),([], '%u:%a:%h',str(os.getuid())+':4755:1'),(['-L'], '%u:%a:%h',str(os.getuid())+':4755:1')]:
    with self.subTest(helper=rel,format=fmt,flags=flags):
     r=self.helper(rel,name,[*flags,'-c',fmt,str(link if flags else f)])
     self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(r.stdout.strip(),expected)
 def test_target_malformed_terse_output_fails_closed(self):
  self.terse_only_busybox(malformed=True);self.assertNotEqual(self.run_inventory().returncode,0)
 def test_target_terse_multi_path_layout_probe(self):
  self.terse_only_busybox();paths=[]
  for i in range(5):
   p=self.root/('private dir '+str(i));p.mkdir(mode=0o700);paths.append(str(p))
  r=self.helper('runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh','updater_stat',['-c','%a:%u',*paths])
  self.assertEqual(r.returncode,0,r.stderr)
  self.assertEqual(r.stdout.splitlines(),['700:'+str(os.getuid())]*5)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(EntwareStat(n) for n in EntwareStat.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
