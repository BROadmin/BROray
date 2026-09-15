"""Durable journal loss detection, isolated files and labelled fake identities."""
import hashlib,json,subprocess,unittest
from test_operations import Operations,WORKSPACE,GUARD,BB,APP
class Journal(unittest.TestCase):
    def setUp(self):
        self.ops=Operations('test_normal_finish_and_next_begin');self.ops.setUp()
        self.journal=self.ops.state/'operation-events'
    def completed(self):
        a=self.ops.begin();self.ops.call('finish',a['operationId'],a['token'],'completed','');return a
    def test_loss_of_complete_last_record_is_detected(self):
        a=self.ops.begin();self.ops.call('finish',a['operationId'],a['token'],'completed','')
        log=self.ops.state/'operation-events/events.jsonl'
        lines=log.read_bytes().splitlines(keepends=True);self.assertGreater(len(lines),1)
        log.write_bytes(b''.join(lines[:-1]))
        result=self.ops.call('events')
        self.assertFalse(result['complete'],'Journal silently accepted loss of a complete final record')
        self.assertIn('JOURNAL_GAP',result['errors'])
    def test_sequence_and_tail_witness_match_successful_append(self):
        self.completed();self.completed()
        result=self.ops.call('events');self.assertTrue(result['complete'])
        self.assertEqual([e['sequence'] for e in result['events']],list(range(1,7)))
        head=json.loads((self.journal/'head.json').read_text())
        self.assertEqual(head['allocatedSequence'],6);self.assertFalse(head['pending'])
        self.assertEqual(head['lastHash'],hashlib.sha256((self.journal/'events.jsonl').read_bytes().splitlines(keepends=True)[-1]).hexdigest())
    def test_missing_middle_record_stays_detectable_after_next_append(self):
        self.completed()
        log=self.journal/'events.jsonl';lines=log.read_bytes().splitlines(keepends=True)
        log.write_bytes(lines[0]+lines[2])
        self.completed();result=self.ops.call('events')
        self.assertFalse(result['complete']);self.assertEqual(result['events'][-1]['sequence'],6)
    def test_lost_whole_segment_is_not_silently_reinitialized(self):
        self.completed();(self.journal/'events.jsonl').unlink()
        self.assertFalse(self.ops.call('events')['complete'])
        self.completed();self.assertFalse(self.ops.call('events')['complete'])
    def test_changed_valid_tail_payload_is_detected(self):
        self.completed();log=self.journal/'events.jsonl';rows=log.read_text().splitlines()
        last=json.loads(rows[-1]);last['source']='SYSTEM_RECOVERY';rows[-1]=json.dumps(last)
        log.write_text('\n'.join(rows)+'\n')
        self.assertFalse(self.ops.call('events')['complete'])
    def crash_event(self,point):
        a=self.ops.begin()
        command=[str(GUARD),str(self.ops.state/'operations.guard'),str(BB),'ash',str(APP/'lib/operation-coordinator.sh'),
          'tick',a['operationId'],a['token'],'fetching']
        p=subprocess.run(command,env=self.ops.env|{'BRORAY_OPS_TEST_JOURNAL_CRASH':point},capture_output=True,timeout=20)
        self.assertEqual(p.returncode,-9,(p.stdout,p.stderr))
        head=json.loads((self.journal/'head.json').read_text());self.assertTrue(head['pending'])
        self.assertEqual(head['allocatedSequence'],3)
        self.assertFalse(self.ops.call('events')['complete'])
        self.ops.call('finish',a['operationId'],a['token'],'completed','')
        result=self.ops.call('events');self.assertFalse(result['complete'])
        self.assertEqual(result['events'][-1]['sequence'],4)
        self.assertFalse((self.ops.temp/'global.lock').is_symlink())
    def test_coordinator_self_crash_after_reservation_leaves_gap(self):self.crash_event('reserved')
    def test_coordinator_self_crash_after_append_leaves_gap(self):self.crash_event('appended')
    def test_corrupt_watermark_cannot_be_reported_complete(self):
        self.completed();(self.journal/'head.json').write_text('{"allocatedSequence":')
        response=self.ops.call('events',expected=1)
        self.assertEqual(response['errorCode'],'JOURNAL_UNAVAILABLE')
if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Journal))
    (WORKSPACE/'docs/evidence/journal-durability-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'isolated Linux journal/coordinator, labelled fake owner identity','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
