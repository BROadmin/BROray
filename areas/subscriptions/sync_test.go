package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
)

const subX = "sub-00000000000000aa"

func stageLinks(t *testing.T, enabled bool, links ...string) []servers.Record {
	t.Helper()
	st, err := stageNodes(subX, &extracted{Lines: links}, enabled, nil)
	if err != nil {
		t.Fatal(err)
	}
	return st.Records
}

func (e *env) sync(t *testing.T, policy string, links ...string) (*syncResult, error) {
	t.Helper()
	return e.s.syncCatalog(context.Background(), subX, stageLinks(t, true, links...), true, "u1", policy)
}

func TestSyncContinuityAndRetention(t *testing.T) {
	e := newEnv(t)
	res, err := e.sync(t, "replace", "trojan://pw1@a.example:443#A", "trojan://pw@b.example:443#B")
	if err != nil || res.Added != 2 || res.Total != 2 {
		t.Fatalf("%v %+v", err, res)
	}
	ids := liveIDs(t, e)
	var idA string
	for _, id := range ids {
		if rec, _ := servers.ReadRecord(e.d, id); rec["address"] == "a.example" {
			idA = id
		}
	}
	// unchanged run does not rewrite files
	st, _ := os.Stat(servers.RecordPath(e.d, idA))
	res, err = e.sync(t, "replace", "trojan://pw1@a.example:443#A", "trojan://pw@b.example:443#B")
	st2, _ := os.Stat(servers.RecordPath(e.d, idA))
	if err != nil || res.Unchanged != 2 || !st.ModTime().Equal(st2.ModTime()) {
		t.Fatalf("unchanged: %v %+v", err, res)
	}
	// credential rotation keeps the server id (continuity), active server re-applied
	if err := e.s.writeActive(idA); err != nil {
		t.Fatal(err)
	}
	res, err = e.sync(t, "replace", "trojan://pw2@a.example:443#A", "trojan://pw@b.example:443#B")
	if err != nil || res.Updated != 1 || res.Unchanged != 1 || res.Added != 0 || res.Removed != 0 || res.Impact != "configuration-changed" {
		t.Fatalf("rotation: %v %+v", err, res)
	}
	rec, err := servers.ReadRecord(e.d, idA)
	if err != nil || rec["password"] != "pw2" || servers.ActiveID(e.d) != idA || len(e.reapplied) != 1 {
		t.Fatalf("continuity: %v %v %v", err, rec, e.reapplied)
	}
	// re-apply failure rolls the catalog back
	e.reapplyErr = errors.New("XRAY_APPLY_FAILED: x")
	_, err = e.sync(t, "replace", "trojan://pw3@a.example:443#A", "trojan://pw@b.example:443#B")
	var se *subErr
	if !errors.As(err, &se) || se.Code != "ACTIVE_SERVER_CONFLICT" || se.Msg != "Новая версия активного сервера не была применена. Восстановлена прежняя рабочая версия." {
		t.Fatalf("reapply failure: %v", err)
	}
	if rec, _ := servers.ReadRecord(e.d, idA); rec["password"] != "pw2" || len(e.reapplied) != 3 {
		t.Fatalf("rollback: %v %v", rec["password"], e.reapplied)
	}
	e.reapplyErr = nil
	// xray test failure: nothing written
	e.testErr = errors.New("rejected")
	_, err = e.sync(t, "replace", "trojan://pw4@a.example:443#A")
	if !errors.As(err, &se) || se.Msg != "Новая конфигурация активного сервера не прошла проверку Xray. Обновление отменено." || len(liveIDs(t, e)) != 2 {
		t.Fatalf("test failure: %v", err)
	}
	e.testErr = nil
	// name change only → interface description sync; failure rolls back
	e.ensureErr = errors.New("KEENETIC_UNAVAILABLE")
	_, err = e.sync(t, "replace", "trojan://pw2@a.example:443#A2", "trojan://pw@b.example:443#B")
	if !errors.As(err, &se) || !strings.HasPrefix(se.Msg, "Новое имя активного сервера не удалось синхронизировать") {
		t.Fatalf("name sync: %v", err)
	}
	if rec, _ := servers.ReadRecord(e.d, idA); rec["name"] != "A" {
		t.Fatal("name not rolled back")
	}
	e.ensureErr = nil
	res, err = e.sync(t, "replace", "trojan://pw2@a.example:443#A2", "trojan://pw@b.example:443#B")
	if err != nil || res.Impact != "display-name-changed" || e.ensured != 3 {
		t.Fatalf("name: %v %+v %d", err, res, e.ensured)
	}
	// disabled source cannot hold the active server
	_, err = e.s.syncCatalog(context.Background(), subX, stageLinks(t, false, "trojan://pw2@a.example:443#A2"), false, "u2", "replace")
	if !errors.As(err, &se) || se.Msg != "Активный сервер не может быть перенесён в выключенный источник." || len(liveIDs(t, e)) != 2 || exists2(e.s.disabledDir(subX)) {
		t.Fatalf("disabled active: %v %v", err, liveIDs(t, e))
	}
	os.Remove(servers.ActiveFile(e.d))
	// partial response: unmatched old servers are retained
	res, err = e.sync(t, "retain-unmatched", "trojan://pw@c.example:443#C")
	if err != nil || res.Retained != 2 || res.Added != 1 || res.Removed != 0 || res.Total != 3 || len(res.Warnings) != 1 ||
		!strings.HasPrefix(res.Warnings[0], "Сохранено прежних серверов: 2. Ответ разобран не полностью;") {
		t.Fatalf("retain: %v %+v", err, res)
	}
	// complete response replaces
	res, err = e.sync(t, "replace", "trojan://pw@c.example:443#C")
	if err != nil || res.Removed != 2 || len(liveIDs(t, e)) != 1 {
		t.Fatalf("replace: %v %+v", err, res)
	}
	// ambiguous continuity: two old endpoints a:443 (different creds) vs one new
	if _, err := e.sync(t, "replace", "trojan://p1@d.example:443#D1", "trojan://p2@d.example:443#D2"); err != nil {
		t.Fatal(err)
	}
	_, err = e.sync(t, "replace", "trojan://p3@d.example:443#D")
	if !errors.As(err, &se) || se.Code != "SERVER_SYNC_AMBIGUOUS_IDENTITY" || se.Msg != errMap {
		t.Fatalf("ambiguous: %v", err)
	}
	// no work dir / marker left behind
	if m, _ := os.ReadDir(e.d.Root.Tmp()); len(m) != 0 {
		t.Fatalf("tmp left: %v", m)
	}
}

