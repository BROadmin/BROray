package servers

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

func TestRating(t *testing.T) {
	for _, c := range []struct {
		ping any
		want string
	}{{nil, "acceptable"}, {int64(0), "excellent"}, {int64(80), "excellent"}, {int64(81), "good"}, {int64(150), "good"},
		{int64(151), "acceptable"}, {int64(300), "acceptable"}, {int64(301), "poor"}} {
		if got := rating(c.ping); got != c.want {
			t.Errorf("rating(%v)=%s want %s", c.ping, got, c.want)
		}
	}
}

func TestFreshnessAndPolicy(t *testing.T) {
	for _, c := range []struct {
		age  int64
		want string
	}{{0, "fresh"}, {1800, "fresh"}, {1801, "stale"}, {10800, "stale"}, {10801, "expired"}} {
		if got := Freshness(c.age, 1800, 10800); got != c.want {
			t.Errorf("age %d: %s", c.age, got)
		}
	}
	if worstFreshness("fresh", "unknown") != "unknown" || worstFreshness("stale", "expired") != "expired" || worstFreshness() != "fresh" {
		t.Error("worstFreshness")
	}
	e := newEnv(t)
	if st, ex := e.s.qualityPolicy(); st != 1800 || ex != 10800 {
		t.Errorf("default policy %d %d", st, ex)
	}
	for _, c := range []struct {
		interval      any
		stale, expire int64
	}{{30, 30*60 + 900, 10800}, {60, 4500, 10800}, {360, 360*60 + 900, 360 * 180}, {45, 4500, 10800}} {
		_ = compat.WriteJSON(autoSwitchFile(e.d), map[string]any{"qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": c.interval})
		if st, ex := e.s.qualityPolicy(); st != c.stale || ex != c.expire {
			t.Errorf("interval %v: %d %d", c.interval, st, ex)
		}
	}
	// summary freshness comes from the quality file mtime
	id := e.addServer(t, "trojan://pw@a.example:443#A")
	_ = writeQuality(e.d, id, map[string]any{"status": "available"})
	now := time.Now()
	old := now.Add(-5 * time.Hour)
	_ = os.Chtimes(QualityPath(e.d, id), old, old)
	e.s.now = func() time.Time { return now }
	_ = compat.WriteJSON(autoSwitchFile(e.d), map[string]any{})
	q := e.s.qualityView(id, 1800, 10800)
	if q["freshness"] != "expired" || q["stale"] != true || !jsonEq(q["ageSeconds"], 5*3600) {
		t.Errorf("quality view %v", q)
	}
	_ = os.WriteFile(QualityPath(e.d, id), []byte("not json"), 0o600)
	if q := e.s.qualityView(id, 1800, 10800); q["status"] != "unknown" || q["freshness"] != "fresh" {
		t.Errorf("invalid file in summary: %v", q)
	}
	if q := e.s.detailsQuality(id, 1800, 10800); q["freshness"] != "unknown" || q["stale"] != true {
		t.Errorf("invalid file in details: %v", q)
	}
}

func TestParsePing(t *testing.T) {
	for _, c := range []struct {
		out          string
		ping, jitter any
	}{
		{"round-trip min/avg/max = 39.6/42.4/45.5 ms", int64(42), int64(6)},
		{"rtt min/avg/max/mdev = 10.5/20.5/30.4/1.0 ms", int64(21), int64(19)},
		{"3 packets transmitted, 0 received, 100% packet loss", nil, nil},
		{"", nil, nil},
	} {
		p, j := parsePing(c.out)
		if !jsonEqAny(p, c.ping) || !jsonEqAny(j, c.jitter) {
			t.Errorf("%q: %v %v", c.out, p, j)
		}
	}
}

func jsonEqAny(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return jsonEq(a, b)
}

func TestConnectionMonitor(t *testing.T) {
	if connectionQuality(42, 1.5, 0) != 100 || connectionQuality(250, 20, 10) != 50 || connectionQuality(400, 60, 50) != 0 {
		t.Error("connectionQuality")
	}
	if qualityLabel(90) != "Отлично" || qualityLabel(75) != "Хорошо" || qualityLabel(50) != "Средне" || qualityLabel(49) != "Плохо" {
		t.Error("qualityLabel")
	}
	e := newEnv(t)
	doc := e.s.monitorStep(context.Background())
	if doc["quality"] != "Нет сервера" || doc["available"] != false {
		t.Fatalf("no server: %v", doc)
	}
	id := e.addServer(t, "trojan://pw@a.example:443#A")
	_ = setActiveID(e.d, id)
	e.pingOutput = "64 bytes: seq=0 time=40.0 ms\n64 bytes: seq=1 time=42.0 ms\n64 bytes: seq=2 time=41.0 ms\n" +
		"3 packets transmitted, 3 packets received, 0% packet loss\nround-trip min/avg/max = 40.0/41.0/42.0 ms\n"
	doc = e.s.monitorStep(context.Background())
	if doc["up"] != true || doc["address"] != "a.example" || doc["ping_ms"] != 41.0 || doc["jitter_ms"] != 1.5 || doc["quality"] != "Отлично" {
		t.Fatalf("up: %v", doc)
	}
	_ = compat.WriteJSON(e.d.RunPath("connection-status.json"), doc)
	e.pingOutput = "3 packets transmitted, 0 packets received, 100% packet loss\n"
	doc = e.s.monitorStep(context.Background())
	if doc["up"] != false || doc["quality"] != "Нет связи" || doc["breaks"] != int64(1) || !jsonEq(doc["packet_loss_percent"], 100) {
		t.Fatalf("down: %v", doc)
	}
}

