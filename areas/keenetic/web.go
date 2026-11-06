package keenetic

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	kn "github.com/BROadmin/broray/internal/keenetic"
)

// KeenDNS HTTP Proxy publication of the WebUI (lib/web-publish.sh).

const webName = "broray"

var (
	webVerifyAttempts = 8
	webVerifyDelay    = time.Second
	reDNSLabel        = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$`)
)

func (s *service) webReceiptPath() string { return s.d.Path("config", "web-publish.json") }
func (s *service) webRecoveryPath() string {
	return s.d.Path("config", "web-publish-recovery-required.json")
}

func (s *service) webPort() int {
	if p := s.d.App.Cfg.WebPort; validPort(p) {
		return p
	}
	return 8080
}

func dnsLabel(v string) bool { return len(v) >= 1 && len(v) <= 63 && reDNSLabel.MatchString(v) }

func dnsDomain(v string) bool {
	if len(v) < 3 || len(v) > 253 {
		return false
	}
	p := strings.Split(v, ".")
	if len(p) < 2 {
		return false
	}
	for _, l := range p {
		if !dnsLabel(l) {
			return false
		}
	}
	return true
}

// webError is one BRORAY_WEB_PUBLISH_ERROR:<stage>:<CODE>:<message> line.
type webError struct{ lines []string }

func (e *webError) Error() string { return strings.Join(e.lines, "\n") }

func (e *webError) add(stage, code, msg string) *webError {
	e.lines = append(e.lines, "BRORAY_WEB_PUBLISH_ERROR:"+stage+":"+code+":"+msg)
	return e
}

func webFail(stage, code, msg string) *webError { return (&webError{}).add(stage, code, msg) }

// webBlock is broray_web_publish_block: trimmed lines from the parent to "!".
func webBlock(config []string) []string {
	var out []string
	inside := false
	for _, l := range config {
		line := strings.TrimSpace(l)
		if line == "ip http proxy "+webName {
			inside = true
		}
		if inside {
			out = append(out, line)
			if line == "!" {
				break
			}
		}
	}
	return out
}

func webBlockExists(config []string) bool {
	for _, l := range webBlock(config) {
		if l == "ip http proxy "+webName {
			return true
		}
	}
	return false
}

// webProfile is broray_web_publish_block_profile: the closed grammar with
// the observed presence of the three optional lines.
func webProfile(config []string, lan string, port int) ([3]string, bool) {
	var prof [3]string
	if !validIPv4(lan) || !validPort(port) {
		return prof, false
	}
	block := webBlock(config)
	if len(block) == 0 {
		return prof, false
	}
	parents := 0
	for _, l := range config {
		if strings.TrimSpace(l) == "ip http proxy "+webName {
			parents++
		}
	}
	count := func(want string) int {
		n := 0
		for _, l := range block {
			if l == want {
				n++
			}
		}
		return n
	}
	if parents != 1 || count("!") != 1 || block[len(block)-1] != "!" || count("ip http proxy "+webName) != 1 ||
		count("upstream http "+lan+" "+strconv.Itoa(port)) != 1 {
		return prof, false
	}
	opt := []string{"domain ndns", "ssl redirect", "security-level public"}
	expected := 2
	for i, o := range opt {
		c := count(o)
		if c > 1 {
			return prof, false
		}
		prof[i] = "omitted"
		if c == 1 {
			prof[i] = "present"
			expected++
		}
	}
	nonEmpty := 0
	for _, l := range block {
		if l != "" && l != "!" {
			nonEmpty++
		}
	}
	return prof, nonEmpty == expected
}

type webReceipt struct {
	SchemaVersion        int    `json:"schemaVersion"`
	Owner                string `json:"owner"`
	Name                 string `json:"name"`
	WritePolicySHA256    string `json:"writePolicySha256"`
	SerializationProfile string `json:"serializationProfile"`
	Upstream             struct {
		Scheme string `json:"scheme"`
		Host   string `json:"host"`
		Port   int    `json:"port"`
	} `json:"upstream"`
	Domain        string `json:"domain"`
	SSLRedirect   bool   `json:"sslRedirect"`
	SecurityLevel string `json:"securityLevel"`
	Serialization struct {
		Domain        string `json:"domain"`
		SSLRedirect   string `json:"sslRedirect"`
		SecurityLevel string `json:"securityLevel"`
	} `json:"serialization"`
	UpdatedAt string `json:"updatedAt"`
}

func presence(v string) bool { return v == "present" || v == "omitted" }

// webReceiptValid is broray_web_publish_owner_record_valid.
func (s *service) webReceiptValid() (webReceipt, bool) {
	var r webReceipt
	st, err := os.Lstat(s.webReceiptPath())
	if err != nil || !st.Mode().IsRegular() {
		return r, false
	}
	b, err := os.ReadFile(s.webReceiptPath())
	if err != nil || json.Unmarshal(b, &r) != nil {
		return webReceipt{}, false
	}
	ok := r.SchemaVersion == 3 && r.Owner == "BROray" && r.Name == webName && r.WritePolicySHA256 == writePolicySHA256 &&
		r.SerializationProfile == "dynamic-bounded-v1" && r.Upstream.Scheme == "http" && validIPv4(r.Upstream.Host) &&
		validPort(r.Upstream.Port) && r.Domain == "ndns" && r.SSLRedirect && r.SecurityLevel == "public" &&
		presence(r.Serialization.Domain) && presence(r.Serialization.SSLRedirect) && presence(r.Serialization.SecurityLevel)
	return r, ok
}

func (s *service) webOwnedExact(config []string) bool {
	r, ok := s.webReceiptValid()
	if !ok {
		return false
	}
	prof, ok := webProfile(config, r.Upstream.Host, r.Upstream.Port)
	return ok && prof == [3]string{r.Serialization.Domain, r.Serialization.SSLRedirect, r.Serialization.SecurityLevel}
}

func (s *service) webExact(config []string, lan string, port int) bool {
	_, ok := webProfile(config, lan, port)
	return ok
}

func (s *service) lan(ctx context.Context) string {
	lan, err := s.d.App.LAN(ctx)
	if err != nil || !validIPv4(lan) {
		return ""
	}
	return lan
}

type ndnsInfo struct {
	available            bool
	name, domain, access string
}

func (s *service) ndns(ctx context.Context) (ndnsInfo, bool) {
	lines, err := s.showText(ctx, "show ndns")
	if err != nil || len(lines) == 0 {
		return ndnsInfo{}, false
	}
	kv := keyValues(lines)
	n := ndnsInfo{name: kv["name"], domain: kv["domain"], access: kv["access"]}
	n.available = dnsLabel(n.name) && dnsDomain(n.domain)
	return n, true
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// webStatus is broray_web_publish_status_json (read-only).
func (s *service) webStatus(ctx context.Context) (map[string]any, error) {
	lan := s.lan(ctx)
	port := s.webPort()
	localAddress := ""
	if lan != "" {
		localAddress = "http://" + lan + ":" + strconv.Itoa(port) + "/"
	}
	nd, _ := s.ndns(ctx)
	address := ""
	if nd.available {
		address = "https://" + webName + "." + nd.name + "." + nd.domain + "/"
	}
	config, err := s.captureConfig(ctx, "running")
	if err != nil {
		return nil, err
	}
	blockExists := webBlockExists(config)
	_, statErr := os.Lstat(s.webReceiptPath())
	ownerPresent := statErr == nil
	r, ownerValid := s.webReceiptValid()
	ownedExact := blockExists && ownerValid && s.webOwnedExact(config)
	_, recErr := os.Lstat(s.webRecoveryPath())
	recovery := recErr == nil
	policy := true
	state, code, msg := "", "", ""
	consistent, canEnable, canDisable := false, false, false
	switch {
	case recovery:
		state, code, msg = "recovery", "WEB_ACCESS_RECOVERY_REQUIRED", "Предыдущая операция с адресом WebUI требует восстановления."
	case ownedExact:
		state, canDisable = "enabled", true
		if lan != "" && r.Upstream.Host == lan && r.Upstream.Port == port {
			consistent = true
		} else if lan != "" && nd.available && policy {
			canEnable = true
		}
		switch {
		case !consistent:
			code, msg = "WEB_ACCESS_LAN_CHANGED", "LAN-адрес роутера изменился; адрес WebUI можно восстановить."
		case !nd.available:
			code, msg = "KEENDNS_ADDRESS_UNAVAILABLE", "Публикация включена, но KeenDNS не вернул корректное доменное имя."
		default:
			code, msg = "WEB_ACCESS_ENABLED", "Адрес WebUI через KeenDNS включён и принадлежит BROray."
		}
	case !blockExists && !ownerPresent:
		state = "disabled"
		switch {
		case lan != "" && nd.available && policy:
			canEnable = true
			code, msg = "WEB_ACCESS_DISABLED", "Адрес WebUI через KeenDNS выключен."
		case lan == "":
			code, msg = "WEB_ACCESS_LAN_UNAVAILABLE", "Не удалось определить локальный адрес WebUI."
		case !nd.available:
			code, msg = "KEENDNS_ADDRESS_UNAVAILABLE", "KeenDNS не вернул корректное доменное имя."
		default:
			code, msg = "WEB_ACCESS_POLICY_UNAVAILABLE", "Управление адресом WebUI недоступно для текущего профиля записи."
		}
	default:
		state, code, msg = "conflict", "WEB_ACCESS_OWNERSHIP_CONFLICT", "Обнаружен чужой, изменённый или неполный объект broray; автоматическое изменение запрещено."
	}
	return map[string]any{"schemaVersion": 1, "state": state, "enabled": state == "enabled", "consistent": consistent,
		"canEnable": canEnable, "canDisable": canDisable, "address": nullable(address), "localAddress": nullable(localAddress),
		"keenDns":          map[string]any{"available": nd.available, "name": nullable(nd.name), "domain": nullable(nd.domain), "access": nullable(nd.access)},
		"ownership":        map[string]any{"liveBlockPresent": blockExists, "receiptPresent": ownerPresent, "receiptValid": ownerValid, "liveBlockOwnedExact": ownedExact},
		"recoveryRequired": recovery, "reason": map[string]any{"code": code, "message": msg}}, nil
}

// keenDNSOrigin is compat.KeenDNSOrigin (operations-origin.sh:106-132):
// the live, owned and consistent publication origin.
func (s *service) keenDNSOrigin(ctx context.Context) (string, bool) {
	s.originMu.Lock()
	if time.Since(s.originAt) < 30*time.Second {
		o, ok := s.origin, s.originOK
		s.originMu.Unlock()
		return o, ok
	}
	s.originMu.Unlock()
	o, ok := "", false
	if st, err := s.webStatus(ctx); err == nil {
		own, _ := st["ownership"].(map[string]any)
		nd, _ := st["keenDns"].(map[string]any)
		name, _ := nd["name"].(string)
		domain, _ := nd["domain"].(string)
		if st["state"] == "enabled" && st["enabled"] == true && st["consistent"] == true && st["recoveryRequired"] == false &&
			nd["available"] == true && own["liveBlockPresent"] == true && own["receiptPresent"] == true && own["receiptValid"] == true &&
			own["liveBlockOwnedExact"] == true && len(name) >= 1 && len(name) <= 63 && !strings.Contains(name, ".") && strings.Contains(domain, ".") {
			want := "https://" + webName + "." + name + "." + domain
			if st["address"] == want+"/" {
				o, ok = compat.NormalizeOrigin(want), true
			}
		}
	}
	s.originMu.Lock()
	s.origin, s.originOK, s.originAt = o, ok, time.Now()
	s.originMu.Unlock()
	return o, ok
}

func (s *service) resetOrigin() {
	s.originMu.Lock()
	s.originAt = time.Time{}
	s.originMu.Unlock()
}

// ---------- writes ----------

func (s *service) webCmd(ctx context.Context, cmds ...string) error {
	for _, c := range cmds {
		area := kn.AreaWeb
		if c == "system configuration save" {
			area = kn.AreaSystem
		}
		if kn.Allow(area, c) != nil {
			return webFail("dispatch", "WEB_PUBLISH_COMMAND_NOT_AUTHORIZED", "Команда не входит в замороженный R14C01 HTTP Proxy grammar.")
		}
	}
	res, err := s.d.App.T.Parse(ctx, cmds)
	if err != nil {
		return err
	}
	for _, r := range res {
		if !r.OK {
			return errors.New("rejected")
		}
	}
	return nil
}

func applyCommands(lan string, port int) []string {
	n := "ip http proxy " + webName
	return []string{n, n + " upstream http " + lan + " " + strconv.Itoa(port), n + " domain ndns", n + " ssl redirect", n + " security-level public"}
}

func (s *service) waitWeb(ctx context.Context, cond func([]string) bool) bool {
	for attempt := 1; attempt <= webVerifyAttempts; attempt++ {
		if c, err := s.captureConfig(ctx, "running"); err == nil && cond(c) {
			return true
		}
		if attempt < webVerifyAttempts && sleepCtx(ctx, webVerifyDelay) != nil {
			return false
		}
	}
	return false
}

func (s *service) webWaitExact(ctx context.Context, lan string, port int) bool {
	return s.waitWeb(ctx, func(c []string) bool { return s.webExact(c, lan, port) })
}

func (s *service) webWaitAbsent(ctx context.Context) bool {
	return s.waitWeb(ctx, func(c []string) bool { return !webBlockExists(c) })
}

func (s *service) webSaveAndWait(ctx context.Context, cond func(context.Context) bool) bool {
	return s.webCmd(ctx, "system configuration save") == nil && cond(ctx)
}

func (s *service) webWriteReceipt(ctx context.Context, lan string, port int) error {
	config, err := s.captureConfig(ctx, "running")
	if err != nil {
		return err
	}
	prof, ok := webProfile(config, lan, port)
	if !ok {
		return errors.New("profile")
	}
	var r webReceipt
	r.SchemaVersion, r.Owner, r.Name, r.WritePolicySHA256, r.SerializationProfile = 3, "BROray", webName, writePolicySHA256, "dynamic-bounded-v1"
	r.Upstream.Scheme, r.Upstream.Host, r.Upstream.Port = "http", lan, port
	r.Domain, r.SSLRedirect, r.SecurityLevel = "ndns", true, "public"
	r.Serialization.Domain, r.Serialization.SSLRedirect, r.Serialization.SecurityLevel = prof[0], prof[1], prof[2]
	r.UpdatedAt = compat.Now()
	_ = os.MkdirAll(s.d.Path("config"), 0o700)
	return compat.WriteJSON(s.webReceiptPath(), r)
}

func (s *service) webRestoreReceipt(backup []byte, existed bool) error {
	if existed {
		return writeBytes(s.webReceiptPath(), backup)
	}
	err := os.Remove(s.webReceiptPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func writeBytes(path string, b []byte) error {
	tmp := path + ".restore"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *service) markWebRecovery(stage string) {
	_ = compat.WriteJSON(s.webRecoveryPath(), map[string]any{"schemaVersion": 1, "state": "recovery-required", "path": "web-publish",
		"failedStage": stage, "writePolicySha256": writePolicySHA256, "updatedAt": compat.Now()})
}

// webTxFail is broray_web_publish_transaction_fail: report, roll back, mark recovery.
func (s *service) webTxFail(ctx context.Context, stage, code, msg, initial, oldHost string, oldPort int, backup []byte, existed bool) error {
	e := webFail(stage, code, msg)
	ok := false
	if initial == "owned" {
		ok = s.webCmd(ctx, applyCommands(oldHost, oldPort)...) == nil && s.webWaitExact(ctx, oldHost, oldPort) &&
			s.webSaveAndWait(ctx, func(c context.Context) bool { return s.webWaitExact(c, oldHost, oldPort) }) &&
			s.webRestoreReceipt(backup, existed) == nil
	} else {
		ok = s.webCmd(ctx, "no ip http proxy "+webName) == nil && s.webWaitAbsent(ctx) &&
			s.webSaveAndWait(ctx, s.webWaitAbsent) && s.webRestoreReceipt(backup, existed) == nil
	}
	if !ok {
		s.markWebRecovery(stage)
		e.add("rollback", "WEB_PUBLISH_ROLLBACK_FAILED", "Byte-exact rollback не подтверждён; установлен recovery marker.")
	}
	return e
}

func (s *service) webMutationPrereq(ctx context.Context, stage string) error {
	if _, err := os.Lstat(s.webRecoveryPath()); err == nil {
		return webFail(stage, "WEB_PUBLISH_RECOVERY_REQUIRED", "Предыдущая HTTP Proxy транзакция требует восстановления.")
	}
	if lines, err := s.showText(ctx, "show ndns"); err != nil || len(lines) == 0 {
		return webFail(stage, "WEB_PUBLISH_NDNS_PREREQUISITE_FAILED", "Read-only команда show ndns не подтвердила доступность KeenDNS.")
	}
	return nil
}

// webEnable is broray_web_publish_ensure.
func (s *service) webEnable(ctx context.Context) error {
	lan := s.lan(ctx)
	if lan == "" {
		return webFail("ensure-preflight", "WEB_PUBLISH_LAN_IP_INVALID", "Не удалось определить актуальный LAN-IP для WebUI.")
	}
	port := s.webPort()
	if _, err := os.Lstat(s.webRecoveryPath()); err == nil {
		return webFail("ensure-preflight", "WEB_PUBLISH_RECOVERY_REQUIRED", "Предыдущая HTTP Proxy транзакция требует восстановления.")
	}
	config, err := s.captureConfig(ctx, "running")
	if err != nil {
		return webFail("ensure-preflight", "WEB_PUBLISH_RUNNING_CONFIG_UNAVAILABLE", "Read-only show running-config завершился ошибкой.")
	}
	existed := false
	var backup []byte
	if _, err := os.Lstat(s.webReceiptPath()); err == nil {
		if _, ok := s.webReceiptValid(); !ok {
			return webFail("ensure-preflight", "WEB_PUBLISH_RECEIPT_INVALID", "Receipt KeenDNS HTTP Proxy повреждён, legacy или не связан с текущим policy SHA.")
		}
		if backup, err = os.ReadFile(s.webReceiptPath()); err != nil {
			return webFail("ensure-preflight", "WEB_PUBLISH_RECEIPT_BACKUP_FAILED", "Не удалось сохранить byte-exact backup receipt.")
		}
		existed = true
	}
	initial, oldHost, oldPort := "absent", "", 0
	if webBlockExists(config) {
		if !s.webOwnedExact(config) {
			return webFail("ensure-preflight", "WEB_PUBLISH_OWNERSHIP_MISMATCH", "HTTP Proxy broray существует, но receipt не совпадает с полным live block.")
		}
		r, _ := s.webReceiptValid()
		oldHost, oldPort = r.Upstream.Host, r.Upstream.Port
		if oldHost == lan && oldPort == port && s.webExact(config, lan, port) {
			return nil // "KeenDNS HTTP Proxy BROray уже настроен и подтверждён."
		}
		initial = "owned"
	} else if existed {
		return webFail("ensure-preflight", "WEB_PUBLISH_RECEIPT_LIVE_MISMATCH", "Receipt существует, но live HTTP Proxy отсутствует; автоматическое усыновление запрещено.")
	}
	if err := s.webMutationPrereq(ctx, "ensure-mutation"); err != nil {
		return err
	}
	fail := func(stage, code, msg string) error {
		return s.webTxFail(ctx, stage, code, msg, initial, oldHost, oldPort, backup, existed)
	}
	if s.webCmd(ctx, applyCommands(lan, port)...) != nil {
		return fail("ensure-apply", "WEB_PUBLISH_APPLY_COMMAND_FAILED", "KeeneticOS отклонила одну из документированных HTTP Proxy команд.")
	}
	if !s.webWaitExact(ctx, lan, port) {
		return fail("ensure-verify-before-save", "WEB_PUBLISH_VERIFY_TIMEOUT", "HTTP Proxy не сошёлся к exact scoped running-config до save.")
	}
	if !s.webSaveAndWait(ctx, func(c context.Context) bool { return s.webWaitExact(c, lan, port) }) {
		return fail("ensure-save", "WEB_PUBLISH_SAVE_CONVERGENCE_FAILED", "Save не подтвердил bounded exact convergence HTTP Proxy.")
	}
	if s.webWriteReceipt(ctx, lan, port) != nil {
		return fail("ensure-receipt", "WEB_PUBLISH_RECEIPT_COMMIT_FAILED", "Live HTTP Proxy сохранён, но authoritative receipt не записан.")
	}
	config, err = s.captureConfig(ctx, "running")
	if r, ok := s.webReceiptValid(); err != nil || !s.webExact(config, lan, port) || !ok || r.Upstream.Host != lan || r.Upstream.Port != port {
		return fail("ensure-final-verify", "WEB_PUBLISH_FINAL_VERIFY_FAILED", "Финальная live/receipt сверка не прошла.")
	}
	return nil
}

// webDisable is broray_web_publish_delete.
func (s *service) webDisable(ctx context.Context) error {
	if _, err := os.Lstat(s.webRecoveryPath()); err == nil {
		return webFail("delete-preflight", "WEB_PUBLISH_RECOVERY_REQUIRED", "Предыдущая HTTP Proxy транзакция требует восстановления.")
	}
	config, err := s.captureConfig(ctx, "running")
	if err != nil {
		return webFail("delete-preflight", "WEB_PUBLISH_RUNNING_CONFIG_UNAVAILABLE", "Read-only show running-config завершился ошибкой.")
	}
	if !webBlockExists(config) {
		if _, err := os.Lstat(s.webReceiptPath()); err == nil {
			return webFail("delete-preflight", "WEB_PUBLISH_RECEIPT_LIVE_MISMATCH", "Receipt существует, но live HTTP Proxy отсутствует; автоматическое удаление receipt запрещено.")
		}
		return nil
	}
	if !s.webOwnedExact(config) {
		return webFail("delete-preflight", "WEB_PUBLISH_DELETE_AUTHORITY_REFUSED", "Чужой, legacy или изменённый HTTP Proxy не будет удалён.")
	}
	r, _ := s.webReceiptValid()
	backup, err := os.ReadFile(s.webReceiptPath())
	if err != nil {
		return webFail("delete-preflight", "WEB_PUBLISH_RECEIPT_BACKUP_FAILED", "Не удалось сохранить byte-exact backup receipt.")
	}
	if err := s.webMutationPrereq(ctx, "delete-mutation"); err != nil {
		return err
	}
	fail := func(stage, code, msg string) error {
		return s.webTxFail(ctx, stage, code, msg, "owned", r.Upstream.Host, r.Upstream.Port, backup, true)
	}
	if s.webCmd(ctx, "no ip http proxy "+webName) != nil {
		return fail("delete-command", "WEB_PUBLISH_DELETE_COMMAND_FAILED", "KeeneticOS отклонила документированную delete-команду HTTP Proxy.")
	}
	if !s.webWaitAbsent(ctx) {
		return fail("delete-verify-before-save", "WEB_PUBLISH_VERIFY_TIMEOUT", "HTTP Proxy не исчез из running-config до save.")
	}
	if !s.webSaveAndWait(ctx, s.webWaitAbsent) {
		return fail("delete-save", "WEB_PUBLISH_SAVE_CONVERGENCE_FAILED", "Save не подтвердил bounded отсутствие HTTP Proxy.")
	}
	if err := os.Remove(s.webReceiptPath()); err != nil && !os.IsNotExist(err) {
		return fail("delete-receipt", "WEB_PUBLISH_RECEIPT_DELETE_FAILED", "Live HTTP Proxy удалён, но authoritative receipt не удалён.")
	}
	return nil
}

func webHTTPStatus(code string) int {
	if strings.Contains(code, "OWNERSHIP") || strings.Contains(code, "RECEIPT") {
		return 409
	}
	for _, suf := range []string{"RECOVERY_REQUIRED", "DEVELOPMENT_WRITE_DISABLED", "WRITE_POLICY_INVALID", "WRITE_PATH_INVALID", "PHYSICAL_SERIALIZATION_REQUIRED"} {
		if strings.HasSuffix(code, suf) {
			return 409
		}
	}
	return 500
}

func (s *service) handleWebStatus(c *cgi.Req) (any, error) {
	st, err := s.webStatus(c.R.Context())
	if err != nil {
		return nil, cgi.E(500, "WEB_ACCESS_STATUS_FAILED", "Не удалось получить состояние адреса WebUI через KeenDNS.")
	}
	return st, nil
}

// handleWebAction is K8/K9 (web-access-action-common.sh).
func (s *service) handleWebAction(c *cgi.Req, action string) (any, error) {
	release, ok := s.d.Lock.TryAcquire("keenetic", "keenetic:web-access-"+action, "keenetic")
	if !ok {
		return nil, cgi.E(409, "OPERATION_BUSY", "Другая конфликтующая операция BROray уже выполняется.")
	}
	defer release()
	op := s.d.Queue.StartOperation("", "router_operation", "keenetic:web-access-"+action, "USER", "protected", "")
	op.Phase("working")
	ctx := context.WithoutCancel(c.R.Context())
	s.mu.Lock()
	var err error
	if action == "enable" {
		err = s.webEnable(ctx)
	} else {
		err = s.webDisable(ctx)
	}
	s.mu.Unlock()
	s.resetOrigin()
	if err != nil {
		code, msg, status := "WEB_ACCESS_ACTION_FAILED", "Не удалось изменить адрес WebUI через KeenDNS.", 500
		var we *webError
		details := err.Error()
		if errors.As(err, &we) && len(we.lines) > 0 {
			last := strings.TrimPrefix(we.lines[len(we.lines)-1], "BRORAY_WEB_PUBLISH_ERROR:")
			parts := strings.SplitN(last, ":", 3)
			if len(parts) == 3 {
				code, msg = parts[1], parts[2]
				status = webHTTPStatus(code)
			}
			if len(we.lines) > 30 {
				we.lines = we.lines[len(we.lines)-30:]
			}
			details = strings.Join(we.lines, "\n")
		}
		op.Finish(errors.New(code + ": " + msg))
		return nil, cgi.Ed(status, code, msg, details)
	}
	st, err := s.webStatus(ctx)
	if err != nil {
		op.Finish(errors.New("WEB_ACCESS_STATUS_FAILED: status"))
		return nil, cgi.Ed(500, "WEB_ACCESS_STATUS_FAILED", "Операция выполнена, но итоговое состояние адреса WebUI не подтверждено.", err.Error())
	}
	op.Finish(nil)
	return st, nil
}

// uninstallWeb removes the owned KeenDNS publication (uninstall stage
// "publish"). Absent or foreign objects are left alone (nil).
func (s *service) uninstallWeb(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, err := s.captureConfig(ctx, "running")
	if err != nil {
		return err
	}
	if !webBlockExists(config) || !s.webOwnedExact(config) {
		return nil
	}
	if err := s.webDisable(ctx); err != nil {
		return err
	}
	s.resetOrigin()
	return nil
}
