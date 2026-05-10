package devnet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cosmos/go-bip39"
)

const (
	mnemonicEntropyBits = 256
	mnemonicFileMode    = 0600
	mnemonicDirMode     = 0700
)

// DefaultMnemonicPath returns the default path for the auto-generated mnemonic
// file: ~/.pour/auto-mnemonic.
func DefaultMnemonicPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("devnet: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".pour", "auto-mnemonic"), nil
}

// LoadOrGenerate returns the mnemonic stored at path. If the file does not
// exist, a new 24-word BIP39 mnemonic is generated, written to path (mode
// 0600), and returned. Parent directories are created as needed (mode 0700).
//
// Callers should use DefaultMnemonicPath unless they need a custom location.
func LoadOrGenerate(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("devnet: read mnemonic at %s: %w", path, err)
	}

	entropy, err := bip39.NewEntropy(mnemonicEntropyBits)
	if err != nil {
		return "", fmt.Errorf("devnet: generate mnemonic entropy: %w", err)
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", fmt.Errorf("devnet: generate mnemonic: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), mnemonicDirMode); err != nil {
		return "", fmt.Errorf("devnet: create mnemonic dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(mnemonic+"\n"), mnemonicFileMode); err != nil {
		return "", fmt.Errorf("devnet: write mnemonic to %s: %w", path, err)
	}
	return mnemonic, nil
}
