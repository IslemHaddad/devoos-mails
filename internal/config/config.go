// Package config manages persistent SMTP server settings for the mailer.
package config

import (
	"encoding/json"
	"os"
	"sync"
)

// Encryption modes supported when connecting to the SMTP relay.
const (
	EncNone     = "none"     // plain, no TLS (e.g. localhost postfix on :25)
	EncSTARTTLS = "starttls" // upgrade to TLS after connecting (usually :587)
	EncSSL      = "ssl"      // implicit TLS from the start (usually :465)
)

// SMTP holds the connection settings for the outbound mail server. This mirrors
// the kind of relay configuration you would put in a postfix main.cf.
type SMTP struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	FromName   string `json:"from_name"`
	FromEmail  string `json:"from_email"`
	Encryption string `json:"encryption"` // one of EncNone, EncSTARTTLS, EncSSL
	SkipVerify bool   `json:"skip_verify"`
}

// Store persists a single SMTP configuration to a JSON file on disk and guards
// concurrent access from HTTP handlers.
type Store struct {
	path string
	mu   sync.RWMutex
	cfg  SMTP
}

// NewStore loads the configuration from path if it exists, otherwise it returns
// a store seeded with sensible localhost-postfix defaults.
func NewStore(path string) (*Store, error) {
	s := &Store{
		path: path,
		cfg: SMTP{
			Host:       "localhost",
			Port:       25,
			Encryption: EncNone,
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &s.cfg); err != nil {
		return nil, err
	}
	return s, nil
}

// Get returns a copy of the current configuration.
func (s *Store) Get() SMTP {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Save replaces the configuration and writes it to disk atomically.
func (s *Store) Save(cfg SMTP) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.cfg = cfg
	return nil
}
