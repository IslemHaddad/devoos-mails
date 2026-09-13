// Package agent defines the master/worker protocol and both endpoints.
//
// A worker runs a check command on an interval; when the command exits 0
// ("true") it POSTs a trigger to the master, which relays an email through the
// SMTP configuration managed in the web UI. The master authenticates workers
// with a shared bearer token.
package agent

// API paths served by the master's agent listener (default :8671).
const (
	PathRegister = "/api/v1/register"
	PathTrigger  = "/api/v1/trigger"
	PathHealth   = "/healthz"
)

// EmailSpec is the message a worker asks the master to send when its check
// fires. The worker owns this payload (its "task"); the master only relays it.
type EmailSpec struct {
	Targets []string `json:"targets"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
	HTML    bool     `json:"html"`
}

// RegisterRequest announces a worker to the master on startup.
type RegisterRequest struct {
	WorkerID    string `json:"worker_id"`
	Hostname    string `json:"hostname"`
	Command     string `json:"command"`
	IntervalSec int    `json:"interval_sec"`
}

// RegisterResponse acknowledges a registration.
type RegisterResponse struct {
	OK bool `json:"ok"`
}

// TriggerRequest reports that a worker's check ran true and asks the master to
// send EmailSpec.
type TriggerRequest struct {
	WorkerID string    `json:"worker_id"`
	Command  string    `json:"command"`
	ExitCode int       `json:"exit_code"`
	Message  string    `json:"message"`
	Email    EmailSpec `json:"email"`
}

// TriggerResponse reports the delivery outcome back to the worker.
type TriggerResponse struct {
	OK     bool   `json:"ok"`
	Sent   int    `json:"sent"`
	Failed int    `json:"failed"`
	Error  string `json:"error,omitempty"`
}
