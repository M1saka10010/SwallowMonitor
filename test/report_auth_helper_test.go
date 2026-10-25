package test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var reportTestKeys sync.Map

func reportTestPublicKey(label string) string {
	seed := sha256.Sum256([]byte("swallow-test-only:" + label))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	publicKey := base64.StdEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey))
	reportTestKeys.Store(publicKey, privateKey)
	return publicKey
}

func reportTestProof(conn *websocket.Conn, publicKey string) error {
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	var challenge struct {
		Type      string
		Challenge string
	}
	if err := conn.ReadJSON(&challenge); err != nil {
		return err
	}
	if challenge.Type != "auth_challenge" || challenge.Challenge == "" {
		return fmt.Errorf("unexpected challenge: %+v", challenge)
	}
	privateKey, ok := reportTestKeys.Load(publicKey)
	if !ok {
		return fmt.Errorf("missing test private key for %s", publicKey)
	}
	signature := ed25519.Sign(privateKey.(ed25519.PrivateKey), []byte("swallow-report-auth-v1\n"+publicKey+"\n"+challenge.Challenge))
	return conn.WriteJSON(map[string]string{"type": "auth_response", "signature": base64.StdEncoding.EncodeToString(signature)})
}

func dialReportTest(url string, header http.Header) (*websocket.Conn, *http.Response, error) {
	conn, resp, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		return conn, resp, err
	}
	publicKey := header.Get("X-Agent-Public-Key")
	if publicKey == "" {
		return conn, resp, nil
	}
	err = reportTestProof(conn, publicKey)
	if err == nil {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var ack struct{ Type string }
		err = conn.ReadJSON(&ack)
		if err == nil && ack.Type != "auth_ok" {
			err = fmt.Errorf("unexpected authentication ack: %s", ack.Type)
		}
		conn.SetReadDeadline(time.Time{})
	}
	if err != nil {
		conn.Close()
		return nil, resp, err
	}
	return conn, resp, nil
}
