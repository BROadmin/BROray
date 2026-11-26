package installstats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

var fixtureBuild = Build{Version: "3.5.10", Revision: "r2"}
var fixtureTime = time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)

func fixtureEvent(t *testing.T) Event {
	t.Helper()
	event, err := NewEvent("install", fixtureBuild, Build{}, "arm64", RouterIdentity("KN-2710", "SYNTHETIC-527841"))
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestInstallStatsNetcrazeModelKeepsOriginalDeviceKey(t *testing.T) {
	const serial = "SYNTHETIC-527841"
	unknown := RouterIdentity("", serial)
	for _, model := range []string{"NC-1812", "Ultra (NC-1812)"} {
		identity := RouterIdentity(model, serial)
		if identity.Model != "NC-1812" || identity.DeviceKey != unknown.DeviceKey || len(identity.DeviceKey) != 64 {
			t.Fatalf("expected model NC-1812 and unchanged device key for %q", model)
		}
		event, err := NewEvent("update", fixtureBuild, Build{"3.5.10", "r1"}, "arm64", identity)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(event)
		if err != nil || bytes.Contains(raw, []byte(serial)) {
			t.Fatal("unexpected wire payload")
		}
		if parsed, err := ParseEvent(raw); err != nil || parsed != event {
			t.Fatalf("could not round trip NC model: %v", err)
		}
	}
}

func TestInstallStatsEventContract(t *testing.T) {
	event := fixtureEvent(t)
	raw, err := json.Marshal(event)
	if err != nil || bytes.Contains(raw, []byte("SYNTHETIC-527841")) || len(event.DeviceKey) != 64 ||
		event.Model != "KN-2710" || len(event.EventID) != 32 {
		t.Fatalf("wrong event contract, %v", err)
	}
	if parsed, err := ParseEvent(raw); err != nil || parsed != event {
		t.Fatalf("could not round trip exact event: %v", err)
	}
	if a, b := RouterIdentity("KN-2710", "SYNTHETIC-527841"), RouterIdentity("KN-2710", "Synthetic-527841"); a != b {
		t.Fatal("same canonical serial produced different keys")
	}
	if a, b := RouterIdentity("KN-2710", "SYNTHETIC-527841"), RouterIdentity("KN-2710", "SYNTHETIC-527842"); a.DeviceKey == b.DeviceKey {
		t.Fatal("different serials produced the same key")
	}
	if x := RouterIdentity("Keenetic owner's title", "127.0.0.1"); x != (Identity{}) {
		t.Fatal("malformed identity projected into event")
	}
	if _, err := NewEvent("update", fixtureBuild, fixtureBuild, "arm64", Identity{}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatal("same-build reinstall becomes an update")
	}
	previous := Build{Version: "3.5.10", Revision: "r1"}
	changed, err := NewEvent("update", fixtureBuild, previous, "mipsle/softfloat", Identity{})
	if err != nil || changed.PreviousRevision != "r1" || changed.PreviousVersion != "3.5.10" {
		t.Fatalf("revision change was ignored: %v", err)
	}
	if _, err := NewEvent("install", fixtureBuild, previous, "arm64", Identity{}); err == nil {
		t.Fatal("installation carries update source fields")
	}
	if _, err := NewEvent("install", fixtureBuild, Build{}, "mipsle/hardfloat", Identity{}); err == nil {
		t.Fatal("unknown platform accepted for released statistics")
	}
	for _, altered := range [][]byte{
		append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"serial":"SYNTHETIC-527841"}`)...),
		append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"eventId":"alias"}`)...),
		[]byte(strings.Replace(string(raw), `"eventId"`, `"EventId"`, 1)),
		append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"previousVersion":""}`)...),
	} {
		if _, err := ParseEvent(altered); !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("invalid event accepted: %v", err)
		}
	}
}

func TestInstallStatsIdentifiesReleaseExecutables(t *testing.T) {
	ctx := context.Background()
	legacy := os.Getenv("BRORAY_STATS_LEGACY_BINARY")
	stamped := os.Getenv("BRORAY_STATS_STAMPED_BINARY")
	if legacy == "" || stamped == "" {
		t.Skip("requires the archived and stamped executables")
	}
	legacyHash, err := ExecutableDigest(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if build, ok := ReadBinaryBuild(ctx, legacy, "3.5.9", "arm64", legacyHash); !ok || build != (Build{"3.5.9", "r9"}) {
		t.Fatal("the exact archived release was not identified")
	}
	if _, ok := ReadBinaryBuild(ctx, legacy, "3.5.9", "arm/7", legacyHash); ok {
		t.Fatal("the archived release was matched to the wrong ABI")
	}
	if _, ok := ReadBinaryBuild(ctx, legacy, "3.5.9", "arm64", strings.Repeat("c", 64)); ok {
		t.Fatal("unknown legacy bytes were assigned a revision")
	}
	currentHash, err := ExecutableDigest(stamped)
	if err != nil {
		t.Fatal(err)
	}
	if build, ok := ReadBinaryBuild(ctx, stamped, "3.5.10", "arm64", currentHash); !ok || build != (Build{"3.5.10", "r14"}) {
		t.Fatal("the actual executing build metadata was not identified")
	}
	if _, ok := ReadBinaryBuild(ctx, stamped, "3.5.10", "mips/softfloat", currentHash); ok {
		t.Fatal("a stamped build was given the wrong ABI")
	}
	if _, ok := ReadBinaryBuild(ctx, stamped, "3.5.9", "arm64", currentHash); ok {
		t.Fatal("a stamped build was given the wrong version")
	}
}

func TestInstallStatsFirstInstallProofAndRecovery(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	queue := NewQueue(root)
	hash := strings.Repeat("a", 64)
	ctx := context.Background()
	if err := queue.ArmFirstInstall(fixtureBuild.Version, hash, fixtureTime); err != nil {
		t.Fatal(err)
	}
	intent, err := queue.readFirstInstall()
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.ArmFirstInstall(fixtureBuild.Version, hash, fixtureTime); err != nil {
		t.Fatal(err)
	}
	if err := queue.ArmFirstInstall(fixtureBuild.Version, strings.Repeat("b", 64), fixtureTime); !errors.Is(err, ErrEventConflict) {
		t.Fatalf("unexpected ownership replacement: %v", err)
	}
	if err := queue.ConfirmFirstInstall(ctx, Build{Version: "3.5.9", Revision: "r1"}, "arm64", fixtureTime,
		func(context.Context, string) (Identity, bool) {
			t.Fatal("foreign build reached readiness proof")
			return Identity{}, false
		}); err != nil {
		t.Fatal(err)
	}
	if err := queue.ConfirmFirstInstall(ctx, fixtureBuild, "arm64", fixtureTime,
		func(context.Context, string) (Identity, bool) { return Identity{}, false }); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.read(intent.EventID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unproven installation was enqueued: %v", err)
	}
	guard, err := queue.locked(".queue.lock")
	if err != nil {
		t.Fatal(err)
	}
	err = queue.ConfirmFirstInstall(ctx, fixtureBuild, "arm64", fixtureTime, func(_ context.Context, expected string) (Identity, bool) {
		return Identity{Model: "KN-2710"}, expected == hash
	})
	guard.Release()
	if !errors.Is(err, platform.ErrLocked) {
		t.Fatalf("did not isolate the interrupted enqueue: %v", err)
	}
	frozen, err := queue.readFirstInstall()
	if err != nil || frozen.Event == nil || frozen.Event.EventID != intent.EventID {
		t.Fatalf("full event was not frozen before enqueue: %v", err)
	}
	reopened := NewQueue(root)
	if err := reopened.ConfirmFirstInstall(ctx, Build{}, "", fixtureTime.Add(time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	enqueued, err := reopened.read(intent.EventID)
	if err != nil || enqueued.Event != *frozen.Event || enqueued.Created != fixtureTime.Unix() {
		t.Fatalf("recovery did not enqueue exact event: %v", err)
	}
	if _, err := reopened.readFirstInstall(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful enqueue left the intent present: %v", err)
	}
}

func TestInstallStatsPersistentRetry(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	queue := NewQueue(root)
	event := fixtureEvent(t)
	ctx := context.Background()
	if err := queue.Add(event, fixtureTime); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	unblock := make(chan struct{})
	defer func() {
		select {
		case <-unblock:
		default:
			close(unblock)
		}
	}()
	var attempts []string
	var mu sync.Mutex
	service := httptest.NewTLSServer(http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		mu.Lock()
		attempts = append(attempts, string(body))
		first := len(attempts) == 1
		mu.Unlock()
		if req.Method != http.MethodPost || req.Header.Get("Authorization") != "" || req.Header.Get("Content-Type") != "application/json" ||
			req.Header.Get("Cookie") != "" {
			t.Error("unexpected request metadata")
		}
		id := event.EventID
		if first {
			close(entered)
			<-unblock
			id = "00000000000000000000000000000000"
		}
		_ = json.NewEncoder(resp).Encode(map[string]any{"eventId": id, "accepted": true})
	}))
	t.Cleanup(service.Close)
	sender, err := newSender(service.URL+"/events", service.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := queue.Flush(ctx, sender, fixtureTime); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("flush did not POST: %v", err)
	case <-time.After(4 * time.Second):
		t.Fatal("fixture did not receive the POST")
	}
	reopened := NewQueue(root)
	if err := reopened.Add(event, fixtureTime); err != nil {
		t.Fatalf("in-flight delivery blocked idempotent queue insertion: %v", err)
	}
	if _, err := reopened.Flush(ctx, sender, fixtureTime); !errors.Is(err, platform.ErrLocked) {
		t.Fatalf("concurrent sender did not preserve single delivery owner: %v", err)
	}
	close(unblock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if out, err := reopened.Flush(ctx, sender, fixtureTime.Add(30*time.Second)); err != nil || out.Acknowledged != 0 {
		t.Fatalf("delivery bypassed persisted retry delay: %+v %v", out, err)
	}
	if out, err := reopened.Flush(ctx, sender, fixtureTime.Add(time.Minute)); err != nil || out.Acknowledged != 1 {
		t.Fatalf("exact acknowledgement did not retire the event: %+v %v", out, err)
	}
	if out, err := reopened.Flush(ctx, sender, fixtureTime.Add(2*time.Hour)); err != nil || out.Acknowledged != 0 {
		t.Fatalf("acknowledged event was replayed: %+v %v", out, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 2 || attempts[0] != attempts[1] || strings.Contains(attempts[0], "SYNTHETIC-527841") ||
		strings.Contains(attempts[0], "nextAttempt") || strings.Contains(attempts[0], "createdAt") {
		t.Fatal("wire body changed between attempts or included local state")
	}
}

func TestInstallStatsBoundedAndConfinedQueue(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	queue := NewQueue(root)
	event := fixtureEvent(t)
	if err := queue.Add(event, fixtureTime); err != nil {
		t.Fatal(err)
	}
	conflicting := event
	conflicting.Revision = "r3"
	if err := queue.Add(conflicting, fixtureTime); !errors.Is(err, ErrEventConflict) {
		t.Fatalf("same ID accepted different contents: %v", err)
	}
	for i := 1; i < queueCapacity; i++ {
		if err := queue.Add(fixtureEvent(t), fixtureTime); err != nil {
			t.Fatal(err)
		}
	}
	if err := queue.Add(fixtureEvent(t), fixtureTime); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("unbounded queue insertion: %v", err)
	}
	foreign := filepath.Join(root.Dir, "foreign")
	if err := os.WriteFile(foreign, []byte("outside queue"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(queue.dir, filename(strings.Repeat("b", 32)))
	if err := os.Symlink(foreign, link); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	service := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer service.Close()
	sender, err := newSender(service.URL+"/events", service.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	late := fixtureTime.Add(DeliveryWindow + time.Minute)
	if out, err := queue.Flush(context.Background(), sender, late); err != nil || out.Expired != queueCapacity || requests.Load() != 0 {
		t.Fatalf("expiry sent stale events or removed a foreign file: %+v %v", out, err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "outside queue" {
		t.Fatal("queue crossed its private file boundary", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("queue touched opaque entry", err)
	}
	if err := queue.Add(fixtureEvent(t), late); err != nil {
		t.Fatalf("expired entries blocked a new event: %v", err)
	}
}

func TestInstallStatsRejectsRedirect(t *testing.T) {
	ctx := context.Background()
	queue := NewQueue(platform.Root{Dir: t.TempDir()})
	event := fixtureEvent(t)
	if err := queue.Add(event, fixtureTime); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"http://example.test/events", "https://user@example.test/events", "https://example.test/events?token=value"} {
		if _, err := NewSender(bad); err == nil {
			t.Fatalf("invalid intake destination accepted")
		}
	}
	var redirected atomic.Int32
	service := httptest.NewTLSServer(http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/events" {
			http.Redirect(resp, req, "/other-path", http.StatusTemporaryRedirect)
			return
		}
		redirected.Add(1)
		_ = json.NewEncoder(resp).Encode(map[string]any{"eventId": event.EventID, "accepted": true})
	}))
	defer service.Close()
	sender, err := newSender(service.URL+"/events", service.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := queue.Flush(ctx, sender, fixtureTime); err != nil || out.Deferred != 1 || redirected.Load() != 0 {
		t.Fatalf("allowed redirect or lost unacknowledged event: %+v %v", out, err)
	}
}
