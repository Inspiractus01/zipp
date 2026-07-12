package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	id, err := loadOrCreateIdentity()
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("backup payload — čučoriedky 🫐")
	var enc bytes.Buffer
	w, err := encryptTo(&enc, id)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(plaintext)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(enc.Bytes(), []byte("backup payload")) {
		t.Fatal("ciphertext contains plaintext")
	}

	r, err := decryptFrom(&enc, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("roundtrip mismatch: %q", got)
	}
}

func TestIdentityPersists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	id1, err := loadOrCreateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := loadOrCreateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if id1.String() != id2.String() {
		t.Error("second load should return the same identity")
	}

	info, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".zipp", "nest.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("key file mode = %o, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".zipp", "nest.key"))
	if !strings.Contains(string(data), "AGE-SECRET-KEY-") {
		t.Error("key file missing secret key")
	}
}