func TestSyncPartialLimit(t *testing.T) {
	e := newEnv(t)
	var old, fresh []string
	for i := 0; i < 400; i++ {
		old = append(old, fmt.Sprintf("trojan://pw@o%d.example:443#O", i))
	}
	for i := 0; i < 101; i++ {
		fresh = append(fresh, fmt.Sprintf("trojan://pw@n%d.example:443#N", i))
	}
	if _, err := e.sync(t, "replace", old...); err != nil {
		t.Fatal(err)
	}
	_, err := e.sync(t, "retain-unmatched", fresh...)
	var se *subErr
	if !errors.As(err, &se) || se.Code != "PARTIAL_UPDATE_LIMIT" ||
		se.Msg != "Не удалось безопасно объединить частичный ответ с прежними серверами. Прежний каталог сохранён." {
		t.Fatalf("%v", err)
	}
	if len(liveIDs(t, e)) != 400 {
		t.Fatal("catalog changed")
	}
	if res, err := e.sync(t, "retain-unmatched", fresh[:100]...); err != nil || res.Total != 500 {
		t.Fatalf("exactly 500: %v", err)
	}
}

func TestSyncPreChecks(t *testing.T) {
	e := newEnv(t)
	recs := stageLinks(t, true, linkA)
	other := deepCopy(recs[0])
	other["source"].(map[string]any)["subscriptionId"] = "sub-other"
	_, err := e.s.syncCatalog(context.Background(), subX, []servers.Record{other}, true, "u", "replace")
	if err == nil || err.(*subErr).Msg != "Подготовленный сервер принадлежит другой подписке." {
		t.Fatalf("%v", err)
	}
	_, err = e.s.syncCatalog(context.Background(), subX, nil, true, "u", "replace")
	if err == nil || err.(*subErr).Msg != "Серверный модуль не получил ни одного узла." {
		t.Fatalf("%v", err)
	}
	broken := deepCopy(recs[0])
	broken["port"] = "x"
	_, err = e.s.syncCatalog(context.Background(), subX, []servers.Record{broken}, true, "u", "replace")
	if err == nil || err.(*subErr).Msg != "Подготовленный сервер не прошёл проверку серверного модуля." {
		t.Fatalf("%v", err)
	}
}

