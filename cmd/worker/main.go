// Command worker runs a check command on an interval and asks the devops-mails
// master to email a target list when the check exits 0 ("true").
//
// Every flag has an environment-variable fallback so the same binary works as a
// systemd service (EnvironmentFile) or a Kubernetes pod (env / envFrom).
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"devops-mails/internal/agent"
	"devops-mails/internal/mailer"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string) bool {
	v := strings.ToLower(os.Getenv(key))
	return v == "1" || v == "true" || v == "yes"
}

func main() {
	host, _ := os.Hostname()

	master := flag.String("master", env("MASTER_URL", "http://localhost:8671"), "master base URL")
	token := flag.String("token", env("AGENT_TOKEN", ""), "shared bearer token")
	name := flag.String("name", env("WORKER_NAME", host), "worker id/name")
	command := flag.String("command", env("CHECK_COMMAND", ""), "check command (run via sh -c; exit 0 == true)")
	intervalStr := flag.String("interval", env("CHECK_INTERVAL", "30s"), "interval between checks")
	repeat := flag.Bool("repeat", envBool("REPEAT"), "notify on every true check, not just the rising edge")
	targets := flag.String("targets", env("MAIL_TARGETS", ""), "recipients (comma/space/newline separated)")
	subject := flag.String("subject", env("MAIL_SUBJECT", ""), "email subject")
	body := flag.String("body", env("MAIL_BODY", ""), "email body")
	html := flag.Bool("html", envBool("MAIL_HTML"), "send body as HTML")
	flag.Parse()

	interval, err := time.ParseDuration(*intervalStr)
	if err != nil || interval <= 0 {
		log.Fatalf("invalid interval %q: %v", *intervalStr, err)
	}
	if strings.TrimSpace(*command) == "" {
		log.Fatal("a check command is required (--command or CHECK_COMMAND)")
	}
	recipients := mailer.ParseRecipients(*targets)
	if len(recipients) == 0 {
		log.Fatal("at least one target is required (--targets or MAIL_TARGETS)")
	}
	subj := *subject
	if strings.TrimSpace(subj) == "" {
		subj = "[devops-mails] check fired on " + *name
	}

	w := agent.NewWorker(agent.WorkerConfig{
		MasterURL: *master,
		Token:     *token,
		WorkerID:  *name,
		Hostname:  host,
		Command:   *command,
		Interval:  interval,
		Repeat:    *repeat,
		Email: agent.EmailSpec{
			Targets: recipients,
			Subject: subj,
			Body:    *body,
			HTML:    *html,
		},
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("worker %q -> master %s | every %s | targets=%s | repeat=%s",
		*name, *master, interval, strings.Join(recipients, ","), strconv.FormatBool(*repeat))
	if err := w.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("worker: %v", err)
	}
}