func TestChooseCandidate(t *testing.T) {
	c := []map[string]any{
		{"id": "a", "ratingRank": 3, "ping": 100.0, "jitter": 5.0},
		{"id": "b", "ratingRank": 4, "ping": 70.0, "jitter": 9.0},
		{"id": "c", "ratingRank": 2, "ping": 20.0, "jitter": 1.0},
	}
	if chooseCandidate(c, "best-quality", "")["id"] != "b" || chooseCandidate(c, "lowest-ping", "")["id"] != "c" ||
		chooseCandidate(c, "preferred", "a")["id"] != "a" || chooseCandidate(c, "preferred", "zzz")["id"] != "b" ||
		chooseCandidate(nil, "best-quality", "") != nil {
		t.Error("chooseCandidate")
	}
	if rankOf("excellent") != 4 || rankOf("poor") != 1 || rankOf("unavailable") != 0 {
		t.Error("rankOf")
	}
	if bounded("7", 3, 1, 10) != 7 || bounded("11", 3, 1, 10) != 3 || bounded(1.5, 3, 1, 10) != 3 {
		t.Error("bounded")
	}
}

func TestCurlStageMapping(t *testing.T) {
	for _, c := range []struct {
		err   error
		stage string
	}{
		{context.DeadlineExceeded, "proxy-timeout"},
		{errors.New("socks connect tcp: general SOCKS server failure"), "socks-handshake"},
		{errors.New("tls: handshake failure"), "proxy-tls"},
		{errors.New("dial tcp: lookup x: no such host"), "proxy-dns"},
		{errors.New("socks connect tcp 127.0.0.1:1: connect: connection refused"), "proxy-connect"},
		{errors.New("EOF"), "proxy-https"},
	} {
		if got := stageOfCurl(curlLike(c.err)); got != c.stage {
			t.Errorf("%v: %s want %s", c.err, got, c.stage)
		}
	}
}

func TestQualityContextDetectsCatalogChange(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	c1, err := e.s.qualityContext("all", "USER")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.qualityContext("all", "SERVER_CHECK_AUTO"); err == nil {
		t.Fatal("SERVER_CHECK_AUTO needs the auto-switch config")
	}
	rec, _ := ReadRecord(e.d, a)
	rec["name"] = "renamed"
	_ = WriteRecord(e.d, rec)
	if c2, _ := e.s.qualityContext("all", "USER"); c2 == c1 {
		t.Fatal("context must change with the record bytes")
	}
	if _, err := e.s.qualitySnapshot("missing"); err != errSnapshotNotFound {
		t.Fatal(err)
	}
}

func TestXrayConfigSettingsAndErrors(t *testing.T) {
	rec, _ := ParseLink("ss://YWVzLTI1Ni1nY206cGFzcw@ss.example:8388#S", "s1", nil)
	cfg, err := XrayConfig(rec, GenSettings{AccessLog: "/a", ErrorLog: "/e"}, "x")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := decodeObject(cfg)
	in := m["inbounds"].([]any)[0]
	if get(in, "listen") != nil || !jsonEq(get(in, "port"), 2080) || get(m, "log", "loglevel") != "warning" {
		t.Fatalf("defaults: %s", cfg)
	}
	ob := m["outbounds"].([]any)[0]
	if get(ob, "streamSettings") != nil || get(ob, "settings", "servers") == nil {
		t.Fatalf("shadowsocks outbound: %s", cfg)
	}
	bad := normalize(rec)
	bad["port"] = 0
	if _, err := XrayConfig(bad, GenSettings{}, "/opt/broray/servers/s1.json"); err == nil ||
		err.Error() != "Ошибка: сервер имеет неправильную структуру: /opt/broray/servers/s1.json" {
		t.Fatalf("validation: %v", err)
	}
}

func TestSocksProbeSetupErrors(t *testing.T) {
	e := newEnv(t)
	s := newSvc(e.d)
	cfgPath := e.d.Root.XrayConfig()
	for _, c := range []struct {
		cfg  string
		want string
	}{
		{"", "CONFIG_UNAVAILABLE"},
		{`{"inbounds":[]}`, "SOCKS_ENDPOINT_UNSUPPORTED"},
		{`{"inbounds":[{"protocol":"socks","port":1080,"settings":{"auth":"password"}}]}`, "SOCKS_ENDPOINT_UNSUPPORTED"},
		{`{"inbounds":[{"protocol":"socks","port":1080,"listen":"host.example"}]}`, "SOCKS_ENDPOINT_UNSUPPORTED"},
		{`{"inbounds":[{"protocol":"socks","port":1080,"listen":"203.0.113.77"}]}`, "SOCKS_ADDRESS_NOT_LOCAL"},
	} {
		os.Remove(cfgPath)
		if c.cfg != "" {
			_ = os.WriteFile(cfgPath, []byte(c.cfg), 0o600)
		}
		r := s.socksProbe(context.Background(), cfgPath, "srv")
		if r["status"] != "unknown" || r["errorCode"] != c.want || r["method"] != "current-socks-https" {
			t.Errorf("%s: %v", c.cfg, r)
		}
	}
	rec, _ := ParseLink("trojan://pw@a.example:443", "a", nil)
	cfg, _ := XrayConfig(rec, GenSettings{}, "x")
	if r := s.isolatedProbe(context.Background(), rec, cfg); r.Success || r.Stage != "probe-xray" {
		t.Errorf("missing binary: %+v", r)
	}
	if r := s.isolatedProbe(context.Background(), rec, []byte(`{"outbounds":[]}`)); r.Stage != "probe-config" {
		t.Errorf("empty outbounds: %+v", r)
	}
}
