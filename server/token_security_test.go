package server

import "testing"

func TestAgentTokenSafeForInstallerAndYAML(t *testing.T) {
	for _, token := range []string{"", "0123456789abcdef", "alpha-BETA_1.2~3"} {
		if !validAgentToken(token) {
			t.Fatalf("safe token rejected: %q", token)
		}
	}
	for _, token := range []string{"--help # comment", "abc#comment", "line1\nreportUrl: evil", "quote'", "space token", string(make([]byte, 129))} {
		if validAgentToken(token) {
			t.Fatalf("unsafe token accepted: %q", token)
		}
	}
}
