package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

var testNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

type bench struct {
	t   *testing.T
	dir string
	key SecretKey
}

// newBench creates a channel directory and an ephemeral test-channel key.
func newBench(t *testing.T) *bench {
	t.Helper()
	sk, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(TrustExtra(TrustedKey{Name: "unit", PublicKey: sk.Public().Encode(), Channels: []string{"test"}, Test: true}))
	return &bench{t: t, dir: t.TempDir(), key: sk}
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func (b *bench) manifest(releases ...Release) *Manifest {
	return &Manifest{SchemaVersion: 1, Channel: "test", IssuedAt: testNow.Add(-time.Hour), ExpiresAt: testNow.Add(24 * time.Hour),
		Releases: releases, Revoked: []string{}}
}

// publish writes manifest.json (+ .sig signed with key) and returns its file:// URL.
func (b *bench) publish(m *Manifest, key SecretKey) string {
	b.t.Helper()
	raw, _ := json.MarshalIndent(m, "", "  ")
	p := filepath.Join(b.dir, "manifest.json")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		b.t.Fatal(err)
	}
	if err := os.WriteFile(p+".sig", key.Sign(raw, "timestamp:1\tfile:manifest.json\thashed", "test"), 0o644); err != nil {
		b.t.Fatal(err)
	}
	return "file://" + p
}

func (b *bench) artefact(name string, content []byte) Artifact {
	b.t.Helper()
	if err := os.WriteFile(filepath.Join(b.dir, name), content, 0o644); err != nil {
		b.t.Fatal(err)
	}
	return Artifact{URL: name, Size: int64(len(content)), SHA256: sha(content)}
}

func rel(v string, arts map[string]Artifact) Release { return Release{Version: v, Installer: arts} }

func src(url string) Source { return Source{Channel: "test", URL: url, AllowTestKeys: true} }

func check(t *testing.T, s Source, now time.Time) (*Checked, error) {
	t.Helper()
	return NewFetcher().Check(context.Background(), s, now)
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if Code(err) != code {
		t.Fatalf("want %s, got %v", code, err)
	}
	if code != "" && Message(err) == "" {
		t.Fatalf("%s without Russian message", code)
	}
}

