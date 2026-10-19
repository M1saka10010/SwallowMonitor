package server

import (
	"bytes"
	"context"
	"errors"
	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
	"github.com/gorilla/websocket"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNotificationPublicAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "224.0.0.1", "0.0.0.0", "0.1.2.3", "100.64.0.1", "192.0.0.1", "192.0.2.1", "192.88.99.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "240.0.0.1", "255.255.255.255", "::", "::1", "fc00::1", "fe80::1", "ff02::1", "::ffff:127.0.0.1", "64:ff9b::a00:1", "2001::1", "2001:db8::1", "2002:7f00:1::", "3fff::1", "invalid"} {
		if isPublicNotificationIP(net.ParseIP(address)) {
			t.Errorf("accepted %s", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		if !isPublicNotificationIP(net.ParseIP(address)) {
			t.Errorf("rejected %s", address)
		}
	}
}

func TestNotificationDialValidatesAllAndPins(t *testing.T) {
	for _, addresses := range [][]string{nil, {"127.0.0.1"}, {"8.8.8.8", "127.0.0.1"}, {"8.8.8.8", "2606:4700:4700::1111"}} {
		calls, lookups := 0, 0
		transport := newNotificationTransport(func(ctx context.Context, network, host string) ([]net.IP, error) {
			lookups++
			if _, ok := ctx.Deadline(); !ok {
				t.Error("missing DNS deadline")
			}
			var ips []net.IP
			for _, address := range addresses {
				ips = append(ips, net.ParseIP(address))
			}
			return ips, nil
		}, func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || net.ParseIP(host) == nil || port != "443" {
				t.Errorf("not pinned: %s", address)
			}
			calls++
			return nil, errors.New("test dial failure")
		})
		_, _ = transport.DialContext(context.Background(), "tcp", "example.com:443")
		want := 0
		if len(addresses) == 2 && addresses[1] != "127.0.0.1" {
			want = 2
		}
		if calls != want || lookups != 1 {
			t.Fatalf("addresses=%v dials=%d lookups=%d", addresses, calls, lookups)
		}
	}
}

func TestNotificationRebindingAndRedirectProxy(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Location", "http://127.0.0.1/secret")
		w.WriteHeader(http.StatusFound)
	}))
	defer target.Close()
	installNotificationTestDial(t, target)
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	client := newNotificationClient()
	if client.CheckRedirect == nil || client.CheckRedirect(&http.Request{}, nil) != http.ErrUseLastResponse {
		t.Fatal("redirects must not be followed")
	}
	if client.Timeout != notificationTimeout || notificationTransport.Proxy != nil {
		t.Fatal("unsafe client settings")
	}
	s := &Server{cfg: &model.Config{}}
	s.sendNotificationGET(client, "http://8.8.8.8/send?text=hello")
	if hits.Load() != 1 {
		t.Fatalf("hits=%d", hits.Load())
	}
	var lookups atomic.Int32
	notificationTransport = newNotificationTransport(func(context.Context, string, string) ([]net.IP, error) {
		if lookups.Add(1) == 1 {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}, func(context.Context, string, string) (net.Conn, error) {
		t.Error("dialed rebound address")
		return nil, errors.New("blocked")
	})
	if _, err := resolveNotificationIPs(context.Background(), "example.com", func(ctx context.Context, network, host string) ([]net.IP, error) {
		lookups.Add(1)
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}); err != nil {
		t.Fatal(err)
	}
	s.sendNotificationGET(newNotificationClient(), "http://example.com/send")
	if lookups.Load() != 2 {
		t.Fatal("send did not re-resolve")
	}
}

func TestNotificationStoredPrivateURLAndSecretLogs(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer target.Close()
	st, _ := newNotificationTestApp(t)
	host, err := st.CreateHost("secret-test", "token", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateNotificationRule(store.NotificationRule{URL: target.URL + "/secret-token?text=%text%", NotifyOnline: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	s := &Server{store: st, cfg: &model.Config{IsDebug: true}}
	s.notifyHostStatus(host.PublicID, true)
	s.sendNotificationGET(newNotificationClient(), "http://user:secret-password@127.0.0.1/secret-token")
	s.sendNotificationGET(newNotificationClient(), "http://example.com/%zz-secret-token")
	if hits.Load() != 0 {
		t.Fatal("stored private URL was sent")
	}
	if strings.Contains(logs.String(), "secret-") {
		t.Fatalf("leaked URL: %s", logs.String())
	}
}

func newNotificationTestApp(t *testing.T) (*store.Store, *http.ServeMux) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mux := http.NewServeMux()
	New(&model.Config{}, st, mux, http.NotFoundHandler())
	return st, mux
}

func installNotificationTestDial(t *testing.T, target *httptest.Server) {
	t.Helper()
	original := notificationTransport
	notificationTransport = newNotificationTransport(net.DefaultResolver.LookupIP, func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:80" {
			t.Errorf("unexpected dial address %q", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, target.Listener.Addr().String())
	})
	t.Cleanup(func() { notificationTransport.CloseIdleConnections(); notificationTransport = original })
}

func TestNotificationDispatchOnStatusChange(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	notifySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query().Get("text"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer notifySrv.Close()
	installNotificationTestDial(t, notifySrv)

	st, mux := newNotificationTestApp(t)
	if _, err := st.CreateTag("prod"); err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}
	host, err := st.CreateHost("Web-01", "token-1", []string{"prod"})
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	_, err = st.CreateNotificationRule(store.NotificationRule{
		Tag:           "prod",
		URL:           "http://8.8.8.8/send?text=%text%",
		NotifyOnline:  true,
		NotifyOffline: true,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule() error = %v", err)
	}

	appSrv := httptest.NewServer(mux)
	defer appSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(appSrv.URL, "http") + "/report"
	header := http.Header{"Token": []string{host.Token}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial report websocket: %v", err)
	}

	waitForNotification(t, &mu, &queries, 1)
	_ = conn.Close()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	if !strings.Contains(got[0], "Web-01") || !strings.Contains(got[0], "上线") {
		t.Fatalf("online text = %q", got[0])
	}
	if len(got) != 1 {
		t.Fatalf("notification count shortly after disconnect = %d, want 1", len(got))
	}
}

func TestNoOnlineNotificationAfterReconnectWithinDelay(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	notifySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query().Get("text"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer notifySrv.Close()
	installNotificationTestDial(t, notifySrv)

	st, mux := newNotificationTestApp(t)
	host, err := st.CreateHost("Web-02", "token-2", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	_, err = st.CreateNotificationRule(store.NotificationRule{
		Tag:           "",
		URL:           "http://8.8.8.8/send?text=%text%",
		NotifyOnline:  true,
		NotifyOffline: true,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule() error = %v", err)
	}

	appSrv := httptest.NewServer(mux)
	defer appSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(appSrv.URL, "http") + "/report"
	header := http.Header{"Token": []string{host.Token}}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("first dial report websocket: %v", err)
	}
	waitForNotification(t, &mu, &queries, 1)
	_ = conn.Close()
	time.Sleep(100 * time.Millisecond)

	conn, _, err = websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("reconnect dial report websocket: %v", err)
	}
	defer conn.Close()
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("notification count after reconnect = %d, want 1 (reconnect within delay must be silent)", len(got))
	}
	if !strings.Contains(got[0], "上线") {
		t.Fatalf("notification text = %q, want 上线", got[0])
	}
}

func waitForNotification(t *testing.T, mu *sync.Mutex, queries *[]string, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(*queries)
		mu.Unlock()
		if got >= count {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("notification count = %d, want at least %d", len(*queries), count)
}
