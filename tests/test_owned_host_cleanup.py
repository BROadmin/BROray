"""VM-only cleanup of a canonical fixture's independent host after assertions."""
from pathlib import Path
import hashlib,json,os,select,shutil,signal

def clear_fixture_host(case):
 hosts=case.updater/'hosts'
 if not hosts.exists():return
 case.assertTrue(Path('/work/migration-transfer.json').is_file())
 for record in hosts.glob('*/host.record'):
  lines=record.read_text().splitlines();case.assertEqual(lines[0],'BROray-independent-app-service/1')
  case.assertEqual(lines[1],str(record.parent));case.assertEqual(lines[5],str(case.root/'router'))
  owner=json.loads(lines[-1]);case.assertEqual(owner['executable'],str(case.native))
  pid=owner['pid'];proc=Path('/proc')/str(pid)
  if not proc.exists():continue
  fd=os.pidfd_open(pid)
  try:
   case.assertEqual(proc.joinpath('stat').read_text().rsplit(') ',1)[1].split()[19],owner['startTicks'])
   case.assertEqual(Path('/proc/sys/kernel/random/boot_id').read_text().strip(),owner['bootId'])
   case.assertEqual(os.readlink(proc/'exe'),owner['executable'])
   case.assertEqual(hashlib.sha256(proc.joinpath('cmdline').read_bytes()).hexdigest(),owner['commandDigest'])
   signal.pidfd_send_signal(fd,signal.SIGTERM)
   case.assertTrue(select.select([fd],[],[],3)[0],'fixture host must exit before its files are removed')
  finally:os.close(fd)
 shutil.rmtree(hosts)
