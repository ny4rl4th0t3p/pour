package devnet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmos/go-bip39"
)

func TestLoadOrGenerate_generatesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "auto-mnemonic")

	mnemonic, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("LoadOrGenerate: %v", err)
	}
	if !bip39.IsMnemonicValid(mnemonic) {
		t.Errorf("generated mnemonic is invalid: %q", mnemonic)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	if strings.TrimSpace(string(data)) != mnemonic {
		t.Errorf("persisted content %q != mnemonic %q", string(data), mnemonic)
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Errorf("file mode: got %o, want 0600", info.Mode().Perm())
	}
}

func TestLoadOrGenerate_reloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-mnemonic")

	first, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first != second {
		t.Errorf("mnemonic changed between calls: %q vs %q", first, second)
	}
}

func TestLoadOrGenerate_trimsWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-mnemonic")
	const mnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	if err := os.WriteFile(path, []byte("  "+mnemonic+"  \n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("LoadOrGenerate: %v", err)
	}
	if got != mnemonic {
		t.Errorf("got %q, want %q", got, mnemonic)
	}
}
