package runstate

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRecorderTracksSuccessAndNoopAlert(t *testing.T) {
	tmp := t.TempDir()
	statePath := tmp + "/state.json"
	logPath := tmp + "/runs.log"

	r, err := NewRecorder(statePath, logPath)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	if err := r.Start(); err != nil {
		t.Fatalf("start run 1: %v", err)
	}
	if err := r.Finish(false, "no changes", nil); err != nil {
		t.Fatalf("finish run 1: %v", err)
	}

	if err := r.Start(); err != nil {
		t.Fatalf("start run 2: %v", err)
	}
	if err := r.Finish(false, "still no changes", nil); err != nil {
		t.Fatalf("finish run 2 should remain healthy: %v", err)
	}

	b, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if !strings.Contains(string(b), "\"consecutive_noop\": 2") {
		t.Fatalf("expected consecutive noop count in state: %s", string(b))
	}

	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logs), "event=noop") {
		t.Fatalf("expected noop log entry")
	}
}

func TestRecorderTracksFailures(t *testing.T) {
	tmp := t.TempDir()
	r, err := NewRecorder(tmp+"/state.json", tmp+"/runs.log")
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	if err := r.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := r.Finish(false, "", errors.New("boom")); err != nil {
		t.Fatalf("finish failure: %v", err)
	}

	b, err := os.ReadFile(tmp + "/state.json")
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "\"last_outcome\": \"error\"") || !strings.Contains(s, "\"consecutive_failures\": 1") {
		t.Fatalf("unexpected state after failure: %s", s)
	}
}

func TestAcquireLockAndRecoverStaleLock(t *testing.T) {
	tmp := t.TempDir()
	lockPath := tmp + "/run.lock"

	release, err := AcquireLock(lockPath, time.Minute)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer release()

	if _, err := AcquireLock(lockPath, time.Hour); err == nil {
		t.Fatalf("expected second lock acquisition to fail")
	}

	release()
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("chtimes stale lock: %v", err)
	}

	release2, err := AcquireLock(lockPath, time.Minute)
	if err != nil {
		t.Fatalf("acquire stale-recovered lock: %v", err)
	}
	release2()
}
