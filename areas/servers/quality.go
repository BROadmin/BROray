package servers

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

// ---------- freshness (server-service.sh:25-72, status-contract.sh:155-170) ----------

// qualityPolicy returns the stale/expired thresholds in seconds.
func (s *svc) qualityPolicy() (stale, expired int64) {
	enabled, interval := false, int64(60)
	if cfg, ok := readObjectFile(autoSwitchFile(s.d)); ok {
		if b, ok := cfg["qualityRefreshEnabled"].(bool); ok {
			enabled = b
		}
		if v := jqRaw(alt(cfg["qualityRefreshIntervalMinutes"], json60)); in(v, "30", "60", "180", "360") {
			interval, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	stale, expired = 1800, 10800
	if enabled {
		stale = interval*60 + 900
		expired = interval * 60 * 3
		if expired < 10800 {
			expired = 10800
		}
	}
	if v, err := strconv.ParseInt(os.Getenv("BRORAY_SERVER_QUALITY_STALE_SECONDS"), 10, 64); err == nil {
		stale = v
	}
	if v, err := strconv.ParseInt(os.Getenv("BRORAY_SERVER_QUALITY_EXPIRED_SECONDS"), 10, 64); err == nil {
		expired = v
	}
	return stale, expired
}

// Freshness is broray_status_freshness_from_age.
func Freshness(age, stale, expired int64) string {
	switch {
	case age <= stale:
		return "fresh"
	case age <= expired:
		return "stale"
	}
	return "expired"
}

func freshRank(f string) int {
	switch f {
	case "expired":
		return 40
	case "stale":
		return 30
	case "fresh":
		return 10
	}
	return 20
}

func worstFreshness(vals ...string) string {
	worst := "fresh"
	for _, v := range vals {
		if freshRank(v) > freshRank(worst) {
			worst = v
		}
	}
	return worst
}

func (s *svc) age(mtime time.Time) int64 {
	a := s.now().Unix() - mtime.Unix()
	if a < 0 {
		return 0
	}
	return a
}

var json60 = jqNumberJSON("60")

func defaultQuality() map[string]any {
	return map[string]any{"status": "unknown", "ping": nil, "jitter": nil, "disconnects": 0, "successfulChecks": 0,
		"failedChecks": 0, "lastCheckedAt": nil, "lastSuccessAt": nil, "durationMs": nil, "rating": "unknown", "error": nil,
		"measurementMethod": "https-proxy+icmp", "measurementSource": "unknown"}
}

// qualityView is the summary row quality (server-service.sh:281-305):
// defaults + file (unparseable → {}) + freshness from the file mtime.
func (s *svc) qualityView(id string, stale, expired int64) map[string]any {
	q := defaultQuality()
	freshness, age := "unknown", any(nil)
	raw, mtime, ok := readQualityRaw(s.d, id)
	var file map[string]any
	if ok {
		file, _ = decodeObject(raw)
		a := s.age(mtime)
		age, freshness = a, Freshness(a, stale, expired)
	}
	for k, v := range file {
		q[k] = v
	}
	q["freshness"], q["ageSeconds"] = freshness, age
	q["stale"] = freshness == "stale" || freshness == "expired"
	q["measurementMethod"] = alt(file["measurementMethod"], "https-proxy+icmp")
	q["measurementSource"] = alt(file["measurementSource"], "unknown")
	return q
}

// detailsQuality is broray_server_get_quality: an invalid file yields the defaults.
func (s *svc) detailsQuality(id string, stale, expired int64) map[string]any {
	raw, mtime, ok := readQualityRaw(s.d, id)
	var file map[string]any
	if ok {
		file, ok = decodeObject(raw)
	}
	if !ok {
		q := defaultQuality()
		q["freshness"], q["ageSeconds"], q["stale"] = "unknown", nil, true
		return q
	}
	a := s.age(mtime)
	f := Freshness(a, stale, expired)
	for k, v := range map[string]any{"freshness": f, "ageSeconds": a, "stale": f == "stale" || f == "expired"} {
		file[k] = v
	}
	file["measurementMethod"] = alt(file["measurementMethod"], "https-proxy+icmp")
	file["measurementSource"] = alt(file["measurementSource"], "unknown")
	return file
}

// ---------- one measurement (broray_server_check + broray_server_measure) ----------

// errCatalogChanged is 3.x rc 76 (snapshot/context no longer matches).
var errCatalogChanged = errors.New("SERVER_CATALOG_CHANGED: Список серверов изменился во время проверки.")

// checkResult is the 3.x check result object.
type checkResult struct {
	Object  map[string]any
	Success bool
}

// checkServer measures one server through a temporary Xray and publishes the
// quality record. source: manual|auto-switch|scheduled. fingerprint (sha256 of
// the record file) binds a queued snapshot; "" = direct check.
func (s *svc) checkServer(ctx context.Context, id, source, fingerprint string) (checkResult, error) {
	if !in(source, "manual", "auto-switch", "scheduled") {
		return checkResult{}, opFailed("")
	}
	if err := validateID(id); err != nil {
		return checkResult{}, opFailed(err.Text())
	}
	if !RecordExists(s.d, id) {
		return checkResult{}, opFailed("")
	}
	path := RecordPath(s.d, id)
	if fingerprint != "" {
		if h, err := fileSHA256(path); err != nil || h != fingerprint {
			return checkResult{}, errCatalogChanged
		}
	}
	mctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	res, err := s.measureOnce(mctx, id, source)
	cancel()
	if err != nil {
		return checkResult{}, err
	}
	if ctx.Err() != nil {
		return checkResult{}, ctx.Err()
	}
	q := res.Object["quality"].(map[string]any)
	if fingerprint == "" && ActiveID(s.d) == id {
		_, ah, _ := s.activeMeasure(ctx, id)
		q["activeHealth"] = ah
	}
	if fingerprint != "" {
		if h, err := fileSHA256(path); err != nil || h != fingerprint {
			return checkResult{}, errCatalogChanged
		}
		q["serverFingerprint"] = fingerprint
	}
	if err := writeQuality(s.d, id, q); err != nil {
		return checkResult{}, err
	}
	res.Object["quality"] = q
	return res, nil
}

// errMeasureTimeout is 3.x rc 124 (helper timeout; the old quality is kept).
var errMeasureTimeout = errors.New("SERVER_CHECK_TIMEOUT: Время проверки истекло. Прежняя оценка сервера сохранена.")

func (s *svc) measureOnce(ctx context.Context, id, source string) (checkResult, error) {
	started := s.now()
	rec, err := ReadRecord(s.d, id)
	success, stage, checkErr := false, "validation", ""
	address := ""
	if rec != nil {
		address = jqRawEmpty(rec["address"])
	}
	if err != nil {
		checkErr = "Ошибка: неправильный JSON: " + RecordPath(s.d, id)
	} else if verr := validate(rec, RecordPath(s.d, id)); verr != nil {
		checkErr = verr.Text()
	} else {
		stage = "xray-config"
		cfg, gerr := XrayConfig(rec, s.genSettings(ctx), RecordPath(s.d, id))
		if gerr != nil {
			checkErr = gerr.Error()
		} else if terr := s.testBytes(ctx, cfg); terr != nil {
			checkErr = terr.Error()
		} else {
			stage = "proxy-https"
			pr := s.measure(ctx, rec, cfg)
			if pr.Success {
				success = true
				stage = orStr(pr.Stage, "proxy-https")
			} else {
				stage = orStr(pr.Stage, "proxy-https")
				checkErr = orStr(pr.Error, "Реальный запрос через сервер завершился ошибкой.")
			}
		}
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return checkResult{}, errMeasureTimeout
		}
		return checkResult{}, ctx.Err()
	}
	var ping, jitter any
	if success && address != "" {
		ping, jitter = parsePing(s.ping(ctx, address, 3, 0))
	}
	finished := s.now()
	checkedAt := compat.Stamp(finished)
	stale, expired := s.qualityPolicy()
	old := s.detailsQuality(id, stale, expired)
	counter := func(k string) int64 {
		f, ok := num(alt(old[k], 0))
		if !ok {
			return 0
		}
		return int64(f)
	}
	succ, failed, disc := counter("successfulChecks"), counter("failedChecks"), counter("disconnects")
	q := map[string]any{"ping": ping, "jitter": jitter, "disconnects": disc, "lastCheckedAt": checkedAt,
		"durationMs": (finished.Unix() - started.Unix()) * 1000, "measurementMethod": "https-proxy+icmp", "measurementSource": source}
	if success {
		q["status"], q["rating"], q["lastSuccessAt"], q["error"] = "available", rating(ping), checkedAt, nil
		succ++
	} else {
		failed++
		q["status"], q["rating"] = "unavailable", "unavailable"
		if ls := jqRawEmpty(old["lastSuccessAt"]); ls != "" {
			q["lastSuccessAt"] = ls
		} else {
			q["lastSuccessAt"] = nil
		}
		q["error"] = orStr(checkErr, "Проверка завершилась ошибкой")
	}
	q["successfulChecks"], q["failedChecks"] = succ, failed
	return checkResult{Success: success, Object: map[string]any{"serverId": id, "success": success, "stage": stage,
		"quality": q, "checkedAt": checkedAt}}, nil
}

func jqNumberJSON(s string) any { return jqNumber(s) }
