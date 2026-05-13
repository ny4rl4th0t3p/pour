package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/admin"
)

// captureOutput redirects os.Stdout for the duration of fn and returns what was written.
// fn must not call t.Fatal — use t.Errorf to keep the function returning normally.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// stubAdmin starts a stub HTTP server, sets POUR_ADMIN_URL and POUR_ADMIN_TOKEN, and
// registers cleanup. All tests using this helper run sequentially (no t.Parallel).
func stubAdmin(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv(adminURLEnv, srv.URL)
	t.Setenv(admin.TokenEnvVar, "test-token")
}

func jsonResp(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ----- relocated command endpoint smoke tests -----

func TestAdminChainsListCmd_endpoint(t *testing.T) {
	var gotMethod, gotPath string
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		jsonResp(w, http.StatusOK, []chainSnapshot{})
	})
	if err := (&AdminChainsListCmd{}).Run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/admin/registry/snapshot" {
		t.Errorf("got %s %s, want GET /admin/registry/snapshot", gotMethod, gotPath)
	}
}

func TestAdminChainsDiffCmd_endpoint(t *testing.T) {
	var gotPath string
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		jsonResp(w, http.StatusOK, []chainSnapshot{{ChainID: "osmosis-1", Enabled: true}})
	})
	cmd := &AdminChainsDiffCmd{Config: "testdata/valid-registry.yml"}
	if err := cmd.Run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/admin/registry/snapshot" {
		t.Errorf("got path %s, want /admin/registry/snapshot", gotPath)
	}
}

func TestAdminChainsPendingCmd_endpoint(t *testing.T) {
	var gotPath string
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		jsonResp(w, http.StatusOK, []pendingChange{})
	})
	if err := (&AdminChainsPendingCmd{}).Run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/admin/registry/pending" {
		t.Errorf("got path %s, want /admin/registry/pending", gotPath)
	}
}

func TestAdminChainsAcceptCmd_endpoint(t *testing.T) {
	var gotMethod, gotPath string
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		jsonResp(w, http.StatusOK, map[string]bool{"ok": true})
	})
	cmd := &AdminChainsAcceptCmd{Chain: "osmosis-1"}
	if err := cmd.Run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/admin/registry/accept" {
		t.Errorf("got %s %s, want POST /admin/registry/accept", gotMethod, gotPath)
	}
}

func TestAdminChainsPinCmd_endpoint(t *testing.T) {
	var gotPath string
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		jsonResp(w, http.StatusOK, []chainSnapshot{{ChainID: "osmosis-1", Bech32Prefix: "osmo"}})
	})
	cmd := &AdminChainsPinCmd{Chain: "osmosis-1", Field: "bech32_prefix"}
	if err := cmd.Run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/admin/registry/snapshot" {
		t.Errorf("got path %s, want /admin/registry/snapshot", gotPath)
	}
}

func TestAdminChainsRefreshCmd_endpoint(t *testing.T) {
	var gotMethod, gotPath string
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		jsonResp(w, http.StatusOK, refreshResult{HotReloaded: 1})
	})
	if err := (&AdminChainsRefreshCmd{}).Run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/admin/registry/refresh" {
		t.Errorf("got %s %s, want POST /admin/registry/refresh", gotMethod, gotPath)
	}
}

// ----- reload -----

