package ops

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/webui"
)

// The BROray page info object (broray_system_info_json, lib/broray-page.sh:
// 1504-1787) built from what 3.5.0 actually has: the running binary, the
// installer layout (versions/<v>, current, /opt/bin/broray, S24broray), the
// Xray binary, the in-process areas and the embedded WebUI. There is no
// OPKG package or app-slot updater in 3.5.0; update facts come from the
// signed release channel (update.go). The fields keep their 3.x names and
// JS semantics with honest 3.5.0 values.

const (
	projectURL = "https://docs.brovibe.cloud/broray/"
	githubURL  = "https://github.com/BROadmin/BROray"
	donateURL  = "https://pay.cloudtips.ru/p/09b23d0a"
)

var capabilities = []string{
	"Управление Xray",
	"Импорт подписок",
	"Выбор активного сервера",
	"Интеграция с управляемым ProxyN",
	"Маршрутизация сервисов",
	"Проверка и восстановление компонентов",
	"Обновление через WebUI",
	"Восстановительная переустановка текущей версии",
	"Безопасная очистка резервных копий и временных файлов",
}

// binaryPath is where the running program lives in the installer layout
// (<root>/current/broray), falling back to the executable path.
func (a *area) binaryPath() string {
	p := filepath.Join(a.d.Root.Current(), "broray")
	if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return p
}

// layout reports the installer registration (3.x "OPKG registration") and
// whether the running version is the installed one.
func (a *area) layout() (registered, consistent bool) {
	target, err := os.Readlink(a.d.Root.Current())
	if err != nil {
		return false, false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(a.d.Root.Dir, target)
	}
	bin := filepath.Join(target, "broray")
	st, err := os.Stat(bin)
	binOK := err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0
	_, linkErr := os.Stat(installer.BinLink())
	initSt, initErr := os.Stat(installer.InitScript())
	registered = binOK && linkErr == nil && initErr == nil && initSt.Mode().IsRegular()
	consistent = binOK && filepath.Base(target) == app.Version &&
		filepath.Clean(filepath.Dir(target)) == filepath.Clean(a.d.Root.Versions())
	return registered, consistent
}

