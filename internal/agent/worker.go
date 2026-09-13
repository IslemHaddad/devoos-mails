package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// WorkerConfig configures the worker loop.
type WorkerConfig struct {
	MasterURL string        // e.g. http://devops-mails:8671
	Token     string        // shared bearer token
	WorkerID  string        // stable identifier
	Hostname  string        // reported to the master
	Command   string        // check, run via `sh -c`; exit 0 == "true"
	Interval  time.Duration // between checks
	Repeat    bool          // notify on every true check, not just the rising edge
	Email     EmailSpec     // what the master should send when the check fires
}

// Worker runs the check loop and notifies the master.
type Worker struct {
	cfg    WorkerConfig
	client *http.Client
}

// NewWorker builds a worker with a default HTTP client.
func NewWorker(cfg WorkerConfig) *Worker {
	return &Worker{cfg: cfg, client: &http.Client{Timeout: 20 * time.Second}}
}

// Run registers with the master and loops until ctx is cancelled. By default it
// is edge-triggered: it notifies only when a check transitions from not-true to
// true, so a persistently-true condition mails once, not every interval.
func (w *Worker) Run(ctx context.Context) error {
	if strings.TrimSpace(w.cfg.Command) == "" {
		return fmt.Errorf("no check command configured")
	}
	if err := w.register(ctx); err != nil {
		// Registration is best-effort; the master may come up after the worker.
		log.Printf("worker: register failed (will still run checks): %v", err)
	}

	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	lastTrue := false
	check := func() {
		code, out := w.runCheck(ctx)
		isTrue := code == 0
		log.Printf("worker: check exit=%d true=%v", code, isTrue)
		if isTrue && (!lastTrue || w.cfg.Repeat) {
			if err := w.trigger(ctx, code, out); err != nil {
				log.Printf("worker: trigger failed: %v", err)
			}
		}
		lastTrue = isTrue
	}

	check() // run once immediately
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			check()
		}
	}
}

// runCheck executes the command via `sh -c` and returns its exit code and a
// short tail of combined output for the trigger message.
func (w *Worker) runCheck(ctx context.Context) (int, string) {
	cmd := exec.CommandContext(ctx, "sh", "-c", w.cfg.Command)
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	if err == nil {
		return 0, msg
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), msg
	}
	// Command could not be started at all.
	return -1, strings.TrimSpace(msg + " " + err.Error())
}

func (w *Worker) register(ctx context.Context) error {
	body := RegisterRequest{
		WorkerID:    w.cfg.WorkerID,
		Hostname:    w.cfg.Hostname,
		Command:     w.cfg.Command,
		IntervalSec: int(w.cfg.Interval.Seconds()),
	}
	return w.post(ctx, PathRegister, body, nil)
}

func (w *Worker) trigger(ctx context.Context, code int, message string) error {
	body := TriggerRequest{
		WorkerID: w.cfg.WorkerID,
		Command:  w.cfg.Command,
		ExitCode: code,
		Message:  message,
		Email:    w.cfg.Email,
	}
	var resp TriggerResponse
	if err := w.post(ctx, PathTrigger, body, &resp); err != nil {
		return err
	}
	suffix := ""
	if resp.Error != "" {
		suffix = " (" + resp.Error + ")"
	}
	log.Printf("worker: master sent %d, failed %d%s", resp.Sent, resp.Failed, suffix)
	return nil
}

func (w *Worker) post(ctx context.Context, path string, in, out any) error {
	buf, err := json.Marshal(in)
	if err != nil {
		return err
	}
	url := strings.TrimRight(w.cfg.MasterURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+w.cfg.Token)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("master returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
