package servers

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

type dpiRequest struct {
	ID       string `json:"id"`
	Enabled  bool   `json:"enabled"`
	Revision string `json:"expectedRevision"`
	Nonce    string `json:"nonce"`
}

func decodeDPIRequest(raw []byte) (dpiRequest, error) {
	v, e := strictjson.Decode(raw)
	m, ok := v.(map[string]any)
	if e != nil || !ok || len(m) != 4 {
		return dpiRequest{}, errReserveRuntime
	}
	id, iok := m["id"].(string)
	enabled, eok := m["enabled"].(bool)
	revision, rok := m["expectedRevision"].(string)
	nonce, nok := m["nonce"].(string)
	if !iok || !eok || !rok || !nok || !ValidID(id) || len(revision) != 64 || len(nonce) != 32 {
		return dpiRequest{}, errReserveRuntime
	}
	if b, e := hex.DecodeString(revision); e != nil || hex.EncodeToString(b) != revision {
		return dpiRequest{}, errReserveRuntime
	}
	if b, e := hex.DecodeString(nonce); e != nil || hex.EncodeToString(b) != nonce {
		return dpiRequest{}, errReserveRuntime
	}
	return dpiRequest{id, enabled, revision, nonce}, nil
}
func (s *svc) defineDPIActions() {
	s.d.Queue.Define(compat.ActionSpec{Name: compat.DPIProfileAction, Type: "server_operation", Priority: map[string]int{"*": 1}, Stages: []compat.Stage{{Name: "profile", Phase: "switching", Global: true, Protected: true, Run: s.dpiProfileStep}}})
}
func (s *svc) handleDPIProfile(c *cgi.Req) {
	if _, ok := compat.OpsBody(c); !ok {
		return
	}
	request, e := decodeDPIRequest(c.Body())
	if e != nil {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректные параметры обхода DPI."))
		return
	}
	raw, e := json.Marshal(request)
	if e != nil {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректные параметры обхода DPI."))
		return
	}
	row, coalesced, e := s.d.Queue.Submit(compat.DPIProfileAction, compat.ReserveOperationTarget, "USER", string(raw), request.Nonce)
	if e != nil {
		compat.WriteOpsError(c.W, e)
		return
	}
	receipt := row.Receipt(coalesced)
	receipt["accepted"] = true
	cgi.WriteJSON(c.W, 202, receipt)
}
func (s *svc) dpiProfileStep(t *compat.Task) error {
	request, e := decodeDPIRequest([]byte(t.Context))
	if e != nil {
		return stepError(t, "INVALID_REQUEST", "Некорректный запрос обхода DPI.")
	}
	e = s.applyDPI(t.Ctx, request.ID, request.Revision, request.Enabled)
	if e != nil {
		if errors.Is(e, errReserveTransactionPending) || errors.Is(e, errReserveProbeCleanup) || errors.Is(e, reserve.ErrProbeWorkspaceUnconfirmed) {
			return errors.Join(errReserveTransactionPending, e)
		}
		if errors.Is(e, errDPIApplyRolledBack) {
			t.Result("Обход DPI не изменён. Восстановлены предыдущие параметры подключения.", "DPI_ROLLED_BACK")
			return e
		}
		var apiError *cgi.APIError
		if errors.As(e, &apiError) {
			t.Result(apiError.Message, apiError.Code)
			return e
		}
		t.Result("Не удалось применить обход DPI. Обновите состояние сервера.", "DPI_NOT_APPLIED")
		return e
	}
	if s.reserveMon != nil {
		s.resetReserveMonitor()
	}
	t.Result("Настройка обхода DPI сохранена.", "")
	return nil
}
func (s *svc) dpiStatus(c *cgi.Req) (any, error) {
	id := detailsID(c.RawQuery)
	if !ValidID(id) {
		return nil, cgi.E(400, "INVALID_REQUEST", "Некорректный сервер.")
	}
	image, raw, e := readInactiveImage(RecordPath(s.d, id), false)
	if e != nil {
		return nil, cgi.E(404, "SERVER_NOT_FOUND", "Сервер не найден.")
	}
	v, e := strictjson.Decode(raw)
	rec, ok := v.(map[string]any)
	if e != nil || !ok || rec["id"] != id || !image.unchanged() {
		return nil, cgi.E(409, "STATE_CHANGED", "Состояние сервера изменилось.")
	}
	pending, e := reserve.TransactionPending(c.R.Context(), s.d.Root)
	if e != nil {
		return nil, e
	}
	result := dpiPresentation(rec)
	result["id"] = id
	result["revision"] = image.hash
	result["paused"] = s.d.Queue.Paused()
	result["recoveryRequired"] = pending || s.d.Lock.Ambiguous()
	return result, nil
}
