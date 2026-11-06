package routes

import (
	"context"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

type scheduleRoundTripFunc func(*http.Request) (*http.Response, error)

func (f scheduleRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestScheduledCatalogCheckStopsWhenInitialStatusCannotPersist(t *testing.T) {
	env, github := schedEnv(t)
	env.setSchedule(1, false)
	if err := os.Mkdir(env.e.scheduleStatusFile(), 0o700); err != nil {
		t.Fatal(err)
	}
	beforeCommands := env.r.cmdCount
	result, err := env.e.runCatalogUpdate(context.Background(), "SCHEDULER", nil)
	if err == nil {
		t.Errorf("scheduled check reported success without durable attempt status: %v", result)
	}
	github.mu.Lock()
	apiCalls, rawCalls := github.apiCalls, github.rawCalls
	github.mu.Unlock()
	if apiCalls != 0 || rawCalls != 0 {
		t.Errorf("GitHub was contacted after status write failed: API=%d raw=%d", apiCalls, rawCalls)
	}
	if env.r.cmdCount != beforeCommands {
		t.Errorf("router changed after status write failed: before=%d after=%d", beforeCommands, env.r.cmdCount)
	}
}

func TestScheduledCatalogCheckReportsFinalStatusWriteFailure(t *testing.T) {
	env, github := schedEnv(t)
	env.setSchedule(1, false)
	base := env.e.http.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	var once sync.Once
	var tamperErr error
	env.e.http.Transport = scheduleRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		once.Do(func() {
			path := env.e.scheduleStatusFile()
			if err := os.Remove(path); err != nil {
				tamperErr = err
				return
			}
			tamperErr = os.Mkdir(path, 0o700)
		})
		return base.RoundTrip(r)
	})
	result, err := env.e.runCatalogUpdate(context.Background(), "SCHEDULER", nil)
	if tamperErr != nil {
		t.Fatal(tamperErr)
	}
	github.mu.Lock()
	apiCalls := github.apiCalls
	github.mu.Unlock()
	if apiCalls == 0 {
		t.Fatal("test did not exercise the successful catalog fetch")
	}
	if err == nil {
		t.Errorf("scheduled check reported success after final status write failed: %v", result)
	}
}

func TestScheduleStatusWriteFailureDefersNextCheckUntilPersistenceRecovers(t *testing.T) {
	env, _ := schedEnv(t)
	env.setSchedule(1, false)
	if err := os.Mkdir(env.e.scheduleStatusFile(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := env.e.runCatalogUpdate(context.Background(), "SCHEDULER", nil); err == nil {
		t.Fatal("scheduled check reported success without durable status")
	}
	if due := env.e.nextDue(env.e.scheduleSettings(), env.e.readScheduleStatus()); !due.After(nowFn()) {
		t.Fatalf("failed status write has no retry backoff: due=%v", due)
	}
	if env.e.enqueueDueCheck() {
		t.Fatal("scheduler enqueued another check immediately after status write failure")
	}
	if err := os.Remove(env.e.scheduleStatusFile()); err != nil {
		t.Fatal(err)
	}
	if err := env.e.writeScheduleStatus(func(m obj) {
		m["lastAttemptEpoch"] = time.Now().Add(-2 * time.Hour).Unix()
	}); err != nil {
		t.Fatal(err)
	}
	if due := env.e.nextDue(env.e.scheduleSettings(), env.e.readScheduleStatus()); due.After(nowFn()) {
		t.Fatalf("successful status write did not clear transient retry backoff: due=%v", due)
	}
}
