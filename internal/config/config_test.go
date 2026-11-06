package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"POSTGRES_DSN": "postgres://x", "TOKEN_SECRET": strings.Repeat("a", 32)}))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.RefreshInterval != 10*time.Second || c.AllowedOrigins[0] != "*" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoadErrors(t *testing.T) {
	_, err := Load(env(map[string]string{"TOKEN_SECRET": "short", "REFRESH_INTERVAL": "abc"}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"POSTGRES_DSN", "TOKEN_SECRET", "REFRESH_INTERVAL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadOrigins(t *testing.T) {
	c, err := Load(env(map[string]string{"POSTGRES_DSN": "x", "TOKEN_SECRET": strings.Repeat("a", 32),
		"CORS_ALLOWED_ORIGINS": " https://a.example , ,https://b.example"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AllowedOrigins) != 2 || c.AllowedOrigins[1] != "https://b.example" {
		t.Fatalf("origins = %v", c.AllowedOrigins)
	}
}
