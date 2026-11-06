package xray

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

// handleDiagnostics is X7 (api/xray/diagnostics.cgi).
func (s *service) handleDiagnostics(c *cgi.Req) (any, error) {
	ctx := c.R.Context()
	status := s.computeStatus(ctx)
	v := s.validate(ctx)
	bin, cfg := s.d.Root.XrayBinary(), s.d.Root.XrayConfig()
	_, binExists := regular(bin)
	exe := executable(bin)
	cfgExists := fileExists(cfg)
	sum := ""
	if binExists {
		sum = fileSHA256(bin)
	}
	socks, _ := status["socks"].(map[string]any)
	optKB, tmpKB := freeKB(s.d.Root.Dir), freeKB("/tmp")
	lastOK, lastFile := false, ""
	for _, cand := range []string{s.d.RunPath("xray-reinstall-test.json"), s.d.RunPath("xray-lifecycle-test.json")} {
		if fileExists(cand) {
			lastFile = cand
			if m, ok := compat.ReadObject(cand); ok && m["success"] == true {
				// a recorded trial counts only for the binary it tested
				if h, _ := m["sha256"].(string); h == "" || h == sum {
					lastOK = true
					if d, _ := m["details"].(string); d != "" {
						lastFile = d
					}
				}
			}
			break
		}
	}
	check := func(id string, ok bool, bad, title, okText, badText string) map[string]any {
		st, d := "ok", okText
		if !ok {
			st, d = bad, badText
		}
		return map[string]any{"id": id, "status": st, "title": title, "details": d}
	}
	tier := func(kb, okMin, warnMin int64) string {
		switch {
		case kb >= okMin:
			return "ok"
		case kb >= warnMin:
			return "warning"
		}
		return "error"
	}
	lastDetails := "Успешный результат испытания не найден."
	if lastOK {
		lastDetails = lastFile
	}
	checks := []map[string]any{
		check("binary-exists", binExists, "error", "Бинарный файл Xray", "Файл найден.", "Файл отсутствует."),
		check("binary-executable", exe, "error", "Права запуска", "Бинарный файл исполняемый.", "Нет права исполнения."),
		check("process-running", status["running"] == true, "warning", "Процесс Xray", "Процесс работает.", "Процесс остановлен."),
		check("config-exists", cfgExists, "error", "Конфигурация", "Файл конфигурации найден.", "Файл конфигурации отсутствует."),
		check("config-valid", v.Success, "error", "Проверка конфигурации", "Configuration OK.", "Конфигурация не прошла проверку."),
		check("socks-active", socks["active"] == true, "warning", "SOCKS", "Локальный SOCKS-порт активен.", "SOCKS-порт не подтверждён."),
		check("sha256", sum != "", "warning", "SHA-256", "Контрольная сумма вычислена.", "Контрольная сумма недоступна."),
		{"id": "opt-storage", "status": tier(optKB, 45000, 20000), "title": "Свободное место /opt", "details": strconv.FormatInt(optKB, 10) + " КБ"},
		{"id": "tmp-storage", "status": tier(tmpKB, 130000, 90000), "title": "Временное хранилище /tmp", "details": strconv.FormatInt(tmpKB, 10) + " КБ"},
		{"id": "real-device-test", "status": map[bool]string{true: "ok", false: "warning"}[lastOK], "title": "Испытание на Keenetic", "details": lastDetails},
	}
	summary := map[string]int{"ok": 0, "warning": 0, "error": 0}
	for _, ch := range checks {
		summary[ch["status"].(string)]++
	}
	return map[string]any{"checkedAt": compat.Stamp(time.Now()), "status": status, "validation": v, "binarySha256": nullStr(sum),
		"checks": checks, "summary": summary}, nil
}

// ---------- log rotation (bin/broray-log-maintenance) ----------

var (
	logMaintenanceInterval = 300 * time.Second
	logKeep                = 2
)

// logLimits are the rotated files of 3.5.0 with their size limits.
var logLimits = map[string]int64{
	"access.log":   1048576,
	"error.log":    262144,
	"xray.log":     262144,
	"brorayd.log":  262144,
	"keenetic.log": 262144,
}

func (s *service) logMaintenance(ctx context.Context) {
	t := time.NewTicker(logMaintenanceInterval)
	defer t.Stop()
	s.rotateLogs()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.rotateLogs()
			s.pruneOperations()
		}
	}
}

func (s *service) rotateLogs() {
	for name, limit := range logLimits {
		rotateFile(filepath.Join(s.d.Root.Logs(), name), limit, logKeep)
	}
}

// rotateFile keeps the last limit bytes of an oversized log as <file>.1.gz
// (older archives shift up to keep) and truncates the live file in place so
// writers keep their descriptor (rotate_file, broray-log-maintenance:28-68).
func rotateFile(path string, limit int64, keep int) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() <= limit {
		return
	}
	for i := keep; i >= 1; i-- {
		src := path + "." + strconv.Itoa(i) + ".gz"
		if i == keep {
			_ = os.Remove(src)
			continue
		}
		_ = os.Rename(src, path+"."+strconv.Itoa(i+1)+".gz")
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(-limit, io.SeekEnd); err != nil {
		return
	}
	tmp := path + ".1.gz.part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	zw := gzip.NewWriter(out)
	_, cerr := io.Copy(zw, f)
	zerr := zw.Close()
	oerr := out.Close()
	if cerr != nil || zerr != nil || oerr != nil {
		_ = os.Remove(tmp)
		return
	}
	if os.Rename(tmp, path+".1.gz") != nil {
		return
	}
	_ = os.Truncate(path, 0)
	_ = os.Chmod(path, 0o600)
}
