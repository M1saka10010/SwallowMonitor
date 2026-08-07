package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
	"github.com/gorilla/websocket"
)

func TestHostStoreCRUDTokenLookupInfoTouchAndDelete(t *testing.T) {
	st, _ := newTestApp(t, nil)
	if _, err := st.CreateTag("prod"); err != nil {
		t.Fatalf("CreateTag(prod) error = %v", err)
	}
	if _, err := st.CreateTag("web"); err != nil {
		t.Fatalf("CreateTag(web) error = %v", err)
	}

	host, err := st.CreateHost(" Web-01 ", "token-host", []string{"prod", "missing", "web"})
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	if host.PublicID == "" || host.Token != "token-host" {
		t.Fatalf("created host = %#v", host)
	}
	if strings.Join(host.Tags, ",") != "prod,web" {
		t.Fatalf("created host tags = %#v, want prod/web", host.Tags)
	}

	publicID, err := st.PublicIDByToken("token-host")
	if err != nil {
		t.Fatalf("PublicIDByToken() error = %v", err)
	}
	if publicID != host.PublicID {
		t.Fatalf("PublicIDByToken() = %q, want %q", publicID, host.PublicID)
	}
	if _, err := st.PublicIDByToken("missing-token"); err != store.ErrNotFound {
		t.Fatalf("PublicIDByToken(missing) error = %v, want ErrNotFound", err)
	}

	if err := st.UpdateHost(host.PublicID, "Web-02", []string{"web"}); err != nil {
		t.Fatalf("UpdateHost() error = %v", err)
	}
	got, err := st.GetHost(host.PublicID)
	if err != nil {
		t.Fatalf("GetHost() after update error = %v", err)
	}
	if got.Nickname != "Web-02" || strings.Join(got.Tags, ",") != "web" {
		t.Fatalf("updated host = %#v", got)
	}
	if err := st.UpdateHost("missing", "Nope", nil); err != store.ErrNotFound {
		t.Fatalf("UpdateHost(missing) error = %v, want ErrNotFound", err)
	}

	info := &model.SystemInfo{
		HostID:             "host-id-1",
		Hostname:           "web-02.local",
		OS:                 "linux",
		Platform:           "ubuntu",
		PlatformVersion:    "24.04",
		KernelArch:         "amd64",
		ModelName:          "Intel CPU",
		Cores:              8,
		VirtualizationRole: "guest",
		BootTime:           123,
	}
	if err := st.UpdateInfo(host.PublicID, info, `{"hostname":"web-02.local"}`); err != nil {
		t.Fatalf("UpdateInfo() error = %v", err)
	}
	if err := st.Touch(host.PublicID, 456); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	got, err = st.GetHost(host.PublicID)
	if err != nil {
		t.Fatalf("GetHost() after info error = %v", err)
	}
	if got.Hostname != "web-02.local" || got.OS != "linux" || got.LastSeen != 456 {
		t.Fatalf("host after info/touch = %#v", got)
	}

	if err := st.DeleteHost(host.PublicID); err != nil {
		t.Fatalf("DeleteHost() error = %v", err)
	}
	if _, err := st.GetHost(host.PublicID); err != store.ErrNotFound {
		t.Fatalf("GetHost(deleted) error = %v, want ErrNotFound", err)
	}
	if err := st.DeleteHost(host.PublicID); err != store.ErrNotFound {
		t.Fatalf("DeleteHost(deleted) error = %v, want ErrNotFound", err)
	}
}

