package runstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State tracks recent run outcomes and aggregate counters.
type State struct {
	LastStartedAt       string `json:"last_started_at,omitempty"`
	LastFinishedAt      string `json:"last_finished_at,omitempty"`
	LastSuccessAt       string `json:"last_success_at,omitempty"`
	LastErrorAt         string `json:"last_error_at,omitempty"`
	LastOutcome         string `json:"last_outcome,omitempty"`
	LastError           string `json:"last_error,omitempty"`
	LastChangeSummary   string `json:"last_change_summary,omitempty"`
	TotalRuns           int    `json:"total_runs"`
	TotalSuccesses      int    `json:"total_successes"`
	TotalFailures       int    `json:"total_failures"`
	TotalChangedRuns    int    `json:"total_changed_runs"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	ConsecutiveNoop     int    `json:"consecutive_noop"`
}

// Recorder persists and appends run-state events.
type Recorder struct {
	statePath string
	logPath   string
	state     State
}

// NewRecorder creates a state recorder and loads prior state if present.
func NewRecorder(statePath, logPath string) (*Recorder, error) {
	r := &Recorder{
		statePath: statePath,
		logPath:   logPath,
	}
	if err := ensureParentDir(statePath); err != nil {
		return nil, err
	}
	if err := ensureParentDir(logPath); err != nil {
		return nil, err
	}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

// Start marks a run as started and appends a start event.
func (r *Recorder) Start() error {
	now := time.Now().UTC().Format(time.RFC3339)
	r.state.TotalRuns++
	r.state.LastStartedAt = now
	r.state.LastOutcome = "running"
	if err := r.save(); err != nil {
		return err
	}
	return r.appendLog("start", "")
}

// Finish records run completion details and appends a terminal event.
func (r *Recorder) Finish(changed bool, summary string, runErr error) error {
	now := time.Now().UTC().Format(time.RFC3339)
	r.state.LastFinishedAt = now

	if runErr != nil {
		r.state.TotalFailures++
		r.state.ConsecutiveFailures++
		r.state.LastErrorAt = now
		r.state.LastOutcome = "error"
		r.state.LastError = runErr.Error()
		if err := r.save(); err != nil {
			return err
		}
		if err := r.appendLog("error", runErr.Error()); err != nil {
			return err
		}
		return nil
	}

	r.state.TotalSuccesses++
	r.state.ConsecutiveFailures = 0
	r.state.LastSuccessAt = now
	r.state.LastError = ""
	r.state.LastChangeSummary = summary

	event := "noop"
	if changed {
		r.state.TotalChangedRuns++
		r.state.ConsecutiveNoop = 0
		r.state.LastOutcome = "changed"
		event = "changed"
	} else {
		r.state.ConsecutiveNoop++
		r.state.LastOutcome = "noop"
	}

	if err := r.save(); err != nil {
		return err
	}
	if err := r.appendLog(event, summary); err != nil {
		return err
	}
	return nil
}

// AcquireLock acquires a lock file or recovers a stale lock.
func AcquireLock(lockPath string, staleAfter time.Duration) (func(), error) {
	if err := ensureParentDir(lockPath); err != nil {
		return nil, err
	}
	created, err := createLock(lockPath)
	if err == nil && created {
		return func() { _ = os.Remove(lockPath) }, nil
	}

	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}

	info, statErr := os.Stat(lockPath)
	if statErr != nil {
		return nil, statErr
	}
	if staleAfter > 0 && time.Since(info.ModTime()) > staleAfter {
		_ = os.Remove(lockPath)
		created, err = createLock(lockPath)
		if err == nil && created {
			return func() { _ = os.Remove(lockPath) }, nil
		}
	}
	return nil, fmt.Errorf("lock already held: %s", lockPath)
}

func createLock(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = f.Close()
	}()
	if _, err := fmt.Fprintf(f, "pid=%d started=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339)); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Recorder) load() error {
	b, err := os.ReadFile(r.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.state = State{}
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &r.state)
}

func (r *Recorder) save() error {
	b, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.statePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, r.statePath)
}

func (r *Recorder) appendLog(event, message string) (err error) {
	f, err := os.OpenFile(r.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	line := fmt.Sprintf("%s event=%s", time.Now().UTC().Format(time.RFC3339), event)
	if message != "" {
		line += fmt.Sprintf(" message=%q", message)
	}
	_, err = f.WriteString(line + "\n")
	return err
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}
