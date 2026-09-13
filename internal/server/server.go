// Package server wires the HTTP routes and HTML UI for the mailer.
package server

import (
	"embed"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"devops-mails/internal/agent"
	"devops-mails/internal/config"
	"devops-mails/internal/mailer"

	"github.com/gin-gonic/gin"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server holds the shared dependencies for the HTTP handlers.
type Server struct {
	store  *config.Store
	master *agent.Master
	engine *gin.Engine
}

// New builds a configured Gin engine backed by the given config store and agent
// master (used by the read-only Agents page).
func New(store *config.Store, master *agent.Master) (*Server, error) {
	s := &Server{store: store, master: master}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	tmpl := template.Must(template.New("").ParseFS(templatesFS, "templates/*.html"))
	r.SetHTMLTemplate(tmpl)

	r.StaticFS("/static", http.FS(mustSub(staticFS)))

	r.GET("/", s.handleIndex)
	r.GET("/config", s.handleConfigForm)
	r.POST("/config", s.handleConfigSave)
	r.GET("/compose", s.handleCompose)
	r.POST("/send", s.handleSend)
	r.GET("/agents", s.handleAgents)

	s.engine = r
	return s, nil
}

// Run starts the HTTP server on addr.
func (s *Server) Run(addr string) error {
	return s.engine.Run(addr)
}

func (s *Server) handleIndex(c *gin.Context) {
	cfg := s.store.Get()
	c.HTML(http.StatusOK, "index.html", gin.H{
		"Title":     "Dashboard",
		"Active":    "home",
		"Config":    cfg,
		"Configured": strings.TrimSpace(cfg.Host) != "" && (cfg.FromEmail != "" || cfg.Username != ""),
	})
}

func (s *Server) handleConfigForm(c *gin.Context) {
	c.HTML(http.StatusOK, "config.html", gin.H{
		"Title":  "SMTP Configuration",
		"Active": "config",
		"Config": s.store.Get(),
	})
}

func (s *Server) handleConfigSave(c *gin.Context) {
	port, _ := strconv.Atoi(c.PostForm("port"))
	if port == 0 {
		port = 25
	}
	cfg := config.SMTP{
		Host:       strings.TrimSpace(c.PostForm("host")),
		Port:       port,
		Username:   strings.TrimSpace(c.PostForm("username")),
		Password:   c.PostForm("password"),
		FromName:   strings.TrimSpace(c.PostForm("from_name")),
		FromEmail:  strings.TrimSpace(c.PostForm("from_email")),
		Encryption: c.PostForm("encryption"),
		SkipVerify: c.PostForm("skip_verify") == "on",
	}
	if err := s.store.Save(cfg); err != nil {
		c.HTML(http.StatusInternalServerError, "config.html", gin.H{
			"Title": "SMTP Configuration", "Active": "config",
			"Config": cfg, "Error": err.Error(),
		})
		return
	}
	c.HTML(http.StatusOK, "config.html", gin.H{
		"Title": "SMTP Configuration", "Active": "config",
		"Config": cfg, "Saved": true,
	})
}

func (s *Server) handleCompose(c *gin.Context) {
	c.HTML(http.StatusOK, "compose.html", gin.H{
		"Title":  "Compose & Send",
		"Active": "compose",
		"Config": s.store.Get(),
	})
}

func (s *Server) handleAgents(c *gin.Context) {
	c.HTML(http.StatusOK, "agents.html", gin.H{
		"Title":   "Agents",
		"Active":  "agents",
		"Workers": s.master.Workers(),
		"Events":  s.master.Events(),
	})
}

func (s *Server) handleSend(c *gin.Context) {
	recipients := mailer.ParseRecipients(c.PostForm("recipients"))
	msg := mailer.Message{
		Recipients: recipients,
		Subject:    c.PostForm("subject"),
		Body:       c.PostForm("body"),
		HTML:       c.PostForm("format") == "html",
	}

	data := gin.H{"Title": "Compose & Send", "Active": "compose", "Config": s.store.Get()}
	data["Subject"] = msg.Subject
	data["Body"] = msg.Body
	data["RecipientsRaw"] = c.PostForm("recipients")
	data["FormatHTML"] = msg.HTML

	if len(recipients) == 0 {
		data["Error"] = "No valid recipients were provided."
		c.HTML(http.StatusBadRequest, "compose.html", data)
		return
	}
	if strings.TrimSpace(msg.Subject) == "" {
		data["Error"] = "Subject is required."
		c.HTML(http.StatusBadRequest, "compose.html", data)
		return
	}

	results, err := mailer.Send(s.store.Get(), msg)
	if err != nil {
		data["Error"] = err.Error()
		c.HTML(http.StatusBadRequest, "compose.html", data)
		return
	}

	sent := 0
	for _, r := range results {
		if r.OK {
			sent++
		}
	}
	data["Results"] = results
	data["Sent"] = sent
	data["Failed"] = len(results) - sent
	c.HTML(http.StatusOK, "compose.html", data)
}
