package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Only the latest 100 GitHub entries are inspected; older releases are not paginated.
const agentReleasesURL = "https://api.github.com/repos/M1saka10010/SwallowAgent/releases?per_page=100"
const agentDownloadBase = "https://github.com/M1saka10010/SwallowAgent/releases/download/"
const agentReleaseBodyLimit = 4 << 20
const agentReleaseTTL = 10 * time.Minute
const agentReleaseCooldown = time.Minute

var stableAgentTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)([+][0-9A-Za-z-]+([.][0-9A-Za-z-]+)*)?$`)
var agentAssetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type agentRelease struct {
	Version string   `json:"version"`
	Assets  []string `json:"assets"`
}

type agentReleaseResponse struct {
	Releases      []agentRelease `json:"releases"`
	LatestVersion string         `json:"latestVersion"`
	CheckedAt     int64          `json:"checkedAt"`
	Stale         bool           `json:"stale"`
	Error         string         `json:"error,omitempty"`
}

type githubAgentRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name  string `json:"name"`
		State string `json:"state"`
		URL   string `json:"browser_download_url"`
	} `json:"assets"`
}

type agentReleaseCache struct {
	mu        sync.Mutex
	client    *http.Client
	nextCheck time.Time
	response  agentReleaseResponse
}

func newAgentReleaseClient() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (s *Server) apiAgentReleases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireUser(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.agentReleases.get())
}

func (c *agentReleaseCache) get() agentReleaseResponse {
	// Hold the lock through the bounded fetch so concurrent misses share one result.
	// The fetch is independent of any one browser's cancellation.
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.nextCheck) {
		return c.response
	}
	if c.client == nil {
		c.client = newAgentReleaseClient()
	}
	releases, err := fetchAgentReleases(c.client)
	now := time.Now()
	if err != nil {
		if c.response.Releases == nil {
			c.response.Releases = []agentRelease{}
		}
		c.response.Stale = true
		c.response.Error = err.Error()
		c.nextCheck = now.Add(agentReleaseCooldown)
		return c.response
	}
	c.response = agentReleaseResponse{Releases: releases, CheckedAt: now.Unix()}
	if len(releases) > 0 {
		c.response.LatestVersion = releases[0].Version
	}
	c.nextCheck = now.Add(agentReleaseTTL)
	return c.response
}

func fetchAgentReleases(client *http.Client) ([]agentRelease, error) {
	req, _ := http.NewRequest(http.MethodGet, agentReleasesURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "SwallowMonitor")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("GitHub releases request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub releases returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, agentReleaseBodyLimit+1))
	if err != nil {
		return nil, errors.New("GitHub releases response read failed")
	}
	if len(body) > agentReleaseBodyLimit {
		return nil, errors.New("GitHub releases response too large")
	}
	var upstream []githubAgentRelease
	if err := json.Unmarshal(body, &upstream); err != nil || upstream == nil {
		return nil, errors.New("GitHub releases response invalid")
	}
	releases := make([]agentRelease, 0, len(upstream))
	seen := make(map[string]bool)
	for _, release := range upstream {
		if release.Draft || release.Prerelease || !stableAgentTag.MatchString(release.Tag) || seen[release.Tag] {
			continue
		}
		seen[release.Tag] = true
		item := agentRelease{Version: release.Tag, Assets: []string{}}
		assets := make(map[string]bool)
		for _, asset := range release.Assets {
			if asset.State != "uploaded" || !agentAssetName.MatchString(asset.Name) || assets[asset.Name] {
				continue
			}
			parsed, err := url.Parse(asset.URL)
			expectedPath := "/M1saka10010/SwallowAgent/releases/download/" + release.Tag + "/" + asset.Name
			if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(asset.URL, "#") || parsed.Path != expectedPath {
				continue
			}
			assets[asset.Name] = true
			item.Assets = append(item.Assets, asset.Name)
		}
		sort.Strings(item.Assets)
		releases = append(releases, item)
	}
	sort.SliceStable(releases, func(i, j int) bool { return agentVersionGreater(releases[i].Version, releases[j].Version) })
	return releases, nil
}

func agentVersionGreater(a, b string) bool {
	x, y := stableAgentTag.FindStringSubmatch(a), stableAgentTag.FindStringSubmatch(b)
	for i := 1; i <= 3; i++ {
		// Validated decimal strings compare without integer or floating-point overflow.
		if len(x[i]) != len(y[i]) {
			return len(x[i]) > len(y[i])
		}
		if x[i] != y[i] {
			return strings.Compare(x[i], y[i]) > 0
		}
	}
	return false
}
