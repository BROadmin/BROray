package ops

import (
	"encoding/json"
	"net/http"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

// /api/operations/*.cgi (api/operations/common.sh + lib/operation-coordinator.sh)
// over the in-process coordinator d.Queue. Common request checks are
// compat.OpsBody; coordinator objects keep the D shape and the status
// mapping (POST success 202 except automation/queue-lookup 200; errors via
// compat.WriteOpsError).

func registerOperations() {
	cgi.RegisterRaw("/api/operations/status.cgi", func(c *cgi.Req) {
		m, ok := compat.OpsBody(c)
		if !ok {
			return
		}
		a := current()
		if c.R.Method == http.MethodPost {
			a.queueLookup(c, m)
			return
		}
		snap := a.d.Queue.Snapshot()
		status := 200
		if snap["ok"] != true {
			status = 503
		}
		compat.WriteOps(c.W, status, snap)
	}, cgi.Methods(http.MethodGet, http.MethodPost))
	cgi.RegisterRaw("/api/operations/events.cgi", func(c *cgi.Req) {
		if _, ok := compat.OpsBody(c); !ok {
			return
		}
		compat.WriteOps(c.W, 200, current().d.Queue.Journal().Events())
	})
	cgi.RegisterRaw("/api/operations/report.cgi", func(c *cgi.Req) {
		if _, ok := compat.OpsBody(c); !ok {
			return
		}
		current().reportHandler(c)
	})
	cgi.RegisterRaw("/api/operations/cancel.cgi", func(c *cgi.Req) {
		m, ok := compat.OpsBody(c)
		if !ok {
			return
		}
		current().cancel(c, m)
	}, cgi.POST())
	cgi.RegisterRaw("/api/operations/stop-background.cgi", func(c *cgi.Req) {
		m, ok := compat.OpsBody(c)
		if !ok {
			return
		}
		if !exactTrue(m, "pauseAutomation") {
			cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Требуется пауза автоматики."))
			return
		}
		out, err := current().d.Queue.StopBackground()
		if err != nil {
			compat.WriteOpsError(c.W, err)
			return
		}
		compat.WriteOps(c.W, 202, out)
	}, cgi.POST())
	cgi.RegisterRaw("/api/operations/automation.cgi", func(c *cgi.Req) {
		m, ok := compat.OpsBody(c)
		if !ok {
			return
		}
		var paused bool
		if len(m) != 1 || m["paused"] == nil || json.Unmarshal(m["paused"], &paused) != nil || !isJSONBool(m["paused"]) {
			cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректное состояние автоматики."))
			return
		}
		if err := current().d.Queue.SetPaused(paused); err != nil {
			compat.WriteOpsError(c.W, &compat.QueueError{Code: "STATE_UNAVAILABLE"})
			return
		}
		compat.WriteOps(c.W, 200, map[string]any{"ok": true})
	}, cgi.POST())
	cgi.RegisterRaw("/api/operations/recover.cgi", func(c *cgi.Req) {
		m, ok := compat.OpsBody(c)
		if !ok {
			return
		}
		if len(m) != 0 {
			cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Параметры восстановления не поддерживаются."))
			return
		}
		a := current()
		out, err := a.d.Queue.EmergencyRecover(func() string {
			if a.page.running() {
				return "updater_pending"
			}
			return ""
		})
		if err != nil {
			status := 503
			if qe, ok := err.(*compat.QueueError); ok {
				status = qe.HTTPStatus()
			}
			compat.WriteOps(c.W, status, out)
			return
		}
		compat.WriteOps(c.W, 202, out)
	}, cgi.POST())
}

func isJSONBool(raw json.RawMessage) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	_, ok := v.(bool)
	return ok
}

// exactTrue: the body is exactly {"<key>":true}.
func exactTrue(m map[string]json.RawMessage, key string) bool {
	if len(m) != 1 {
		return false
	}
	var v any
	if raw, ok := m[key]; !ok || json.Unmarshal(raw, &v) != nil {
		return false
	}
	return v == true
}

// exactString: the body is exactly {"<key>":"<string>"}.
func exactString(m map[string]json.RawMessage, key string) (string, bool) {
	if len(m) != 1 {
		return "", false
	}
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// queueLookup is POST status.cgi (queue-lookup): body exactly {"nonce":<32 hex>}.
func (a *area) queueLookup(c *cgi.Req, m map[string]json.RawMessage) {
	nonce, ok := exactString(m, "nonce")
	if !ok || !reHex32Token.MatchString(nonce) {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректный идентификатор запроса."))
		return
	}
	r, ok := a.d.Queue.Lookup(nonce)
	if !ok {
		compat.WriteOpsError(c.W, &compat.QueueError{Code: "REQUEST_UNCONFIRMED"})
		return
	}
	compat.WriteOps(c.W, 200, r.Receipt(r.Nonce != nonce))
}

// cancel is cancel.cgi: {"operationId":<op id>} or {"requestId":"q-<32 hex>"}.
func (a *area) cancel(c *cgi.Req, m map[string]json.RawMessage) {
	if id, ok := exactString(m, "operationId"); ok && compat.ValidOperationID(id) {
		out, err := a.d.Queue.CancelOperation(id)
		if err != nil {
			compat.WriteOpsError(c.W, err)
			return
		}
		compat.WriteOps(c.W, 202, out)
		return
	}
	if id, ok := exactString(m, "requestId"); ok && compat.PublicRequestID(id) != nil {
		r, err := a.d.Queue.CancelRequest(id)
		if err != nil {
			compat.WriteOpsError(c.W, err)
			return
		}
		compat.WriteOps(c.W, 202, map[string]any{"ok": true, "requestId": r.RequestID, "state": "cancelled"})
		return
	}
	cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректный идентификатор операции."))
}