func hasAreaRoutes(name string) bool {
	prefix := "/api/" + name + "/"
	for _, p := range cgi.Paths() {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

func areaLoaded(name string) bool {
	for _, n := range compat.AreaNames() {
		if n == name {
			return true
		}
	}
	return false
}

func component(id, name, path, version string, installed, healthy bool) map[string]any {
	var v any
	if version != "" {
		v = version
	}
	return map[string]any{"id": id, "name": name, "path": path, "version": v, "installed": installed, "required": true, "healthy": healthy}
}

func (a *area) xrayVersion(ctx context.Context) string {
	if a.d.App == nil || a.d.App.Xray == nil {
		return ""
	}
	return a.d.App.Xray.Status(ctx).Version
}

func (a *area) components(ctx context.Context) []any {
	bin := a.binaryPath()
	_, binErr := os.Stat(bin)
	out := []any{component("core", "Ядро BROray", bin, app.Version, binErr == nil, binErr == nil)}
	xst, xerr := os.Stat(a.d.Root.XrayBinary())
	xok := xerr == nil && xst.Mode().IsRegular() && xst.Mode().Perm()&0o111 != 0
	// Xray is installed separately (Xray page) in 3.5.0, not by the BROray
	// installer: its absence is shown on the component and the Xray card but
	// does not make the BROray installation "damaged".
	x := component("xray", "Xray", a.d.Root.XrayBinary(), a.xrayVersion(ctx), xerr == nil, xok)
	x["required"] = false
	out = append(out, x)
	for _, m := range []struct{ id, name string }{{"servers", "Серверы"}, {"subscriptions", "Подписки"}, {"keenetic", "Keenetic"}, {"routes", "Маршруты"}} {
		loaded := areaLoaded(m.id)
		out = append(out, component(m.id, m.name, bin+" ("+m.id+")", app.Version, loaded, loaded && hasAreaRoutes(m.id)))
	}
	_, webErr := fs.Stat(webui.Files, "static/home.html")
	web := component("webui", "WebUI", bin+" (webui/home.html)", webUIBuild, webErr == nil, webErr == nil)
	web["reasonCode"] = map[bool]string{true: "WEBUI_LOCAL_HEALTHY", false: "WEBUI_RUNTIME_UNHEALTHY"}[webErr == nil]
	web["publication"] = map[string]any{"state": "unknown", "consistent": false, "reason": map[string]any{"code": "WEB_PUBLISH_STATUS_UNAVAILABLE"}}
	out = append(out, web)
	out = append(out, component("updater", "Установщик", bin+" (installer)", app.Version, binErr == nil, binErr == nil))
	_, initErr := os.Stat(installer.InitScript())
	out = append(out, component("service", "Служба автозапуска", installer.InitScript(), app.Version, initErr == nil, initErr == nil))
	return out
}

func reason(code, message string) map[string]any {
	return map[string]any{"code": code, "message": message, "details": nil}
}

// architecture is the Xray/Go architecture token 3.x read from `xray version`
// ("linux/<arch>"); BROray and Xray are built for the same GOARCH.
func architecture() string { return runtime.GOARCH }

// readyHandoff is the platform-handoff status of a ready installation: 3.5.0
// has no separate updater platform, so the page shows "Универсальный".
func readyHandoff(now string) map[string]any {
	return map[string]any{"schemaVersion": 1, "contract": "broray-universal-platform-handoff/1", "state": "success", "running": false,
		"code": "UNIVERSAL_PLATFORM_READY", "message": "Обновление выполняет встроенный установщик BROray 3.5.0.",
		"mutationStarted": false, "rollbackPerformed": false, "operationId": nil, "candidateId": app.Version, "recordedAt": now}
}

// info builds the info object; enrich adds the info.cgi handoff fields
// (the home snapshot stores it without them, like `broray-system info`).
func (a *area) info(ctx context.Context, enrich bool) map[string]any {
	now := compat.StampUTC(a.now())
	registered, consistent := a.layout()
	comps := a.components(ctx)
	installationHealthy := registered
	for _, c := range comps {
		if m := c.(map[string]any); m["required"] == true && m["healthy"] != true {
			installationHealthy = false
		}
	}
	last := a.page.lastOperation()
	opRunning, opState := false, "idle"
	if last != nil {
		opRunning = last["running"] == true
		opState, _ = last["state"].(string)
	}
	// Update facts come from the last signed-channel check (update.go).
	chk, checked, checkFresh := a.checkCache()
	updateAvailable := checkFresh && chk.UpdateAvailable && registered
	reinstallSupported := checkFresh && chk.ReinstallAvailable && registered
	var availableVersion, lastCheckedAt any
	relation := "not-checked"
	if checked {
		lastCheckedAt = compat.StampUTC(chk.CheckedAt)
		relation = relation3x(chk.Relation)
		if chk.Latest != "" {
			availableVersion = chk.Latest
		}
	}
	cfg, _ := platform.LoadConfig(a.d.Root)
	channel := cfg.Update.Channel
	reasons := []any{}
	if !registered {
		reasons = append(reasons, reason("INSTALL_LAYOUT_INVALID", "Установка BROray не зарегистрирована: нет каталога текущей версии, ссылки "+installer.BinLink()+" или службы "+installer.InitScript()+"."))
	}
	if !installationHealthy {
		reasons = append(reasons, reason("INSTALLATION_INCOMPLETE", "Один или несколько обязательных компонентов BROray отсутствуют."))
	}
	if !consistent {
		reasons = append(reasons, reason("VERSION_MISMATCH", "Запущенная версия BROray не совпадает с установленной ("+a.d.Root.Current()+")."))
	}
	j, pending := a.recoveryPending()
	if pending {
		reasons = append(reasons, reason("UPDATE_RECOVERY_REQUIRED", "Операция обновления "+j.OperationID+" прервана на этапе «"+j.State+"»; она будет завершена или откатана при следующем запуске BROray (broray ctl recover)."))
	}
	if chk.CurrentRevoked && checkFresh {
		reasons = append(reasons, reason("CURRENT_VERSION_REVOKED", "Установленная версия "+app.Version+" отозвана в канале обновлений; установите обновление."))
	}
	if opState == "error" {
		reasons = append(reasons, reason("LAST_OPERATION_FAILED", "Последняя операция завершилась ошибкой; установка продолжает оцениваться отдельно."))
	}
	severity := "ok"
	switch {
	case !installationHealthy || !consistent:
		severity = "error"
	case opRunning:
		severity = "busy"
	case updateAvailable || opState == "error" || len(reasons) > 0:
		severity = "warning"
	}
	var lastOp any
	if last != nil {
		lastOp = last
	}
	facts := map[string]any{"version": app.Version, "installedPackageVersion": app.Version, "releaseId": app.Version,
		"candidateId": app.Version, "webUIBuild": webUIBuild, "updateChannel": channel, "packageTrack": "single-binary",
		"releaseManifestValid": true, "opkgRegistrationHealthy": registered, "installationHealthy": installationHealthy,
		"versionsConsistent": consistent, "backupValid": false, "updateCheckFresh": checkFresh}
	health := map[string]any{"schemaVersion": 1, "module": "broray", "availability": "available", "severity": severity,
		"operational": installationHealthy, "consistent": consistent, "actionRequired": severity != "ok" && severity != "busy",
		"freshness": map[string]any{"state": "fresh", "checkedAt": now}, "reasons": reasons, "facts": facts, "lastOperation": lastOp}
	protocols := []any{}
	for _, p := range []string{"vless", "vmess", "trojan", "hysteria2", "shadowsocks"} {
		protocols = append(protocols, map[string]any{"id": p, "supported": true})
	}
	out := map[string]any{
		"ok": true, "version": app.Version,
		"installedPackageVersion": app.Version, "installedPackageRevision": nil,
		"installedReleaseId": app.Version, "installedWebUIBuild": webUIBuild,
		"reinstallSupported": reinstallSupported, "opkgRegistrationHealthy": registered,
		"build": app.Version, "releaseId": app.Version, "candidateId": app.Version,
		"releaseSource": "broray-installer", "releaseCommit": nil, "releaseBuiltAt": nil,
		"webUIBuild": webUIBuild, "architecture": architecture(),
		"updateChannel": channel, "packageTrack": "single-binary",
		"updateAvailable": updateAvailable, "updateCheckFresh": checkFresh,
		"availableVersion": availableVersion, "availablePackageVersion": availableVersion, "availableCandidateId": availableVersion,
		"releaseRelation": relation, "candidateRelation": relation, "lastCheckedAt": lastCheckedAt,
		"lastBackup": nil, "backupValid": false, "backupProblem": nil,
		"recoveryAvailable":   pending,
		"installationHealthy": installationHealthy, "versionsConsistent": consistent,
		"components": comps, "protocols": protocols, "health": health, "lastOperation": lastOp,
		"capabilities": append([]string(nil), capabilities...),
		"links":        map[string]any{"github": githubURL, "project": projectURL, "donate": donateURL},
		"updatedAt":    now,
	}
	if enrich {
		out["universalUpdaterReady"] = true
		out["platformHandoff"] = readyHandoff(now)
	}
	return out
}
