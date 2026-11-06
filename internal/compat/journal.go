package compat

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Journal is the operation event journal (lib/operation-journal.sh):
// JSON lines with consecutive sequence numbers, rotated at 256 KiB into
// events.1.jsonl / events.2.jsonl; the API returns the last 500 events.
//
// Gap handling (operation-journal.sh:130-166): a record that could not be
// persisted, an unreadable line or non-consecutive sequences mark a gap.
// The gap marker (file "gap", the latest missing sequence) survives restarts;
// events.cgi reports complete:false + errors:["JOURNAL_GAP"] while the gap
// lies inside the returned window.
type Journal struct {
	mu    sync.Mutex
	dir   string
	seq   int64
	total int64
	gap   int64            // latest missing sequence, 0 = none
	tail  []map[string]any // last 500 stored records
}

const (
	journalSegment = 262144
	journalKeep    = 500
	journalRecord  = 2048
)

func NewJournal(dir string) *Journal {
	j := &Journal{dir: dir}
	_ = os.MkdirAll(dir, 0o700)
	if b, err := os.ReadFile(filepath.Join(dir, "gap")); err == nil {
		if n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && n > 0 {
			j.gap = n
		} else {
			j.gap = 1
		}
	}
	for _, name := range []string{"events.2.jsonl", "events.1.jsonl", "events.jsonl"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 4096), 4096)
		for sc.Scan() {
			var ev map[string]any
			if json.Unmarshal(sc.Bytes(), &ev) != nil || ev == nil {
				j.markGapLocked(j.seq+1, false)
				continue
			}
			n, ok := ev["sequence"].(float64)
			if !ok || n <= 0 || n != float64(int64(n)) {
				j.markGapLocked(j.seq+1, false)
				continue
			}
			if j.seq != 0 && int64(n) != j.seq+1 {
				j.markGapLocked(j.seq+1, false)
			}
			if int64(n) > j.seq {
				j.seq = int64(n)
			}
			j.total++
			j.push(ev)
		}
		if sc.Err() != nil {
			j.markGapLocked(j.seq+1, false)
		}
		f.Close()
	}
	return j
}

func (j *Journal) push(ev map[string]any) {
	j.tail = append(j.tail, ev)
	if len(j.tail) > journalKeep {
		j.tail = j.tail[len(j.tail)-journalKeep:]
	}
}

// markGapLocked records a missing sequence. Only the latest gap matters:
// the window is incomplete iff it starts at or before the latest gap.
func (j *Journal) markGapLocked(seq int64, persist bool) {
	if seq > j.gap {
		j.gap = seq
	}
	if persist {
		_ = os.WriteFile(filepath.Join(j.dir, "gap"), []byte(strconv.FormatInt(j.gap, 10)+"\n"), 0o600)
	}
}

// Append records an event for an operation. result: success|failure|cancelled|pending.
func (j *Journal) Append(o *Operation, event, result, message string) {
	o.mu.Lock()
	var ec any
	if o.ErrorCode != "" {
		ec = o.ErrorCode
	}
	ev := map[string]any{"eventId": nil, "timestamp": StampUTC(time.Now()), "operationId": o.ID, "operationType": o.Type,
		"source": o.Source, "event": event, "pid": os.Getpid(), "result": result, "errorCode": ec, "message": message}
	o.mu.Unlock()
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	j.total++
	ev["sequence"] = j.seq
	j.push(ev)
	b, err := Marshal(ev)
	if err == nil && len(b) > journalRecord {
		ev["message"] = "" // the public message is derived from the event name
		b, err = Marshal(ev)
	}
	if err != nil || len(b) > journalRecord {
		j.markGapLocked(j.seq, true)
		return
	}
	path := filepath.Join(j.dir, "events.jsonl")
	if st, err := os.Stat(path); err == nil && st.Size()+int64(len(b)) > journalSegment {
		_ = os.Rename(filepath.Join(j.dir, "events.1.jsonl"), filepath.Join(j.dir, "events.2.jsonl"))
		_ = os.Rename(path, filepath.Join(j.dir, "events.1.jsonl"))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		j.markGapLocked(j.seq, true)
		return
	}
	if _, err := f.Write(b); err != nil {
		j.markGapLocked(j.seq, true)
	}
	if err := f.Close(); err != nil {
		j.markGapLocked(j.seq, true)
	}
}

