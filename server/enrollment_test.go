package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func enrollmentBody(t *testing.T, token, key string, private ed25519.PrivateKey) string {
	t.Helper()
	signature := ed25519.Sign(private, []byte("swallow-enroll-v1\n"+token+"\n"+key))
	b, err := json.Marshal(map[string]string{"token": token, "publicKey": key, "signature": base64.StdEncoding.EncodeToString(signature)})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func enrollRequest(s *Server, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/enroll", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleEnroll(w, r)
	return w
}

func TestEnrollmentHTTPSignatureAndReplay(t *testing.T) {
	s, _ := securityServer(t)
	e, err := s.store.CreateEnrollmentHost("pending", nil)
	if err != nil {
		t.Fatal(err)
	}
	key, private := freshDeviceKey(t)
	_, wrong := freshDeviceKey(t)
	w := enrollRequest(s, http.MethodPost, enrollmentBody(t, e.Token, key, wrong))
	if w.Code != 400 {
		t.Fatalf("bad signature: %d", w.Code)
	}
	body := enrollmentBody(t, e.Token, key, private)
	w = enrollRequest(s, http.MethodPost, body)
	if w.Code != 200 {
		t.Fatalf("valid signature: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable enrollment")
	}
	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["publicId"] != e.Host.PublicID || len(response) != 1 {
		t.Fatalf("response: %v", response)
	}
	if w = enrollRequest(s, http.MethodPost, body); w.Code != 400 {
		t.Fatalf("replay: %d", w.Code)
	}
}

func TestEnrollmentHTTPStrictBody(t *testing.T) {
	for _, kind := range []string{"unknown", "secondJSON", "trailingGarbage", "oversize", "oversizeWhitespace", "badKey", "badSignature", "wrongDomain", "get", "put", "delete"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := securityServer(t)
			e, err := s.store.CreateEnrollmentHost("pending", nil)
			if err != nil {
				t.Fatal(err)
			}
			key, private := freshDeviceKey(t)
			body := enrollmentBody(t, e.Token, key, private)
			method := http.MethodPost
			switch kind {
			case "unknown":
				body = strings.TrimSuffix(body, "}") + ",\"extra\":true}"
			case "secondJSON":
				body += " {}"
			case "trailingGarbage":
				body += " garbage"
			case "oversize":
				body = strings.Repeat(" ", 4096) + body
			case "oversizeWhitespace":
				body += strings.Repeat(" ", 4096)
			case "badKey":
				body = enrollmentBody(t, e.Token, "not-a-key", private)
			case "badSignature":
				body = strings.Replace(body, "\"signature\":\"", "\"signature\":\"!", 1)
			case "wrongDomain":
				b, _ := json.Marshal(map[string]string{"token": e.Token, "publicKey": key, "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(e.Token+"\n"+key)))})
				body = string(b)
			default:
				method = strings.ToUpper(kind)
			}
			w := enrollRequest(s, method, body)
			if w.Code < 400 || w.Code >= 500 {
				t.Fatalf("invalid request: %d %s", w.Code, w.Body.String())
			}
			if method != http.MethodPost && w.Code != 405 {
				t.Fatalf("method status: %d", w.Code)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable rejection")
			}
			if w = enrollRequest(s, http.MethodPost, enrollmentBody(t, e.Token, key, private)); w.Code != 200 {
				t.Fatalf("rejected request consumed token: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestEnrollmentHTTPDuplicateKeyRollback(t *testing.T) {
	s, _ := securityServer(t)
	key, private := freshDeviceKey(t)
	if _, err := s.store.CreateHost("bound", key, nil); err != nil {
		t.Fatal(err)
	}
	e, err := s.store.CreateEnrollmentHost("pending", nil)
	if err != nil {
		t.Fatal(err)
	}
	if w := enrollRequest(s, http.MethodPost, enrollmentBody(t, e.Token, key, private)); w.Code < 400 {
		t.Fatal("duplicate key accepted")
	}
	key, private = freshDeviceKey(t)
	if w := enrollRequest(s, http.MethodPost, enrollmentBody(t, e.Token, key, private)); w.Code != 200 {
		t.Fatalf("duplicate consumed token: %d %s", w.Code, w.Body.String())
	}
}

func TestEnrollmentHTTPIndependentLimiter(t *testing.T) {
	s, _ := securityServer(t)
	for i := 0; i < 30; i++ {
		if w := enrollRequest(s, http.MethodPost, "{}"); w.Code != 400 {
			t.Fatalf("request %d: %d", i, w.Code)
		}
	}
	w := enrollRequest(s, http.MethodPost, "{}")
	if w.Code != 429 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("limit: %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "/enroll", nil)
	if !s.reportLimiter.Allow(s.clientIP(r)) {
		t.Fatal("enrollment exhausted report limiter")
	}
	r.RemoteAddr = "192.0.2.2:1234"
	w = httptest.NewRecorder()
	s.handleEnroll(w, r)
	if w.Code == 429 {
		t.Fatal("another IP shares enrollment budget")
	}
}

func enrollmentAPI(s *Server, method, path, body, session, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if session != "" {
		r.AddCookie(&http.Cookie{Name: s.sessionCookieName(), Value: session})
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.handleAPI(w, r)
	return w
}

func TestEnrollmentAPIManagement(t *testing.T) {
	s, _ := securityServer(t)
	s.oauth = securityAuthServer().oauth
	session := s.sessions.create("admin")
	w := enrollmentAPI(s, "POST", "/api/hosts", "{\"nickname\":\"pending\",\"tags\":[\"production\"]}", session, "")
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		PublicID            string
		Nickname            string
		EnrollmentToken     string
		EnrollmentExpiresAt int64
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.PublicID == "" || created.Nickname != "pending" || len(created.EnrollmentToken) != 64 || created.EnrollmentExpiresAt <= time.Now().Unix() {
		t.Fatalf("not flattened enrollment response: %s", w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable create")
	}
	path := "/api/hosts/" + created.PublicID + "/enrollment"
	for _, tc := range []struct {
		method, session, origin string
		status                  int
	}{
		{"POST", "", "", 401}, {"POST", session, "https://evil.example", 403}, {"GET", session, "", 405},
	} {
		w = enrollmentAPI(s, tc.method, path, "", tc.session, tc.origin)
		if w.Code != tc.status {
			t.Fatalf("permissions: %+v: %d", tc, w.Code)
		}
	}
	for _, path := range []string{"/api/hosts", "/api/hosts/" + created.PublicID} {
		w = enrollmentAPI(s, "GET", path, "", "", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), created.EnrollmentToken) || strings.Contains(w.Body.String(), "enrollmentToken") {
			t.Fatalf("anonymous disclosure: %d %s", w.Code, w.Body.String())
		}
	}
	w = enrollmentAPI(s, "POST", path, "", session, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("refresh: %d %s", w.Code, w.Body.String())
	}
	var refreshed struct{ EnrollmentToken string }
	if err := json.Unmarshal(w.Body.Bytes(), &refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed.EnrollmentToken == "" || refreshed.EnrollmentToken == created.EnrollmentToken {
		t.Fatal("refresh did not rotate token")
	}
	key, private := freshDeviceKey(t)
	if w = enrollRequest(s, "POST", enrollmentBody(t, created.EnrollmentToken, key, private)); w.Code != 400 {
		t.Fatal("old token valid")
	}
	if w = enrollRequest(s, "POST", enrollmentBody(t, refreshed.EnrollmentToken, key, private)); w.Code != 200 {
		t.Fatalf("refreshed token: %d", w.Code)
	}
	if w = enrollmentAPI(s, "POST", path, "", session, ""); w.Code < 400 {
		t.Fatal("bound host refreshed")
	}
	for _, body := range []string{"{\"nickname\":\"legacy\",\"token\":\"old\"}", "{\"nickname\":\"legacy\",\"token\":\"\"}"} {
		if w = enrollmentAPI(s, "POST", "/api/hosts", body, session, ""); w.Code != 400 {
			t.Fatalf("legacy token accepted: %d", w.Code)
		}
	}
}