func TestHostAPICRUDVisibilityValidationAndAuth(t *testing.T) {
	st, mux := newTestApp(t, nil)
	if _, err := st.CreateTag("prod"); err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}

	badReq := httptest.NewRequest(http.MethodPost, "/api/hosts", strings.NewReader(`{"nickname":"   ","token":"bad"}`))
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("empty nickname status = %d, want %d", badRec.Code, http.StatusBadRequest)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/hosts", strings.NewReader(`{"nickname":"Web-01","token":"token-api","tags":["prod","missing"]}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/hosts status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created store.Host
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created host: %v", err)
	}
	if created.Token != "token-api" || strings.Join(created.Tags, ",") != "prod" {
		t.Fatalf("created host = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/hosts status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var hosts []struct {
		PublicID string `json:"publicId"`
		Token    string `json:"token"`
		Nickname string `json:"nickname"`
		Online   bool   `json:"online"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&hosts); err != nil {
		t.Fatalf("decode hosts: %v", err)
	}
	if len(hosts) != 1 || hosts[0].Token != "token-api" || hosts[0].Online {
		t.Fatalf("hosts = %#v", hosts)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/hosts/"+created.PublicID, strings.NewReader(`{"nickname":"Web-02","tags":[]}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /api/hosts status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/hosts/"+created.PublicID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/hosts/{id} status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		PublicID string `json:"publicId"`
		Nickname string `json:"nickname"`
		Token    string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode host: %v", err)
	}
	if got.Nickname != "Web-02" || got.Token != "token-api" {
		t.Fatalf("host after patch = %#v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/hosts/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing host status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/hosts/"+created.PublicID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/hosts status = %d, body = %s", rec.Code, rec.Body.String())
	}

	_, authMux := newTestApp(t, &model.Config{GitHub: model.GitHubConfig{ClientID: "client"}})
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/hosts", `{"nickname":"Web"}`},
		{http.MethodPatch, "/api/hosts/some-id", `{"nickname":"Web"}`},
		{http.MethodDelete, "/api/hosts/some-id", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		authMux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without session status = %d, want %d", tc.method, tc.path, rec.Code, http.StatusUnauthorized)
		}
	}
}

