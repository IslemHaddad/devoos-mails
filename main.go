// Command devops-mails runs the master: a web UI to configure an SMTP relay and
// send mail (:8080), plus an agent listener (:8671) that workers connect to and
// trigger sends through.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"devops-mails/internal/agent"
	"devops-mails/internal/config"
	"devops-mails/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "web UI listen address")
	agentAddr := flag.String("agent-addr", ":8671", "agent (worker) listen address")
	cfgPath := flag.String("config", "smtp-config.json", "path to the SMTP config file")
	flag.Parse()

	store, err := config.NewStore(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	master := agent.NewMaster(os.Getenv("AGENT_TOKEN"), store)

	srv, err := server.New(store, master)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	// Agent listener for workers (separate port from the web UI).
	go func() {
		log.Printf("agent listener on %s", *agentAddr)
		if err := http.ListenAndServe(*agentAddr, master.Handler()); err != nil {
			log.Fatalf("agent listener: %v", err)
		}
	}()

	log.Printf("devops-mails web UI on %s (config: %s)", *addr, *cfgPath)
	if err := srv.Run(*addr); err != nil {
		log.Fatalf("server: %v", err)
	}
}
