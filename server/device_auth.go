package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/gorilla/websocket"
)

const deviceAuthTimeout = 10 * time.Second
const deviceAuthDomain = "swallow-report-auth-v1\n"

func validDevicePublicKey(key string) bool {
	if len(key) != base64.StdEncoding.EncodedLen(ed25519.PublicKeySize) {
		return false
	}
	b, err := base64.StdEncoding.Strict().DecodeString(key)
	return err == nil && len(b) == ed25519.PublicKeySize && base64.StdEncoding.EncodeToString(b) == key
}

// authenticateDevice binds a fresh challenge to this connection. No online
// state is changed here, and the fixed deadline is never extended by traffic.
func authenticateDevice(conn *websocket.Conn, publicKey string) error {
	return authenticateDeviceBefore(conn, publicKey, time.Now().Add(deviceAuthTimeout))
}

func authenticateDeviceBefore(conn *websocket.Conn, publicKey string, deadline time.Time) error {
	key, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("invalid device public key")
	}
	conn.SetReadLimit(4096)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	// Bound control traffic even before a device has authenticated.
	controls := 0
	ping := conn.PingHandler()
	consume := func() error {
		controls++
		if controls > 8 {
			return errors.New("authentication control limit exceeded")
		}
		return nil
	}
	conn.SetPingHandler(func(data string) error {
		if err := consume(); err != nil {
			return err
		}
		return ping(data)
	})
	conn.SetPongHandler(func(string) error { return consume() })
	defer conn.SetPingHandler(nil)
	defer conn.SetPongHandler(nil)

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	challenge := base64.StdEncoding.EncodeToString(nonce)
	if err := conn.WriteJSON(map[string]string{"type": "auth_challenge", "challenge": challenge}); err != nil {
		return err
	}
	var response struct {
		Type      string `json:"type"`
		Signature string `json:"signature"`
	}
	if err := conn.ReadJSON(&response); err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(response.Signature)
	if err != nil || response.Type != "auth_response" || time.Now().After(deadline) ||
		!ed25519.Verify(key, []byte(deviceAuthDomain+publicKey+"\n"+challenge), sig) {
		return errors.New("invalid device proof")
	}
	return nil
}
