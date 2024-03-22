package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultValidate(t *testing.T) {
	t.Parallel()
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDSN(t *testing.T) {
	t.Setenv(EnvDatabaseURL, "")
	t.Setenv(EnvPGScopeDSN, "")

	if _, err := ResolveDSN(""); err == nil {
		t.Fatal("expected error when no dsn configured")
	}

	t.Setenv(EnvDatabaseURL, "postgres://db")
	got, err := ResolveDSN("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "postgres://db" {
		t.Fatalf("got %q", got)
	}

	got, err = ResolveDSN("postgres://explicit")
	if err != nil {
		t.Fatal(err)
	}
	if got != "postgres://explicit" {
		t.Fatalf("explicit should win, got %q", got)
	}
}

func TestLoadFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "pgscope.yaml")
	content := []byte("format: json\ntimeout: 45s\nmax_conns: 2\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	if err := LoadFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Format != "json" {
		t.Fatalf("format=%q", cfg.Format)
	}
	if cfg.Timeout != 45*time.Second {
		t.Fatalf("timeout=%v", cfg.Timeout)
	}
	if cfg.MaxConns != 2 {
		t.Fatalf("max_conns=%d", cfg.MaxConns)
	}
}
