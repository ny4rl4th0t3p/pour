package admin

import (
	"os"
	"strings"
	"testing"
)

func TestNewTokenStore_envVar(t *testing.T) {
	t.Setenv(TokenEnvVar, "token-from-env")
	ts, err := NewTokenStore()
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	if ts.token != "token-from-env" {
		t.Errorf("token: got %q, want token-from-env", ts.token)
	}
}

func TestNewTokenStore_file(t *testing.T) {
	t.Setenv(TokenEnvVar, "") // ensure env var does not short-circuit
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	if err := os.WriteFile(TokenFile, []byte("token-from-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ts, err := NewTokenStore()
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	if ts.token != "token-from-file" {
		t.Errorf("token: got %q, want token-from-file", ts.token)
	}
}

func TestNewTokenStore_generate(t *testing.T) {
	t.Setenv(TokenEnvVar, "") // ensure env var does not short-circuit
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	ts, err := NewTokenStore()
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	if ts.token == "" {
		t.Fatal("generated token is empty")
	}
	if !strings.HasPrefix(ts.token, tokenPrefix) {
		t.Errorf("token %q does not have prefix %q", ts.token, tokenPrefix)
	}

	info, statErr := os.Stat(TokenFile)
	if statErr != nil {
		t.Fatalf("token file not created: %v", statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token file mode: got %04o, want 0600", info.Mode().Perm())
	}
}
