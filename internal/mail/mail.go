// Package mail sends Zigrt's email over SMTP (plan §10): password reset
// links and the weekly summary, as plain text. The service queues messages
// in an outbox; this package only delivers one.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Security is how the connection to the SMTP server is protected.
type Security string

const (
	StartTLS Security = "starttls" // upgrade a plain connection; port 587
	TLS      Security = "tls"      // TLS from the start; port 465
	None     Security = "none"     // no encryption; only for a relay on the same host or network
)

// Config is an SMTP server and the sender address.
type Config struct {
	Host     string
	Port     int
	Security Security
	Username string
	Password string
	From     string // an address, optionally with a name: Zigrt <zigrt@example.com>
	Timeout  time.Duration
}

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

var _ Sender = (*SMTP)(nil)

// SMTP is a Sender that talks to an SMTP server.
type SMTP struct {
	Config
	// Now dates the message. Default time.Now.
	Now func() time.Time
	// tlsConfig is for tests (a server with a self-signed certificate).
	tlsConfig *tls.Config
}

// Check validates a configuration, naming every problem.
func (c Config) Check() error {
	var errs []error
	if c.Host == "" {
		errs = append(errs, errors.New("no SMTP host"))
	}
	switch c.Security {
	case StartTLS, TLS, None:
	default:
		errs = append(errs, fmt.Errorf("SMTP security must be starttls, tls or none, got %q", c.Security))
	}
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("SMTP port %d is not a port", c.Port))
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		errs = append(errs, fmt.Errorf("SMTP from address %q: %w", c.From, err))
	}
	return errors.Join(errs...)
}

// DefaultPort is the usual port for a security mode.
func DefaultPort(s Security) int {
	switch s {
	case TLS:
		return 465
	case None:
		return 25
	}
	return 587
}

// Send delivers a plain-text message to one address.
func (s *SMTP) Send(ctx context.Context, to, subject, body string) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return err
	}
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("to %q: %w", to, err)
	}
	msg, err := s.message(from, rcpt, subject, body)
	if err != nil {
		return err
	}

	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	tlsCfg := s.tlsConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	}
	var conn net.Conn
	d := &net.Dialer{}
	if s.Security == TLS {
		conn, err = (&tls.Dialer{NetDialer: d, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if s.Security == StartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the SMTP server does not offer STARTTLS; set the security to tls or none")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if s.Username != "" {
		// PlainAuth refuses to send a password unencrypted except to localhost.
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (s *SMTP) message(from, to *mail.Address, subject, body string) ([]byte, error) {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	domain := from.Address[strings.LastIndexByte(from.Address, '@')+1:]
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", from.String())
	h("To", to.String())
	h("Subject", mime.QEncoding.Encode("utf-8", subject))
	h("Date", now().Format(time.RFC1123Z))
	h("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=utf-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	h("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n"))); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
