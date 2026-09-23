"""Legacy observation-only evidence cannot authorize any stop boundary.

The former stop-time-adoption crash case is no longer reachable by contract.
Its positive safety requirements are exercised in
test_preflight_generation_authorization.py (durable binding/owner loss/native
crash/lost acknowledgement) and test_updater_generation.py (EXITKILL).
All processes below are private offline fixtures.
"""
import json,unittest
from test_preflight_service_stop import BoundStop

class AuthorizationBoundary(BoundStop):
 def refuse_boundary(self,command,live):
  service=None;script=None
  if live:service,script=self.start_service()
  before=script.read_bytes() if script else None
  # Valid coordinator operation/token/nonce; legacy refusal must happen before
  # looking for any stop-time supervisor or authorizing signals.
  prefix='cp "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/platform-service.json" "$TEST_HOME/boundary-before"\n'
  r=self.run_binding(prefix+command)
  self.assert_legacy_refusal(r,service)
  self.assertIn('UPDATER_LEGACY_REBOOT_REQUIRED',r.stdout+r.stderr)
  self.assertEqual((self.home/'boundary-before').read_bytes(),(self.operation()/'platform-service.json').read_bytes())
  self.assertFalse((self.operation()/'supervisors.json').exists())
  if script:self.assertEqual(script.read_bytes(),before)
  print('AUTHORIZATION_BOUNDARY_EVIDENCE '+json.dumps({'test':self.id(),'legacyRefused':True,'signalsAuthorized':False,'phase':'STOP_INTENT','fenceRetained':True,'platformUnchanged':True,'liveFixturePreserved':bool(service),'routerAccess':False}),flush=True)
 def registration(self,live):
  self.refuse_boundary('broray_ops_call updater-stop-supervisor-register "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',live)
 def authorization(self,live):
  self.refuse_boundary('broray_ops_call platform-preflight-stop-authorize "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "$$" "$$"',live)
 def stopped_boundary(self,live):
  self.refuse_boundary('broray_ops_call platform-preflight-service-stopped "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" "$BRORAY_PREFLIGHT_STOP_NONCE"',live)
 def test_live_legacy_cannot_register_stop_supervisor(self):self.registration(True)
 def test_absent_legacy_cannot_register_stop_supervisor(self):self.registration(False)
 def test_live_legacy_cannot_authorize_signals(self):self.authorization(True)
 def test_absent_legacy_cannot_authorize_signals(self):self.authorization(False)
 def test_live_legacy_cannot_confirm_stopped(self):self.stopped_boundary(True)
 def test_absent_legacy_cannot_confirm_stopped(self):self.stopped_boundary(False)

if __name__=='__main__':
 suite=unittest.TestSuite(AuthorizationBoundary(n) for n in sorted(AuthorizationBoundary.__dict__) if n.startswith('test_'))
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
 raise SystemExit(0 if result.wasSuccessful() else 1)
