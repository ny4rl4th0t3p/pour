//go:build integration && !no_ui

package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// mockAltchaJS defines a minimal <altcha-widget> custom element.
// When its challenge property is set it fires statechange:{state:"verified"} after 10 ms,
// allowing tests to assert the JS wiring without a real PoW solve.
const mockAltchaJS = `customElements.define('altcha-widget', class extends HTMLElement {
	set challenge(v) {
		if (!v) return;
		const self = this;
		setTimeout(function() {
			self.value = btoa('mock-solution');
			self.dispatchEvent(new CustomEvent('statechange', {detail: {state: 'verified'}, bubbles: true}));
		}, 10);
	}
});`

func findChrome(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Fatal("no Chromium/Chrome binary found; install chromium-browser to run UI integration tests")
	return ""
}

type uiStubConfig struct {
	powEnabled bool
	sigEnabled bool
}

func newTestServer(t *testing.T, cfg uiStubConfig) *httptest.Server {
	t.Helper()

	htmlBytes, err := assets.ReadFile("index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := strings.Replace(
		string(htmlBytes),
		"https://cdn.jsdelivr.net/gh/altcha-org/altcha/dist/altcha.min.js",
		"/altcha.min.js",
		1,
	)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html)) //nolint:errcheck
	})
	mux.HandleFunc("GET /altcha.min.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(mockAltchaJS)) //nolint:errcheck
	})
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"abuse": map[string]any{
				"pow_enabled":                 cfg.powEnabled,
				"signature_challenge_enabled": cfg.sigEnabled,
			},
		})
	})
	mux.HandleFunc("GET /v1/chains", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"chains": []map[string]any{
				{"chain_id": "testnet-1", "drip_amount": "1000000utest"},
			},
		})
	})
	mux.HandleFunc("GET /v1/pow/challenge", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		challenge := `{"algorithm":"SHA-256","challenge":"mockhex","number":1,"salt":"salt","signature":"sig"}`
		json.NewEncoder(w).Encode(map[string]string{"challenge": challenge}) //nolint:errcheck
	})

	return httptest.NewServer(mux)
}

func newChromedpCtx(t *testing.T, chromePath string) (context.Context, context.CancelFunc) {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.Flag("disable-gpu", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	return ctx, func() {
		cancelTimeout()
		cancelCtx()
		cancelAlloc()
	}
}

func TestUI_powDisabled(t *testing.T) {
	chromePath := findChrome(t)
	srv := newTestServer(t, uiStubConfig{powEnabled: false})
	defer srv.Close()

	ctx, cancel := newChromedpCtx(t, chromePath)
	defer cancel()

	var altchaCount int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.WaitEnabled("#submit"),
		chromedp.Evaluate(`document.querySelectorAll('altcha-widget').length`, &altchaCount),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if altchaCount != 0 {
		t.Errorf("want 0 altcha-widget elements, got %d", altchaCount)
	}
}

func TestUI_powEnabled(t *testing.T) {
	chromePath := findChrome(t)
	srv := newTestServer(t, uiStubConfig{powEnabled: true})
	defer srv.Close()

	ctx, cancel := newChromedpCtx(t, chromePath)
	defer cancel()

	var altchaCount int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.WaitEnabled("#submit"),
		chromedp.Evaluate(`document.querySelectorAll('altcha-widget').length`, &altchaCount),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if altchaCount == 0 {
		t.Error("expected altcha-widget to be present, got none")
	}
}

func TestUI_sigEnabled(t *testing.T) {
	chromePath := findChrome(t)
	srv := newTestServer(t, uiStubConfig{sigEnabled: true})
	defer srv.Close()

	ctx, cancel := newChromedpCtx(t, chromePath)
	defer cancel()

	var keplrCount int
	var keplrDisabled bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.WaitEnabled("#submit"),
		chromedp.Evaluate(`document.querySelectorAll('.keplr-btn').length`, &keplrCount),
		chromedp.Evaluate(`document.querySelector('.keplr-btn').disabled`, &keplrDisabled),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if keplrCount == 0 {
		t.Error("expected .keplr-btn to be present, got none")
	}
	if !keplrDisabled {
		t.Error("expected .keplr-btn to be disabled")
	}
}

func TestUI_powAndSigEnabled(t *testing.T) {
	chromePath := findChrome(t)
	srv := newTestServer(t, uiStubConfig{powEnabled: true, sigEnabled: true})
	defer srv.Close()

	ctx, cancel := newChromedpCtx(t, chromePath)
	defer cancel()

	var altchaCount, keplrCount int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.WaitEnabled("#submit"),
		chromedp.Evaluate(`document.querySelectorAll('altcha-widget').length`, &altchaCount),
		chromedp.Evaluate(`document.querySelectorAll('.keplr-btn').length`, &keplrCount),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if altchaCount == 0 {
		t.Error("expected altcha-widget to be present, got none")
	}
	if keplrCount == 0 {
		t.Error("expected .keplr-btn to be present, got none")
	}
}
