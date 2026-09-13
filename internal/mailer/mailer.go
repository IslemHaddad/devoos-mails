// Package mailer sends messages through an SMTP relay using the stored config.
package mailer

import (
	"crypto/tls"
	"fmt"
	"strings"

	"devops-mails/internal/config"

	"github.com/wneessen/go-mail"
)

// Message is a single email to deliver to one or more recipients.
type Message struct {
	Recipients []string
	Subject    string
	Body       string
	HTML       bool // true to send Body as text/html, otherwise text/plain
}

// Result records the delivery outcome for one recipient.
type Result struct {
	Recipient string `json:"recipient"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

// clientFor builds a go-mail client from the SMTP configuration.
func clientFor(c config.SMTP) (*mail.Client, error) {
	opts := []mail.Option{
		mail.WithPort(c.Port),
		mail.WithTimeout(30 * 1_000_000_000), // 30s
	}

	switch c.Encryption {
	case config.EncSSL:
		opts = append(opts, mail.WithSSLPort(false), mail.WithTLSPolicy(mail.TLSMandatory))
	case config.EncSTARTTLS:
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	default:
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	}

	if c.Username != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(c.Username),
			mail.WithPassword(c.Password),
		)
	}

	if c.SkipVerify {
		opts = append(opts, mail.WithTLSConfig(&tls.Config{InsecureSkipVerify: true})) //nolint:gosec // user opt-in
	}

	return mail.NewClient(c.Host, opts...)
}

// buildMessage renders the go-mail message for a single recipient.
func buildMessage(c config.SMTP, m Message, to string) (*mail.Msg, error) {
	msg := mail.NewMsg()
	from := c.FromEmail
	if from == "" {
		from = c.Username
	}
	if c.FromName != "" {
		if err := msg.FromFormat(c.FromName, from); err != nil {
			return nil, err
		}
	} else if err := msg.From(from); err != nil {
		return nil, err
	}
	if err := msg.To(to); err != nil {
		return nil, err
	}
	msg.Subject(m.Subject)
	if m.HTML {
		msg.SetBodyString(mail.TypeTextHTML, m.Body)
	} else {
		msg.SetBodyString(mail.TypeTextPlain, m.Body)
	}
	return msg, nil
}

// Send delivers the message to every recipient, one at a time, and returns a
// per-recipient result so partial failures are visible in the UI.
func Send(c config.SMTP, m Message) ([]Result, error) {
	if strings.TrimSpace(c.Host) == "" {
		return nil, fmt.Errorf("SMTP host is not configured")
	}
	if (c.FromEmail == "" && c.Username == "") {
		return nil, fmt.Errorf("a From address is required (set From email or username)")
	}

	client, err := clientFor(c)
	if err != nil {
		return nil, fmt.Errorf("build SMTP client: %w", err)
	}

	results := make([]Result, 0, len(m.Recipients))
	for _, to := range m.Recipients {
		to = strings.TrimSpace(to)
		if to == "" {
			continue
		}
		res := Result{Recipient: to, OK: true}
		msg, err := buildMessage(c, m, to)
		if err != nil {
			res.OK, res.Error = false, err.Error()
			results = append(results, res)
			continue
		}
		if err := client.DialAndSend(msg); err != nil {
			res.OK, res.Error = false, err.Error()
		}
		results = append(results, res)
	}
	return results, nil
}

// ParseRecipients splits a free-form textarea (newlines, commas, semicolons or
// spaces) into a de-duplicated list of trimmed addresses.
func ParseRecipients(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}
