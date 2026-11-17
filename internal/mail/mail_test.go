package mail

import (
	"bufio"
	"context"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

// fakeServer is a minimal SMTP server: it accepts one message and records
// the envelope and data.
type fakeServer struct {
	addr     string
	startTLS bool // advertise STARTTLS
	got      chan received
}

type received struct {
	from, to string
	data     string
}

func newFake(t *testing.T, startTLS bool) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeServer{addr: ln.Addr().String(), startTLS: startTLS, got: make(chan received, 1)}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r, w := bufio.NewReader(conn), conn
		say := func(s string) { _, _ = io.WriteString(w, s+"\r\n") }
		say("220 fake ESMTP")
		var rec received
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				if f.startTLS {
					say("250-fake")
					say("250 STARTTLS")
				} else {
					say("250 fake")
				}
			case strings.HasPrefix(cmd, "MAIL FROM:"):
				rec.from = strings.Trim(strings.TrimSpace(line)[10:], "<>")
				say("250 ok")
			case strings.HasPrefix(cmd, "RCPT TO:"):
				rec.to = strings.Trim(strings.TrimSpace(line)[8:], "<>")
				say("250 ok")
			case cmd == "DATA":
				say("354 go on")
				var b strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					b.WriteString(strings.TrimPrefix(l, "."))
				}
				rec.data = b.String()
				f.got <- rec
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	return f
}

func (f *fakeServer) port(t *testing.T) int {
	_, p, _ := net.SplitHostPort(f.addr)
	n := 0
	for _, c := range p {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestSend(t *testing.T) {
	f := newFake(t, false)
	s := &SMTP{Config: Config{Host: "127.0.0.1", Port: f.port(t), Security: None, From: "Zigrt <zigrt@example.com>"},
		Now: func() time.Time { return time.Date(2026, 11, 22, 18, 0, 0, 0, time.UTC) }}
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	body := "Ava: 120 XP of 105 — goal met.\nA line that is long enough to need wrapping by quoted-printable because it runs well past seventy-six characters."
	if err := s.Send(context.Background(), "Sam <sam@example.com>", "Zigrt: the week of 16 November ✓", body); err != nil {
		t.Fatal(err)
	}
	rec := <-f.got
	if rec.from != "zigrt@example.com" || rec.to != "sam@example.com" {
		t.Fatalf("envelope %+v", rec)
	}
	m, err := mail.ReadMessage(strings.NewReader(rec.data))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mime.WordDecoder)
	subj, _ := dec.DecodeHeader(m.Header.Get("Subject"))
	got, _ := io.ReadAll(quotedprintable.NewReader(m.Body))
	if subj != "Zigrt: the week of 16 November ✓" || strings.TrimRight(strings.ReplaceAll(string(got), "\r\n", "\n"), "\n") != body ||
		m.Header.Get("Content-Type") != "text/plain; charset=utf-8" || !strings.HasSuffix(m.Header.Get("Message-Id"), "@example.com>") ||
		m.Header.Get("Date") != "Sun, 22 Nov 2026 18:00:00 +0000" {
		t.Fatalf("message:\n%s\nsubject %q body %q", rec.data, subj, got)
	}
}

func TestSendNeedsSTARTTLS(t *testing.T) {
	f := newFake(t, false)
	s := &SMTP{Config: Config{Host: "127.0.0.1", Port: f.port(t), Security: StartTLS, From: "zigrt@example.com"}}
	if err := s.Send(context.Background(), "sam@example.com", "x", "y"); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("no STARTTLS offered: %v", err)
	}
}

func TestCheck(t *testing.T) {
	for _, c := range []Config{
		{Host: "", Port: 587, Security: StartTLS, From: "a@b.c"},
		{Host: "h", Port: 587, Security: "ssl", From: "a@b.c"},
		{Host: "h", Port: 0, Security: TLS, From: "a@b.c"},
		{Host: "h", Port: 587, Security: StartTLS, From: "not an address"},
	} {
		if c.Check() == nil {
			t.Errorf("%+v passed", c)
		}
	}
	if DefaultPort(TLS) != 465 || DefaultPort(StartTLS) != 587 || DefaultPort(None) != 25 {
		t.Error("default ports")
	}
}
