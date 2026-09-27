"""Focused existing retention/guard regressions; no unrelated protocol suites."""
import importlib,unittest

def load_tests(loader,tests,pattern):
 suite=unittest.TestSuite()
 for module,cls,names in [
  ('test_operations','Operations',['test_history_prunes_only_confirmed_dead_terminal_owners','test_history_prune_preserves_live_and_corrupt_records','test_history_byte_budget_removes_only_dead_terminal_records']),
  ('test_live_audit_regressions','LiveAudit',['test_log_maintenance_preserves_platform_background_and_unknown_evidence']),
  ('test_monitor_service','MonitorService',['test_real_monitor_start_stop_publishes_status','test_busy_cycle_returns_pending_then_stops_without_signals','test_stop_leaves_unrelated_persistent_process_alive']),
  ('test_route_resource_locks','RouteLocks',['test_all_eight_preserve_unknown_existing_lock','test_dead_pid_and_old_boot_evidence_are_not_reclaimed','test_kernel_guard_conflict_is_bounded_and_next_owner_can_acquire'])]:
  case=getattr(importlib.import_module(module),cls)
  for name in names:suite.addTest(case(name))
 return suite

if __name__=='__main__':unittest.main(verbosity=2)