func TestCheckSignatureAndTrust(t *testing.T) {
	b := newBench(t)
	art := b.artefact("broray", []byte("bin"))
	m := b.manifest(rel("3.5.1", map[string]Artifact{Platform(): art}))
	url := b.publish(m, b.key)

	chk, err := check(t, src(url), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if chk.Manifest.Releases[0].Version != "3.5.1" || chk.KeyID != b.key.Public().IDString() || len(chk.SHA256) != 64 {
		t.Fatalf("%+v", chk)
	}

	// test keys only when the config selects the test channel
	s := src(url)
	s.AllowTestKeys = false
	_, err = check(t, s, testNow)
	wantCode(t, err, ErrKeyUnknown)

	// a test-channel key does not sign other channels
	m.Channel = "stable"
	url = b.publish(m, b.key)
	_, err = check(t, Source{Channel: "stable", URL: url}, testNow)
	wantCode(t, err, ErrKeyUnknown)
	m.Channel = "test"

	// unknown key
	other, _ := GenerateKey()
	url = b.publish(m, other)
	_, err = check(t, src(url), testNow)
	wantCode(t, err, ErrKeyUnknown)

	// tampered manifest (one byte) → bad signature
	url = b.publish(m, b.key)
	p := strings.TrimPrefix(url, "file://")
	raw, _ := os.ReadFile(p)
	if err := os.WriteFile(p, bytes.Replace(raw, []byte("3.5.1"), []byte("3.5.9"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = check(t, src(url), testNow)
	wantCode(t, err, ErrSignatureInvalid)

	// forged trusted comment → bad signature
	url = b.publish(m, b.key)
	sig, _ := os.ReadFile(p + ".sig")
	if err := os.WriteFile(p+".sig", bytes.Replace(sig, []byte("timestamp:1"), []byte("timestamp:2"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = check(t, src(url), testNow)
	wantCode(t, err, ErrSignatureInvalid)

	// garbage / missing signature
	if err := os.WriteFile(p+".sig", []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = check(t, src(url), testNow)
	wantCode(t, err, ErrSignatureInvalid)
	_ = os.Remove(p + ".sig")
	_, err = check(t, src(url), testNow)
	wantCode(t, err, ErrSignatureMissing)

	// channel name in the signed manifest must match the configured one
	url = b.publish(m, b.key)
	s = src(url)
	s.Channel = "beta"
	b.t.Cleanup(TrustExtra(TrustedKey{Name: "beta", PublicKey: b.key.Public().Encode()}))
	_, err = check(t, s, testNow)
	wantCode(t, err, ErrChannelMismatch)

	// a signed but schema-invalid manifest
	bad := b.manifest()
	bad.SchemaVersion = 2
	_, err = check(t, src(b.publish(bad, b.key)), testNow)
	wantCode(t, err, ErrIndexInvalid)
}

func TestCheckTime(t *testing.T) {
	b := newBench(t)
	url := b.publish(b.manifest(rel("3.5.1", map[string]Artifact{Platform(): b.artefact("x", []byte("x"))})), b.key)
	cases := []struct {
		now  time.Time
		code string
	}{
		{testNow, ""},
		{testNow.Add(-time.Hour - ClockSkew + time.Minute), ""},    // slight skew tolerated
		{testNow.Add(-2*time.Hour - ClockSkew), ErrManifestFuture}, // issuedAt in the future
		{testNow.Add(25 * time.Hour), ErrManifestExpired},
		{time.Date(2000, 1, 1, 0, 0, 5, 0, time.UTC), ErrClockUnset},
		{time.Unix(0, 0), ErrClockUnset},
	}
	for _, c := range cases {
		_, err := check(t, src(url), c.now)
		if Code(err) != c.code {
			t.Errorf("%s: want %q got %v", c.now, c.code, err)
		}
	}
	if _, err := check(t, src(url), time.Unix(0, 0)); !strings.Contains(Message(err), "NTP") {
		t.Fatal(Message(err))
	}
}

func TestSourceAndSchemes(t *testing.T) {
	cfg := platform.DefaultConfig()
	s, err := SourceFrom(cfg, "")
	if err != nil || s.Channel != "stable" || s.URL != DefaultURL("stable") || s.AllowTestKeys {
		t.Fatalf("%+v %v", s, err)
	}
	// Explicit placeholder remains fail-closed without contacting the real default host.
	placeholder := s
	placeholder.URL = "https://updates.broray.invalid/channels/stable/manifest.json"
	_, err = check(t, placeholder, testNow)
	wantCode(t, err, ErrChannelUnavailable)
	if Message(err) != "Канал обновлений не настроен." {
		t.Fatal(Message(err))
	}
	cfg.Update.Channel, cfg.Update.ChannelURL = "test", "file:///srv/manifest.json"
	if s, _ = SourceFrom(cfg, ""); s.URL != "file:///srv/manifest.json" || !s.AllowTestKeys {
		t.Fatalf("%+v", s)
	}
	// the URL override belongs to the configured channel only
	if s, _ = SourceFrom(cfg, "stable"); s.URL != DefaultURL("stable") || s.AllowTestKeys {
		t.Fatalf("%+v", s)
	}
	if _, err := SourceFrom(cfg, "../x"); Code(err) != ErrChannelInvalid {
		t.Fatal(err)
	}
	for _, u := range []string{"http://example.com/m.json", "ftp://x/m.json", "file://host/m.json", "file:relative", "https:///m.json",
		"https://u:p@example.com/m.json", "https://example.com/m.json?x=1", "/abs/path"} {
		_, err := check(t, src(u), testNow)
		if Code(err) != ErrChannelInvalid {
			t.Errorf("%s: %v", u, err)
		}
	}
	_, err = check(t, src("file:///nonexistent/manifest.json"), testNow)
	wantCode(t, err, ErrChannelUnavailable)
}

func TestTargetSelection(t *testing.T) {
	p := "mipsle/softfloat"
	a := Artifact{URL: "x", Size: 1, SHA256: strings.Repeat("a", 64)}
	m := &Manifest{Channel: "test", Releases: []Release{
		rel("3.5.0", map[string]Artifact{p: a}),
		rel("3.5.2", map[string]Artifact{p: a}),
		rel("3.5.3", map[string]Artifact{"arm64": a}),
		{Version: "3.6.0", Installer: map[string]Artifact{p: a}, MinimumSourceVersion: "3.5.1"},
	}, Revoked: []string{"3.5.2"}}
	if l, _ := m.Latest(); l.Version != "3.6.0" {
		t.Fatal(l.Version)
	}
	if _, _, err := m.Target("3.5.0", "", p, false); Code(err) != ErrSourceTooOld {
		t.Fatal(err)
	}
	if r, _, err := m.Target("3.5.1", "", p, false); err != nil || r.Version != "3.6.0" {
		t.Fatal(r, err)
	}
	if _, _, err := m.Target("3.5.0", "3.5.2", p, false); Code(err) != ErrVersionRevoked {
		t.Fatal(err)
	}
	if _, _, err := m.Target("3.5.0", "3.5.3", p, false); Code(err) != ErrArchUnsupported {
		t.Fatal(err)
	}
	if _, _, err := m.Target("3.5.0", "3.5.3", "arm/7", false); Code(err) != ErrArchUnsupported {
		t.Fatal(err)
	}
	if _, _, err := m.Target("3.5.0", "3.5.3", "arm64", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Target("3.6.0", "3.5.0", p, false); Code(err) != ErrNotAvailable {
		t.Fatal("downgrade must be refused", err)
	}
	// reinstall: the current version must be listed and not revoked
	if _, _, err := m.Target("3.5.0", "", p, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Target("3.5.2", "", p, true); Code(err) != ErrVersionRevoked {
		t.Fatal(err)
	}
	if _, _, err := m.Target("3.4.9", "", p, true); Code(err) != ErrVersionNotListed {
		t.Fatal(err)
	}
	// bare GOARCH entries match any variant
	if _, ok := (Release{Installer: map[string]Artifact{"mipsle": a}}).ArtifactFor(p); !ok {
		t.Fatal("bare arch")
	}
	r := Summarize(&Checked{Manifest: m}, "3.5.1", p, testNow)
	if !r.UpdateAvailable || r.Latest != "3.6.0" || r.Relation != "newer" || r.ReinstallAvailable {
		t.Fatalf("%+v", r)
	}
	r = Summarize(&Checked{Manifest: m}, "3.5.0", p, testNow)
	if r.UpdateAvailable || r.ProblemCode != ErrSourceTooOld || !r.ReinstallAvailable {
		t.Fatalf("%+v", r)
	}
	r = Summarize(&Checked{Manifest: m}, "3.7.0", p, testNow)
	if r.Relation != "older" || r.UpdateAvailable {
		t.Fatalf("%+v", r)
	}
}

func TestManifestValidation(t *testing.T) {
	good := `{"schemaVersion":1,"channel":"test","issuedAt":"2026-11-01T00:00:00Z","expiresAt":"2026-12-01T00:00:00Z",
	 "releases":[{"version":"3.5.1","installer":{"arm64":{"url":"a","size":1,"sha256":"` + strings.Repeat("0", 64) + `"}}}],
	 "revoked":[],"rollout":{"percent":50},"futureField":true}`
	if _, err := ParseManifest([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string][2]string{
		"schema":      {`"schemaVersion":1`, `"schemaVersion":2`},
		"channel":     {`"channel":"test"`, `"channel":"Te st"`},
		"inverted":    {`"expiresAt":"2026-12-01T00:00:00Z"`, `"expiresAt":"2026-10-01T00:00:00Z"`},
		"version":     {`"version":"3.5.1"`, `"version":"../../etc"`},
		"size":        {`"size":1`, `"size":0`},
		"huge":        {`"size":1`, `"size":999999999`},
		"sha":         {strings.Repeat("0", 64), strings.Repeat("Z", 64)},
		"format":      {`"size":1`, `"size":1,"format":"zip"`},
		"entry":       {`"size":1`, `"size":1,"format":"tar.gz","entry":"../broray","unpackedSize":5`},
		"noUnpacked":  {`"size":1`, `"size":1,"format":"tar.gz","entry":"broray"`},
		"revoked":     {`"revoked":[]`, `"revoked":["x"]`},
		"rollout":     {`"percent":50`, `"percent":101`},
		"noInstaller": {`"installer":{"arm64":{"url":"a","size":1,"sha256":"` + strings.Repeat("0", 64) + `"}}`, `"installer":{}`},
	} {
		if _, err := ParseManifest([]byte(strings.Replace(good, mut[0], mut[1], 1))); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestDownloadVerification(t *testing.T) {
	b := newBench(t)
	content := bytes.Repeat([]byte("BROray"), 1000)
	good := b.artefact("broray-bin", content)
	f := NewFetcher()
	mURL := "file://" + filepath.Join(b.dir, "manifest.json")
	dl := func(a Artifact, manifestURL string) (string, error) {
		return f.Download(context.Background(), a, manifestURL, t.TempDir(), nil)
	}
	p, err := dl(good, mURL)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, content) {
		t.Fatal("content")
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o755 {
		t.Fatal(st.Mode())
	}
	short, long, bad := good, good, good
	short.Size++
	long.Size--
	bad.SHA256 = sha([]byte("other"))
	_, err = dl(short, mURL)
	wantCode(t, err, ErrDownloadSize)
	_, err = dl(long, mURL)
	wantCode(t, err, ErrDownloadSize)
	_, err = dl(bad, mURL)
	wantCode(t, err, ErrDownloadHash)

	// tarball: only the named member is extracted; links and traversal ignored
	var tb bytes.Buffer
	gz := gzip.NewWriter(&tb)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "../../evil", Mode: 0o644, Size: 4, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("evil"))
	_ = tw.WriteHeader(&tar.Header{Name: "pkg/link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink})
	_ = tw.WriteHeader(&tar.Header{Name: "pkg/broray", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(content)
	_ = tw.Close()
	_ = gz.Close()
	ta := b.artefact("broray.tar.gz", tb.Bytes())
	ta.Format, ta.Entry, ta.UnpackedSize = "tar.gz", "pkg/broray", int64(len(content))
	dir := t.TempDir()
	p, err = f.Download(context.Background(), ta, mURL, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, content) {
		t.Fatal("tar content")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("only the installer may be extracted: %v", ents)
	}
	wrongEntry := ta
	wrongEntry.Entry = "pkg/link"
	_, err = dl(wrongEntry, mURL)
	wantCode(t, err, ErrArtifactInvalid)
	wrongSize := ta
	wrongSize.UnpackedSize++
	_, err = dl(wrongSize, mURL)
	wantCode(t, err, ErrArtifactInvalid)

	// https: short body, oversized body, Content-Length mismatch, 404
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write(content)
		case "/short": // announced length is right, the connection ends early
			w.Header().Set("Content-Length", "6000")
			_, _ = w.Write(content[:100])
		case "/chunked-long":
			w.(http.Flusher).Flush()
			_, _ = w.Write(append(content, 'x'))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f.Client = srv.Client()
	hURL := srv.URL + "/channel/manifest.json"
	for path, code := range map[string]string{"/ok": "", "/short": ErrDownloadFailed, "/chunked-long": ErrDownloadSize, "/missing": ErrDownloadFailed} {
		a := good
		a.URL = path
		_, err := dl(a, hURL)
		if Code(err) != code {
			t.Errorf("%s: want %q got %v", path, code, err)
		}
	}
	// file:// artefacts are not accepted from an https manifest
	a := good
	a.URL = "file:///etc/passwd"
	_, err = dl(a, hURL)
	wantCode(t, err, ErrArtifactInvalid)
	// relative URLs resolve against the manifest
	if u, err := resolve("https://h/c/test/manifest.json", "../x/broray.tar.gz"); err != nil || u.String() != "https://h/c/x/broray.tar.gz" {
		t.Fatal(u, err)
	}
}

func TestHTTPSChannel(t *testing.T) {
	b := newBench(t)
	art := b.artefact("broray", []byte("bin"))
	b.publish(b.manifest(rel("3.5.1", map[string]Artifact{Platform(): art})), b.key)
	srv := httptest.NewTLSServer(http.FileServer(http.Dir(b.dir)))
	defer srv.Close()
	f := NewFetcher()
	f.Client = srv.Client()
	chk, err := f.Check(context.Background(), src(srv.URL+"/manifest.json"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Download(context.Background(), art, chk.URL, t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
	_, err = f.Check(context.Background(), src(srv.URL+"/nothing.json"), testNow)
	wantCode(t, err, ErrChannelUnavailable)
}

func TestCheckSpace(t *testing.T) {
	dir := t.TempDir()
	if err := CheckSpace(map[string]int64{dir: 1}); err != nil {
		t.Fatal(err)
	}
	err := CheckSpace(map[string]int64{dir: 1 << 50})
	wantCode(t, err, ErrSpaceRequired)
	// two directories on the same filesystem add up
	var sf syscall.Statfs_t
	if err := syscall.Statfs(dir, &sf); err != nil {
		t.Fatal(err)
	}
	half := int64(uint64(sf.Bavail)*uint64(sf.Bsize)/2) + 1
	if err := CheckSpace(map[string]int64{dir: half - SpaceMargin}); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	_ = os.Mkdir(sub, 0o700)
	wantCode(t, CheckSpace(map[string]int64{dir: half, sub: half}), ErrSpaceRequired)
}

func TestVersions(t *testing.T) {
	order := []string{"3.2.0-r9", "3.2.0-r12", "3.5.0-dev5", "3.5.0-dev10", "3.5.0-rc1", "3.5.0-rc2", "3.5.0", "3.5.1", "3.10.0"}
	for i := range order {
		for j := range order {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := CompareVersions(order[i], order[j]); got != want {
				t.Errorf("%s vs %s: %d", order[i], order[j], got)
			}
		}
	}
	for _, v := range []string{"", "3.5", "3.5.0/..", "3.5.0-", "../3.5.0", "3.5.0 -x", "3.5.0-a..b"} {
		if ValidVersion(v) {
			t.Errorf("%q valid", v)
		}
	}
	for goarch, want := range map[string]string{"arm64": "arm64", "amd64": "amd64"} {
		if PlatformKey(goarch, nil) != want {
			t.Error(goarch)
		}
	}
	if PlatformKey("arm", map[string]string{"GOARM": "7,softfloat"}) != "arm/7" || PlatformKey("arm", nil) != "arm/7" ||
		PlatformKey("mipsle", map[string]string{"GOMIPS": "softfloat"}) != "mipsle/softfloat" || PlatformKey("mips", nil) != "mips/hardfloat" {
		t.Fatal("variants")
	}
}

// TestInstallerABIv1Golden freezes the launcher → installer command line.
func TestInstallerABIv1Golden(t *testing.T) {
	got := InstallerArgs("3.5.0-dev5", "/opt/broray", "stable")
	want := []string{"installer", "apply", "--abi", "1", "--from", "3.5.0-dev5", "--state", "/opt/broray", "--channel", "stable", "--progress", "json"}
	if !reflect.DeepEqual(got, want) || ABI != 1 {
		t.Fatalf("ABI v1 changed: %q", got)
	}
}

func TestMinisignKeyFiles(t *testing.T) {
	sk, _ := GenerateKey()
	back, err := ParseSecretKeyFile(sk.EncodeFile())
	if err != nil || back.ID != sk.ID || !bytes.Equal(back.Key, sk.Key) {
		t.Fatal(err)
	}
	f := sk.EncodeFile()
	f[len(f)-5] ^= 1 // break the checksum (inside the base64 payload)
	if _, err := ParseSecretKeyFile(f); err == nil {
		t.Fatal("corrupted key accepted")
	}
	pk, err := ParsePublicKey("untrusted comment: x\n" + sk.Public().Encode() + "\n")
	if err != nil || pk.ID != sk.ID {
		t.Fatal(err)
	}
	if _, err := ParsePublicKey(testChannelPublicKey); err != nil {
		t.Fatal("built-in test key", err)
	}
	msg := []byte("m")
	sig, err := ParseSignature(sk.Sign(msg, "tc", "uc"))
	if err != nil || sig.Verify(pk, msg) != nil || sig.Verify(pk, []byte("n")) == nil {
		t.Fatal("sign/verify")
	}
}

// TestLauncherRelaysInstaller runs Apply with a shell-script "installer":
// its progress is relayed and its result decides; no result = failure.
func TestLauncherRelaysInstaller(t *testing.T) {
	for _, c := range []struct {
		name, script, code string
		ok                 bool
	}{
		{"ok", `echo '{"event":"stage","stage":"staging","progress":45,"message":"m"}'; echo noise; echo '{"event":"result","ok":true,"state":"committed"}'`, "", true},
		{"rolled back", `echo '{"event":"result","ok":false,"state":"rolled-back","code":"UPDATE_HEALTH_FAILED","message":"нет"}'; exit 1`, "UPDATE_HEALTH_FAILED", false},
		{"no result", `echo '{"event":"stage","stage":"switching","progress":70}'; exit 0`, ErrInstallerFailed, false},
		{"ok but exit 1", `echo '{"event":"result","ok":true,"state":"committed"}'; exit 1`, ErrInstallerFailed, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBench(t)
			script := "#!/bin/sh\n[ \"$1 $2 $3 $4\" = \"installer apply --abi 1\" ] || exit 9\n" + c.script + "\n"
			art := b.artefact("installer.sh", []byte(script))
			url := b.publish(b.manifest(rel("3.5.1", map[string]Artifact{Platform(): art})), b.key)
			root := platform.Root{Dir: t.TempDir()}
			_ = root.EnsureDirs()
			var out bytes.Buffer
			err := Apply(context.Background(), ApplyOptions{Root: root, Source: src(url), Current: "3.5.0", Now: func() time.Time { return testNow },
				Progress: NewEmitter(&out, "launcher"), OperationID: "update-1"})
			if (err == nil) != c.ok || (err != nil && Code(err) != c.code) {
				t.Fatalf("%v\n%s", err, out.String())
			}
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			var last Event
			_ = json.Unmarshal([]byte(lines[len(lines)-1]), &last)
			if last.Event != "result" || last.Source != "launcher" || *last.OK != c.ok {
				t.Fatalf("last line %s", lines[len(lines)-1])
			}
			if c.name == "ok" && !strings.Contains(out.String(), `"stage":"staging"`) {
				t.Fatal("installer progress not relayed")
			}
			if ents, _ := os.ReadDir(root.Tmp()); len(ents) != 0 {
				t.Fatalf("tmp not cleaned: %v", ents)
			}
		})
	}
	// a second launcher is refused while one runs
	root := platform.Root{Dir: t.TempDir()}
	_ = root.EnsureDirs()
	l, _ := platform.TryLock(root.LauncherLock())
	defer l.Release()
	err := Apply(context.Background(), ApplyOptions{Root: root, Source: src("file:///x"), Current: "3.5.0"})
	wantCode(t, err, ErrBusy)
}
