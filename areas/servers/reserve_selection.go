package servers

import (
	"encoding/hex"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
)

type reserveSelection struct {
	server string
	mode   string
	remove string
}

func proposedReserveSelection(prefs reservePreferencesSnapshot, choice reserveSelection) (reservePreferences, *cgi.APIError) {
	next := prefs.reservePreferences
	member := reserveRosterContains(prefs, choice.server)
	next.Emergency = nil
	switch choice.mode {
	case "":
		if !member || choice.remove != "" {
			return next, reserveOutsideRosterError()
		}
	case "ordinary":
		if member || choice.remove != "" {
			return next, cgi.E(409, "RESERVE_SELECTION_INVALID", "Выберите сервер вне сохранённого состава.")
		}
		next.Enabled = false
	case "promote":
		mustRemove := !member && len(prefs.BackupServerIDs) == 2
		if (mustRemove && (choice.remove == "" || !reserveRosterContains(prefs, choice.remove))) ||
			(!mustRemove && choice.remove != "") {
			return next, cgi.E(409, "RESERVE_REMOVAL_REQUIRED", "Укажите, какой сервер исключить из заполненного состава.")
		}
		next.PrimaryServerID = choice.server
		next.BackupServerIDs = make([]string, 0, 2)
		for _, old := range append([]string{prefs.PrimaryServerID}, prefs.BackupServerIDs...) {
			if old != choice.server && old != choice.remove {
				next.BackupServerIDs = append(next.BackupServerIDs, old)
			}
		}
	default:
		return next, cgi.E(400, "INVALID_REQUEST", "Некорректное действие резервирования.")
	}
	return next, nil
}

func (s *svc) handleConfirmedReserveSelection(c *cgi.Req) {
	if _, ok := compat.OpsBody(c); !ok {
		return
	}
	value, err := strictjson.Decode(c.Body())
	m, ok := value.(map[string]any)
	id, idOK := m["id"].(string)
	mode, modeOK := m["mode"].(string)
	remove, removeOK := m["removeServerId"].(string)
	revision, revOK := m["expectedRevision"].(string)
	nonce, nonceOK := m["nonce"].(string)
	if err != nil || !ok || len(m) != 5 || !idOK || !modeOK || !removeOK || !revOK || !nonceOK ||
		!BackupIDValid(id) || (mode != "promote" && mode != "ordinary") ||
		(remove != "" && !BackupIDValid(remove)) || len(revision) != 64 || len(nonce) != 32 {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректное действие резервирования."))
		return
	}
	if _, err = hex.DecodeString(revision); err != nil {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректная ревизия состава."))
		return
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil || !prefs.Enabled || prefs.Revision != revision {
		cgi.WriteErr(c.W, cgi.E(409, "SETTINGS_CHANGED", "Состав или режим резервирования изменился. Обновите состояние."))
		return
	}
	if _, problem := proposedReserveSelection(prefs, reserveSelection{server: id, mode: mode, remove: remove}); problem != nil {
		cgi.WriteErr(c.W, problem)
		return
	}
	record, err := ReadRecord(s.d, id)
	if err != nil {
		cgi.WriteErr(c.W, cgi.E(404, "SERVER_NOT_FOUND", "Сервер не найден."))
		return
	}
	if err := s.requireRuntimeSupport(s.d.Ctx, record); err != nil {
		cgi.WriteErr(c.W, cgi.MapError(err))
		return
	}
	hash, err := fileSHA256(RecordPath(s.d, id))
	if err != nil {
		cgi.WriteErr(c.W, cgi.E(503, "STATE_UNAVAILABLE", "Не удалось подтвердить сервер."))
		return
	}
	request, coalesced, err := s.d.Queue.Submit(compat.ReserveSelectAction, id, "USER", revision+":"+hash+":"+mode+":"+remove, nonce)
	if err != nil {
		compat.WriteOpsError(c.W, err)
		return
	}
	receipt := request.Receipt(coalesced)
	receipt["accepted"] = true
	cgi.WriteJSON(c.W, 202, receipt)
}
