package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/M1saka10010/SwallowMonitor/store"
)

const enrollmentDomain = "swallow-enroll-v1\n"

type enrollmentView struct {
	*store.Host
	EnrollmentToken     string `json:"enrollmentToken"`
	EnrollmentExpiresAt int64  `json:"enrollmentExpiresAt"`
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.enrollLimiter.Allow(s.clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer rc.SetReadDeadline(time.Time{})
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var b struct {
		Token     string `json:"token"`
		PublicKey string `json:"publicKey"`
		Signature string `json:"signature"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&b); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	raw, err := hex.DecodeString(b.Token)
	if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != b.Token || !validDevicePublicKey(b.PublicKey) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	key, _ := base64.StdEncoding.Strict().DecodeString(b.PublicKey)
	sig, err := base64.StdEncoding.Strict().DecodeString(b.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize || base64.StdEncoding.EncodeToString(sig) != b.Signature || !ed25519.Verify(key, []byte(enrollmentDomain+b.Token+"\n"+b.PublicKey), sig) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	id, err := s.store.ConsumeEnrollment(b.Token, b.PublicKey, time.Now().Unix())
	if err != nil {
		if errors.Is(err, store.ErrEnrollmentInvalid) {
			http.Error(w, "invalid enrollment", http.StatusBadRequest)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"publicId": id})
}
