package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// keyPath is where the client encryption identity lives. Anyone restoring
// nest backups on another machine must copy this file there first.
func keyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".zipp", "nest.key")
}

// loadOrCreateIdentity returns the age identity for nest encryption,
// generating and persisting one on first use.
func loadOrCreateIdentity() (*age.X25519Identity, error) {
	path := keyPath()
	data, err := os.ReadFile(path)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
				return age.ParseX25519Identity(line)
			}
		}
		return nil, fmt.Errorf("no age key found in %s", path)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	content := fmt.Sprintf("# zipp nest encryption key — back this up!\n# public key: %s\n%s\n",
		id.Recipient(), id)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return nil, err
	}
	return id, nil
}

// encryptTo wraps dst so everything written is age-encrypted for id.
func encryptTo(dst io.Writer, id *age.X25519Identity) (io.WriteCloser, error) {
	return age.Encrypt(dst, id.Recipient())
}

// decryptFrom wraps src so reads yield the decrypted plaintext.
func decryptFrom(src io.Reader, id *age.X25519Identity) (io.Reader, error) {
	return age.Decrypt(src, id)
}