func TestDownloadSecurity(t *testing.T) {
	e := newEnv(t)
	p := newProvider(t, linkA)
	srvURL := strings.Replace(p.URL, "127.0.0.1", "sub.example", 1)
	fetch := func(u string, in fetchInput) error {
		in.URL = u
		_, err := e.s.fetch(context.Background(), in, newMetadata())
		return err
	}
	code := func(err error) string {
		var se *subErr
		if errors.As(err, &se) {
			return se.Code + ":" + se.Msg
		}
		return fmt.Sprint(err)
	}
	addrAllowed = ipPublic
	e.dns["sub.example"] = []string{"8.8.8.8", "10.0.0.1"}
	e.dns["empty.example"] = nil
	cases := []struct{ url, want string }{
		{"http://127.0.0.1/x", "DOWNLOAD_SECURITY:Локальный или служебный IP-адрес запрещён."},
		{"http://[::1]/x", "DOWNLOAD_SECURITY:Локальный или служебный IP-адрес запрещён."},
		{"http://010.1.1.1/x", "DOWNLOAD_SECURITY:Локальный или служебный IP-адрес запрещён."},
		{srvURL + "/x", "DOWNLOAD_SECURITY:Имя подписки разрешается в локальный или служебный адрес."},
		{"http://empty.example/x", "HTTP_ERROR:Не удалось определить IP-адрес сервера подписки."},
		{"http://x.localhost/x", "DOWNLOAD_SECURITY:Локальные и служебные адреса запрещены."},
	}
	for _, c := range cases {
		if got := code(fetch(c.url, fetchInput{})); got != c.want {
			t.Errorf("%s: %s", c.url, got)
		}
	}
	if got := code(fetch("https://a.example/", fetchInput{UserAgent: "é"})); got != "INVALID_USER_AGENT:User-Agent должен содержать не более 128 печатных ASCII-символов." {
		t.Error(got)
	}
	if got := code(fetch("https://a.example/", fetchInput{Hwid: "bad!"})); got != "CLIENT_ID_ERROR:Анонимный идентификатор клиента подписки повреждён." {
		t.Error(got)
	}
	// the checked address is the one dialled (pinning): name → 127.0.0.1 allowed only for loopback in this test
	addrAllowed = func(a netip.Addr) bool { return a.IsLoopback() }
	e.dns["sub.example"] = []string{"127.0.0.1"}
	if err := fetch(srvURL+"/x", fetchInput{}); err != nil {
		t.Fatalf("pinned fetch: %v", err)
	}
	addrAllowed = func(netip.Addr) bool { return true }

	mux := http.NewServeMux()
	hits := 0
	mux.HandleFunc("/r0", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/r1", 302) })
	mux.HandleFunc("/r1", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "r2", 301) })
	mux.HandleFunc("/r2", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "?q=1", 307) })
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) { hits++; http.Redirect(w, r, "/loop", 302) })
	mux.HandleFunc("/noloc", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(302) })
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) { w.Write(make([]byte, maxBodyBytes+1)) })
	mux.HandleFunc("/hdr", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Big", strings.Repeat("a", 70000))
	})
	mux.HandleFunc("/limit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-hwid-limit", "TRUE")
		w.WriteHeader(403)
	})
	mux.HandleFunc("/nohwid", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("x-hwid-not-supported", "true") })
	mux.HandleFunc("/ambig", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("x-hwid-limit", "true")
		w.Header().Add("x-hwid-limit", "false")
	})
	mux.HandleFunc("/br", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "br")
		w.Write([]byte("x"))
	})
	mux.HandleFunc("/404", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	mux.HandleFunc("/toplain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://127.0.0.1/x")
		w.WriteHeader(302)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery == "q=1" {
			w.Write([]byte(linkA))
			return
		}
		w.WriteHeader(500)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	tls := httptest.NewTLSServer(mux)
	defer tls.Close()
	checks := []struct{ path, want string }{
		{"/r0", "<nil>"},
		{"/loop", "HTTP_ERROR:Сервер подписки выполнил слишком много перенаправлений."},
		{"/noloc", "HTTP_ERROR:Сервер подписки вернул перенаправление без адреса."},
		{"/big", "CONTENT_TOO_LARGE:Ответ подписки превышает допустимый размер."},
		{"/hdr", "CONTENT_TOO_LARGE:Заголовки ответа подписки превышают допустимый размер."},
		{"/limit", "SUBSCRIPTION_DEVICE_LIMIT_REACHED:Провайдер отклонил подписку: достигнут лимит зарегистрированных устройств."},
		{"/nohwid", "SUBSCRIPTION_DEVICE_ID_REJECTED:Провайдер не принял анонимный идентификатор клиента подписки."},
		{"/ambig", "HTTP_ERROR:Провайдер вернул противоречивые служебные заголовки."},
		{"/br", "UNSUPPORTED_CONTENT:Сервер прислал сжатый ответ в неподдерживаемом формате."},
		{"/404", "HTTP_ERROR:Сервер подписки вернул HTTP 404."},
	}
	for _, c := range checks {
		if got := code(fetch(srv.URL+c.path, fetchInput{})); got != c.want {
			t.Errorf("%s: %s", c.path, got)
		}
	}
	if hits != 4 {
		t.Errorf("redirect hops: %d", hits)
	}
	// https → http downgrade refused
	tlsConfig = tls.Client().Transport.(*http.Transport).TLSClientConfig
	defer func() { tlsConfig = nil }()
	if got := code(fetch(tls.URL+"/toplain", fetchInput{})); got != "HTTP_ERROR:Переход с HTTPS на незащищённый HTTP отклонён." {
		t.Errorf("downgrade: %s", got)
	}
	if got := code(fetch(tls.URL+"/r0", fetchInput{})); got != "<nil>" {
		t.Errorf("https redirects: %s", got)
	}
	// device headers stop after a cross-origin redirect
	dev := newProvider(t, linkA)
	jump := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dev.URL+"/final", 302)
	}))
	defer jump.Close()
	if err := fetch(jump.URL+"/start", fetchInput{SendDevice: true, Hwid: "broray-0123456789"}); err != nil {
		t.Fatal(err)
	}
	if h := dev.header(0); h.Get("X-Device-Os") != "" || h.Get("X-Hwid") != "" {
		t.Fatalf("cross-origin device headers: %v", h)
	}
}