// Events returns the events.cgi object.
func (j *Journal) Events() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	evs := make([]map[string]any, 0, len(j.tail))
	for _, ev := range j.tail {
		evs = append(evs, EventPublic(ev))
	}
	var first, last any
	var firstSeq int64
	if len(evs) > 0 {
		first, last = evs[0]["sequence"], evs[len(evs)-1]["sequence"]
		firstSeq = asInt64(j.tail[0]["sequence"])
	}
	complete := j.gap == 0 || (len(evs) > 0 && j.gap < firstSeq)
	errs := []string{}
	if !complete {
		errs = append(errs, "JOURNAL_GAP")
	}
	return map[string]any{"ok": true, "complete": complete, "truncated": j.total > int64(len(evs)), "nextCursor": nil,
		"firstSequence": first, "lastSequence": last, "errors": errs, "events": evs}
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		if n == float64(int64(n)) {
			return int64(n)
		}
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

var (
	reHex32     = regexp.MustCompile(`^[0-9a-f]{32}$`)
	reTimestamp = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
	eventNames  = []string{"started", "lock_acquired", "lock_conflict", "phase_changed", "owner_transferred", "cancel_requested",
		"completed", "failed", "aborted", "recovered", "ambiguous_owner", "heartbeat_problem", "term", "kill"}
	eventMessages = map[string]string{
		"started":           "Операция запущена",
		"lock_acquired":     "Ресурсы зарезервированы",
		"lock_conflict":     "Действие отложено: выполняется другая операция",
		"cancel_requested":  "Запрошена остановка операции",
		"completed":         "Операция завершена",
		"failed":            "Операция завершилась с ошибкой",
		"aborted":           "Операция прервана",
		"recovered":         "Незавершённая операция восстановлена",
		"ambiguous_owner":   "Состояние владельца не удалось подтвердить",
		"heartbeat_problem": "Нет свежего сообщения о ходе операции",
		"term":              "Управляемому процессу отправлен сигнал завершения",
		"kill":              "Управляемый процесс принудительно завершён",
		"phase_changed":     "Этап операции изменился",
		"owner_transferred": "Операция передана фоновому исполнителю",
	}
)

// PublicTimestamp returns s when it is a YYYY-MM-DDTHH:MM:SSZ stamp, else nil.
func PublicTimestamp(v any) any {
	if s, ok := v.(string); ok && reTimestamp.MatchString(s) {
		return s
	}
	return nil
}

// EventPublic is event_public (lib/operation-public.jq:78-86).
func EventPublic(ev map[string]any) map[string]any {
	str := func(k string) string { s, _ := ev[k].(string); return s }
	name := enumOr(str("event"), "unknown", eventNames...)
	msg, ok := eventMessages[name]
	if !ok {
		msg = "Событие не распознано"
	}
	var seq, pid, eventID, ec any
	if n := asInt64(ev["sequence"]); n > 0 && n <= 9007199254740991 {
		seq = n
	}
	if n := asInt64(ev["pid"]); n > 1 && n <= 2147483647 {
		pid = n
	}
	if s := str("eventId"); reHex32.MatchString(s) {
		eventID = s
	}
	if s := str("errorCode"); enumHas(s, publicErrorCodes...) {
		ec = s
	}
	return map[string]any{"sequence": seq, "eventId": eventID, "timestamp": PublicTimestamp(ev["timestamp"]),
		"operationId": PublicOperationID(str("operationId")), "operationType": PublicType(str("operationType")),
		"source": PublicSource(str("source")), "event": name, "pid": pid,
		"result":    enumOr(str("result"), "unknown", "success", "failure", "cancelled", "pending", "unknown"),
		"errorCode": ec, "message": msg}
}
