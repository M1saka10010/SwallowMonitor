package server

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDevicePublicKeyCanonicalEncoding(t *testing.T) {
	key := reportTestPublicKey("validation")
	if !validDevicePublicKey(key) {
		t.Fatal("valid public key rejected")
	}
	for _, key := range []string{"", "0123456789abcdef", "alpha-BETA_1.2~3", "--help # comment", "line1\nreportUrl: evil", "quote'", "space token", strings.TrimRight(key, "="), key + "\n", base64.StdEncoding.EncodeToString(make([]byte, 31)), base64.StdEncoding.EncodeToString(make([]byte, 33))} {
		if validDevicePublicKey(key) {
			t.Fatalf("invalid public key accepted: %q", key)
		}
	}
}