func TestSummaryHealth(t *testing.T) {
	e := newEnv(t)
	s := e.s.summary()
	h := s["health"].(map[string]any)
	if s["total"] != 0 || s["lastUpdateStatus"] != "never" || s["lastUpdatedAt"] != nil || h["severity"] != "ok" || h["module"] != "subscriptions" {
		t.Fatalf("%v", s)
	}
	id := e.create(t, "https://a.example/s", "")
	e.create(t, "https://b.example/s", `,"enabled":false`)
	s = e.s.summary()
	h = s["health"].(map[string]any)
	if s["total"] != 2 || s["enabled"] != 1 || s["neverUpdatedCount"] != 1 || h["severity"] != "warning" ||
		h["freshness"].(map[string]any)["state"] != "unknown" || h["reasons"].([]any)[0].(map[string]any)["code"] != "SUBSCRIPTION_NEVER_UPDATED" {
		t.Fatalf("%v", s)
	}
	rec := e.record(t, id)
	rec["lastUpdatedEpoch"] = now().Unix() - 13*3600
	rec["lastUpdatedAt"] = "2026-01-01T00:00:00+0300"
	rec["lastUpdateStatus"] = "partial"
	rec["lastUpdateResult"] = map[string]any{"warnings": []any{"w1"}}
	if err := e.s.writeRecord(rec); err != nil {
		t.Fatal(err)
	}
	s = e.s.summary()
	h = s["health"].(map[string]any)
	if s["staleCount"] != 1 || s["partialCount"] != 1 || s["lastUpdateStatus"] != "partial" || s["lastWarnings"].([]any)[0] != "w1" ||
		h["freshness"].(map[string]any)["state"] != "stale" || h["consistent"] != false {
		t.Fatalf("%v", s)
	}
	fn, ok := compat.SnapshotProducer("subscriptions")
	if !ok {
		t.Fatal("snapshot not registered")
	}
	if snap, err := fn(context.Background()); err != nil || snap["health"] == nil {
		t.Fatalf("snapshot %v %v", snap, err)
	}
}
