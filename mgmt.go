package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mortal/cpa-xai-quota-guard/internal/xaiquota"
)

// hostLogger adapts host.log to xaiquota.Logger.
type hostLogger struct{}

func (hostLogger) Log(level, message string) {
	hostLog(level, "[cpa-xai-quota-guard] "+message)
}

// mgmtAuth implements xaiquota.AuthFileLookup via CPA management API.
type mgmtAuth struct {
	url string
	key string
}

func newMgmtAuth(cfg xaiquota.Config) *mgmtAuth {
	return &mgmtAuth{
		url: strings.TrimRight(strings.TrimSpace(cfg.ManagementURL), "/"),
		key: strings.TrimSpace(cfg.ManagementKey),
	}
}

type mgmtAuthEntry struct {
	AuthIndex string `json:"auth_index"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Account   string `json:"account"`
	Email     string `json:"email"`
	Disabled  bool   `json:"disabled"`
}

func (m *mgmtAuth) List() ([]xaiquota.AuthFile, error) {
	if m == nil || m.url == "" || m.key == "" {
		return nil, fmt.Errorf("management not configured")
	}
	body, err := mgmtHTTP(http.MethodGet, m.url+"/v0/management/auth-files", nil, m.key)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Files []mgmtAuthEntry `json:"files"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode auth-files: %w", err)
	}
	out := make([]xaiquota.AuthFile, 0, len(resp.Files))
	for _, f := range resp.Files {
		account := f.Account
		if account == "" {
			account = f.Email
		}
		out = append(out, xaiquota.AuthFile{
			AuthIndex: f.AuthIndex,
			Name:      f.Name,
			Provider:  f.Provider,
			Account:   account,
			Disabled:  f.Disabled,
		})
	}
	return out, nil
}

func (m *mgmtAuth) SetDisabled(authIndex string, disabled bool) (bool, error) {
	if m == nil || m.url == "" || m.key == "" {
		return false, fmt.Errorf("management not configured")
	}
	files, err := m.List()
	if err != nil {
		return false, err
	}
	var name string
	prev := false
	found := false
	for _, f := range files {
		if f.AuthIndex == authIndex {
			name = f.Name
			prev = f.Disabled
			found = true
			break
		}
	}
	if !found || name == "" {
		return false, fmt.Errorf("auth file not found for index %s", authIndex)
	}
	if prev == disabled {
		return prev, nil
	}
	payload, _ := json.Marshal(map[string]any{"name": name, "disabled": disabled})
	if _, err := mgmtHTTP(http.MethodPatch, m.url+"/v0/management/auth-files/status", payload, m.key); err != nil {
		return prev, err
	}
	return prev, nil
}

func mgmtHTTP(method, target string, body []byte, key string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, target, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Management-Key", key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return raw, fmt.Errorf("mgmt %s %s status %d: %s", method, target, resp.StatusCode, truncate(string(raw), 160))
	}
	return raw, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}