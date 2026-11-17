package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lookupMap(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestDefaults(t *testing.T) {
	c, err := Parse(lookupMap(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL != "sqlite:///data/zigrt.db" || c.Listen != ":8080" || c.LogFormat != "json" ||
		c.BackupRetain != 14 || c.Timezone != "UTC" || c.InsecureCookies || c.SecretKey != nil {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestFileSecretsAndProxies(t *testing.T) {
	dir := t.TempDir()
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	path := filepath.Join(dir, "key")
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Parse(lookupMap(map[string]string{
		"ZIGRT_SECRET_KEY_FILE":  path,
		"ZIGRT_TRUSTED_PROXIES":  "10.0.0.0/8, 192.168.1.1",
		"ZIGRT_INSECURE_COOKIES": "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.SecretKey) != 32 || len(c.TrustedProxies) != 2 || !c.InsecureCookies {
		t.Fatalf("got %+v", c)
	}
}

func TestErrors(t *testing.T) {
	_, err := Parse(lookupMap(map[string]string{
		"ZIGRT_SECRET_KEY":    "c2hvcnQ=",
		"ZIGRT_LOG_FORMAT":    "xml",
		"ZIGRT_BACKUP_RETAIN": "0",
		"TZ":                  "Mars/Olympus",
	}))
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"SECRET_KEY", "LOG_FORMAT", "BACKUP_RETAIN", "TZ"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %s", err, want)
		}
	}
}

func TestEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(path, []byte("# comment\nexport ZIGRT_LISTEN=\":9090\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	vals, err := readEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if vals["ZIGRT_LISTEN"] != ":9090" {
		t.Fatalf("got %v", vals)
	}
}

func TestSMTPAndMetrics(t *testing.T) {
	c, err := Parse(lookupMap(nil))
	if err != nil || c.MailOn() || c.MetricsListen != "" || c.MCPEnabled {
		t.Fatalf("email, metrics or MCP on by default: %+v %v", c, err)
	}
	pw := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(pw, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err = Parse(lookupMap(map[string]string{
		"ZIGRT_SMTP_HOST": "smtp.example.com", "ZIGRT_SMTP_FROM": "Zigrt <zigrt@example.com>",
		"ZIGRT_SMTP_USERNAME": "zigrt", "ZIGRT_SMTP_PASSWORD_FILE": pw, "ZIGRT_SMTP_SECURITY": "TLS",
		"ZIGRT_BASE_URL": "https://zigrt.example.com", "ZIGRT_METRICS_LISTEN": ":9090", "ZIGRT_MCP_ENABLED": "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.MailOn() || c.SMTP.Port != 465 || c.SMTP.Password != "s3cret" || c.SMTP.Security != "tls" || c.MetricsListen != ":9090" || !c.MCPEnabled {
		t.Fatalf("got %+v", c)
	}
	_, err = Parse(lookupMap(map[string]string{"ZIGRT_SMTP_HOST": "smtp.example.com", "ZIGRT_SMTP_PORT": "x", "ZIGRT_MCP_ENABLED": "maybe"}))
	for _, want := range []string{"ZIGRT_SMTP_PORT", "from address", "ZIGRT_BASE_URL", "ZIGRT_MCP_ENABLED"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error %v missing %s", err, want)
		}
	}
}
