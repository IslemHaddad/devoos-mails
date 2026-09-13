package agent

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"devops-mails/internal/config"
	"devops-mails/internal/mailer"
)

const maxEvents = 100

// WorkerState is the master's view of a registered worker.
type WorkerState struct {
	ID          string    `json:"id"`
	Hostname    string    `json:"hostname"`
	Command     string    `json:"command"`
	IntervalSec int       `json:"interval_sec"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	Triggers    int       `json:"triggers"`
}

// Event records a single trigger and its delivery outcome (newest first).
type Event struct {
	Time       time.Time `json:"time"`
	WorkerID   string    `json:"worker_id"`
	Command    string    `json:"command"`
	ExitCode   int       `json:"exit_code"`
	Message    string    `json:"message"`
	Recipients int       `json:"recipients"`
	Sent       int       `json:"sent"`
	Failed     int       `json:"failed"`
	Error      string    `json:"error,omitempty"`
}

// Master handles worker registrations and triggers, relaying emails through the
// shared SMTP configuration.
type Master struct {
	token string
	cfg   *config.Store

	mu      sync.Mutex
	workers map[string]*WorkerState
	events  []Event
}

// NewMaster builds a master. If token is empty, authentication is disabled and
// a warning is logged (any client can trigger a send — set a token in prod).
func NewMaster(token string, cfg *config.Store) *Master {
	if token == "" {
		log.Printf("agent: WARNING no AGENT_TOKEN set — the :8671 endpoint is unauthenticated")
	}
	return &Master{token: token, cfg: cfg, workers: map[string]*WorkerState{}}
}

// Handler returns the HTTP mux for the agent listener.
func (m *Master) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(PathHealth, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc(PathRegister, m.auth(m.handleRegister))
	mux.HandleFunc(PathTrigger, m.auth(m.handleTrigger))
	return mux
}

// auth enforces the bearer token when one is configured.
func (m *Master) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if m.token != "" && r.Header.Get("Authorization") != "Bearer "+m.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (m *Master) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WorkerID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	now := time.Now()
	m.mu.Lock()
	ws := m.workers[req.WorkerID]
	if ws == nil {
		ws = &WorkerState{ID: req.WorkerID, FirstSeen: now}
		m.workers[req.WorkerID] = ws
	}
	ws.Hostname = req.Hostname
	ws.Command = req.Command
	ws.IntervalSec = req.IntervalSec
	ws.LastSeen = now
	m.mu.Unlock()

	log.Printf("agent: registered worker %q (%s) cmd=%q", req.WorkerID, req.Hostname, req.Command)
	writeJSON(w, RegisterResponse{OK: true})
}

func (m *Master) handleTrigger(w http.ResponseWriter, r *http.Request) {
	var req TriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WorkerID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ev := Event{
		Time:       time.Now(),
		WorkerID:   req.WorkerID,
		Command:    req.Command,
		ExitCode:   req.ExitCode,
		Message:    req.Message,
		Recipients: len(req.Email.Targets),
	}

	results, err := mailer.Send(m.cfg.Get(), mailer.Message{
		Recipients: req.Email.Targets,
		Subject:    req.Email.Subject,
		Body:       req.Email.Body,
		HTML:       req.Email.HTML,
	})
	resp := TriggerResponse{OK: true}
	if err != nil {
		ev.Error = err.Error()
		resp = TriggerResponse{OK: false, Error: err.Error()}
	} else {
		for _, res := range results {
			if res.OK {
				ev.Sent++
			} else {
				ev.Failed++
			}
		}
		resp.Sent, resp.Failed = ev.Sent, ev.Failed
	}

	m.mu.Lock()
	if ws := m.workers[req.WorkerID]; ws != nil {
		ws.LastSeen = ev.Time
		ws.Triggers++
	}
	m.events = append([]Event{ev}, m.events...)
	if len(m.events) > maxEvents {
		m.events = m.events[:maxEvents]
	}
	m.mu.Unlock()

	log.Printf("agent: trigger from %q -> sent %d/%d", req.WorkerID, ev.Sent, ev.Recipients)
	writeJSON(w, resp)
}

// Workers returns a snapshot of registered workers.
func (m *Master) Workers() []WorkerState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]WorkerState, 0, len(m.workers))
	for _, ws := range m.workers {
		out = append(out, *ws)
	}
	return out
}

// Events returns a snapshot of recent trigger events (newest first).
func (m *Master) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
