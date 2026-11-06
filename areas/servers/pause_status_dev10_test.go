package servers

import "testing"

func TestAutoSwitchDev10FreshPauseStatus(t *testing.T) {
	e := newEnv(t)
	for _, paused := range []bool{false, true, false} {
		if err := e.d.Queue.SetPaused(paused); err != nil {
			t.Fatal(err)
		}
		r := call(t, "GET", "/api/servers/auto-switch-status.cgi", "", nil)
		if r.Code != 200 || get(r.Body, "data", "automation", "complete") != true || get(r.Body, "data", "automation", "paused") != paused {
			t.Errorf("fresh paused=%v not represented: %s", paused, r.Raw)
		}
	}
}