func TestHostAPITokenHiddenForAnonymousWhenOAuthEnabled(t *testing.T) {
	st, mux := newTestApp(t, &model.Config{GitHub: model.GitHubConfig{ClientID: "client"}})
	host, err := st.CreateHost("Web-01", "secret-token", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/hosts status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Fatalf("anonymous hosts response exposed token: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/hosts/"+host.PublicID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/hosts/{id} status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Fatalf("anonymous host response exposed token: %s", rec.Body.String())
	}
}

func TestUsageStoreLatestQuerySamplingAndPrune(t *testing.T) {
	st, _ := newTestApp(t, nil)
	host, err := st.CreateHost("Web-01", "token-usage", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}

	if latest, err := st.LatestUsage(host.PublicID); err != nil || latest != nil {
		t.Fatalf("LatestUsage(empty) = %#v, %v; want nil, nil", latest, err)
	}

	for i := 0; i < 1000; i++ {
		u := &model.SystemUsage{Timestamp: uint64(i * 10), CPUUsage: float64(i), MemoryTotal: 1000, MemoryUsed: uint64(i)}
		if err := st.InsertUsage(host.PublicID, u); err != nil {
			t.Fatalf("InsertUsage(%d) error = %v", i, err)
		}
	}

	latest, err := st.LatestUsage(host.PublicID)
	if err != nil {
		t.Fatalf("LatestUsage() error = %v", err)
	}
	if latest == nil || latest.Timestamp != 9990 || latest.CPUUsage != 999 {
		t.Fatalf("LatestUsage() = %#v", latest)
	}

	raw, err := st.QueryUsage(host.PublicID, 100, 300)
	if err != nil {
		t.Fatalf("QueryUsage(raw) error = %v", err)
	}
	if len(raw) != 21 || raw[0].Timestamp != 100 || raw[len(raw)-1].Timestamp != 300 {
		t.Fatalf("raw usage len=%d first=%v last=%v", len(raw), raw[0].Timestamp, raw[len(raw)-1].Timestamp)
	}

	// 分级保留：原始数据先下沉到 5 分钟聚合表，长区间查询改走聚合表。
	if err := st.AggregateUsage(time.Now()); err != nil {
		t.Fatalf("AggregateUsage() error = %v", err)
	}
	bucketed, err := st.QueryUsage(host.PublicID, 0, 9990)
	if err != nil {
		t.Fatalf("QueryUsage(bucketed) error = %v", err)
	}
	if len(bucketed) == 0 || len(bucketed) >= 1000 {
		t.Fatalf("bucketed usage len = %d, want between 1 and 999", len(bucketed))
	}
	for i := 1; i < len(bucketed); i++ {
		if bucketed[i].Timestamp < bucketed[i-1].Timestamp {
			t.Fatalf("bucketed usage not ordered at %d: %d < %d", i, bucketed[i].Timestamp, bucketed[i-1].Timestamp)
		}
	}
	if bucketed[0].Timestamp%300 != 0 {
		t.Fatalf("bucketed timestamp %d not aligned to 5m", bucketed[0].Timestamp)
	}
	// 聚合后原始表仍保留（1 天内），短区间查询不受影响。
	rawAfter, err := st.QueryUsage(host.PublicID, 100, 300)
	if err != nil {
		t.Fatalf("QueryUsage(raw after aggregate) error = %v", err)
	}
	if len(rawAfter) != 21 {
		t.Fatalf("raw usage after aggregate len = %d, want 21", len(rawAfter))
	}

	pruneStore, _ := newTestApp(t, nil)
	oldHost, err := pruneStore.CreateHost("Old", "token-old", nil)
	if err != nil {
		t.Fatalf("CreateHost(old) error = %v", err)
	}
	now := uint64(time.Now().Unix())
	if err := pruneStore.InsertUsage(oldHost.PublicID, &model.SystemUsage{Timestamp: now - 3*24*3600}); err != nil {
		t.Fatalf("InsertUsage(old) error = %v", err)
	}
	if err := pruneStore.InsertUsage(oldHost.PublicID, &model.SystemUsage{Timestamp: now}); err != nil {
		t.Fatalf("InsertUsage(recent) error = %v", err)
	}
	deleted, err := pruneStore.PruneUsages(1)
	if err != nil {
		t.Fatalf("PruneUsages() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("PruneUsages() deleted = %d, want 1", deleted)
	}
	if deleted, err := pruneStore.PruneUsages(0); err != nil || deleted != 0 {
		t.Fatalf("PruneUsages(0) = %d, %v; want 0, nil", deleted, err)
	}
}

func TestPruneUsagesBatched(t *testing.T) {
	st, _ := newTestApp(t, nil)
	host, err := st.CreateHost("Web-01", "token-prune-batch", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	now := uint64(time.Now().Unix())
	const oldRows = 6000 // exceeds one prune batch (5000)
	for i := 0; i < oldRows; i++ {
		if err := st.InsertUsage(host.PublicID, &model.SystemUsage{Timestamp: now - 3*24*3600 - uint64(i)}); err != nil {
			t.Fatalf("InsertUsage(old %d) error = %v", i, err)
		}
	}
	if err := st.InsertUsage(host.PublicID, &model.SystemUsage{Timestamp: now}); err != nil {
		t.Fatalf("InsertUsage(recent) error = %v", err)
	}
	deleted, err := st.PruneUsages(1)
	if err != nil {
		t.Fatalf("PruneUsages() error = %v", err)
	}
	if deleted != oldRows {
		t.Fatalf("PruneUsages() deleted = %d, want %d", deleted, oldRows)
	}
	latest, err := st.LatestUsage(host.PublicID)
	if err != nil {
		t.Fatalf("LatestUsage() error = %v", err)
	}
	if latest == nil || latest.Timestamp != now {
		t.Fatalf("recent row lost after prune: %#v", latest)
	}
}

func TestAggregateUsageValuesAndTiers(t *testing.T) {
	st, _ := newTestApp(t, nil)
	host, err := st.CreateHost("Web-01", "token-agg", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}

	// 桶 0（ts 0-30）：cpu 1..4 → AVG 2.5；memory_used → MAX 400。
	for i, cpu := range []float64{1, 2, 3, 4} {
		if err := st.InsertUsage(host.PublicID, &model.SystemUsage{
			Timestamp: uint64(i * 10), CPUUsage: cpu, MemoryUsed: uint64((i + 1) * 100),
		}); err != nil {
			t.Fatalf("InsertUsage(%d) error = %v", i, err)
		}
	}
	// 桶 300：单点 cpu 10。
	if err := st.InsertUsage(host.PublicID, &model.SystemUsage{Timestamp: 300, CPUUsage: 10}); err != nil {
		t.Fatalf("InsertUsage(300) error = %v", err)
	}

	if err := st.AggregateUsage(time.Now()); err != nil {
		t.Fatalf("AggregateUsage() error = %v", err)
	}

	fiveMin, err := st.QueryUsage(host.PublicID, 0, 9999)
	if err != nil {
		t.Fatalf("QueryUsage(5m) error = %v", err)
	}
	if len(fiveMin) != 2 {
		t.Fatalf("5m buckets = %d, want 2", len(fiveMin))
	}
	if fiveMin[0].Timestamp != 0 || fiveMin[0].CPUUsage != 2.5 || fiveMin[0].MemoryUsed != 400 {
		t.Fatalf("5m bucket 0 = %#v, want cpu 2.5 mem 400", fiveMin[0])
	}
	if fiveMin[1].Timestamp != 300 || fiveMin[1].CPUUsage != 10 {
		t.Fatalf("5m bucket 300 = %#v, want cpu 10", fiveMin[1])
	}

	// >30 天范围路由到 1h 表：两个 5m 桶同属小时桶 0，cpu AVG(2.5,10)=6.25。
	hourly, err := st.QueryUsage(host.PublicID, 0, 31*24*3600)
	if err != nil {
		t.Fatalf("QueryUsage(1h) error = %v", err)
	}
	if len(hourly) != 1 {
		t.Fatalf("1h buckets = %d, want 1", len(hourly))
	}
	if hourly[0].Timestamp != 0 || hourly[0].CPUUsage != 6.25 || hourly[0].MemoryUsed != 400 {
		t.Fatalf("1h bucket = %#v, want cpu 6.25 mem 400", hourly[0])
	}
}

func TestPruneDownsampled(t *testing.T) {
	st, _ := newTestApp(t, nil)
	host, err := st.CreateHost("Web-01", "token-prune-ds", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	now := time.Now().Unix()
	for _, ts := range []uint64{uint64(now - 400*24*3600), uint64(now - 3*24*3600), uint64(now)} {
		if err := st.InsertUsage(host.PublicID, &model.SystemUsage{Timestamp: ts, CPUUsage: 1}); err != nil {
			t.Fatalf("InsertUsage(%d) error = %v", ts, err)
		}
	}
	if err := st.AggregateUsage(time.Now()); err != nil {
		t.Fatalf("AggregateUsage() error = %v", err)
	}

	deleted, err := st.PruneDownsampled(7)
	if err != nil {
		t.Fatalf("PruneDownsampled() error = %v", err)
	}
	// 400 天前：5m 桶（7 天保留）和 1h 桶（365 天保留）均过期；3 天前和当前桶保留。
	if deleted < 2 {
		t.Fatalf("PruneDownsampled() deleted = %d, want >= 2", deleted)
	}

	rows, err := st.QueryUsage(host.PublicID, 0, 31*24*3600)
	if err != nil {
		t.Fatalf("QueryUsage(1h) error = %v", err)
	}
	for _, r := range rows {
		if r.Timestamp < uint64(now-30*24*3600) {
			t.Fatalf("1h bucket %d survived prune", r.Timestamp)
		}
	}
}

func TestVacuumIfFragmented(t *testing.T) {
	st, _ := newTestApp(t, nil)
	host, err := st.CreateHost("Web-01", "token-vacuum", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}

	ran, err := st.VacuumIfFragmented()
	if err != nil {
		t.Fatalf("VacuumIfFragmented(empty) error = %v", err)
	}
	if ran {
		t.Fatal("VacuumIfFragmented(empty) ran, want false")
	}

	for i := 0; i < 20000; i++ {
		if err := st.InsertUsage(host.PublicID, &model.SystemUsage{Timestamp: uint64(1000 + i)}); err != nil {
			t.Fatalf("InsertUsage(%d) error = %v", i, err)
		}
	}
	if _, err := st.PruneUsages(1); err != nil {
		t.Fatalf("PruneUsages() error = %v", err)
	}

	ran, err = st.VacuumIfFragmented()
	if err != nil {
		t.Fatalf("VacuumIfFragmented(fragmented) error = %v", err)
	}
	if !ran {
		t.Fatal("VacuumIfFragmented(fragmented) did not run, want true")
	}

	ran, err = st.VacuumIfFragmented()
	if err != nil {
		t.Fatalf("VacuumIfFragmented(clean) error = %v", err)
	}
	if ran {
		t.Fatal("VacuumIfFragmented(clean) ran again, want false")
	}
}

func TestUsageAPIQueryParametersAndRangeClamp(t *testing.T) {
	st, mux := newTestApp(t, nil)
	host, err := st.CreateHost("Web-01", "token-usage-api", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	for _, ts := range []uint64{10, 20, 30, 40} {
		if err := st.InsertUsage(host.PublicID, &model.SystemUsage{Timestamp: ts, CPUUsage: float64(ts)}); err != nil {
			t.Fatalf("InsertUsage(%d) error = %v", ts, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/hosts/"+host.PublicID+"/usage?from=35&to=15", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET usage status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var points []model.SystemUsage
	if err := json.NewDecoder(rec.Body).Decode(&points); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if len(points) != 2 || points[0].Timestamp != 20 || points[1].Timestamp != 30 {
		t.Fatalf("usage points = %#v, want timestamps 20/30", points)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/hosts/"+host.PublicID+"/usage", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST usage status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestReportWebSocketAuthInfoUsageAndBadMessages(t *testing.T) {
	st, mux := newTestApp(t, nil)
	host, err := st.CreateHost("Web-01", "token-report", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	appSrv := httptest.NewServer(mux)
	defer appSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(appSrv.URL, "http") + "/report"

	if _, resp, err := websocket.DefaultDialer.Dial(wsURL, nil); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial without token err=%v status=%d, want unauthorized", err, status)
	}
	if _, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Token": []string{"missing"}}); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial unknown token err=%v status=%d, want unauthorized", err, status)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Token": []string{host.Token}})
	if err != nil {
		t.Fatalf("dial report websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`not-json`)); err != nil {
		t.Fatalf("write bad json: %v", err)
	}
	if err := conn.WriteJSON(model.Envelope{Type: "unknown", Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("write unknown envelope: %v", err)
	}
	info := model.SystemInfo{Hostname: "reported.local", OS: "linux", Platform: "ubuntu", Cores: 4, HostID: "host-id"}
	infoData, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	if err := conn.WriteJSON(model.Envelope{Type: model.TypeSystemInfo, Data: infoData}); err != nil {
		t.Fatalf("write system info: %v", err)
	}
	waitForHost(t, st, host.PublicID, func(h *store.Host) bool { return h.Hostname == "reported.local" && h.Cores == 4 })

	usage := model.SystemUsage{CPUUsage: 12.5, MemoryTotal: 100, MemoryUsed: 50, Timestamp: 777}
	usageData, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("marshal usage: %v", err)
	}
	if err := conn.WriteJSON(model.Envelope{Type: model.TypeSystemUsage, Data: usageData}); err != nil {
		t.Fatalf("write system usage: %v", err)
	}
	waitForUsage(t, st, host.PublicID, 777)
	got, err := st.GetHost(host.PublicID)
	if err != nil {
		t.Fatalf("GetHost() after usage error = %v", err)
	}
	if got.LastSeen != 777 {
		t.Fatalf("LastSeen after usage = %d, want 777", got.LastSeen)
	}
}

func waitForHost(t *testing.T, st *store.Store, publicID string, ok func(*store.Host) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		host, err := st.GetHost(publicID)
		if err == nil && ok(host) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	host, err := st.GetHost(publicID)
	t.Fatalf("host did not reach expected state: host=%#v err=%v", host, err)
}

func waitForUsage(t *testing.T, st *store.Store, publicID string, timestamp uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		latest, err := st.LatestUsage(publicID)
		if err == nil && latest != nil && latest.Timestamp == timestamp {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	latest, err := st.LatestUsage(publicID)
	t.Fatalf("latest usage = %#v, %v; want timestamp %d", latest, err, timestamp)
}
