// Package config loads Zigrt's configuration from environment variables
// (plan §10.1). An optional /data/config.env file supplies defaults; real
// environment variables always win. Any variable may instead be given as
// NAME_FILE pointing at a file that holds the value (Docker and K8s secrets).
package config

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chinny-labs/zigrt/internal/mail"
)

// Config is the parsed runtime configuration.
type Config struct {
	DatabaseURL     string
	SecretKey       []byte // signs CSRF tokens; nil means generate one and keep it in the database
	BaseURL         string
	Listen          string
	TrustedProxies  []netip.Prefix
	InsecureCookies bool
	BackupSchedule  string
	BackupRetain    int
	LogFormat       string // json | text
	Timezone        string // default household timezone at setup
	ContentDir      string // folder scanned for content packs newer than the embedded one
	MCPEnabled      bool   // serve the MCP server at /mcp (plan §11)
	// SMTP turns email on when SMTP.Host is set (plan §10).
	SMTP mail.Config
	// MetricsListen is where Prometheus metrics are served, on their own
	// port so a public ingress never exposes them; "" is off.
	MetricsListen string
}

// MailOn reports whether email is configured.
func (c Config) MailOn() bool { return c.SMTP.Host != "" }

// DefaultEnvFile is read when present, before the process environment.
const DefaultEnvFile = "/data/config.env"

// Load reads configuration using os.LookupEnv, with envFile as defaults.
func Load(envFile string) (Config, error) {
	fileVals, err := readEnvFile(envFile)
	if err != nil {
		return Config{}, err
	}
	return parse(func(k string) (string, bool) {
		if v, ok := os.LookupEnv(k); ok {
			return v, true
		}
		v, ok := fileVals[k]
		return v, ok
	})
}

// Parse builds a Config from an arbitrary lookup function (used by tests).
func Parse(lookup func(string) (string, bool)) (Config, error) { return parse(lookup) }

func parse(lookup func(string) (string, bool)) (Config, error) {
	get := func(name, def string) (string, error) {
		if v, ok := lookup(name); ok && v != "" {
			return v, nil
		}
		if path, ok := lookup(name + "_FILE"); ok && path != "" {
			b, err := os.ReadFile(path) //nolint:gosec // the operator names this file (NAME_FILE)
			if err != nil {
				return "", fmt.Errorf("%s_FILE: %w", name, err)
			}
			return strings.TrimSpace(string(b)), nil
		}
		return def, nil
	}

	var errs []error
	str := func(name, def string) string {
		v, err := get(name, def)
		if err != nil {
			errs = append(errs, err)
		}
		return v
	}

	c := Config{
		DatabaseURL:    str("ZIGRT_DATABASE_URL", "sqlite:///data/zigrt.db"),
		BaseURL:        strings.TrimRight(str("ZIGRT_BASE_URL", ""), "/"),
		Listen:         str("ZIGRT_LISTEN", ":8080"),
		BackupSchedule: str("ZIGRT_BACKUP_SCHEDULE", "03:15"),
		LogFormat:      str("ZIGRT_LOG_FORMAT", "json"),
		Timezone:       str("TZ", "UTC"),
		ContentDir:     str("ZIGRT_CONTENT_DIR", "/data/content"),
	}

	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("ZIGRT_LOG_FORMAT must be json or text, got %q", c.LogFormat))
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		errs = append(errs, fmt.Errorf("TZ: %w", err))
	}
	if _, err := ParseClock(c.BackupSchedule); err != nil {
		errs = append(errs, fmt.Errorf("ZIGRT_BACKUP_SCHEDULE: %w", err))
	}

	retain := str("ZIGRT_BACKUP_RETAIN", "14")
	if n, err := strconv.Atoi(retain); err != nil || n < 1 {
		errs = append(errs, fmt.Errorf("ZIGRT_BACKUP_RETAIN must be a positive integer, got %q", retain))
	} else {
		c.BackupRetain = n
	}

	for name, dst := range map[string]*bool{"ZIGRT_MCP_ENABLED": &c.MCPEnabled} {
		if b, err := strconv.ParseBool(str(name, "false")); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		} else {
			*dst = b
		}
	}

	c.MetricsListen = str("ZIGRT_METRICS_LISTEN", "")
	c.SMTP = mail.Config{
		Host:     str("ZIGRT_SMTP_HOST", ""),
		Security: mail.Security(strings.ToLower(str("ZIGRT_SMTP_SECURITY", string(mail.StartTLS)))),
		Username: str("ZIGRT_SMTP_USERNAME", ""),
		Password: str("ZIGRT_SMTP_PASSWORD", ""),
		From:     str("ZIGRT_SMTP_FROM", ""),
	}
	if c.SMTP.Host != "" {
		c.SMTP.Port = mail.DefaultPort(c.SMTP.Security)
		if p := str("ZIGRT_SMTP_PORT", ""); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil {
				errs = append(errs, fmt.Errorf("ZIGRT_SMTP_PORT must be a number, got %q", p))
			}
			c.SMTP.Port = n
		}
		if err := c.SMTP.Check(); err != nil {
			errs = append(errs, fmt.Errorf("ZIGRT_SMTP_*: %w", err))
		}
		if c.BaseURL == "" {
			errs = append(errs, errors.New("ZIGRT_BASE_URL is needed for the links in emails when ZIGRT_SMTP_HOST is set"))
		}
	}

	insecure := str("ZIGRT_INSECURE_COOKIES", "false")
	if b, err := strconv.ParseBool(insecure); err != nil {
		errs = append(errs, fmt.Errorf("ZIGRT_INSECURE_COOKIES: %w", err))
	} else {
		c.InsecureCookies = b
	}

	for _, s := range strings.FieldsFunc(str("ZIGRT_TRUSTED_PROXIES", ""), func(r rune) bool { return r == ',' || r == ' ' }) {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			if a, aerr := netip.ParseAddr(s); aerr == nil {
				p = netip.PrefixFrom(a, a.BitLen())
			} else {
				errs = append(errs, fmt.Errorf("ZIGRT_TRUSTED_PROXIES: %w", err))
				continue
			}
		}
		c.TrustedProxies = append(c.TrustedProxies, p)
	}

	if key := str("ZIGRT_SECRET_KEY", ""); key != "" {
		b, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(key)
		}
		switch {
		case err != nil:
			errs = append(errs, errors.New("ZIGRT_SECRET_KEY must be base64 (try: openssl rand -base64 32)"))
		case len(b) < 32:
			errs = append(errs, fmt.Errorf("ZIGRT_SECRET_KEY must decode to at least 32 bytes, got %d", len(b)))
		default:
			c.SecretKey = b
		}
	}

	return c, errors.Join(errs...)
}

// ParseClock parses "HH:MM" into hours and minutes since midnight.
func ParseClock(s string) (time.Duration, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("want HH:MM, got %q", s)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

func readEnvFile(path string) (map[string]string, error) {
	vals := map[string]string{}
	if path == "" {
		return vals, nil
	}
	f, err := os.Open(path) //nolint:gosec // fixed config path
	if errors.Is(err, os.ErrNotExist) {
		return vals, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE", path, n)
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		vals[strings.TrimSpace(k)] = v
	}
	return vals, sc.Err()
}