func TestAdminChainsReload(t *testing.T) {
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/reload" {
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		jsonResp(w, http.StatusOK, map[string]bool{"ok": true})
	})
	out := captureOutput(t, func() {
		if err := (&AdminChainsReloadCmd{}).Run(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "reloaded") {
		t.Errorf("output %q does not contain 'reloaded'", out)
	}
}

// ----- status -----

func TestAdminChainsStatus_healthy(t *testing.T) {
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/chains/osmosis-1/status" {
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		jsonResp(w, http.StatusOK, chainStatusResponse{
			Suspended:           false,
			MultiSendDisabled:   false,
			SendFailStreak:      0,
			MultiSendFailStreak: 0,
		})
	})
	out := captureOutput(t, func() {
		if err := (&AdminChainsStatusCmd{Chain: "osmosis-1"}).Run(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	for _, want := range []string{"suspended", "false", "send_fail_streak", "multisend_fail_streak"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestAdminChainsStatus_suspended(t *testing.T) {
	stubAdmin(t, func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, chainStatusResponse{
			Suspended:      true,
			SuspendReason:  "too many consecutive failures",
			SendFailStreak: 5,
		})
	})
	out := captureOutput(t, func() {
		if err := (&AdminChainsStatusCmd{Chain: "osmosis-1"}).Run(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	for _, want := range []string{"true", "too many consecutive failures", "5"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// ----- resume -----

func TestAdminChainsResume_ok(t *testing.T) {
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/chains/cosmoshub-4/resume" {
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		jsonResp(w, http.StatusOK, map[string]bool{"ok": true})
	})
	out := captureOutput(t, func() {
		if err := (&AdminChainsResumeCmd{Chain: "cosmoshub-4"}).Run(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "resumed cosmoshub-4") {
		t.Errorf("output %q does not contain 'resumed cosmoshub-4'", out)
	}
}

func TestAdminChainsResume_notFound(t *testing.T) {
	stubAdmin(t, func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusNotFound, map[string]string{"error": "chain not found"})
	})
	err := (&AdminChainsResumeCmd{Chain: "cosmoshub-4"}).Run()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q does not mention 404", err.Error())
	}
}

// ----- api-keys create -----

func TestAdminAPIKeys_create(t *testing.T) {
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api-keys" {
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		jsonResp(w, http.StatusCreated, createKeyResponse{ //nolint:gosec // test fixture, not a real credential
			ID:     "key_abc123",
			Secret: "pour_key_supersecret",
		})
	})
	cmd := &AdminAPIKeysCreateCmd{Chain: []string{"cosmoshub-4"}}
	out := captureOutput(t, func() {
		if err := cmd.Run(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "key_abc123") {
		t.Errorf("output missing id: %q", out)
	}
	if !strings.Contains(out, "pour_key_supersecret") {
		t.Errorf("output missing secret: %q", out)
	}
	if !strings.Contains(out, "WARNING") {
		t.Errorf("output missing WARNING line: %q", out)
	}
}

func TestAdminAPIKeys_create_perChainDrip(t *testing.T) {
	var gotBody createKeyBody
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		jsonResp(w, http.StatusCreated, createKeyResponse{ //nolint:gosec // test fixture
			ID:     "key_abc123",
			Secret: "pour_key_supersecret",
		})
	})
	cmd := &AdminAPIKeysCreateCmd{
		Chain:        []string{"osmosis-1"},
		PerChainDrip: []string{"osmosis-1=3000000uosmo"},
	}
	captureOutput(t, func() {
		if err := cmd.Run(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if got := gotBody.PerChainDrips["osmosis-1"]; got != "3000000uosmo" {
		t.Errorf("per_chain_drips[osmosis-1] = %q, want %q", got, "3000000uosmo")
	}
}

func TestAdminAPIKeys_create_perChainDripInvalid(t *testing.T) {
	stubAdmin(t, func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusCreated, createKeyResponse{})
	})
	cmd := &AdminAPIKeysCreateCmd{
		Chain:        []string{"osmosis-1"},
		PerChainDrip: []string{"osmosis-1"},
	}
	if err := cmd.Run(); err == nil {
		t.Error("expected error for missing '=' in --per-chain-drip, got nil")
	}
}

// ----- api-keys list -----

func TestAdminAPIKeys_list(t *testing.T) {
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	lastUsed := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	stubAdmin(t, func(w http.ResponseWriter, _ *http.Request) {
		jsonResp(w, http.StatusOK, listKeysResponse{
			Keys: []apiKeyListItem{
				{
					ID:               "key_abc123",
					Label:            "ci-bot",
					ChainScope:       []string{"cosmoshub-4"},
					PerChainDrips:    map[string]string{"cosmoshub-4": "2000000uatom"},
					RateLimitPerHour: 100,
					ExpiresAt:        &exp,
					CreatedAt:        created,
					LastUsedAt:       &lastUsed,
				},
			},
		})
	})
	out := captureOutput(t, func() {
		if err := (&AdminAPIKeysListCmd{}).Run(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	for _, want := range []string{
		"ID", "LABEL", "CHAINS", "PER_CHAIN_DRIPS", "RATE_LIMIT", "EXPIRES", "CREATED", "LAST_USED",
		"key_abc123", "ci-bot", "cosmoshub-4", "cosmoshub-4=2000000uatom", "100/hr",
		created.Format(time.RFC3339), lastUsed.Format(time.RFC3339),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// ----- api-keys revoke -----

func TestAdminAPIKeys_revoke(t *testing.T) {
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/admin/api-keys/key_abc123" {
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		jsonResp(w, http.StatusOK, map[string]bool{"ok": true})
	})
	out := captureOutput(t, func() {
		if err := (&AdminAPIKeysRevokeCmd{ID: "key_abc123"}).Run(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "revoked key_abc123") {
		t.Errorf("output missing revoked id: %q", out)
	}
	if !strings.Contains(out, "WARNING") {
		t.Errorf("output missing WARNING line: %q", out)
	}
}

// ----- api-keys rotate -----

func TestAdminAPIKeys_rotate(t *testing.T) {
	stubAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api-keys/rotate-admin" {
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		jsonResp(w, http.StatusOK, rotateResponse{Token: "pour_admin_newtoken"})
	})
	out := captureOutput(t, func() {
		if err := (&AdminAPIKeysRotateCmd{}).Run(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "pour_admin_newtoken") {
		t.Errorf("output missing new token: %q", out)
	}
	if !strings.Contains(out, "WARNING") {
		t.Errorf("output missing WARNING line: %q", out)
	}
}

// ----- adminclient token errors -----

// chdirTemp changes to a fresh temp dir for the duration of the test and restores the
// original working directory on cleanup. Required for tests that check TokenFile behavior
// since admin.TokenFile is a relative path (".pour-admin-token").
func chdirTemp(t *testing.T) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestNewAdminClient_noToken(t *testing.T) {
	chdirTemp(t)
	t.Setenv(admin.TokenEnvVar, "")

	_, err := newAdminClient()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "admin token not found") {
		t.Errorf("error %q does not contain 'admin token not found'", err.Error())
	}
}

func TestNewAdminClient_emptyFile(t *testing.T) {
	chdirTemp(t)
	t.Setenv(admin.TokenEnvVar, "")

	if err := os.WriteFile(admin.TokenFile, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := newAdminClient()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "admin token file is empty") {
		t.Errorf("error %q does not contain 'admin token file is empty'", err.Error())
	}
}

// ----- postJSON accepts 201 -----

func TestPostJSON_accepts201(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintln(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := &adminClient{baseURL: srv.URL, token: "test", http: &http.Client{}}
	var result map[string]bool
	if err := c.postJSON("/test", nil, &result); err != nil {
		t.Errorf("postJSON rejected 201: %v", err)
	}
}
