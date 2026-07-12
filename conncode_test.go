package main

import "testing"

func TestDecodeNestCodeWords(t *testing.T) {
	// wordList[0] = "ace", so ace-ace-ace-ace = 0.0.0.0
	addr, token, err := decodeNestCode("ace-ace-ace-ace")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "0.0.0.0:9090" {
		t.Errorf("addr = %q, want 0.0.0.0:9090", addr)
	}
	if token != "" {
		t.Errorf("token = %q, want empty", token)
	}
}

func TestDecodeNestCodeRoundtrip(t *testing.T) {
	// every word must decode back to its index
	for i, w := range wordList {
		if wordIndex[w] != i {
			t.Fatalf("word %q at %d has duplicate or wrong index %d", w, i, wordIndex[w])
		}
	}
}

func TestDecodeNestCodeWithToken(t *testing.T) {
	addr, token, err := decodeNestCode("ace-age-aid-aim-9f3a2b1c")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "0.1.2.3:9090" {
		t.Errorf("addr = %q, want 0.1.2.3:9090", addr)
	}
	if token != "9f3a2b1c" {
		t.Errorf("token = %q, want 9f3a2b1c", token)
	}
}

func TestDecodeNestCodeRawIP(t *testing.T) {
	addr, _, err := decodeNestCode("192.168.1.5")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "192.168.1.5:9090" {
		t.Errorf("addr = %q", addr)
	}
	addr, _, err = decodeNestCode("10.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "10.0.0.1:8080" {
		t.Errorf("addr = %q", addr)
	}
}

func TestDecodeNestCodeInvalid(t *testing.T) {
	if _, _, err := decodeNestCode("oak-fox"); err == nil {
		t.Error("expected error for short code")
	}
	if _, _, err := decodeNestCode("zzz-zzz-zzz-zzz"); err == nil {
		t.Error("expected error for unknown words")
	}
}
